package youtube

import (
	"context"
	"sort"
	"time"
)

type State struct {
	CursorID, CursorTime         string
	Videos                       map[string]Video
	Queue, CheckedIDs, ActiveIDs []string
}
type Result struct {
	Videos                           []Video
	Queue, CheckedIDs                []string
	NextCursor, NextCursorTime       string
	Errors                           []string
	Discovered, Requested, Inspected int
}
type Adapter struct {
	Client  *Client
	Options Options
	Now     func() time.Time
}

func NewAdapter(c *Client, options Options) *Adapter {
	if c == nil {
		c = NewClient(nil)
	}
	return &Adapter{c, options, time.Now}
}
func (a *Adapter) FetchRecent(ctx context.Context, state State, days int) (Result, error) {
	result := Result{Videos: []Video{}, Errors: []string{}, Queue: []string{}, CheckedIDs: []string{}}
	if days < 1 || days > 365 || a.Options.MaxDetailRequests < 1 || a.Options.MaxDetailRequests > 50 {
		return result, &Error{"youtube_invalid_options"}
	}
	now := a.Now()
	cutoff := now.Add(-time.Duration(days) * 24 * time.Hour)
	body, err := a.Client.Feed(ctx, a.Options.ChannelID)
	if err != nil {
		return result, err
	}
	feed, err := ParseFeed(body)
	if err != nil {
		return result, err
	}
	result.Discovered = len(feed)
	if state.Videos == nil {
		state.Videos = map[string]Video{}
	}
	// RSS lists waiting rooms and premieres like ordinary uploads, so recent
	// entries also become detail candidates for their schedule.
	feedIDs := []string{}
	for _, v := range feed {
		t, _ := time.Parse(time.RFC3339Nano, v.PublishedAt)
		if !t.Before(cutoff) {
			feedIDs = append(feedIDs, v.ID)
		}
		if _, exists := state.Videos[v.ID]; !exists && !t.Before(cutoff) && t.Before(now) {
			v.FeedOnly = true
			result.Videos = append(result.Videos, v)
			state.Videos[v.ID] = v
		}
	}
	sort.Slice(feed, func(i, j int) bool {
		ti, _ := time.Parse(time.RFC3339Nano, feed[i].PublishedAt)
		tj, _ := time.Parse(time.RFC3339Nano, feed[j].PublishedAt)
		return ti.After(tj)
	})
	result.NextCursor, result.NextCursorTime = feed[0].ID, feed[0].PublishedAt
	streamIDs := []string{}
	body, err = a.Client.Streams(ctx, a.Options.ChannelID)
	if err != nil {
		result.Errors = append(result.Errors, safeError(err))
	} else {
		streamIDs = DiscoverIDs(body)
	}
	checked := map[string]bool{}
	for _, id := range state.CheckedIDs {
		checked[id] = true
	}
	eligible := append([]string{}, state.Queue...)
	for _, id := range append(append([]string{}, streamIDs...), feedIDs...) {
		v, exists := state.Videos[id]
		t, _ := time.Parse(time.RFC3339Nano, v.ScheduledAt)
		if !exists || !checked[id] || v.IsUpcoming || (!t.IsZero() && !t.Before(now.Add(-24*time.Hour))) {
			eligible = append(eligible, id)
		}
	}
	eligible = append(eligible, state.ActiveIDs...)
	result.Queue = uniqueIDs(eligible, 1000)
	requests := append([]string{}, result.Queue...)
	if len(requests) > a.Options.MaxDetailRequests {
		requests = requests[:a.Options.MaxDetailRequests]
	}
	for _, id := range requests {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		// Rotate before fetching so a permanent failure cannot starve later IDs.
		result.Queue = append(withoutID(result.Queue, id), id)
		result.Requested++
		body, err = a.Client.Watch(ctx, id)
		if err != nil {
			result.Errors = append(result.Errors, safeError(err))
			continue
		}
		v, err := ParseWatch(body, id)
		if err != nil {
			result.Errors = append(result.Errors, safeError(err))
			continue
		}
		old := state.Videos[id]
		v.PublishedAt = old.PublishedAt
		if v.ScheduledAt == "" {
			v.ScheduledAt = old.ScheduledAt
		}
		if v.ThumbnailURL == "" {
			v.ThumbnailURL = old.ThumbnailURL
		}
		if v.ThumbnailURL == "" {
			v.ThumbnailURL = "https://i.ytimg.com/vi/" + id + "/hqdefault.jpg"
		}
		t, _ := time.Parse(time.RFC3339Nano, v.ScheduledAt)
		v.IsUpcoming = v.IsUpcoming || !t.IsZero() && !t.Before(now)
		v.Kind = "LIVE ARCHIVE"
		if v.IsUpcoming {
			v.Kind = "UPCOMING LIVE"
		}
		if t.IsZero() || v.Premiere {
			v.Kind = Classify(v.Title)
		}
		result.Videos = append(result.Videos, v)
		result.Inspected++
		checked[id] = true
		if !v.IsUpcoming && (t.IsZero() || t.Before(now.Add(-24*time.Hour))) {
			result.Queue = withoutID(result.Queue, id)
		}
	}
	for _, id := range uniqueIDs(append(append(streamIDs, feedIDs...), state.ActiveIDs...), 1000) {
		if checked[id] {
			result.CheckedIDs = append(result.CheckedIDs, id)
		}
	}
	return result, nil
}
func safeError(err error) string {
	if e, ok := err.(*Error); ok {
		return e.Code
	}
	return "youtube_unavailable"
}
func uniqueIDs(ids []string, limit int) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, id := range ids {
		if videoID.MatchString(id) && !seen[id] {
			out = append(out, id)
			seen[id] = true
			if len(out) == limit {
				break
			}
		}
	}
	return out
}
func withoutID(ids []string, id string) []string {
	out := []string{}
	for _, v := range ids {
		if v != id {
			out = append(out, v)
		}
	}
	return out
}
