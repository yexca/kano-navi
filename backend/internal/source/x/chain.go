package x

import (
	"context"
	"errors"
	"slices"
	"time"
)

// DiscoveryAPI is the chain's official API method.
const DiscoveryAPI = "api"

// Attempt is one chain method a run tried and its controlled code, empty
// for the method that served.
type Attempt struct {
	Method string `json:"method"`
	Code   string `json:"code,omitempty"`
}

// apiMinPage and apiMaxPage bound max_results of the user posts endpoint.
const (
	apiMinPage = 5
	apiMaxPage = 100
)

// discover tries the chain's methods in order until one serves the account
// (ADR-0004). A method falls through when its first page fails, when it lists
// none of the account's own statuses, or when the chain skips it (the API's
// credential, gate or daily cap); a later page's failure keeps what was read.
// An API read that finds nothing newer than the cursor serves. Every method
// tried is recorded with its code, and when none serves the last error is
// returned with the account's gap as it was.
func (a *Adapter) discover(ctx context.Context, account string, opts Options, cutoff time.Time, result *Result) (discovery, error) {
	chain := opts.Chain
	if len(chain) == 0 {
		chain = []string{DiscoveryTimeline, DiscoveryProfile}
	}
	result.Gap = opts.Gap
	var last error = &Error{Code: ErrInvalidResponse, Err: errors.New("no listing method")}
	for _, method := range chain {
		var found discovery
		var err error
		switch method {
		case DiscoveryTimeline:
			found, err = a.discoverTimeline(ctx, account, opts, cutoff)
		case DiscoveryProfile:
			found, err = a.discoverProfile(ctx, account, opts, cutoff)
		case DiscoveryAPI:
			if code := a.apiSkip(opts, result.TimelineFailed); code != "" {
				err = &Error{Code: code}
			} else {
				found, err = a.discoverAPI(ctx, account, opts, cutoff, result)
			}
		default:
			continue
		}
		if err == nil && len(found.ids) == 0 && method != DiscoveryAPI {
			err = &Error{Code: ErrEmptyListing}
		}
		if err != nil {
			result.Tried = append(result.Tried, Attempt{Method: method, Code: safeCode(err)})
			// A cancelled run is no failure of the timeline.
			if method == DiscoveryTimeline && ctx.Err() == nil {
				result.TimelineFailed = true
			}
			last = err
			continue
		}
		result.Tried = append(result.Tried, Attempt{Method: method})
		result.Discovery, result.Gap, result.Notes = method, found.gap, found.notes
		return found, nil
	}
	return discovery{}, last
}

// apiSkip is the code that keeps the API method out of this run, or "".
func (a *Adapter) apiSkip(opts Options, timelineFailed bool) ErrorCode {
	failures := opts.TimelineFailures
	if timelineFailed {
		failures++
	}
	switch {
	case a.API == nil:
		return ErrAPIUnavailable
	case opts.APIGate > 0 && failures < opts.APIGate:
		return ErrAPIGated
	case opts.APIBudget < apiMinPage:
		return ErrAPIDailyCap
	}
	return ""
}

// withoutContinuations keeps a gap's floors and drops its FxTwitter
// continuations, which no other method can resume; the timeline then reads
// each gap again from the newest page.
func withoutContinuations(gap *Gap) *Gap {
	if gap == nil {
		return nil
	}
	return &Gap{Floor: gap.Floor, Older: withoutContinuations(gap.Older)}
}

// discoverProfile lists the IDs on the logged-out profile page, which shows
// only the newest few statuses; every one needs a detail request. When none
// of them reaches the stored cursor, a gap stays for a later read.
func (a *Adapter) discoverProfile(ctx context.Context, account string, opts Options, cutoff time.Time) (discovery, error) {
	profile, err := a.Client.Profile(ctx, account)
	if err != nil {
		return discovery{}, err
	}
	found := discovery{ids: DiscoverIDs(profile, account, opts.DiscoveryLimit), gap: withoutContinuations(opts.Gap)}
	if opts.CursorID != "" && len(found.ids) > 0 && !slices.ContainsFunc(found.ids, func(id string) bool { return reachesStored(id, opts, cutoff) }) {
		found.gap = withGap(found.gap, Gap{Floor: opts.CursorID})
	}
	return found, nil
}

// discoverAPI reads the account's own posts through the official API, newest
// first: those newer than the stored cursor (since_id), or without a cursor
// those within the bootstrap days, at most DiscoveryLimit that are not
// stored. A read the cap, the daily budget, the page bound or a later failed
// page stops short opens a gap at the cursor. Once the head is read, each
// gap is read exactly, between its floor (since_id) and the oldest stored
// status above it (until_id), so nothing depends on FxTwitter
// continuations. A failed first page is returned for the chain's next
// method.
func (a *Adapter) discoverAPI(ctx context.Context, account string, opts Options, cutoff time.Time, result *Result) (discovery, error) {
	found := discovery{ids: []string{}, posts: map[string]*Post{}, gap: withoutContinuations(opts.Gap)}
	limit := opts.DiscoveryLimit
	if limit <= 0 {
		limit = DefaultOptions().DiscoveryLimit
	}
	budget, taken, pages := opts.APIBudget, 0, 0
	seen := map[string]bool{}
	// span reads one stretch to its end and reports whether it got there.
	span := func(since, until, start string) (bool, error) {
		token := ""
		for {
			need := limit - taken
			switch {
			case need <= 0 || pages >= maxTimelinePages:
				return false, nil
			case budget < apiMinPage:
				found.notes = append(found.notes, string(ErrAPIDailyCap))
				return false, nil
			}
			pages++
			page, err := a.API.AccountPosts(ctx, APIQuery{Account: account, SinceID: since, UntilID: until, StartTime: start, Max: min(max(need, apiMinPage), apiMaxPage, budget), Token: token})
			result.APIRead += page.Read
			budget -= page.Read
			if err != nil {
				return false, err
			}
			for _, post := range page.Posts {
				if seen[post.ID] {
					continue
				}
				// The rest of the page stays unread; the gap's ceiling
				// resumes below the last post taken.
				if taken >= limit {
					return false, nil
				}
				seen[post.ID] = true
				if !opts.KnownIDs[post.ID] {
					taken++
				}
				p := post
				found.ids = append(found.ids, p.ID)
				found.posts[p.ID] = &p
			}
			if page.Next == "" || page.Next == token {
				return true, nil
			}
			token = page.Next
		}
	}
	start := ""
	if opts.CursorID == "" && !cutoff.IsZero() {
		start = cutoff.UTC().Format(time.RFC3339)
	}
	done, err := span(opts.CursorID, "", start)
	if err != nil {
		if pages == 1 {
			return found, err
		}
		found.notes = append(found.notes, "x api: "+safeCode(err))
	}
	if !done {
		// Without a cursor the bootstrap is bounded on purpose and opens no gap.
		if opts.CursorID != "" {
			found.gap = withGap(found.gap, Gap{Floor: opts.CursorID})
		}
		return found, nil
	}
	for gap := found.gap; gap != nil; {
		done, err = span(gap.Floor, opts.GapCeilings[gap.Floor], "")
		if err != nil {
			if ctx.Err() == nil {
				found.notes = append(found.notes, "x api gap: "+safeCode(err))
			}
			return found, nil
		}
		if !done {
			found.gap = gap
			return found, nil
		}
		gap = gap.Older
		found.gap = gap
	}
	return found, nil
}
