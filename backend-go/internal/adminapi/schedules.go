package adminapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/httpx"
	"github.com/yexca/kano-navi/backend-go/internal/jobs"
	"github.com/yexca/kano-navi/backend-go/internal/source/history"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// --- source schedules -------------------------------------------------------

// bilibiliFloor is BILIBILI_MIN_INTERVAL_MINUTES as the process reads it, or
// its default when this process has no Bilibili reader.
func (a *api) bilibiliFloor() int {
	if a.Bilibili == nil {
		return 180
	}
	return int(a.Bilibili.Options.MinInterval / time.Minute)
}

// sourceSchedules lists each source's timer for the console, with its bounds
// and whether SKIP_* disables it in this process.
func (a *api) sourceSchedules(ctx context.Context) ([]map[string]any, error) {
	schedules, err := a.Store.SourceSchedules(ctx, a.bilibiliFloor())
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(schedules))
	for _, schedule := range schedules {
		entry, err := a.scheduleEntry(ctx, schedule)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, nil
}

func (a *api) scheduleEntry(ctx context.Context, schedule sqlite.SourceSchedule) (map[string]any, error) {
	low, high := sqlite.SourceScheduleBounds(schedule.Source, a.bilibiliFloor())
	entry := map[string]any{
		"source": schedule.Source, "enabled": schedule.Enabled, "intervalMinutes": schedule.IntervalMinutes,
		"minIntervalMinutes": low, "maxIntervalMinutes": high, "sync": schedule.Sync,
		"nextRunAt": schedule.NextRunAt, "lastRun": schedule.LastRun, "skipped": a.skipped(schedule.Source),
	}
	if a.skipped(schedule.Source) {
		entry["reason"] = "environment"
	}
	chain, err := a.chainEntry(ctx, schedule)
	if err != nil {
		return nil, err
	}
	entry["chain"] = chain
	return entry, nil
}

// chainEntry describes a schedule's method chain (ADR-0004): each method in
// order with whether it is enabled, whether this process can run it and the
// variable it needs. X adds its API gate and cap, today's usage and each
// account's timeline failure streak. Bilibili's one method is fixed.
func (a *api) chainEntry(ctx context.Context, schedule sqlite.SourceSchedule) (map[string]any, error) {
	methods := []map[string]any{}
	for _, method := range schedule.Chain.Methods {
		item := map[string]any{"id": method.ID, "enabled": method.Enabled, "available": true}
		if method.ID == history.MethodAPI {
			if described, ok := history.Lookup(schedule.Source, method.ID); ok {
				item["requires"] = described.Requires
			}
			item["available"] = a.methodAvailable(schedule.Source, method.ID)
		}
		methods = append(methods, item)
	}
	out := map[string]any{"methods": methods, "fixed": !slices.Contains(sqlite.ChainSources, schedule.Source)}
	if schedule.Source == "x" {
		state, err := a.Store.XAPIStatus(ctx, a.XHandles, a.Now())
		if err != nil {
			return nil, err
		}
		out["apiGate"], out["apiDailyCap"] = schedule.Chain.APIGate, schedule.Chain.APIDailyCap
		out["apiUsage"], out["timelineFailures"] = state.Usage, state.TimelineFailures
	}
	return out, nil
}

// updateSourceSchedule accepts any subset of { enabled, intervalMinutes,
// sync, chain }; omitted fields keep their value.
func (a *api) updateSourceSchedule(w http.ResponseWriter, r *http.Request) {
	source := r.PathValue("source")
	if !slices.Contains(sqlite.ScheduleSources, source) {
		httpx.WriteCode(w, http.StatusNotFound, "not_found")
		return
	}
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	floor := a.bilibiliFloor()
	schedule, err := a.Store.SourceScheduleFor(r.Context(), source, floor)
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	low, high := sqlite.SourceScheduleBounds(source, floor)
	for key, raw := range body {
		switch key {
		case "enabled":
			if json.Unmarshal(raw, &schedule.Enabled) != nil {
				httpx.WriteError(w, httpx.Invalid("enabled must be a boolean"))
				return
			}
		case "intervalMinutes":
			minutes, rangeErr := integer(raw, "intervalMinutes", low, high)
			if rangeErr != nil {
				httpx.WriteError(w, rangeErr)
				return
			}
			schedule.IntervalMinutes = minutes
		case "sync":
			if schedule.Sync == nil {
				httpx.WriteError(w, httpx.Invalid("%s has no sync setting", source))
				return
			}
			setting, syncErr := sourceSync(raw)
			if syncErr != nil {
				httpx.WriteError(w, syncErr)
				return
			}
			schedule.Sync = &setting
		case "chain":
			chain, chainErr := sourceChain(source, raw, schedule.Chain)
			if chainErr != nil {
				httpx.WriteError(w, chainErr)
				return
			}
			schedule.Chain = chain
		default:
			httpx.WriteError(w, httpx.Invalid("%s is not a source schedule field", key))
			return
		}
	}
	saved, err := a.Store.SaveSourceSchedule(r.Context(), schedule, floor, a.Now())
	if errors.Is(err, sqlite.ErrWorkflowValidation) {
		httpx.WriteError(w, httpx.Invalid("source schedule is invalid"))
		return
	}
	if err != nil {
		workflowError(w, err, "not_found")
		return
	}
	entry, err := a.scheduleEntry(r.Context(), saved)
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, entry)
}

// sourceChain reads { methods: [{ id, enabled }], apiGate, apiDailyCap } over
// the current chain: methods, when given, list every method of the source
// once with at least one enabled; the X API gate and cap apply to X only.
func sourceChain(source string, raw json.RawMessage, current sqlite.SourceChain) (sqlite.SourceChain, error) {
	if !slices.Contains(sqlite.ChainSources, source) {
		return current, httpx.Invalid("the %s chain is fixed", source)
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(raw, &body) != nil || body == nil {
		return current, httpx.Invalid("chain must be an object")
	}
	chain := current
	chain.Methods = slices.Clone(current.Methods)
	for key, value := range body {
		switch {
		case key == "methods":
			var methods []map[string]json.RawMessage
			if json.Unmarshal(value, &methods) != nil {
				return current, httpx.Invalid("chain methods must be a list of { id, enabled }")
			}
			chain.Methods = []sqlite.ChainMethod{}
			seen := map[string]bool{}
			for _, item := range methods {
				var method sqlite.ChainMethod
				if len(item) != 2 || json.Unmarshal(item["id"], &method.ID) != nil || json.Unmarshal(item["enabled"], &method.Enabled) != nil {
					return current, httpx.Invalid("chain methods must be a list of { id, enabled }")
				}
				if !slices.Contains(sqlite.ChainMethodIDs(source), method.ID) {
					return current, httpx.Invalid("%s is not a method of the %s chain", method.ID, source)
				}
				if seen[method.ID] {
					return current, httpx.Invalid("the chain lists %s twice", method.ID)
				}
				seen[method.ID] = true
				chain.Methods = append(chain.Methods, method)
			}
			if len(seen) != len(sqlite.ChainMethodIDs(source)) {
				return current, httpx.Invalid("the chain must list every method: %s", strings.Join(sqlite.ChainMethodIDs(source), ", "))
			}
			if len(chain.Enabled()) == 0 {
				return current, httpx.Invalid("the chain must enable at least one method")
			}
		case key == "apiGate" && source == "x":
			gate, err := integer(value, "apiGate", 0, sqlite.MaxXAPIGate)
			if err != nil {
				return current, err
			}
			chain.APIGate = gate
		case key == "apiDailyCap" && source == "x":
			limit, err := integer(value, "apiDailyCap", 1, sqlite.MaxXAPIDailyCap)
			if err != nil {
				return current, err
			}
			chain.APIDailyCap = limit
		default:
			return current, httpx.Invalid("%s is not a field of the %s chain", key, source)
		}
	}
	return chain, nil
}

// runSource starts Run now: the source's fixed chain once, triggered by the
// operator and read with its schedule's sync setting. It does not move the
// schedule's next run.
func (a *api) runSource(w http.ResponseWriter, r *http.Request) {
	source := r.PathValue("source")
	if !slices.Contains(sqlite.ScheduleSources, source) {
		httpx.WriteCode(w, http.StatusNotFound, "not_found")
		return
	}
	if a.skipped(source) {
		httpx.WriteError(w, httpx.Invalid("the %s source is disabled for this process", source))
		return
	}
	result, err := a.Runs.Start(r.Context(), jobs.KindWorkflow, "admin", jobs.Options{Source: source})
	if err != nil {
		workflowError(w, err, "not_found")
		return
	}
	writeStart(w, result)
}
