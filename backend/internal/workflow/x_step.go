package workflow

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	xsource "github.com/yexca/kano-navi/backend/internal/source/x"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

type XRecentExecutor struct {
	Store   *sqlite.Store
	Adapter *xsource.Adapter
	Options xsource.Options
	// Now dates the X API daily cap; nil is the clock.
	Now func() time.Time
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
	// The run reads through its frozen chain, or the default one.
	chain := sqlite.DefaultSourceChain("x")
	if in.Chain != nil {
		chain = *in.Chain
	}
	opts.Chain = chain.Enabled()
	// The gate counts timeline failures, so a chain without the timeline
	// leaves the API open.
	if chain.Uses(xsource.DiscoveryTimeline) {
		opts.APIGate = chain.APIGate
	}
	now := time.Now()
	if x.Now != nil {
		now = x.Now()
	}
	day := sqlite.JapanDay(now)
	budget := 0
	if chain.Uses(xsource.DiscoveryAPI) {
		usage, usageErr := x.Store.XAPIUsageOn(ctx, day)
		if usageErr != nil {
			return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
		}
		budget = max(chain.APIDailyCap-usage.Posts, 0)
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
	// Where each account was served from, every chain method tried, and the
	// posts API pages carried and the posts API answers returned.
	discovery, tried := []string{}, []string{}
	apiPosts, apiRead := 0, 0
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
		accountOpts.GapCeilings = state.GapCeilings
		failures, failuresErr := x.Store.XTimelineFailures(ctx, account)
		if failuresErr != nil {
			return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
		}
		accountOpts.TimelineFailures, accountOpts.APIBudget = failures, budget
		result, err := x.Adapter.FetchRecent(ctx, account, accountOpts)
		if writeErr := x.recordChain(ctx, account, day, failures, result); writeErr != nil {
			return AttemptResult{}, NewStepError("x_source_write_failed", ErrSchema)
		}
		budget = max(budget-result.APIRead, 0)
		apiRead += result.APIRead
		served := result.Discovery
		if served == "" {
			served = "none"
		}
		discovery = append(discovery, account+": "+served)
		for _, attempt := range result.Tried {
			entry := account + ": " + attempt.Method
			if attempt.Code != "" {
				entry += ": " + attempt.Code
			}
			tried = append(tried, entry)
			// The cap leaves the run partial; the gate and a missing
			// credential are the chain working as set.
			if attempt.Code == string(xsource.ErrAPIDailyCap) {
				notes = append(notes, account+": "+attempt.Code)
			}
		}
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
		if result.Discovery == xsource.DiscoveryAPI {
			apiPosts += len(result.FromListing)
		} else {
			fromTimeline += len(result.FromListing)
		}
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
	out := map[string]any{"source": "x", "count": total, "posts": total, "discovered": discovered, "requested": requested, "timelinePosts": fromTimeline, "profileFallbacks": profileFallbacks, "timelineGaps": gaps, "errors": errorsOut, "discovery": discovery, "tried": tried}
	if apiRead > 0 || apiPosts > 0 {
		out["apiPosts"], out["apiRead"] = apiPosts, apiRead
	}
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

// ExecuteRelated is the x_related step: it reads every related account's
// timeline (ReadRelated) with the bounds of RelatedOptions, or only the
// account a manual fetch's window names. Without a related account it
// succeeds as skipped without a request. An account whose read failed leaves
// the run partial with its code in errors, as do the reads' notes; when every
// account failed the step fails with x_related_failed. A manual fetch fails
// once instead, with x_related_account_unknown when its account is no longer
// related or with its account's failure code.
func (x *XRecentExecutor) ExecuteRelated(ctx context.Context, in StepInput) (AttemptResult, error) {
	if x == nil || x.Store == nil || x.Adapter == nil {
		return AttemptResult{}, NewStepError("x_source_unavailable", ErrDependency)
	}
	only := ""
	if in.Window != nil {
		only = in.Window.Account
	}
	read, err := x.ReadRelated(ctx, x.RelatedOptions(in), only)
	if err != nil {
		return AttemptResult{}, err
	}
	// A manual fetch fails once with its code, as the other manual fetches
	// do; it is not retried.
	if only != "" && read.Accounts == 0 {
		return failedResult("x_related_account_unknown"), nil
	}
	if only != "" && read.Failed == read.Accounts {
		return failedResult(read.Errors[0]), nil
	}
	if read.Accounts == 0 {
		return AttemptResult{Status: "succeeded", Result: map[string]any{"skipped": true, "reason": "no_accounts"}}, nil
	}
	if read.Failed == read.Accounts {
		return AttemptResult{}, NewStepError("x_related_failed", ErrDependency)
	}
	out := map[string]any{"source": sqlite.XRelatedStep, "accounts": read.Accounts, "listed": read.Listed, "promoted": read.Promoted, "demoted": read.Demoted}
	if len(read.Errors) == 0 {
		return AttemptResult{Status: "succeeded", Result: out}, nil
	}
	out["errors"] = read.Errors
	return AttemptResult{Status: "partial", Result: out}, nil
}

// RelatedOptions are the read bounds of the related accounts' timelines: the
// x_related schedule's incremental cap, else the X discovery limit, and the
// run's lookback days, else the X first-run age.
func (x *XRecentExecutor) RelatedOptions(in StepInput) xsource.Options {
	var opts xsource.Options
	if x != nil {
		opts = x.Options
	}
	if len(opts.Handles) == 0 {
		opts = xsource.OptionsFromEnv()
	}
	if in.LookbackDays > 0 {
		opts.BootstrapDays = in.LookbackDays
	}
	if in.Sync.Mode == sqlite.SyncIncremental && in.Sync.Max > 0 {
		opts.DiscoveryLimit = in.Sync.Max
	}
	return opts
}

// RelatedRead is what one read of the related accounts did: how many
// accounts it read and how many of them failed, the statuses their
// timelines listed, promoted and demoted, and the failure codes and notes
// as "@<handle>: <code>".
type RelatedRead struct {
	Accounts, Failed          int
	Listed, Promoted, Demoted int
	Errors                    []string
}

// ReadRelated reads each related account's own timeline, or only the one
// named by only, into the X status pool. It uses the FxTwitter timeline alone: no profile page, no
// X API and no detail request, since a page carries each status and
// relevance needs its text. Each account keeps its own x-related cursor,
// known statuses and gaps, and records a failed read's code as its last
// error; its reposts and statuses without content are left out.
// Cancellation and store failures stop it.
func (x *XRecentExecutor) ReadRelated(ctx context.Context, base xsource.Options, only string) (RelatedRead, error) {
	read := RelatedRead{Errors: []string{}}
	accounts, err := x.Store.XRelatedAccounts(ctx)
	if err != nil {
		return read, NewStepError("schema_failure", ErrSchema)
	}
	// A handle is in one role at a time: an own account is never read as a
	// related one.
	accounts = slices.DeleteFunc(accounts, func(account sqlite.XRelatedAccount) bool {
		return (only != "" && !strings.EqualFold(account.Handle, only)) || slices.ContainsFunc(x.Options.Handles, func(own string) bool {
			return strings.EqualFold(strings.TrimPrefix(own, "@"), account.Handle)
		})
	})
	read.Accounts = len(accounts)
	for _, account := range accounts {
		if ctx.Err() != nil {
			return read, NewStepError("cancelled", ErrCancelled)
		}
		state, stateErr := x.Store.XRelatedState(ctx, account.Handle)
		if stateErr != nil {
			return read, NewStepError("schema_failure", ErrSchema)
		}
		opts := base
		opts.Chain = []string{xsource.DiscoveryTimeline}
		opts.MaxDetailRequests, opts.RefreshKnown = 0, 0
		opts.PendingIDs, opts.PendingRetries, opts.RefreshIDs = nil, nil, nil
		opts.KnownIDs, opts.CursorID, opts.Gap, opts.GapCeilings = state.KnownIDs, state.CursorID, state.Gap, state.GapCeilings
		opts.ScheduleKeywords, opts.APIGate, opts.APIBudget, opts.TimelineFailures = nil, 0, 0, 0
		result, fetchErr := x.Adapter.FetchRecent(ctx, account.Handle, opts)
		if fetchErr != nil {
			if ctx.Err() != nil {
				return read, NewStepError("cancelled", ErrCancelled)
			}
			result.Errors = append([]string{fetchErrorCode(fetchErr)}, result.Errors...)
			read.Failed++
			read.Errors = append(read.Errors, "@"+account.Handle+": "+fetchErrorCode(fetchErr))
		}
		written, writeErr := x.Store.WriteXRelated(ctx, account.Handle, result, fetchErr == nil)
		if writeErr != nil {
			return read, NewStepError("x_source_write_failed", ErrSchema)
		}
		read.Listed += written.Listed
		read.Promoted += written.Promoted
		read.Demoted += written.Demoted
		for _, note := range result.Notes {
			read.Errors = append(read.Errors, "@"+account.Handle+": "+note)
		}
	}
	return read, nil
}

// fetchErrorCode is a source error's controlled code.
func fetchErrorCode(err error) string {
	var sourceErr *xsource.Error
	if errors.As(err, &sourceErr) {
		return string(sourceErr.Code)
	}
	return "x_error"
}

// recordChain keeps the X API state the chain reads: the posts API answers
// returned count toward the Japan day's usage, and the account's timeline
// failure streak grows when its timeline failed and starts over when the
// timeline served. A cancelled run changes no streak.
func (x *XRecentExecutor) recordChain(ctx context.Context, account, day string, failures int, result xsource.Result) error {
	if err := x.Store.AddXAPIUsage(ctx, day, result.APIRead); err != nil {
		return err
	}
	switch {
	case ctx.Err() != nil:
	case result.Discovery == xsource.DiscoveryTimeline && failures != 0:
		return x.Store.SetXTimelineFailures(ctx, account, 0)
	case result.TimelineFailed:
		return x.Store.SetXTimelineFailures(ctx, account, failures+1)
	}
	return nil
}

// Refresh reads only the operator refreshes of every account, within the
// detail request budget. A count setting, whose timeline read leaves the
// incremental state as it is, calls it after that read. It reports how many
// refreshes took their detail request.
func (x *XRecentExecutor) Refresh(ctx context.Context) (int, error) {
	if x == nil || x.Store == nil || x.Adapter == nil {
		return 0, nil
	}
	opts := x.Options
	if len(opts.Handles) == 0 {
		opts = xsource.OptionsFromEnv()
	}
	opts.RefreshOnly = true
	remaining, refreshed := max(opts.MaxDetailRequests, 0), 0
	for _, account := range opts.Handles {
		if remaining == 0 || ctx.Err() != nil {
			break
		}
		state, err := x.Store.XRecentState(ctx, account)
		if err != nil {
			return refreshed, err
		}
		if len(state.RefreshIDs) == 0 {
			continue
		}
		accountOpts := opts
		accountOpts.RefreshIDs = state.RefreshIDs
		accountOpts.MaxDetailRequests = remaining
		result, fetchErr := x.Adapter.FetchRecent(ctx, account, accountOpts)
		remaining -= min(result.RequestedCount, remaining)
		if len(result.Refreshed) == 0 {
			continue
		}
		refreshed += len(result.Refreshed)
		if err = x.Store.WriteXRecent(ctx, account, result, fetchErr == nil); err != nil {
			return refreshed, err
		}
	}
	return refreshed, nil
}
