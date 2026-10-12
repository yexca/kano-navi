package workflow

import (
	"context"

	"strings"

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
	if opts.MaxDetailRequests < 0 {
		opts.MaxDetailRequests = 0
	}
	if len(opts.Handles) == 0 {
		return AttemptResult{}, NewStepError("x_no_accounts", ErrValidation)
	}
	remaining := opts.MaxDetailRequests
	total := 0
	discovered := 0
	requested := 0
	partial := false
	errorsOut := []string{}
	for i, account := range opts.Handles {
		alloc := remaining
		if alloc > 0 {
			left := len(opts.Handles) - i
			alloc = (remaining + left - 1) / left
		}
		if alloc < 0 {
			alloc = 0
		}
		accountOpts := opts
		accountOpts.MaxDetailRequests = alloc
		result, err := x.Adapter.FetchRecent(ctx, account, accountOpts)
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
	return AttemptResult{Status: status, Result: map[string]any{"source": "x", "posts": total, "discovered": discovered, "requested": requested, "errors": errorsOut}}, nil
}

// XRecentTrigger is the narrow admin API seam; it intentionally accepts only
// the recent window and delegates durable run/step/attempt persistence to core.
type XRecentTrigger struct {
	Executor   *Executor
	WorkflowID string
}

func (t *XRecentTrigger) Trigger(ctx context.Context, days int) (*sqlite.WorkflowRun, error) {
	if t == nil || t.Executor == nil {
		return nil, NewStepError("x_source_unavailable", ErrDependency)
	}
	if days < 1 || days > 30 {
		return nil, NewStepError("invalid_recent_days", ErrValidation)
	}
	return t.Executor.Run(ctx, t.WorkflowID, TriggerOperator, "x-recent:"+strings.TrimSpace(t.WorkflowID)+":"+itoa(days))
}
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	b := []byte{}
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}
