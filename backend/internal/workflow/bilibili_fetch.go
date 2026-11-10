package workflow

import (
	"context"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	bilisource "github.com/yexca/kano-navi/backend/internal/source/bilibili"
	"github.com/yexca/kano-navi/backend/internal/source/history"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

// DefaultBilibiliPageGap spaces the listing pages of one Bilibili fetch.
// Bilibili's risk control answers quick runs of anonymous requests with a
// captcha, so the gap is far wider than the other sources' request spacing.
const DefaultBilibiliPageGap = 15 * time.Second

// bilibiliItemRunes bounds a dynamic's item title, the start of its text.
const bilibiliItemRunes = 80

// bilibiliFetch runs an operator's fetch of the Bilibili uploads or
// dynamics through the space listing: the newest Count records, or the
// listing to its end, within the manual fetch page budget and
// BilibiliPageGap apart. Each page commits with the account state and its
// continuation, so a repeated fetch resumes where the budget stopped it. A
// block, a missing or expired login, or a captcha met on the first page fails
// the step with the code; one met later keeps the committed pages and stops.
// The fetch is never forced and leaves the timers' interval clocks alone.
func (s *SourceSteps) bilibiliFetch(ctx context.Context, source string, window history.Window) (AttemptResult, error) {
	window, err := window.Normalize(source)
	if err != nil {
		return failedResult(historyMessage(err)), nil
	}
	settings, err := s.Store.SyncSettings(ctx)
	if err != nil {
		return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
	}
	maxPages := min(max(settings.FetchMaxPages, 1), 20)
	account := s.Bilibili.Options.Account
	key := window.CheckpointKey(account)
	checkpoint, err := s.Store.SyncState(ctx, source+"-window", key)
	if err != nil {
		return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
	}
	cursor, previous := checkpoint.CursorID, 0
	if source == history.SourceBilibili {
		// An upload continuation is a later page number; any other stored
		// value is no continuation.
		if number, err := strconv.Atoi(cursor); err != nil || number < 2 {
			cursor = ""
		}
	}
	if cursor != "" {
		if count, ok := checkpoint.Metadata["discovered"].(float64); ok && count > 0 {
			previous = int(count)
		}
	}
	counted := window.Mode == history.ModeCount
	total := history.Result{Items: []history.Item{}, Errors: []string{}}
	requested, committed, restarted, stopped := 0, 0, false, false
	for {
		state, stateErr := s.Store.BilibiliState(ctx, account)
		if stateErr != nil {
			return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
		}
		videos, dynamics, following, made, next, fetchErr := s.bilibiliPage(ctx, source, state, cursor)
		requested += made
		if made > 0 {
			total.Requested++
		}
		if ctx.Err() != nil {
			return AttemptResult{}, NewStepError("cancelled", ErrCancelled)
		}
		if fetchErr != nil {
			if made > 0 && s.Store.SaveBilibiliState(ctx, account, next) != nil {
				return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
			}
			code := bilisource.ErrorCode(fetchErr)
			// A continuation that no longer answers must not pin the fetch:
			// it is cleared at once, and a fetch with no page committed yet
			// begins again at the newest page. A captcha, a block or a login
			// problem says nothing about the continuation.
			if cursor != "" && !restarted && made > 0 && retryableBilibiliPage(code) {
				restarted = true
				cursor, previous = "", 0
				if s.Store.ClearBilibiliFetchCheckpoint(ctx, source, key) != nil {
					return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
				}
				if committed == 0 && total.Requested < maxPages {
					if s.bilibiliWait(ctx) != nil {
						return AttemptResult{}, NewStepError("cancelled", ErrCancelled)
					}
					continue
				}
			}
			if committed == 0 {
				return AttemptResult{Status: "failed", Result: map[string]any{"error": code, "pages": total.Requested, "requested": requested}}, nil
			}
			total.Errors = append(total.Errors, code)
			stopped = true
			break
		}
		taken := len(videos) + len(dynamics)
		if counted {
			left := max(window.Count-previous-total.Listed, 0)
			videos = videos[:min(len(videos), left)]
			dynamics = dynamics[:min(len(dynamics), left)]
			taken = len(videos) + len(dynamics)
		}
		total.Listed += taken
		switch {
		case counted && previous+total.Listed >= window.Count:
			following = ""
		case following != "" && following == cursor:
			total.Errors = append(total.Errors, "bilibili_repeated_page")
			following = ""
		}
		written, writeErr := s.Store.WriteBilibiliFetchPage(ctx, sqlite.BilibiliFetchPage{
			Account: account, Videos: videos, Dynamics: dynamics, State: next,
			Source: source, CheckpointKey: key, Next: following, Discovered: previous + total.Listed, Window: window,
		})
		if writeErr != nil {
			return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
		}
		committed++
		total.New += written.New
		total.Stored += written.Stored
		total.Failed += taken - written.New - written.Stored
		titles := bilibiliItems(videos, dynamics)
		for _, id := range written.NewIDs {
			if item, ok := titles[id]; ok && len(total.Items) < history.MaxItems {
				total.Items = append(total.Items, item)
			}
		}
		cursor = following
		if cursor == "" || total.Requested >= maxPages {
			break
		}
		if s.bilibiliWait(ctx) != nil {
			return AttemptResult{}, NewStepError("cancelled", ErrCancelled)
		}
	}
	if cursor != "" && !stopped {
		total.Errors = append(total.Errors, history.BudgetMessage)
	}
	out := counts(history.Result{Listed: total.Listed, New: total.New, Stored: total.Stored, Failed: total.Failed, Requested: total.Requested, HasMore: cursor != "" || stopped})
	out["items"], out["errors"], out["requested"] = total.Items, total.Errors, requested
	return AttemptResult{Status: stepStatus(total.Errors), Result: out}, nil
}

// bilibiliPage reads one listing page after cursor (a page number for the
// uploads, an offset for the dynamics; empty for the newest) and returns
// its own records, the next page's cursor (empty at the end of the
// listing), the requests made and the state to store.
func (s *SourceSteps) bilibiliPage(ctx context.Context, source string, state bilisource.State, cursor string) ([]bilisource.Video, []bilisource.Dynamic, string, int, bilisource.State, error) {
	if source == history.SourceBilibili {
		number := 1
		if cursor != "" {
			number, _ = strconv.Atoi(cursor)
		}
		page, next, made, err := s.Bilibili.FetchUploadsPage(ctx, state, number)
		if err != nil {
			return nil, nil, "", made, next, err
		}
		size := s.Bilibili.Options.PageSize
		more := page.Seen >= size
		if page.Total > 0 {
			more = number*size < page.Total
		}
		following := ""
		if more && page.Seen > 0 {
			following = strconv.Itoa(number + 1)
		}
		return page.Videos, nil, following, made, next, nil
	}
	page, next, made, err := s.Bilibili.FetchDynamicsPage(ctx, state, cursor)
	if err != nil {
		return nil, nil, "", made, next, err
	}
	following := ""
	if page.HasMore && page.Seen > 0 {
		following = page.Offset
	}
	return nil, page.Dynamics, following, made, next, nil
}

// retryableBilibiliPage reports a failed page that may be the continuation's
// fault rather than Bilibili's or the login's.
func retryableBilibiliPage(code string) bool {
	switch code {
	case "bilibili_blocked", "bilibili_login_required", "bilibili_login_expired":
		return false
	}
	return !bilisource.IsRiskControl(code)
}

// bilibiliWait spaces two pages of one fetch.
func (s *SourceSteps) bilibiliWait(ctx context.Context) error {
	gap := s.BilibiliPageGap
	if gap == 0 {
		gap = DefaultBilibiliPageGap
	}
	if gap < 0 {
		return ctx.Err()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(gap):
		return nil
	}
}

// bilibiliItems names a page's records as the console lists new ones: an
// upload's title, or the first line of a dynamic's text cut to 80
// characters.
func bilibiliItems(videos []bilisource.Video, dynamics []bilisource.Dynamic) map[string]history.Item {
	out := map[string]history.Item{}
	for _, v := range videos {
		out[v.BVID] = history.Item{ID: v.BVID, Title: v.Title}
	}
	for _, d := range dynamics {
		title, _, _ := strings.Cut(strings.TrimSpace(d.Text), "\n")
		if utf8.RuneCountInString(title) > bilibiliItemRunes {
			title = string([]rune(title)[:bilibiliItemRunes])
		}
		out[d.PostID()] = history.Item{ID: d.PostID(), Title: title}
	}
	return out
}
