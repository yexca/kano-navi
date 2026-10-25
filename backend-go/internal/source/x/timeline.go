package x

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
)

// Discovery sources a run can report.
const (
	DiscoveryTimeline = "timeline"
	DiscoveryProfile  = "profile"
)

// timelinePage is one page of FxTwitter's newest-first status list.
type timelinePage struct {
	// IDs are the account's own statuses in page order; reposts are left out.
	IDs  []string
	Next string
}

func parseTimeline(body []byte, account string) (timelinePage, error) {
	var payload struct {
		Code    int `json:"code"`
		Results []struct {
			ID     string `json:"id"`
			Author struct {
				ScreenName string `json:"screen_name"`
			} `json:"author"`
		} `json:"results"`
		Cursor struct {
			Bottom string `json:"bottom"`
		} `json:"cursor"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return timelinePage{}, &Error{Code: ErrInvalidJSON}
	}
	if (payload.Code != 0 && payload.Code != 200) || payload.Results == nil {
		return timelinePage{}, &Error{Code: ErrInvalidResponse, Err: errors.New("timeline without results")}
	}
	page := timelinePage{IDs: []string{}, Next: payload.Cursor.Bottom}
	for _, status := range payload.Results {
		// A repost carries the original author; only the account's own posts
		// are its feed.
		if statusIDPattern.MatchString(status.ID) && strings.EqualFold(status.Author.ScreenName, account) {
			page.IDs = append(page.IDs, status.ID)
		}
	}
	return page, nil
}

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
			entry.Post, entry.PublishedAt = post, post.PublishedAt
		} else if len(id) >= 15 {
			entry.PublishedAt = snowflakeTime(id).Format(time.RFC3339Nano)
		}
		page.Statuses = append(page.Statuses, entry)
	}
	return page, nil
}

// discoverTimeline pages the account's timeline back until a page ends at a
// stored post, the stored cursor or the bootstrap cutoff, within the
// DiscoveryLimit. A failed first page is returned so the caller can fall back
// to the profile page; a later failure keeps the newer pages already read.
func (a *Adapter) discoverTimeline(ctx context.Context, account string, opts Options, cutoff time.Time) ([]string, error) {
	pages := (opts.DiscoveryLimit + timelinePageSize - 1) / timelinePageSize
	if pages < 1 {
		pages = 1
	}
	if pages > maxTimelinePages {
		pages = maxTimelinePages
	}
	ids := []string{}
	seen := map[string]bool{}
	cursor := ""
	for i := 0; i < pages; i++ {
		body, err := a.Client.Timeline(ctx, account, cursor, timelinePageSize)
		var page timelinePage
		if err == nil {
			page, err = parseTimeline(body, account)
		}
		if err != nil {
			if i == 0 {
				return nil, err
			}
			break
		}
		for _, id := range page.IDs {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		if opts.DiscoveryLimit > 0 && len(ids) >= opts.DiscoveryLimit {
			return ids[:opts.DiscoveryLimit], nil
		}
		if len(page.IDs) > 0 && reachesStored(page.IDs[len(page.IDs)-1], opts, cutoff) {
			break
		}
		if page.Next == "" || page.Next == cursor {
			break
		}
		cursor = page.Next
	}
	return ids, nil
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
