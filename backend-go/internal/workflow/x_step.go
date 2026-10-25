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
	partial := false
	errorsOut := []string{}
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
		accountOpts.KnownIDs = state.KnownIDs
		accountOpts.CursorID = state.CursorID
		result, err := x.Adapter.FetchRecent(ctx, account, accountOpts)
		if result.Discovery == xsource.DiscoveryProfile {
			profileFallbacks++
		}
		discovered += result.DiscoveredCount
		requested += result.RequestedCount
		total += len(result.Posts)
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
	if partial {
		status = "partial"
	}
	return AttemptResult{Status: status, Result: map[string]any{"source": "x", "count": total, "posts": total, "discovered": discovered, "requested": requested, "profileFallbacks": profileFallbacks, "errors": errorsOut}}, nil
}
