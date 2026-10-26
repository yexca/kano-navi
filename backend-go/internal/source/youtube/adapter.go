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
	// RefreshIDs are operator requests, checked before any other detail work.
	// FollowUpIDs are videos whose next FollowUpOffsets check is due; they
	// follow the active reservations.
	RefreshIDs, FollowUpIDs []string
}
type Result struct {
	Videos            []Video
	Queue, CheckedIDs []string
	// InspectedIDs are the videos whose watch page passed the completeness
	// checks in this run.
	InspectedIDs                     []string
	NextCursor, NextCursorTime       string
	Errors                           []string
	Discovered, Requested, Inspected int
	// Shorts is the feed's format verdict (Video.Short) for every listed
	// entry whose alternate link named one, stored or new and of any age.
	Shorts map[string]bool
}

// FollowUpOffsets are the times after a video's anchor (its broadcast end,
// otherwise its start or publication) at which its watch page is read again,
// because creators replace a premiere's thumbnail or retitle an upload after
// it goes out. A video is checked once per offset that has passed.
var FollowUpOffsets = []time.Duration{time.Hour, 24 * time.Hour, 72 * time.Hour, 7 * 24 * time.Hour}

// FollowUpWindow bounds how old an anchor may be for a follow-up, so an
// upgrade does not reread the whole history.
const FollowUpWindow = 8 * 24 * time.Hour

// FollowUpDue reports whether the newest passed follow-up offset after anchor
// has no successful check at or after it. A zero lastChecked was never checked.
func FollowUpDue(anchor, lastChecked, now time.Time) bool {
	if anchor.IsZero() || now.Sub(anchor) > FollowUpWindow {
		return false
	}
	var due time.Time
	for _, offset := range FollowUpOffsets {
		if at := anchor.Add(offset); !at.After(now) {
			due = at
		}
	}
	return !due.IsZero() && lastChecked.Before(due)
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
	result := Result{Videos: []Video{}, Errors: []string{}, Queue: []string{}, CheckedIDs: []string{}, InspectedIDs: []string{}, Shorts: map[string]bool{}}
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
		if v.Short != nil {
			result.Shorts[v.ID] = *v.Short
		}
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
	// An operator's refresh request goes ahead of the rotating queue; due
	// follow-ups come last so they never starve new discoveries.
	eligible := append(append([]string{}, state.RefreshIDs...), state.Queue...)
	for _, id := range append(append([]string{}, streamIDs...), feedIDs...) {
		v, exists := state.Videos[id]
		t, _ := time.Parse(time.RFC3339Nano, v.ScheduledAt)
		if !exists || !checked[id] || v.IsUpcoming || (!t.IsZero() && !t.Before(now.Add(-24*time.Hour))) {
			eligible = append(eligible, id)
		}
	}
	eligible = append(append(eligible, state.ActiveIDs...), state.FollowUpIDs...)
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
		v = completeDetails(v, state.Videos[id], now)
		t, _ := time.Parse(time.RFC3339Nano, v.ScheduledAt)
		result.Videos = append(result.Videos, v)
		result.Inspected++
		result.InspectedIDs = append(result.InspectedIDs, id)
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

// completeDetails fills what a watch page left out from the stored video and
// derives the reservation state and kind.
func completeDetails(v, old Video, now time.Time) Video {
	v.PublishedAt = old.PublishedAt
	if v.ScheduledAt == "" {
		v.ScheduledAt = old.ScheduledAt
	}
	if v.ThumbnailURL == "" {
		v.ThumbnailURL = old.ThumbnailURL
	}
	if v.ThumbnailURL == "" {
		v.ThumbnailURL = "https://i.ytimg.com/vi/" + v.ID + "/hqdefault.jpg"
	}
	t, _ := time.Parse(time.RFC3339Nano, v.ScheduledAt)
	// A broadcast the page reports as live or over is not upcoming, even when
	// it started before its reserved time.
	started := v.LiveStatus == LiveStatusLive || v.LiveStatus == LiveStatusEnded
	v.IsUpcoming = v.IsUpcoming || !started && !t.IsZero() && !t.Before(now)
	v.Kind = "LIVE ARCHIVE"
	if v.IsUpcoming {
		v.Kind = "UPCOMING LIVE"
	}
	if t.IsZero() || v.Premiere {
		v.Kind = Classify(v.Title)
	}
	return v
}

// MaxLiveProbes bounds the watch pages one live-status probe requests.
const MaxLiveProbes = 5

// ProbeResult is what one live-status probe learned. Videos holds only pages
// that passed the watch-page completeness checks.
type ProbeResult struct {
	Videos []Video
	Errors []string
	// Attempted lists the IDs whose pages were requested, answered or not.
	Attempted            []string
	Requested, Inspected int
}

// ProbeLive requests only the watch pages of the given stored videos, at most
// MaxLiveProbes of them: no feed and no streams page. A failed or incomplete
// page records its code and leaves that video's snapshot to the caller.
func (a *Adapter) ProbeLive(ctx context.Context, videos []Video) (ProbeResult, error) {
	result := ProbeResult{Videos: []Video{}, Errors: []string{}, Attempted: []string{}}
	if len(videos) > MaxLiveProbes {
		videos = videos[:MaxLiveProbes]
	}
	for _, old := range videos {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		result.Requested++
		result.Attempted = append(result.Attempted, old.ID)
		body, err := a.Client.Watch(ctx, old.ID)
		if err != nil {
			result.Errors = append(result.Errors, safeError(err))
			continue
		}
		v, err := ParseWatch(body, old.ID)
		if err != nil {
			result.Errors = append(result.Errors, safeError(err))
			continue
		}
		result.Videos = append(result.Videos, completeDetails(v, old, a.Now()))
		result.Inspected++
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
