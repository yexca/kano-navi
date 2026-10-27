package x

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Discovery sources a run can report.
const (
	DiscoveryTimeline = "timeline"
	DiscoveryProfile  = "profile"
)

// TimelineStatus is one of the account's own statuses as a timeline page lists
// it. Post is nil when the entry has no usable publication time or content;
// PublishedAt then comes from the Snowflake ID when it is a real one.
type TimelineStatus struct {
	ID, PublishedAt string
	Post            *Post
}

// TimelineStatuses is one newest-first page of the account's own statuses.
type TimelineStatuses struct {
	Statuses []TimelineStatus
	Next     string
}

var handlePattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,30}$`)

// TimelineStatuses reads one timeline page of up to count statuses and
// normalizes each own status from the page itself, so an operator window
// needs no detail requests. Reposts are left out as in discovery.
func (a *Adapter) TimelineStatuses(ctx context.Context, account, cursor string, count int) (TimelineStatuses, error) {
	account = strings.TrimPrefix(strings.TrimSpace(account), "@")
	if !handlePattern.MatchString(account) {
		return TimelineStatuses{}, &Error{Code: ErrInvalidResponse, Err: errors.New("invalid account")}
	}
	body, err := a.Client.Timeline(ctx, account, cursor, count)
	if err != nil {
		return TimelineStatuses{}, err
	}
	return parseTimelineStatuses(body, account)
}

func parseTimelineStatuses(body []byte, account string) (TimelineStatuses, error) {
	var payload struct {
		Code    int              `json:"code"`
		Results []map[string]any `json:"results"`
		Cursor  struct {
			Bottom string `json:"bottom"`
		} `json:"cursor"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return TimelineStatuses{}, &Error{Code: ErrInvalidJSON}
	}
	if (payload.Code != 0 && payload.Code != 200) || payload.Results == nil {
		return TimelineStatuses{}, &Error{Code: ErrInvalidResponse, Err: errors.New("timeline without results")}
	}
	page := TimelineStatuses{Statuses: []TimelineStatus{}, Next: payload.Cursor.Bottom}
	for _, status := range payload.Results {
		id := stringValue(status["id"])
		author := object(status["author"])
		if !statusIDPattern.MatchString(id) || !strings.EqualFold(stringValue(author["screen_name"]), account) {
			continue
		}
		// Every status repeats the whole author profile; the post keeps its name.
		status["author"] = map[string]any{"screen_name": author["screen_name"], "name": author["name"]}
		entry := TimelineStatus{ID: id}
		if post := Normalize(status, id, account); post != nil && post.ID == id {
			// Like the detail answer, raw_json declares the post's own images,
			// without a quoted status's, for schedule extraction.
			own := []any{}
			for _, u := range fxMediaURLs(object(status["media"])) {
				if u != "" {
					own = append(own, u)
				}
			}
			post.Raw["mediaURLs"] = own
			entry.Post, entry.PublishedAt = post, post.PublishedAt
		} else if len(id) >= 15 {
			entry.PublishedAt = snowflakeTime(id).Format(time.RFC3339Nano)
		}
		page.Statuses = append(page.Statuses, entry)
	}
	return page, nil
}

// Gap is a stretch of an account's timeline that an incremental run could not
// read within its cap: statuses newer than Floor that older pages still hold.
// Cursor is the FxTwitter continuation where reading resumes; empty restarts
// at the newest page. See docs/decisions/ADR-0002-count-based-source-sync.md.
type Gap struct {
	Cursor string `json:"cursor,omitempty"`
	Floor  string `json:"floor"`
}

// ErrTimelineGap reports a gap the timeline ended before closing, which only
// a date window through the API can still fill.
const ErrTimelineGap ErrorCode = "x_timeline_gap"

// withGap merges a newly opened gap into an earlier one: reading on from the
// newer gap down to the older floor covers both.
func withGap(earlier *Gap, opened Gap) *Gap {
	if earlier != nil && idAfter(opened.Floor, earlier.Floor) {
		opened.Floor = earlier.Floor
	}
	return &opened
}

// discovery is what one run read from the timeline. posts holds the statuses
// a page listed with usable content, by ID; gap is the account's gap after the
// run; notes are controlled codes about it.
type discovery struct {
	ids   []string
	posts map[string]*Post
	gap   *Gap
	notes []string
}

// timelinePage is one page's own status IDs, newest first, and continuation.
type timelinePage struct {
	IDs  []string
	Next string
}

// discoverTimeline pages the account's timeline back until a page ends at a
// stored post, the stored cursor or the bootstrap cutoff. It takes at most
// DiscoveryLimit statuses that are not stored, in as many pages of 20 (at most
// 10). A head that stops short of the stored cursor opens a gap, so the cap
// never skips statuses; a head that reaches it continues an earlier gap with
// a page budget of its own. A failed first page is returned so the caller can
// fall back to the profile page; a later failure keeps the newer pages
// already read.
func (a *Adapter) discoverTimeline(ctx context.Context, account string, opts Options, cutoff time.Time) (discovery, error) {
	pages := (opts.DiscoveryLimit + timelinePageSize - 1) / timelinePageSize
	pages = min(max(pages, 1), maxTimelinePages)
	found := discovery{ids: []string{}, posts: map[string]*Post{}, gap: opts.Gap}
	seen := map[string]bool{}
	taken := 0
	full := func() bool { return opts.DiscoveryLimit > 0 && taken >= opts.DiscoveryLimit }
	// take adds a page's statuses while keep holds and the cap allows, and
	// returns the rest. Stored statuses are listed for refreshes but do not
	// count against the cap.
	take := func(ids []string, keep func(string) bool) []string {
		for i, id := range ids {
			if !keep(id) {
				return ids[i:]
			}
			if seen[id] {
				continue
			}
			if !opts.KnownIDs[id] {
				if full() {
					return ids[i:]
				}
				taken++
			}
			seen[id] = true
			found.ids = append(found.ids, id)
		}
		return nil
	}
	read := 0
	timeline := func(cursor string) (timelinePage, error) {
		read++
		body, err := a.Client.Timeline(ctx, account, cursor, timelinePageSize)
		if err != nil {
			return timelinePage{}, err
		}
		statuses, err := parseTimelineStatuses(body, account)
		if err != nil {
			return timelinePage{}, err
		}
		// The page already carries each status, so the run stores it without
		// a detail request; one without usable content is left to the detail.
		page := timelinePage{IDs: make([]string, 0, len(statuses.Statuses)), Next: statuses.Next}
		for _, status := range statuses.Statuses {
			page.IDs = append(page.IDs, status.ID)
			if status.Post != nil {
				found.posts[status.ID] = status.Post
			}
		}
		return page, nil
	}
	all := func(string) bool { return true }
	fresh := func(id string) bool { return !reachesStored(id, opts, cutoff) }
	cursor, resume, short := "", "", false
	for {
		page, err := timeline(cursor)
		if err != nil {
			if read == 1 {
				return found, err
			}
			resume, short = cursor, true
			break
		}
		rest := take(page.IDs, all)
		end := page.Next == "" || page.Next == cursor
		stored := len(page.IDs) > 0 && reachesStored(page.IDs[len(page.IDs)-1], opts, cutoff)
		if full() {
			switch {
			case slices.ContainsFunc(rest, fresh):
				resume, short = cursor, true
			case !stored && !end:
				resume, short = page.Next, true
			}
			break
		}
		if stored || end {
			break
		}
		cursor = page.Next
		if read >= pages {
			resume, short = cursor, true
			break
		}
	}
	// Without a cursor the bootstrap is bounded on purpose and opens no gap.
	if short {
		if opts.CursorID != "" {
			found.gap = withGap(found.gap, Gap{Cursor: resume, Floor: opts.CursorID})
		}
		return found, nil
	}
	gap := found.gap
	if gap == nil {
		return found, nil
	}
	above := func(id string) bool { return idAfter(id, gap.Floor) }
	cursor, read = gap.Cursor, 0
	restarted := false
	for read < pages && !full() {
		page, err := timeline(cursor)
		if err != nil {
			if ctx.Err() != nil {
				return found, nil
			}
			// A stale continuation must not pin the gap: read it again from
			// the newest page, now or in the next run.
			if cursor != "" && !restarted {
				restarted, cursor = true, ""
				found.gap = &Gap{Floor: gap.Floor}
				continue
			}
			found.notes = append(found.notes, "timeline gap: "+safeCode(err))
			return found, nil
		}
		rest := take(page.IDs, above)
		switch {
		case len(rest) > 0 && !above(rest[0]):
			// The floor is reached: the gap is read.
			found.gap = nil
			return found, nil
		case len(rest) > 0:
			// The cap ends the run inside this page, so the next one reads it again.
			found.gap = &Gap{Cursor: cursor, Floor: gap.Floor}
			return found, nil
		case page.Next == "" || page.Next == cursor:
			found.gap = nil
			found.notes = append(found.notes, string(ErrTimelineGap)+"; the timeline ends before the stored cursor")
			return found, nil
		}
		cursor = page.Next
		found.gap = &Gap{Cursor: cursor, Floor: gap.Floor}
	}
	return found, nil
}

// reachesStored reports whether a page's oldest own status shows that older
// pages hold nothing new: it is already stored, not after the stored cursor,
// or before the bootstrap cutoff.
func reachesStored(id string, opts Options, cutoff time.Time) bool {
	if opts.KnownIDs[id] || (opts.CursorID != "" && !idAfter(id, opts.CursorID)) {
		return true
	}
	return !cutoff.IsZero() && len(id) >= 15 && snowflakeTime(id).Before(cutoff)
}

// idAfter compares decimal status IDs, which X assigns in time order.
func idAfter(a, b string) bool {
	a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
	if len(a) != len(b) {
		return len(a) > len(b)
	}
	return a > b
}
