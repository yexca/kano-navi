package adminapi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"slices"
	"strings"

	"github.com/yexca/kano-navi/backend-go/internal/httpx"
	"github.com/yexca/kano-navi/backend-go/internal/jobs"
	"github.com/yexca/kano-navi/backend-go/internal/source/history"
	ytsource "github.com/yexca/kano-navi/backend-go/internal/source/youtube"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

const (
	workflowMinIntervalMinutes = 15
	workflowMaxIntervalMinutes = 7 * 24 * 60
)

// workflowSteps is the step catalog in execution order.
var workflowSteps = []map[string]string{
	{"id": "x", "group": "source"},
	{"id": "youtube", "group": "source"},
	{"id": "media", "group": "process"},
	{"id": "schedule", "group": "process"},
	{"id": "translate", "group": "process"},
	{"id": "reconcile", "group": "process"},
}

// --- runs -------------------------------------------------------------------

func (a *api) listRuns(w http.ResponseWriter, r *http.Request) {
	page, pageSize := queryInt(r, "page", 1), min(queryInt(r, "pageSize", 20), 50)
	items, total, err := a.Runs.Page(r.Context(), page, pageSize)
	if err != nil {
		workflowError(w, err, "run_not_found")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, pageOf(items, page, pageSize, total))
}

func (a *api) getRun(w http.ResponseWriter, r *http.Request) {
	run, err := a.Runs.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		workflowError(w, err, "run_not_found")
		return
	}
	if run == nil {
		httpx.WriteCode(w, http.StatusNotFound, "run_not_found")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"run": run})
}

// startRun accepts exactly one kind of work: a full sync, the schedule scan,
// a saved workflow or one source window.
func (a *api) startRun(w http.ResponseWriter, r *http.Request) {
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	var kind string
	if json.Unmarshal(body["kind"], &kind) != nil {
		httpx.WriteError(w, httpx.Invalid("kind must be sync, scan, translate, workflow or fetch"))
		return
	}
	allowed := map[string][]string{"sync": {"kind"}, "scan": {"kind"}, "translate": {"kind"}, "workflow": {"kind", "workflowId"}, "fetch": {"kind", "source", "window"}}
	fields, known := allowed[kind]
	if !known {
		httpx.WriteError(w, httpx.Invalid("kind must be sync, scan, translate, workflow or fetch"))
		return
	}
	for key := range body {
		if !slices.Contains(fields, key) {
			httpx.WriteError(w, httpx.Invalid("%s is not supported for %s", key, kind))
			return
		}
	}
	var options jobs.Options
	jobKind := jobs.KindSync
	switch kind {
	case "scan":
		jobKind = jobs.KindScan
	case "translate":
		jobKind = jobs.KindTranslate
	case "workflow":
		jobKind = jobs.KindWorkflow
		if json.Unmarshal(body["workflowId"], &options.WorkflowID) != nil || strings.TrimSpace(options.WorkflowID) == "" {
			httpx.WriteError(w, httpx.Invalid("workflowId is required"))
			return
		}
	case "fetch":
		jobKind = jobs.KindWorkflow
		var source string
		if json.Unmarshal(body["source"], &source) != nil || (source != "x" && source != "youtube") {
			httpx.WriteError(w, httpx.Invalid("source must be x or youtube"))
			return
		}
		window, parseErr := history.Parse(body["window"])
		if parseErr != nil {
			httpx.WriteError(w, httpx.Invalid("%s", parseErr.Error()))
			return
		}
		// Past windows need the official API credential, except the X windows
		// the public timeline serves without it.
		if !a.windowAvailable(source, window.Mode) {
			if window.Mode == history.ModeCount {
				message := "count windows are available for X only"
				if source == "x" {
					message = "count windows need the public X timeline"
				}
				httpx.WriteError(w, httpx.Invalid("%s", message))
				return
			}
			credential := "YOUTUBE_API_KEY"
			if source == "x" {
				credential = "X_API_BEARER_TOKEN"
			}
			httpx.WriteError(w, httpx.Invalid("%s is required for history search", credential))
			return
		}
		options.Steps = []string{source, "media"}
		options.FetchWindows = map[string]history.Window{source: window}
	}
	result, err := a.Runs.Start(r.Context(), jobKind, "admin", options)
	if err != nil {
		workflowError(w, err, "workflow_not_found")
		return
	}
	writeStart(w, result)
}

func writeStart(w http.ResponseWriter, result jobs.StartResult) {
	switch {
	case result.Accepted:
		httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"run": result.Job})
	case result.Reason != nil && *result.Reason == jobs.ReasonWorkflowNotFound:
		httpx.WriteCode(w, http.StatusNotFound, "workflow_not_found")
	case result.Reason != nil && *result.Reason == jobs.ReasonNoSteps:
		httpx.WriteError(w, httpx.Invalid("the workflow has no steps"))
	default:
		httpx.WriteJSON(w, http.StatusConflict, map[string]any{"error": "busy", "run": result.Job})
	}
}

func (a *api) cancelRun(w http.ResponseWriter, r *http.Request) {
	run, err := a.Runs.Cancel(r.Context(), r.PathValue("id"))
	if err == nil && run == nil {
		err = sqlite.ErrWorkflowNotFound
	}
	if err != nil {
		workflowError(w, err, "run_not_found")
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"run": run})
}

func (a *api) retryRun(w http.ResponseWriter, r *http.Request) {
	run, err := a.Runs.Retry(r.Context(), r.PathValue("id"))
	if errors.Is(err, sqlite.ErrWorkflowBusy) {
		active, _ := a.Runs.Active(r.Context())
		httpx.WriteJSON(w, http.StatusConflict, map[string]any{"error": "busy", "run": active})
		return
	}
	if err == nil && run == nil {
		err = sqlite.ErrWorkflowNotFound
	}
	if err != nil {
		workflowError(w, err, "run_not_found")
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"run": run})
}

// --- workflows --------------------------------------------------------------

func (a *api) listWorkflows(w http.ResponseWriter, r *http.Request) {
	page, err := a.Store.ListWorkflows(r.Context(), 1, 100)
	if err != nil {
		workflowError(w, err, "workflow_not_found")
		return
	}
	skipped := a.SkippedSteps
	if skipped == nil {
		skipped = []string{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"items":        page.Workflows,
		"steps":        workflowSteps,
		"limits":       map[string]int{"minIntervalMinutes": workflowMinIntervalMinutes, "maxIntervalMinutes": workflowMaxIntervalMinutes},
		"skippedSteps": skipped,
	})
}

func (a *api) createWorkflow(w http.ResponseWriter, r *http.Request) {
	a.saveWorkflow(w, r, "")
}

func (a *api) updateWorkflow(w http.ResponseWriter, r *http.Request) {
	a.saveWorkflow(w, r, r.PathValue("id"))
}

func (a *api) saveWorkflow(w http.ResponseWriter, r *http.Request, id string) {
	create := id == ""
	input := sqlite.WorkflowDefinition{IntervalMinutes: 60}
	var existing *sqlite.WorkflowDefinition
	if !create {
		found, err := a.Store.GetWorkflow(r.Context(), id)
		if err == nil && found == nil {
			err = sqlite.ErrWorkflowNotFound
		}
		if err != nil {
			workflowError(w, err, "workflow_not_found")
			return
		}
		existing = found
		input = *found
	}
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err = workflowInput(body, existing, &input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	saved, err := a.Store.SaveWorkflow(r.Context(), input, create)
	if errors.Is(err, sqlite.ErrWorkflowValidation) {
		httpx.WriteError(w, httpx.Invalid("workflow is invalid"))
		return
	}
	if err != nil {
		workflowError(w, err, "workflow_not_found")
		return
	}
	status := http.StatusOK
	if create {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, map[string]any{"workflow": saved})
}

func (a *api) deleteWorkflow(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.DeleteWorkflow(r.Context(), r.PathValue("id")); err != nil {
		workflowError(w, err, "workflow_not_found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var workflowFields = []string{"name", "steps", "scheduleEnabled", "intervalMinutes", "sourceLookbackDays"}

func workflowInput(body map[string]json.RawMessage, existing *sqlite.WorkflowDefinition, input *sqlite.WorkflowDefinition) error {
	for key := range body {
		if !slices.Contains(workflowFields, key) {
			return httpx.Invalid("%s is not a workflow field", key)
		}
	}
	if raw, ok := body["name"]; ok && !jsonNull(raw) {
		var name string
		if json.Unmarshal(raw, &name) != nil {
			return httpx.Invalid("name must be a string")
		}
		input.Name = strings.TrimSpace(name)
	}
	if input.Name == "" {
		return httpx.Invalid("name is required")
	}
	if len([]rune(input.Name)) > 80 {
		return httpx.Invalid("name is too long")
	}
	if raw, ok := body["steps"]; ok && !jsonNull(raw) {
		var values []string
		if json.Unmarshal(raw, &values) != nil {
			return httpx.Invalid("steps must be an array of step ids")
		}
		distinct := map[string]bool{}
		for _, value := range values {
			distinct[strings.ToLower(strings.TrimSpace(value))] = true
		}
		names := make([]string, 0, len(distinct))
		for name := range distinct {
			names = append(names, name)
		}
		steps := jobs.NormalizeSteps(names)
		if len(steps) != len(distinct) {
			return httpx.Invalid("steps contains an unsupported value")
		}
		input.Steps = steps
	} else if existing == nil {
		return httpx.Invalid("steps must be an array of step ids")
	}
	if len(input.Steps) == 0 {
		return httpx.Invalid("steps must not be empty")
	}
	if raw, ok := body["scheduleEnabled"]; ok && !jsonNull(raw) {
		if json.Unmarshal(raw, &input.ScheduleEnabled) != nil {
			return httpx.Invalid("scheduleEnabled must be a boolean")
		}
	}
	if raw, ok := body["intervalMinutes"]; ok && !jsonNull(raw) {
		minutes, err := integer(raw, "intervalMinutes", workflowMinIntervalMinutes, workflowMaxIntervalMinutes)
		if err != nil {
			return err
		}
		input.IntervalMinutes = minutes
	}
	lookback := map[string]int{"x": 7, "youtube": 14}
	for source, days := range input.SourceLookbackDays {
		lookback[source] = days
	}
	if raw, ok := body["sourceLookbackDays"]; ok && !jsonNull(raw) {
		var days map[string]json.RawMessage
		if json.Unmarshal(raw, &days) != nil {
			return httpx.Invalid("sourceLookbackDays must contain x and/or youtube")
		}
		for source, value := range days {
			if source != "x" && source != "youtube" {
				return httpx.Invalid("sourceLookbackDays must contain x and/or youtube")
			}
			if jsonNull(value) {
				continue
			}
			number, err := integer(value, "days", 1, 365)
			if err != nil {
				return err
			}
			lookback[source] = number
		}
	}
	input.SourceLookbackDays = lookback
	return nil
}

func jsonNull(raw json.RawMessage) bool {
	return len(raw) == 0 || strings.TrimSpace(string(raw)) == "null"
}

func integer(raw json.RawMessage, name string, minimum, maximum int) (int, error) {
	var number float64
	if json.Unmarshal(raw, &number) != nil || math.Trunc(number) != number || number < float64(minimum) || number > float64(maximum) {
		return 0, httpx.Invalid("%s must be an integer from %d to %d", name, minimum, maximum)
	}
	return int(number), nil
}

// --- sources ----------------------------------------------------------------

func (a *api) sources(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	accounts := []map[string]any{}
	for _, account := range a.XHandles {
		oldest, err := a.Store.OldestSourceTimestamp(ctx, "x", account)
		if err != nil {
			httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
			return
		}
		accounts = append(accounts, map[string]any{"account": account, "oldest": oldest})
	}
	oldest, err := a.Store.OldestSourceTimestamp(ctx, "youtube", "")
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	probe, err := a.liveProbeState(ctx)
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"x":       map[string]any{"skipped": a.skipped("x"), "historyAvailable": a.historyAvailable("x"), "windows": a.windows("x"), "accounts": accounts},
		"youtube": map[string]any{"skipped": a.skipped("youtube"), "historyAvailable": a.historyAvailable("youtube"), "windows": a.windows("youtube"), "oldest": oldest, "push": a.pushStatus(), "liveProbe": probe},
	})
}

// liveProbeState is the live-status probe as the console sees it: the
// operator settings and their bounds, whether this process runs the probe,
// when the newest probe run was queued and how many videos it would check now.
func (a *api) liveProbeState(ctx context.Context) (map[string]any, error) {
	settings, err := a.Store.LiveProbeSettings(ctx)
	if err != nil {
		return nil, err
	}
	last, err := a.Store.LastLiveProbeAt(ctx)
	if err != nil {
		return nil, err
	}
	active, err := a.Store.YoutubeLiveCandidates(ctx, a.Now(), ytsource.MaxLiveProbes)
	if err != nil {
		return nil, err
	}
	available := false
	if a.LiveProbe != nil {
		available = a.LiveProbe()["available"] == true
	}
	var lastRunAt any
	if last != nil {
		lastRunAt = sqlite.ISOTime(*last)
	}
	return map[string]any{
		"enabled": settings.Enabled, "intervalMinutes": settings.IntervalMinutes,
		"minIntervalMinutes": sqlite.LiveProbeMinMinutes, "maxIntervalMinutes": sqlite.LiveProbeMaxMinutes,
		"available": available && !a.skipped("youtube"), "lastRunAt": lastRunAt,
		"activeVideos": len(active), "maxVideos": ytsource.MaxLiveProbes,
	}, nil
}

func (a *api) liveProbe(w http.ResponseWriter, r *http.Request) {
	state, err := a.liveProbeState(r.Context())
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, state)
}

// updateLiveProbe accepts { enabled?, intervalMinutes? }; omitted fields keep
// their value.
func (a *api) updateLiveProbe(w http.ResponseWriter, r *http.Request) {
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	settings, err := a.Store.LiveProbeSettings(r.Context())
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	for key, raw := range body {
		switch key {
		case "enabled":
			if json.Unmarshal(raw, &settings.Enabled) != nil {
				httpx.WriteError(w, httpx.Invalid("enabled must be a boolean"))
				return
			}
		case "intervalMinutes":
			minutes, rangeErr := integer(raw, "intervalMinutes", sqlite.LiveProbeMinMinutes, sqlite.LiveProbeMaxMinutes)
			if rangeErr != nil {
				httpx.WriteError(w, rangeErr)
				return
			}
			settings.IntervalMinutes = minutes
		default:
			httpx.WriteError(w, httpx.Invalid("%s is not a live probe field", key))
			return
		}
	}
	if err = a.Store.SaveLiveProbeSettings(r.Context(), settings); err != nil {
		workflowError(w, err, "not_found")
		return
	}
	a.liveProbe(w, r)
}

func (a *api) pushStatus() map[string]any {
	if a.YoutubePush == nil {
		return map[string]any{"available": false, "enabled": false}
	}
	return a.YoutubePush()
}

// --- overview ---------------------------------------------------------------

func (a *api) overview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	counts, err := a.Store.AdminCounts(ctx, a.Now().In(japanZone).Format("2006-01-02"))
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	lastSuccess, err := a.Store.SourceLastSuccess(ctx)
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	workflows := map[string]any{"total": 0, "scheduled": 0, "nextRunAt": nil}
	if page, listErr := a.Store.ListWorkflows(ctx, 1, 100); listErr == nil {
		scheduled := 0
		var next *string
		for _, workflow := range page.Workflows {
			if !workflow.ScheduleEnabled {
				continue
			}
			scheduled++
			if workflow.NextRunAt != nil && (next == nil || *workflow.NextRunAt < *next) {
				next = workflow.NextRunAt
			}
		}
		workflows = map[string]any{"total": page.Total, "scheduled": scheduled, "nextRunAt": next}
	}
	var active *jobs.Job
	recent := []jobs.Job{}
	if a.Runs != nil {
		active, _ = a.Runs.Active(ctx)
		if listed, _, listErr := a.Runs.Page(ctx, 1, 20); listErr == nil {
			recent = listed
		}
	}
	scheduler := map[string]any{"enabled": false, "running": false, "tickSeconds": nil}
	if a.Scheduler != nil {
		scheduler = a.Scheduler()
	}
	push := a.pushStatus()
	source := func(id string) map[string]any {
		return map[string]any{"skipped": a.skipped(id), "lastSuccessAt": lastSuccess[id], "lastStep": lastStep(recent, id)}
	}
	youtube := source("youtube")
	youtube["push"] = map[string]any{"enabled": push["enabled"] == true, "verified": push["verified"] == true}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"activeRun":  active,
		"recentRuns": recent[:min(len(recent), 6)],
		"scheduler":  scheduler,
		"workflows":  workflows,
		"sources":    map[string]any{"x": source("x"), "youtube": youtube},
		"review": map[string]int{
			"pendingImages":          counts.PendingImages,
			"suspectedCancellations": counts.SuspectedCancellations,
			"failedPosts":            counts.FailedPosts,
			"reprocessRequested":     counts.ReprocessRequested,
		},
		"counts": map[string]int{
			"upcomingEvents": counts.UpcomingEvents,
			"events":         counts.Events,
			"posts":          counts.Posts,
			"videos":         counts.Videos,
		},
	})
}

// lastStep is the outcome of a source in the newest finished run that
// included it.
func lastStep(runs []jobs.Job, step string) map[string]any {
	for _, run := range runs {
		status, included := run.Progress[step]
		if !included || status == "pending" || status == "running" {
			continue
		}
		return map[string]any{"runId": run.ID, "status": status, "finishedAt": run.FinishedAt}
	}
	return nil
}
