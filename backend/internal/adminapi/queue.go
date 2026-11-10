package adminapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/yexca/kano-navi/backend/internal/httpx"
	"github.com/yexca/kano-navi/backend/internal/schedule"
	ytsource "github.com/yexca/kano-navi/backend/internal/source/youtube"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

// The queue preview reports, per step, what a run started now would work
// through and in which order. Every queue is computed when a run starts, so
// these routes compute it the same way when asked; they read the snapshot,
// write nothing and contact no source or model.

// QueuePreviews are the steps' own preview functions; a nil one answers its
// route with 503 unavailable.
type QueuePreviews struct {
	Schedule    func(context.Context) (schedule.QueuePreview, error)
	Translation func(context.Context) (schedule.TranslationPreview, error)
}

func (a *api) scheduleQueue(w http.ResponseWriter, r *http.Request) {
	if a.Queues.Schedule == nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	preview, err := a.Queues.Schedule(r.Context())
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"queue": preview, "skipped": a.skipped("schedule")})
}

func (a *api) translationQueue(w http.ResponseWriter, r *http.Request) {
	if a.Queues.Translation == nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	preview, err := a.Queues.Translation(r.Context())
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"queue": preview, "skipped": a.skipped("translate")})
}

// youtubeQueueEntry is one video of the YouTube detail queue.
type youtubeQueueEntry struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
	// Group is refresh, queue, active or followUp.
	Group       string `json:"group"`
	PublishedAt string `json:"publishedAt,omitempty"`
	ScheduledAt string `json:"scheduledAt,omitempty"`
	Upcoming    bool   `json:"upcoming"`
}

// youtubeQueueLimit is how many IDs the detail queue keeps.
const youtubeQueueLimit = 1000

// youtubeQueue lists the YouTube step's detail queue in the order the step
// builds it: refresh requests, the stored rotating queue, then (after the
// videos the run's listing discovers) the active reservations and the due
// follow-ups, each ID once.
func (a *api) youtubeQueue(w http.ResponseWriter, r *http.Request) {
	if a.Youtube == nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	ctx := r.Context()
	state, err := a.Store.YoutubeRecentState(ctx, a.Youtube.Options.ChannelID)
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	settings, err := a.Store.SyncSettings(ctx)
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	groups := []struct {
		name string
		ids  []string
	}{{"refresh", state.RefreshIDs}, {"queue", state.Queue}, {"active", state.ActiveIDs}, {"followUp", state.FollowUpIDs}}
	counts := map[string]int{}
	entries := []youtubeQueueEntry{}
	seen := map[string]bool{}
	missing := []string{}
	for _, group := range groups {
		counts[group.name] = len(group.ids)
		for _, id := range group.ids {
			if id == "" || seen[id] || len(entries) >= youtubeQueueLimit {
				continue
			}
			seen[id] = true
			entries = append(entries, youtubeQueueEntry{ID: id, Group: group.name})
			if _, known := state.Videos[id]; !known {
				missing = append(missing, id)
			}
		}
	}
	stored, err := a.Store.StoredYoutubeVideos(ctx, missing)
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	for i, entry := range entries {
		video, known := state.Videos[entry.ID]
		if !known {
			video, known = stored[entry.ID]
		}
		if known {
			entries[i].Title, entries[i].URL = video.Title, video.URL
			entries[i].PublishedAt, entries[i].ScheduledAt, entries[i].Upcoming = video.PublishedAt, video.ScheduledAt, video.IsUpcoming
		}
		if entries[i].URL == "" {
			entries[i].URL = "https://www.youtube.com/watch?v=" + entry.ID
		}
	}
	api := a.Youtube.Options.APIKey != ""
	apiVideos := 0
	if api {
		apiVideos = ytsource.MaxAPIDetailCalls * ytsource.MaxAPIVideos
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"queue": map[string]any{
		"generatedAt": sqlite.ISOTime(a.Now()),
		"skipped":     a.skipped("youtube"),
		"api":         api,
		// One run reads up to apiVideos queued videos through the Data API and
		// up to watchPages watch pages.
		"apiVideos":  apiVideos,
		"watchPages": settings.YoutubeMaxDetailRequests,
		"limit":      youtubeQueueLimit,
		"counts":     counts,
		"entries":    entries,
	}})
}

// refreshQueueLimit bounds the refresh requests listed.
const refreshQueueLimit = 500

type refreshQueueEntry struct {
	sqlite.RefreshRequest
	// Status is next (this run reads it), later (its account has more
	// requests than one run reads) or notRead (the X step does not read its
	// account).
	Status string `json:"status"`
}

// xRefreshQueue lists the post refresh requests the X step reads: each
// watched account's oldest requests first, XRefreshPerAccount of them per
// run, ahead of the account's other detail requests.
func (a *api) xRefreshQueue(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	items, total, err := a.Store.RefreshRequests(ctx, refreshQueueLimit)
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	settings, err := a.Store.SyncSettings(ctx)
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	reads, err := a.refreshReads(ctx, items, settings.XMaxStatusRequests)
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	taken := map[string]int{}
	entries := make([]refreshQueueEntry, 0, len(items))
	for _, item := range items {
		account := strings.ToLower(item.AccountHandle)
		status := "notRead"
		if read, watched := reads[account]; watched {
			status = "later"
			if taken[account] < read {
				status = "next"
			}
			taken[account]++
		}
		item.Text = shortText(item.Text)
		entries = append(entries, refreshQueueEntry{RefreshRequest: item, Status: status})
	}
	handles := a.XHandles
	if handles == nil {
		handles = []string{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"queue": map[string]any{
		"generatedAt":  sqlite.ISOTime(a.Now()),
		"skipped":      a.skipped("x"),
		"handles":      handles,
		"perAccount":   sqlite.XRefreshPerAccount,
		"detailBudget": settings.XMaxStatusRequests,
		"total":        total,
		"limit":        refreshQueueLimit,
		"entries":      entries,
	}})
}

// refreshReads is how many refresh requests the next X run reads for each
// watched account. The run takes the accounts from its rotation's start and
// gives each an even share of the detail budget left, and an account's
// refreshes go ahead of its other detail requests, at most XRefreshPerAccount
// of them; the share an account leaves unused passes to the next.
func (a *api) refreshReads(ctx context.Context, items []sqlite.RefreshRequest, budget int) (map[string]int, error) {
	out := map[string]int{}
	if len(a.XHandles) == 0 {
		return out, nil
	}
	start, err := a.Store.XAccountStart(ctx, a.XHandles)
	if err != nil {
		return nil, err
	}
	queued := map[string]int{}
	for _, item := range items {
		queued[strings.ToLower(item.AccountHandle)]++
	}
	handles := append(append([]string{}, a.XHandles[start:]...), a.XHandles[:start]...)
	remaining := max(budget, 0)
	for i, handle := range handles {
		account := strings.ToLower(handle)
		left := len(handles) - i
		share := (remaining + left - 1) / left
		read := min(queued[account], sqlite.XRefreshPerAccount, share)
		out[account] = read
		remaining -= read
	}
	return out, nil
}

// shortText is a one-line excerpt of a post.
func shortText(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if runes := []rune(text); len(runes) > 140 {
		return string(runes[:140]) + "…"
	}
	return text
}
