package history

import (
	"context"
	"errors"
	"time"

	xsource "github.com/yexca/kano-navi/backend-go/internal/source/x"
)

// TimelineCheckpoint is the sync_state source of timeline window checkpoints,
// apart from the API's x-window next_tokens and the incremental x cursors.
const TimelineCheckpoint = "x-timeline-window"

// Timeline reads one newest-first page of an X account's own statuses; the X
// adapter implements it over the public FxTwitter timeline.
type Timeline interface {
	TimelineStatuses(ctx context.Context, account, cursor string, count int) (xsource.TimelineStatuses, error)
}

// timelineCode keeps the X adapter's controlled codes.
func timelineCode(err error) string {
	var source *xsource.Error
	if errors.As(err, &source) {
		return string(source.Code)
	}
	return "x_timeline_failed"
}

// FetchTimeline reads one X account's recent, range or count window from the
// public timeline, newest first, within the page budget. It never reads or
// moves the incremental cursor, and posts already stored keep what the detail
// path wrote, so a window only fills gaps. A recent or range window ends at
// the first page whose oldest status predates it; a count window after the
// account's newest Count statuses. Each page commits with its timeline cursor
// under TimelineCheckpoint, so repeating the window continues it.
func (f *Fetcher) FetchTimeline(ctx context.Context, request Request) (Result, error) {
	if request.Source != SourceX || f.Timeline == nil {
		return Result{}, invalid("unsupported source")
	}
	input, err := request.Window.Normalize()
	if err != nil {
		return Result{}, err
	}
	if input.Mode == ModeBefore {
		return Result{}, invalid("X_API_BEARER_TOKEN is required for history search")
	}
	now := time.Now()
	if f.Now != nil {
		now = f.Now()
	}
	fresh, err := input.Resolve("", now)
	if err != nil {
		return Result{}, err
	}
	key := input.checkpointKey(request.Account)
	checkpoint, err := f.Store.SyncState(ctx, TimelineCheckpoint, key)
	if err != nil {
		return Result{}, err
	}
	window, cursor, previous := fresh, checkpoint.CursorID, 0
	if cursor != "" {
		saved, ok := windowFromMetadata(checkpoint.Metadata["window"])
		savedEnd, _ := parseTime(saved.EndTime)
		start, _ := parseTime(fresh.StartTime)
		// A recent window resumes only while its interval is still relevant.
		if ok && (input.Mode != ModeRecent || savedEnd.After(start)) {
			window = saved
			if count, ok := checkpoint.Metadata["discovered"].(float64); ok && count > 0 {
				previous = int(count)
			}
		} else {
			cursor = ""
		}
	}
	maxPages := f.Options.MaxPages
	if request.PageLimit > 0 {
		maxPages = request.PageLimit
	}
	commit := func(page Page) (Written, error) {
		page.Source, page.Account, page.CheckpointKey = SourceX, request.Account, key
		page.Checkpoint, page.Window, page.KeepStored = TimelineCheckpoint, window, true
		return f.Store.WriteHistoryPage(ctx, page)
	}
	result := Result{Window: window, Errors: []string{}}
	committed, restarted, failed := 0, false, false
	for {
		size := xsource.MaxTimelinePageSize
		if input.Mode == ModeCount {
			size = max(1, min(size, window.Count-previous-result.Discovered))
		}
		result.Requested++
		page, fetchErr := f.Timeline.TimelineStatuses(ctx, request.Account, cursor, size)
		if fetchErr != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			code := timelineCode(fetchErr)
			// A stale cursor must not pin the window: begin again at the newest
			// page, now or on the next fetch when the budget is spent.
			if cursor != "" && committed == 0 && !restarted {
				restarted = true
				window, cursor, previous = fresh, "", 0
				result.Window = window
				if _, err := commit(Page{}); err != nil {
					return result, err
				}
				if result.Requested < maxPages {
					continue
				}
			}
			if committed == 0 {
				return result, &Error{Code: code, Requested: result.Requested}
			}
			result.Errors = append(result.Errors, code)
			failed = true
			break
		}
		start, _ := parseTime(window.StartTime)
		posts := []xsource.Post{}
		candidates := []xsource.ScheduleCandidate{}
		reached := false
		for _, status := range page.Statuses {
			if input.Mode == ModeCount {
				if previous+result.Discovered >= window.Count {
					break
				}
			} else {
				// Page order is newest first, so the last own status decides
				// whether older pages can still hold the window.
				at, ok := parseTime(status.PublishedAt)
				reached = ok && at.Before(start)
				if !window.Contains(status.PublishedAt) {
					continue
				}
			}
			result.Discovered++
			if status.Post == nil {
				result.Errors = append(result.Errors, status.ID+": "+string(xsource.ErrInvalidResponse))
				continue
			}
			posts = append(posts, *status.Post)
			if len(status.Post.MediaURLs) > 0 && matchesKeyword(status.Post.Text, f.Options.ScheduleKeywords) {
				candidates = append(candidates, xsource.ScheduleCandidate{PostID: status.Post.ID, SourceURL: status.Post.URL, MediaURL: status.Post.MediaURLs[0], AccountHandle: request.Account})
			}
		}
		if input.Mode == ModeCount {
			reached = previous+result.Discovered >= window.Count
		}
		following := page.Next
		switch {
		case reached:
			following = ""
		case following == "":
			result.Errors = append(result.Errors, "x_timeline_end; the timeline lists no older posts")
		case following == cursor:
			result.Errors = append(result.Errors, "x_timeline_repeated_page")
			following = ""
		}
		written, writeErr := commit(Page{Next: following, Discovered: previous + result.Discovered, Posts: posts, ScheduleCandidates: candidates})
		if writeErr != nil {
			return result, writeErr
		}
		committed++
		result.Count += written.Posts
		result.ScheduleAssets += written.ScheduleAssets
		cursor = following
		if cursor == "" || result.Requested >= maxPages {
			break
		}
		if f.Options.RequestDelay > 0 {
			select {
			case <-ctx.Done():
				return result, ctx.Err()
			case <-time.After(f.Options.RequestDelay):
			}
		}
	}
	if cursor != "" && !failed {
		result.Errors = append(result.Errors, "history_page_budget_reached; repeat this fetch to continue")
	}
	result.HasMore = cursor != ""
	return result, nil
}
