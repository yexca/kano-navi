package youtube

import (
	"context"
	"slices"
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
	// InspectedIDs are the videos whose Data API details or watch page passed
	// the completeness checks in this run.
	InspectedIDs                     []string
	NextCursor, NextCursorTime       string
	Errors                           []string
	Discovered, Requested, Inspected int
	// Listing names where this run's uploads came from: ListingAPI,
	// ListingFeed, ListingVideosPage, or "" when nothing answered.
	Listing string
	// APIRequests counts Data API calls, one quota unit each.
	APIRequests int
	// Shorts is the feed's format verdict (Video.Short) for every listed
	// entry whose alternate link named one, stored or new and of any age.
	// When the feed fails with a key set, it holds true for the listed or
	// stored uploads the Shorts playlist names, and no false verdicts.
	Shorts map[string]bool
	// Localized holds, for every video a videos.list call answered, the
	// titles its creator localized by TitleLocales locale; an empty map means
	// it has none now. Videos no call answered are absent.
	Localized map[string]map[string]string
}

// Upload listings, in the order the recent step tries them.
const (
	ListingAPI        = "api"
	ListingFeed       = "rss"
	ListingVideosPage = "videos_page"
)

// videosPageLimit bounds the IDs taken from the uploads tab, the newest ones,
// to what one feed lists.
const videosPageLimit = 15

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

// List selects the listed uploads one recent run considers, newest first.
type List struct {
	// Limit is how many listed uploads the run takes, 1 to MaxListLimit.
	Limit int
	// Count takes Limit uploads even past stored ones. Otherwise the uploads
	// playlist is paged only until a page holds a stored video.
	Count bool
	// Since leaves out older entries, for an operator's recent window.
	Since time.Time
}

// MaxListLimit bounds a list: four uploads-playlist pages of 50.
const MaxListLimit = 200

// feedLimit is what the public feed lists, the reach of a run without a key.
const feedLimit = 15

const uploadsPageSize = 50

// FetchRecent considers the newest listed uploads published within days, as
// one playlist page or the feed lists them.
func (a *Adapter) FetchRecent(ctx context.Context, state State, days int) (Result, error) {
	if days < 1 || days > 365 {
		return Result{Videos: []Video{}, Errors: []string{}, Queue: []string{}, CheckedIDs: []string{}, InspectedIDs: []string{}, Shorts: map[string]bool{}, Localized: map[string]map[string]string{}}, &Error{"youtube_invalid_options"}
	}
	return a.FetchUploads(ctx, state, List{Limit: uploadsPageSize, Since: a.Now().Add(-time.Duration(days) * 24 * time.Hour)})
}

// FetchUploads lists the channel's newest uploads as list selects them, adds
// the ones not stored yet and refreshes reservations through the detail queue.
func (a *Adapter) FetchUploads(ctx context.Context, state State, list List) (Result, error) {
	result := Result{Videos: []Video{}, Errors: []string{}, Queue: []string{}, CheckedIDs: []string{}, InspectedIDs: []string{}, Shorts: map[string]bool{}, Localized: map[string]map[string]string{}}
	if list.Limit < 1 || list.Limit > MaxListLimit || a.Options.MaxDetailRequests < 1 || a.Options.MaxDetailRequests > 50 {
		return result, &Error{"youtube_invalid_options"}
	}
	now := a.Now()
	listed := func(t time.Time) bool { return list.Since.IsZero() || !t.Before(list.Since) }
	feed, held, listErr := a.listUploads(ctx, &result, state, list)
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	newest := append([]Video(nil), feed...)
	sort.SliceStable(newest, func(i, j int) bool {
		ti, _ := time.Parse(time.RFC3339Nano, newest[i].PublishedAt)
		tj, _ := time.Parse(time.RFC3339Nano, newest[j].PublishedAt)
		return ti.After(tj)
	})
	floor, hasFloor := cursorFloor(state)
	if !list.Count && list.Since.IsZero() {
		// Incremental: only uploads not stored yet count toward the cap, so
		// ones an earlier capped run left are taken now. The newest Limit
		// entries stay as detail candidates, stored or not, in listing order.
		kept := map[string]bool{}
		for _, v := range newest[:min(len(newest), list.Limit)] {
			kept[v.ID] = true
		}
		fresh := 0
		for _, v := range newest {
			if _, stored := state.Videos[v.ID]; stored {
				continue
			}
			if fresh < list.Limit {
				kept[v.ID] = true
				fresh++
				continue
			}
			// A new upload above the cursor is left for the next run, so the
			// cursor stays.
			if t, _ := time.Parse(time.RFC3339Nano, v.PublishedAt); hasFloor && t.After(floor) {
				held = true
			}
		}
		feed = slices.DeleteFunc(feed, func(v Video) bool { return !kept[v.ID] })
		newest = slices.DeleteFunc(newest, func(v Video) bool { return !kept[v.ID] })
	} else if len(feed) > list.Limit {
		// The newest Limit entries stay, in listing order.
		kept := map[string]bool{}
		for _, v := range newest[:list.Limit] {
			kept[v.ID] = true
		}
		feed = slices.DeleteFunc(feed, func(v Video) bool { return !kept[v.ID] })
		newest = newest[:list.Limit]
	} else if list.Count && len(feed) < list.Limit && result.Listing == ListingFeed && len(feed) >= feedLimit {
		// The feed cannot list more; only the Data API reaches further back.
		result.Errors = append(result.Errors, "youtube_count_limited")
	}
	result.Discovered = len(feed)
	if state.Videos == nil {
		state.Videos = map[string]Video{}
	}
	// The listing names waiting rooms and premieres like ordinary uploads, so
	// recent entries also become detail candidates for their schedule.
	feedIDs := []string{}
	for _, v := range feed {
		if v.Short != nil {
			result.Shorts[v.ID] = *v.Short
		}
		t, _ := time.Parse(time.RFC3339Nano, v.PublishedAt)
		if listed(t) {
			feedIDs = append(feedIDs, v.ID)
		}
		if _, exists := state.Videos[v.ID]; !exists && listed(t) && t.Before(now) {
			v.FeedOnly = true
			result.Videos = append(result.Videos, v)
			state.Videos[v.ID] = v
		}
	}
	if len(newest) > 0 && !held {
		result.NextCursor, result.NextCursorTime = newest[0].ID, newest[0].PublishedAt
	}
	streamIDs := []string{}
	body, err := a.Client.Streams(ctx, a.Options.ChannelID)
	if err != nil {
		result.Errors = append(result.Errors, safeError(err))
	} else {
		streamIDs = DiscoverIDs(body)
	}
	// Without a listing, the uploads tab still names the newest videos. It has
	// no publication times, so its IDs are only detail candidates, like the
	// streams page's, and the watch page supplies the time.
	pageIDs := []string{}
	if feed == nil {
		body, err = a.Client.VideosPage(ctx, a.Options.ChannelID)
		if err != nil {
			result.Errors = append(result.Errors, safeError(err))
		} else if pageIDs = DiscoverIDs(body); len(pageIDs) > 0 {
			pageIDs = pageIDs[:min(len(pageIDs), videosPageLimit)]
			result.Listing = ListingVideosPage
		}
	}
	checked := map[string]bool{}
	for _, id := range state.CheckedIDs {
		checked[id] = true
	}
	// An operator's refresh request goes ahead of the rotating queue; due
	// follow-ups come last so they never starve new discoveries.
	eligible := append(append([]string{}, state.RefreshIDs...), state.Queue...)
	refresh := map[string]bool{}
	for _, id := range state.RefreshIDs {
		refresh[id] = true
	}
	for _, id := range append(append(append([]string{}, streamIDs...), pageIDs...), feedIDs...) {
		v, exists := state.Videos[id]
		t, _ := time.Parse(time.RFC3339Nano, v.ScheduledAt)
		if !exists || !checked[id] || v.IsUpcoming || (!t.IsZero() && !t.Before(now.Add(-24*time.Hour))) {
			eligible = append(eligible, id)
		}
	}
	eligible = append(append(eligible, state.ActiveIDs...), state.FollowUpIDs...)
	result.Queue = uniqueIDs(eligible, 1000)
	// The Data API answers the first batch of the queue in one call; the watch
	// page budget then goes to the rest, newest work first as before. Room
	// left in that batch goes to the newest listed uploads, whose answers
	// only refresh their localized titles, which creators often add later.
	batch := uniqueIDs(append(append([]string{}, result.Queue...), feedIDs...), MaxAPIVideos)
	fromAPI, unclassified := a.apiDetails(ctx, batch, state.Videos, result.Localized, &result.APIRequests, &result.Errors)
	budget := a.Options.MaxDetailRequests
	for _, id := range append([]string{}, result.Queue...) {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		v, answered := fromAPI[id]
		fallback, hasFallback := unclassified[id]
		// An operator's refresh request reads the watch page while the budget
		// allows, so it can settle a premiere verdict the API answer only
		// repeats; that answer stands in when the page fails.
		if answered && refresh[id] && budget > 0 {
			fallback, hasFallback, answered = v, true, false
		}
		if !answered && budget == 0 {
			continue
		}
		// Rotate before fetching so a permanent failure cannot starve later IDs.
		result.Queue = append(withoutID(result.Queue, id), id)
		if !answered {
			budget--
			result.Requested++
			body, err = a.Client.Watch(ctx, id)
			if err == nil {
				v, err = ParseWatch(body, id)
			}
			if err != nil {
				result.Errors = append(result.Errors, safeError(err))
				// The page could not decide, so what the API said still counts.
				if v, answered = fallback, hasFallback; !answered {
					continue
				}
			}
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
	for _, id := range uniqueIDs(append(append(append(streamIDs, pageIDs...), feedIDs...), state.ActiveIDs...), 1000) {
		if checked[id] {
			result.CheckedIDs = append(result.CheckedIDs, id)
		}
	}
	// Only a run that learned nothing fails: no listing, not even the uploads
	// tab, and no detail check. Its snapshots stay and the step is retried;
	// the caller keeps only the rotated Queue, so IDs that keep failing do not
	// starve the rest.
	if listErr != nil && result.Listing == "" && result.Inspected == 0 {
		return result, listErr
	}
	return result, nil
}

// listUploads reads the channel's newest uploads: the Data API's uploads
// playlist when a key is set, then the RSS feed. Each failure is recorded in
// the result; when neither answers it returns nil and the feed's error, so a
// public outage keeps its retryable category. held reports an incremental
// playlist read that stopped at its cap above the cursor.
func (a *Adapter) listUploads(ctx context.Context, result *Result, state State, list List) (feed []Video, held bool, err error) {
	if a.Options.APIKey != "" {
		var uploads []Video
		uploads, held, err = a.apiUploads(ctx, result, state, list)
		if err == nil {
			result.Listing = ListingAPI
			a.feedShorts(ctx, result, uploads, state.Videos)
			return uploads, held, nil
		}
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		result.Errors = append(result.Errors, safeError(err))
	}
	body, err := a.Client.Feed(ctx, a.Options.ChannelID)
	if err == nil {
		feed, err = ParseFeed(body)
	}
	if err == nil {
		result.Listing = ListingFeed
		return feed, false, nil
	}
	if ctx.Err() == nil {
		result.Errors = append(result.Errors, safeError(err))
	}
	return nil, false, err
}

// cursorFloor is the publication time of the stored cursor, the newest upload
// an earlier incremental run listed.
func cursorFloor(state State) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, state.CursorTime)
	return t, state.CursorID != "" && err == nil
}

// apiUploads pages the uploads playlist, newest first, until list.Limit
// entries (for an incremental list, entries not stored yet), the playlist's
// end or, unless list.Count, a page that reaches what was read before: an
// entry at or below the cursor for an incremental list with a cursor,
// otherwise a stored video or one older than list.Since. A failed first page
// is returned; a later failure is recorded and keeps the pages already read.
// held reports an incremental list that stopped at its cap with a next page
// and without reaching the cursor, so uploads above it may be unread.
func (a *Adapter) apiUploads(ctx context.Context, result *Result, state State, list List) (out []Video, held bool, err error) {
	out = []Video{}
	seen := map[string]bool{}
	token := ""
	incremental := !list.Count && list.Since.IsZero()
	floor, hasFloor := cursorFloor(state)
	fresh := 0
	for page := 0; page*uploadsPageSize < MaxListLimit; page++ {
		result.APIRequests++
		body, err := a.Client.PlaylistPage(ctx, a.Options.APIKey, a.Options.ChannelID, token)
		var videos []Video
		next := ""
		if err == nil {
			videos, next, err = ParseUploadsPage(body)
		}
		if err != nil {
			if page == 0 || ctx.Err() != nil {
				return nil, false, err
			}
			result.Errors = append(result.Errors, safeError(err))
			// The failed page may hold uploads above the cursor.
			return out, incremental && hasFloor, nil
		}
		reached := false
		for _, v := range videos {
			if seen[v.ID] {
				continue
			}
			seen[v.ID] = true
			out = append(out, v)
			_, stored := state.Videos[v.ID]
			if !stored {
				fresh++
			}
			t, _ := time.Parse(time.RFC3339Nano, v.PublishedAt)
			if incremental && hasFloor {
				// Uploads an earlier capped run left sit below stored ones, so
				// only the cursor ends the read.
				reached = reached || v.ID == state.CursorID || !t.After(floor)
			} else if stored || v.ID == state.CursorID || (!list.Since.IsZero() && t.Before(list.Since)) {
				reached = true
			}
		}
		if next == "" || next == token || (reached && !list.Count) {
			break
		}
		taken := len(out)
		if incremental {
			taken = fresh
		}
		if taken >= list.Limit {
			return out, incremental && hasFloor, nil
		}
		token = next
	}
	return out, false, nil
}

// feedShorts reads the feed for its Shorts verdicts alone, the one signal the
// Data API listing lacks. A failure is recorded, and the Shorts playlist then
// names the Shorts among uploads, the IDs this run listed, and stored.
func (a *Adapter) feedShorts(ctx context.Context, result *Result, uploads []Video, stored map[string]Video) {
	body, err := a.Client.Feed(ctx, a.Options.ChannelID)
	var feed []Video
	if err == nil {
		feed, err = ParseFeed(body)
	}
	if err != nil {
		if ctx.Err() == nil {
			result.Errors = append(result.Errors, safeError(err))
			a.playlistShorts(ctx, result, uploads, stored)
		}
		return
	}
	for _, v := range feed {
		if v.Short != nil {
			result.Shorts[v.ID] = *v.Short
		}
	}
}

// playlistShorts marks the listed or stored uploads on the newest page of the
// channel's Shorts playlist as Shorts. Absence from the playlist proves
// nothing, so it never marks a regular video; only the feed does. A failure
// is recorded and costs only the verdicts.
func (a *Adapter) playlistShorts(ctx context.Context, result *Result, uploads []Video, stored map[string]Video) {
	known := map[string]bool{}
	for _, v := range uploads {
		known[v.ID] = true
	}
	for id := range stored {
		known[id] = true
	}
	result.APIRequests++
	body, err := a.Client.ShortsPlaylist(ctx, a.Options.APIKey, a.Options.ChannelID)
	var shorts []Video
	if err == nil {
		shorts, _, err = ParseUploadsPage(body)
	}
	if err != nil {
		if ctx.Err() == nil {
			result.Errors = append(result.Errors, safeError(err))
		}
		return
	}
	for _, v := range shorts {
		if known[v.ID] {
			result.Shorts[v.ID] = true
		}
	}
}

// apiDetails reads the first MaxAPIVideos queued IDs in one videos.list call
// when a key is set. It returns the videos the API could describe; the rest,
// including any the API left out or could not classify, fall back to their
// watch pages. Ended broadcasts it could not classify are also returned as
// unclassified, for use only when their page fails. Every video the call
// answered, classified or not, has its localized titles put in localized.
// The call is counted in requests; a failed one is recorded in errs and
// answers none. The recent step and the live-status probe share it.
func (a *Adapter) apiDetails(ctx context.Context, queue []string, stored map[string]Video, localized map[string]map[string]string, requests *int, errs *[]string) (out, unclassified map[string]Video) {
	out, unclassified = map[string]Video{}, map[string]Video{}
	if a.Options.APIKey == "" || len(queue) == 0 {
		return out, unclassified
	}
	ids := queue[:min(len(queue), MaxAPIVideos)]
	*requests++
	body, err := a.Client.Videos(ctx, a.Options.APIKey, ids)
	var items map[string]apiVideo
	if err == nil {
		items, err = ParseAPIVideos(body)
	}
	if err != nil {
		if ctx.Err() == nil {
			*errs = append(*errs, safeError(err))
		}
		return out, unclassified
	}
	for _, id := range ids {
		if item, found := items[id]; found {
			localized[id] = localizedTitles(item)
			v, ok := fromAPI(item, stored[id])
			switch {
			case ok:
				out[id] = v
			case v.Unclassified:
				unclassified[id] = v
			}
		}
	}
	return out, unclassified
}

// completeDetails fills what a watch page or the Data API left out from the
// stored video and derives the reservation state and kind. A stored
// publication time wins over the page's.
func completeDetails(v, old Video, now time.Time) Video {
	if old.PublishedAt != "" {
		v.PublishedAt = old.PublishedAt
	}
	if v.ScheduledAt == "" {
		v.ScheduledAt = old.ScheduledAt
	}
	if v.ThumbnailURL == "" {
		v.ThumbnailURL = old.ThumbnailURL
	}
	if !v.PremiereKnown && old.PremiereKnown {
		v.Premiere = old.Premiere
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
	// An unclassified answer keeps the stored kind; only the waiting-room kind
	// of the same judgement turns into its ended form.
	if v.Unclassified && old.Kind != "" {
		v.Kind = old.Kind
		if v.Kind == "UPCOMING LIVE" {
			v.Kind = "LIVE ARCHIVE"
		}
	}
	return v
}

// MaxLiveProbes bounds the watch pages one live-status probe requests.
const MaxLiveProbes = 5

// ProbeResult is what one live-status probe learned. Videos holds only Data
// API answers and pages that passed the completeness checks.
type ProbeResult struct {
	Videos []Video
	Errors []string
	// Attempted lists the IDs the probe asked about, answered or not.
	Attempted []string
	// Requested counts watch pages; APIRequests counts Data API calls, one
	// quota unit each.
	Requested, Inspected, APIRequests int
	// Localized holds the localized titles of the videos the Data API
	// answered, as in Result.
	Localized map[string]map[string]string
}

// ProbeLive reads the broadcast state of the given stored videos, at most
// MaxLiveProbes of them: no feed and no streams page. With a key, one
// videos.list call asks about all of them first, under the recent step's
// classification rules; a video it left out, could not classify or gave no
// broadcast state, or every video when the call failed, has its watch page
// read instead. A failed or incomplete page records its code and leaves that
// video's snapshot to the caller, unless the API's unclassified answer for an
// ended broadcast can stand in for it.
func (a *Adapter) ProbeLive(ctx context.Context, videos []Video) (ProbeResult, error) {
	result := ProbeResult{Videos: []Video{}, Errors: []string{}, Attempted: []string{}, Localized: map[string]map[string]string{}}
	if len(videos) > MaxLiveProbes {
		videos = videos[:MaxLiveProbes]
	}
	ids, stored := []string{}, map[string]Video{}
	for _, v := range videos {
		ids = append(ids, v.ID)
		stored[v.ID] = v
	}
	fromAPI, unclassified := a.apiDetails(ctx, ids, stored, result.Localized, &result.APIRequests, &result.Errors)
	for _, old := range videos {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		result.Attempted = append(result.Attempted, old.ID)
		// The probe is there to learn the broadcast state, so an answer
		// that names none still needs the page.
		v, answered := fromAPI[old.ID]
		if !answered || v.LiveStatus == "" {
			result.Requested++
			body, err := a.Client.Watch(ctx, old.ID)
			if err == nil {
				v, err = ParseWatch(body, old.ID)
			}
			if err != nil {
				result.Errors = append(result.Errors, safeError(err))
				if v, answered = unclassified[old.ID]; !answered {
					continue
				}
			}
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
