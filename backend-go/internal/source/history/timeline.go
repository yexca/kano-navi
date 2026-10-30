package history

import (
	"context"
	"errors"

	xsource "github.com/yexca/kano-navi/backend-go/internal/source/x"
)

// TimelineCheckpoint is the sync_state source of timeline fetch checkpoints,
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

// FetchTimeline reads one X account's timeline selection, newest first,
// within the page budget: the account's newest Count statuses, or every page
// to the end of the timeline. It never reads or moves the incremental cursor,
// and posts already stored keep what the detail path wrote, so a fetch only
// fills gaps. Each page commits with its timeline cursor under
// TimelineCheckpoint, so repeating the fetch continues it.
func (f *Fetcher) FetchTimeline(ctx context.Context, request Request) (Result, error) {
	if request.Source != SourceX || f.Timeline == nil {
		return Result{}, invalid("unsupported source")
	}
	window, err := request.Window.Normalize(SourceX)
	if err != nil {
		return Result{}, err
	}
	if window.Method != MethodTimeline {
		return Result{}, ErrRetired
	}
	key := window.checkpointKey(request.Account)
	checkpoint, err := f.Store.SyncState(ctx, TimelineCheckpoint, key)
	if err != nil {
		return Result{}, err
	}
	cursor, previous := checkpoint.CursorID, 0
	headFetchedAt, _ := checkpoint.Metadata["headFetchedAt"].(string)
	if cursor != "" {
		if count, ok := checkpoint.Metadata["discovered"].(float64); ok && count > 0 {
			previous = int(count)
		}
	}
	counted := window.Mode == ModeCount
	maxPages := f.pageLimit(request)
	commit := func(page Page) (Written, error) {
		page.Source, page.Account, page.CheckpointKey = SourceX, request.Account, key
		page.Checkpoint, page.Window, page.KeepStored = TimelineCheckpoint, window, true
		page.HeadFetchedAt = headFetchedAt
		return f.Store.WriteHistoryPage(ctx, page)
	}
	result := Result{Window: window, Errors: []string{}, Items: []Item{}}
	committed, restarted, failed := 0, false, false
	for {
		size := xsource.MaxTimelinePageSize
		if counted {
			size = max(1, min(size, window.Count-previous-result.Listed))
		}
		result.Requested++
		page, fetchErr := f.Timeline.TimelineStatuses(ctx, request.Account, cursor, size)
		if fetchErr != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			code := timelineCode(fetchErr)
			// A stale cursor must not pin the fetch: begin again at the newest
			// page, now or on the next fetch when the budget is spent.
			if cursor != "" && committed == 0 && !restarted {
				restarted = true
				cursor, previous = "", 0
				if _, err := commit(Page{CheckpointOnly: true}); err != nil {
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
		if cursor == "" {
			headFetchedAt = formatTime(f.now())
		}
		posts := []xsource.Post{}
		candidates := []xsource.ScheduleCandidate{}
		for _, status := range page.Statuses {
			if counted && previous+result.Listed >= window.Count {
				break
			}
			result.Listed++
			if status.Post == nil {
				result.Failed++
				result.Errors = append(result.Errors, status.ID+": "+string(xsource.ErrInvalidResponse))
				continue
			}
			posts = append(posts, *status.Post)
			if len(status.Post.MediaURLs) > 0 && matchesKeyword(status.Post.Text, f.Options.ScheduleKeywords) {
				candidates = append(candidates, xsource.ScheduleCandidate{PostID: status.Post.ID, SourceURL: status.Post.URL, MediaURL: status.Post.MediaURLs[0], AccountHandle: request.Account})
			}
		}
		following := page.Next
		switch {
		case counted && previous+result.Listed >= window.Count:
			following = ""
		case following == "":
			// The end of the timeline completes an all fetch; a count fetch
			// asked for more than it lists.
			if counted {
				result.Errors = append(result.Errors, "x_timeline_end; the timeline lists no older posts")
			}
		case following == cursor:
			result.Errors = append(result.Errors, "x_timeline_repeated_page")
			following = ""
		}
		written, writeErr := commit(Page{Next: following, Discovered: previous + result.Listed, Posts: posts, ScheduleCandidates: candidates})
		if writeErr != nil {
			return result, writeErr
		}
		committed++
		result.add(written, postItems(posts, request.Account))
		cursor = following
		if cursor == "" || result.Requested >= maxPages {
			break
		}
		if err := f.wait(ctx); err != nil {
			return result, err
		}
	}
	if cursor != "" && !failed {
		result.Errors = append(result.Errors, budgetMessage)
	}
	result.HasMore = cursor != ""
	return result, nil
}
