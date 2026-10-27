package workflow

import (
	"context"

	xsource "github.com/yexca/kano-navi/backend-go/internal/source/x"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

type XRecentExecutor struct {
	Store   *sqlite.Store
	Adapter *xsource.Adapter
	Options xsource.Options
}

func (x *XRecentExecutor) Execute(ctx context.Context, in StepInput) (AttemptResult, error) {
	if x == nil || x.Store == nil || x.Adapter == nil {
		return AttemptResult{}, NewStepError("x_source_unavailable", ErrDependency)
	}
	opts := x.Options
	if len(opts.Handles) == 0 {
		opts = xsource.OptionsFromEnv()
	}
	if in.LookbackDays > 0 {
		opts.BootstrapDays = in.LookbackDays
	}
	// A source schedule's incremental cap replaces X_DISCOVERY_LIMIT.
	if in.Sync.Mode == sqlite.SyncIncremental && in.Sync.Max > 0 {
		opts.DiscoveryLimit = in.Sync.Max
	}
	if opts.MaxDetailRequests < 0 {
		opts.MaxDetailRequests = 0
	}
	if len(opts.Handles) == 0 {
		return AttemptResult{}, NewStepError("x_no_accounts", ErrValidation)
	}
	start, startErr := x.Store.XAccountStart(ctx, opts.Handles)
	if startErr != nil {
		return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
	}
	handles := append(append([]string{}, opts.Handles[start:]...), opts.Handles[:start]...)
	if err := x.Store.SetXNextAccount(ctx, opts.Handles[(start+1)%len(opts.Handles)]); err != nil {
		return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
	}
	remaining := opts.MaxDetailRequests
	total := 0
	discovered := 0
	requested := 0
	profileFallbacks := 0
	// Posts read straight from timeline pages, pending IDs waiting for their
	// backoff, and pending IDs the run stopped requesting.
	fromTimeline, held := 0, 0
	// Operator refreshes whose detail request was made.
	refreshed := 0
	dropped := []string{}
	partial := false
	errorsOut := []string{}
	// Gap notes do not count as fetch failures: they never fail the step.
	notes := []string{}
	gaps := 0
	for i, account := range handles {
		alloc := remaining
		if alloc > 0 {
			left := len(handles) - i
			alloc = (remaining + left - 1) / left
		}
		if alloc < 0 {
			alloc = 0
		}
		accountOpts := opts
		accountOpts.MaxDetailRequests = alloc
		state, stateErr := x.Store.XRecentState(ctx, account)
		if stateErr != nil {
			return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
		}
		accountOpts.PendingIDs = state.PendingIDs
		accountOpts.PendingRetries = state.PendingRetries
		accountOpts.KnownIDs = state.KnownIDs
		accountOpts.CursorID = state.CursorID
		accountOpts.Gap = state.Gap
		accountOpts.RefreshIDs = state.RefreshIDs
		result, err := x.Adapter.FetchRecent(ctx, account, accountOpts)
		for _, note := range result.Notes {
			notes = append(notes, account+": "+note)
		}
		if result.Gap != nil {
			gaps++
		}
		if result.Discovery == xsource.DiscoveryProfile {
			profileFallbacks++
		}
		discovered += result.DiscoveredCount
		requested += result.RequestedCount
		total += len(result.Posts)
		fromTimeline += len(result.FromTimeline)
		held += result.Held
		refreshed += len(result.Refreshed)
		for _, d := range result.Dropped {
			dropped = append(dropped, account+": "+d.ID+": "+d.Code)
		}
		errorsOut = append(errorsOut, result.Errors...)
		if result.RequestedCount > 0 {
			remaining -= result.RequestedCount
		}
		if err != nil {
			partial = true
			if result.DiscoveredCount > 0 || result.RequestedCount > 0 {
				if writeErr := x.Store.WriteXRecent(ctx, account, result, false); writeErr != nil {
					return AttemptResult{}, NewStepError("x_source_write_failed", ErrSchema)
				}
			}
			errorsOut = append(errorsOut, account+": "+err.Error())
			continue
		}
		if writeErr := x.Store.WriteXRecent(ctx, account, result, len(result.Posts) > 0); writeErr != nil {
			return AttemptResult{}, NewStepError("x_source_write_failed", ErrSchema)
		}
		if len(result.Errors) > 0 {
			partial = true
		}
	}
	status := "succeeded"
	if total == 0 && requested == 0 && discovered > 0 {
		partial = true
	}
	if total == 0 && len(errorsOut) > 0 {
		return AttemptResult{}, NewStepError("x_source_failed", ErrDependency)
	}
	if len(notes) > 0 {
		partial = true
		errorsOut = append(errorsOut, notes...)
	}
	if partial {
		status = "partial"
	}
	out := map[string]any{"source": "x", "count": total, "posts": total, "discovered": discovered, "requested": requested, "timelinePosts": fromTimeline, "profileFallbacks": profileFallbacks, "timelineGaps": gaps, "errors": errorsOut}
	// Like the extraction backoff keys, these appear only when used.
	if held > 0 {
		out["pendingHeld"] = held
	}
	if len(dropped) > 0 {
		out["pendingDropped"] = dropped
	}
	if refreshed > 0 {
		out["refreshed"] = refreshed
	}
	return AttemptResult{Status: status, Result: out}, nil
}
