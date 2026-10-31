package workflow

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/yexca/kano-navi/backend/internal/source/history"
	xsource "github.com/yexca/kano-navi/backend/internal/source/x"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

// HistorySource runs the operator's manual fetches, each through the one
// listing method it names. A method that fails fails the step; a manual fetch
// never falls back to another method. Credentials come from the environment
// and never enter a result.
type HistorySource struct {
	Fetcher *history.Fetcher
	Store   *sqlite.Store
	// XToken is the X API credential. YoutubeKey only reports whether the
	// YouTube API method can run; the uploads adapter holds its own copy.
	XToken, YoutubeKey string
	Handles            []string
	ChannelID          string
	// RequestLimit is the X page budget of one step, shared by all accounts.
	RequestLimit int
}

// MethodAvailable reports whether a source's method can run now: the API
// methods need their credential, and every method its adapter.
func (h *HistorySource) MethodAvailable(source, method string) bool {
	if h == nil || h.Fetcher == nil || h.Store == nil {
		return false
	}
	switch {
	case source == history.SourceX && method == history.MethodTimeline:
		return h.Fetcher.Timeline != nil
	case source == history.SourceX && method == history.MethodAPI:
		return strings.TrimSpace(h.XToken) != ""
	case source == history.SourceYoutube && method == history.MethodAPI:
		return h.Fetcher.Uploads != nil && strings.TrimSpace(h.YoutubeKey) != ""
	case source == history.SourceYoutube && method == history.MethodRSS:
		return h.Fetcher.Uploads != nil
	}
	return false
}

// Methods describes a source's listing methods and which can run now.
func (h *HistorySource) Methods(source string) []history.MethodStatus {
	out := []history.MethodStatus{}
	for _, method := range history.Methods(source) {
		out = append(out, history.MethodStatus{Method: method, Available: h.MethodAvailable(source, method.ID)})
	}
	return out
}

// historyMessage keeps controlled codes and operator-facing validation text;
// anything else is reduced to a fixed code.
func historyMessage(err error) string {
	var controlled *history.Error
	if errors.As(err, &controlled) {
		return controlled.Code
	}
	if history.IsValidation(err) {
		return err.Error()
	}
	return "source_write_failed"
}

func failedResult(message string) AttemptResult {
	return AttemptResult{Status: "failed", Result: map[string]any{"error": message}}
}

func stepStatus(messages []string) string {
	if len(messages) > 0 {
		return "partial"
	}
	return "succeeded"
}

// selection validates a window frozen on a run. A selection no method serves,
// such as one frozen before fetches named their method, fails cleanly.
func (h *HistorySource) selection(source string, window history.Window) (history.Window, string) {
	normalized, err := window.Normalize(source)
	if err != nil {
		return history.Window{}, historyMessage(err)
	}
	if !h.MethodAvailable(source, normalized.Method) {
		return history.Window{}, history.UnavailableMessage(source, normalized.Method)
	}
	return normalized, ""
}

// counts is the console's summary of a fetch: what the selection listed,
// split into new, already stored and failed records, the pages it read,
// whether it reached the end of what it asked for, and the first new items.
func counts(result history.Result) map[string]any {
	return map[string]any{
		"listed": result.Listed, "new": result.New, "stored": result.Stored, "failed": result.Failed,
		"pages": result.Requested, "reachedEnd": !result.HasMore,
	}
}

// FetchX fetches every configured account through the window's method. One
// account's failure keeps the others; the rotation spreads a short request
// budget across runs.
func (h *HistorySource) FetchX(ctx context.Context, window history.Window) (AttemptResult, error) {
	window, message := h.selection(history.SourceX, window)
	if message != "" {
		return failedResult(message), nil
	}
	fetch := h.Fetcher.FetchTimeline
	if window.Method == history.MethodAPI {
		fetch = h.Fetcher.FetchSearch
	}
	if len(h.Handles) == 0 {
		return AttemptResult{}, NewStepError("x_no_accounts", ErrValidation)
	}
	rotationID := "window-" + window.Mode
	rotation, err := h.Store.SyncState(ctx, "x-budget", rotationID)
	if err != nil {
		return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
	}
	next := 0
	last, _ := rotation.Metadata["lastAccount"].(string)
	for i, handle := range h.Handles {
		if handle == last {
			next = (i + 1) % len(h.Handles)
		}
	}
	ordered := append(append([]string{}, h.Handles[next:]...), h.Handles[:next]...)
	if h.Store.WriteSyncMetadata(ctx, "x-budget", rotationID, map[string]any{"lastAccount": ordered[0]}) != nil {
		return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
	}
	remaining := h.RequestLimit
	if remaining < 1 {
		remaining = 12
	}
	accounts := []map[string]any{}
	messages := []string{}
	total := history.Result{Items: []history.Item{}}
	for index, handle := range ordered {
		if window.Mode == history.ModeBefore {
			oldest, lookupErr := h.Store.OldestSourceTimestamp(ctx, history.SourceX, handle)
			if lookupErr != nil {
				return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
			}
			if oldest == nil {
				message := "no stored records for this account; select a date range first"
				accounts = append(accounts, map[string]any{"handle": handle, "listed": 0, "new": 0, "stored": 0, "failed": 0, "pages": 0, "reachedEnd": true})
				messages = append(messages, handle+": "+message)
				continue
			}
		}
		if remaining <= 0 {
			total.HasMore = true
			messages = append(messages, handle+": X request budget exhausted")
			continue
		}
		left := len(ordered) - index
		limit := (remaining + left - 1) / left
		result, fetchErr := fetch(ctx, history.Request{
			Source: history.SourceX, Account: handle, Credential: h.XToken, Window: window,
			PageLimit: min(limit, h.Fetcher.Options.MaxPages),
		})
		if ctx.Err() != nil {
			return AttemptResult{}, NewStepError("cancelled", ErrCancelled)
		}
		total.Requested += result.Requested
		remaining -= result.Requested
		// A manual fetch ignores the timed chain's gate and cap, but the posts
		// it reads count toward the day's X API usage (ADR-0004).
		total.PostsRead += result.PostsRead
		if h.Store.AddXAPIUsage(ctx, sqlite.JapanDay(time.Now()), result.PostsRead) != nil {
			return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
		}
		if fetchErr != nil {
			total.HasMore = true
			messages = append(messages, handle+": "+historyMessage(fetchErr))
			continue
		}
		account := counts(result)
		account["handle"] = handle
		accounts = append(accounts, account)
		total.Listed += result.Listed
		total.New += result.New
		total.Stored += result.Stored
		total.Failed += result.Failed
		total.ScheduleAssets += result.ScheduleAssets
		total.HasMore = total.HasMore || result.HasMore
		for _, item := range result.Items {
			if len(total.Items) < history.MaxItems {
				total.Items = append(total.Items, item)
			}
		}
		for _, message := range result.Errors {
			messages = append(messages, handle+": "+message)
		}
	}
	if len(accounts) == 0 {
		failure := "X fetch failed for every account"
		if len(messages) > 0 {
			failure += " (" + messages[0] + ")"
		}
		return failedResult(failure), nil
	}
	out := counts(total)
	out["source"], out["scheduleAssets"], out["items"], out["accounts"], out["errors"] = "x", total.ScheduleAssets, total.Items, accounts, messages
	out["discovery"], out["apiRead"] = "api", total.PostsRead
	if window.Method == history.MethodTimeline {
		out["discovery"] = xsource.DiscoveryTimeline
		delete(out, "apiRead")
	}
	return AttemptResult{Status: stepStatus(messages), Result: out}, nil
}

// FetchYoutube fetches the configured channel through the window's method.
func (h *HistorySource) FetchYoutube(ctx context.Context, window history.Window) (AttemptResult, error) {
	window, message := h.selection(history.SourceYoutube, window)
	if message != "" {
		return failedResult(message), nil
	}
	result, err := h.Fetcher.FetchUploads(ctx, history.Request{Source: history.SourceYoutube, Account: h.ChannelID, Window: window})
	if ctx.Err() != nil {
		return AttemptResult{}, NewStepError("cancelled", ErrCancelled)
	}
	if err != nil {
		return failedResult(historyMessage(err)), nil
	}
	out := counts(result)
	out["items"], out["errors"] = result.Items, tallyCodes(result.Errors)
	// The API method spends quota on its listing and its detail checks.
	if window.Method == history.MethodAPI {
		out["apiRequests"], out["inspected"] = result.APIRequests, result.Inspected
	}
	return AttemptResult{Status: stepStatus(result.Errors), Result: out}, nil
}
