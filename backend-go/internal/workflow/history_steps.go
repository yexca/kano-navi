package workflow

import (
	"context"
	"errors"
	"strings"

	"github.com/yexca/kano-navi/backend-go/internal/source/history"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// HistorySource runs operator-selected windows through the official source
// APIs. Credentials come from the environment and never enter a result.
type HistorySource struct {
	Fetcher            *history.Fetcher
	Store              *sqlite.Store
	XToken, YoutubeKey string
	Handles            []string
	ChannelID          string
	// RequestLimit is the X page budget of one step, shared by all accounts.
	RequestLimit int
}

// Available reports whether a source can search past windows.
func (h *HistorySource) Available(source string) bool {
	if h == nil || h.Fetcher == nil || h.Store == nil {
		return false
	}
	if source == history.SourceX {
		return strings.TrimSpace(h.XToken) != ""
	}
	return source == history.SourceYoutube && strings.TrimSpace(h.YoutubeKey) != ""
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

// FetchX searches every configured account. One account's failure keeps the
// others; the rotation spreads a short request budget across runs.
func (h *HistorySource) FetchX(ctx context.Context, window history.Window) (AttemptResult, error) {
	if !h.Available(history.SourceX) {
		return failedResult("X_API_BEARER_TOKEN is required for history search"), nil
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
	count, discovered, requested, scheduleAssets := 0, 0, 0, 0
	for index, handle := range ordered {
		if window.Mode == history.ModeBefore {
			oldest, lookupErr := h.Store.OldestSourceTimestamp(ctx, history.SourceX, handle)
			if lookupErr != nil {
				return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
			}
			if oldest == nil {
				message := "no stored records for this account; select a date range first"
				accounts = append(accounts, map[string]any{"handle": handle, "count": 0, "discovered": 0, "requested": 0, "supplemented": 0, "errors": []string{message}})
				messages = append(messages, handle+": "+message)
				continue
			}
		}
		if remaining <= 0 {
			messages = append(messages, handle+": X request budget exhausted")
			continue
		}
		left := len(ordered) - index
		limit := (remaining + left - 1) / left
		result, fetchErr := h.Fetcher.Fetch(ctx, history.Request{
			Source: history.SourceX, Account: handle, Credential: h.XToken, Window: window,
			PageLimit: min(limit, h.Fetcher.Options.MaxPages),
		})
		if ctx.Err() != nil {
			return AttemptResult{}, NewStepError("cancelled", ErrCancelled)
		}
		requested += result.Requested
		remaining -= result.Requested
		if fetchErr != nil {
			messages = append(messages, handle+": "+historyMessage(fetchErr))
			continue
		}
		accounts = append(accounts, map[string]any{
			"handle": handle, "count": result.Count, "discovered": result.Discovered, "requested": result.Requested,
			"supplemented": 0, "scheduleAssets": result.ScheduleAssets, "window": result.Window,
			"hasMore": result.HasMore, "errors": result.Errors,
		})
		count += result.Count
		discovered += result.Discovered
		scheduleAssets += result.ScheduleAssets
		for _, message := range result.Errors {
			messages = append(messages, handle+": "+message)
		}
	}
	if len(accounts) == 0 {
		failure := "X history search failed for every account"
		if len(messages) > 0 {
			failure += " (" + messages[0] + ")"
		}
		return failedResult(failure), nil
	}
	return AttemptResult{Status: stepStatus(messages), Result: map[string]any{
		"source": "x", "count": count, "posts": count, "discovered": discovered, "supplemented": 0,
		"requested": requested, "scheduleAssets": scheduleAssets, "accounts": accounts, "errors": messages,
	}}, nil
}

// FetchYoutube searches the configured channel. The result's counters use the
// keys the operator console reads.
func (h *HistorySource) FetchYoutube(ctx context.Context, window history.Window) (AttemptResult, error) {
	if !h.Available(history.SourceYoutube) {
		return failedResult("YOUTUBE_API_KEY is required for history search"), nil
	}
	result, err := h.Fetcher.Fetch(ctx, history.Request{Source: history.SourceYoutube, Account: h.ChannelID, Credential: h.YoutubeKey, Window: window})
	if ctx.Err() != nil {
		return AttemptResult{}, NewStepError("cancelled", ErrCancelled)
	}
	if err != nil {
		return failedResult(historyMessage(err)), nil
	}
	return AttemptResult{Status: stepStatus(result.Errors), Result: map[string]any{
		"count": result.Count, "videos": result.Count, "discovered": result.Discovered, "requested": result.Requested,
		"window": result.Window, "hasMore": result.HasMore, "errors": result.Errors,
	}}, nil
}
