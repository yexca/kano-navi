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

// workflowSteps is the step catalog in execution order.
var workflowSteps = []map[string]string{
	{"id": "x", "group": "source"},
	{"id": "youtube", "group": "source"},
	{"id": "bilibili", "group": "source"},
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

// stepCatalog lists the steps a one-off step run can pick, and those SKIP_*
// disables in this process.
func (a *api) stepCatalog(w http.ResponseWriter, _ *http.Request) {
	skipped := a.SkippedSteps
	if skipped == nil {
		skipped = []string{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"steps": workflowSteps, "skippedSteps": skipped})
}

// startRun accepts exactly one kind of work: a full sync, the schedule scan,
// translation, a one-off step run or one source window.
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
	if _, saved := body["workflowId"]; saved {
		httpx.WriteError(w, httpx.Invalid("saved workflows are removed; send the steps to run once, or use a source's Run now"))
		return
	}
	allowed := map[string][]string{"sync": {"kind"}, "scan": {"kind"}, "translate": {"kind"}, "workflow": {"kind", "steps"}, "fetch": {"kind", "source", "window"}}
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
		steps, stepsErr := catalogSteps(body["steps"])
		if stepsErr != nil {
			httpx.WriteError(w, stepsErr)
			return
		}
		options.Steps = steps
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
				httpx.WriteError(w, httpx.Invalid("count windows need the public X timeline"))
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
		workflowError(w, err, "run_not_found")
		return
	}
	writeStart(w, result)
}

// catalogSteps reads a one-off step run's steps: distinct catalog IDs, run
// in catalog order.
func catalogSteps(raw json.RawMessage) ([]string, error) {
	var values []string
	if jsonNull(raw) || json.Unmarshal(raw, &values) != nil {
		return nil, httpx.Invalid("steps must be an array of step ids")
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
		return nil, httpx.Invalid("steps contains an unsupported value")
	}
	if len(steps) == 0 {
		return nil, httpx.Invalid("steps must not be empty")
	}
	return steps, nil
}

func writeStart(w http.ResponseWriter, result jobs.StartResult) {
	switch {
	case result.Accepted:
		httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"run": result.Job})
	case result.Reason != nil && *result.Reason == jobs.ReasonNoSteps:
		httpx.WriteError(w, httpx.Invalid("the run has no steps"))
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

// sourceSync validates a source schedule's sync, {mode: "incremental", max}
// or {mode: "count", count};
// an incremental setting without max takes the default cap.
func sourceSync(raw json.RawMessage) (sqlite.SourceSync, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return sqlite.SourceSync{}, httpx.Invalid("sync must be an object")
	}
	var mode string
	_ = json.Unmarshal(fields["mode"], &mode)
	for key := range fields {
		if key != "mode" && key != "max" && key != "count" {
			return sqlite.SourceSync{}, httpx.Invalid("%s is not a sync field", key)
		}
	}
	switch mode {
	case sqlite.SyncIncremental:
		if _, ok := fields["count"]; ok {
			return sqlite.SourceSync{}, httpx.Invalid("incremental sync takes max, not count")
		}
		setting := sqlite.SourceSync{Mode: mode, Max: sqlite.DefaultSyncMax}
		if raw, ok := fields["max"]; ok && !jsonNull(raw) {
			number, err := integer(raw, "max", 1, sqlite.MaxSyncLimit)
			if err != nil {
				return sqlite.SourceSync{}, err
			}
			setting.Max = number
		}
		return setting, nil
	case sqlite.SyncCount:
		if _, ok := fields["max"]; ok {
			return sqlite.SourceSync{}, httpx.Invalid("count sync takes count, not max")
		}
		number, err := integer(fields["count"], "count", 1, sqlite.MaxSyncLimit)
		if err != nil {
			return sqlite.SourceSync{}, err
		}
		return sqlite.SourceSync{Mode: mode, Count: number}, nil
	}
	return sqlite.SourceSync{}, httpx.Invalid("sync mode must be incremental or count")
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
	bilibili, err := a.bilibiliStatus(ctx)
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	schedules, err := a.sourceSchedules(ctx)
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"schedules": schedules,
		"x":         map[string]any{"skipped": a.skipped("x"), "historyAvailable": a.historyAvailable("x"), "windows": a.windows("x"), "accounts": accounts},
		"youtube":   map[string]any{"skipped": a.skipped("youtube"), "historyAvailable": a.historyAvailable("youtube"), "windows": a.windows("youtube"), "oldest": oldest, "push": a.pushStatus(), "liveProbe": probe},
		"bilibili":  bilibili,
	})
}

// liveProbeState is the live-status probe as the console sees it: the
// operator settings and their bounds, whether this process runs the probe,
// when the newest probe run was queued, how many videos are active and when
// the next run is planned.
func (a *api) liveProbeState(ctx context.Context) (map[string]any, error) {
	settings, err := a.Store.LiveProbeSettings(ctx)
	if err != nil {
		return nil, err
	}
	last, err := a.Store.LastLiveProbeAt(ctx)
	if err != nil {
		return nil, err
	}
	now := a.Now()
	active, err := a.Store.LiveProbeTargets(ctx, now, settings)
	if err != nil {
		return nil, err
	}
	available := false
	if a.LiveProbe != nil {
		available = a.LiveProbe()["available"] == true
	}
	available = available && !a.skipped("youtube")
	var lastRunAt, nextRunAt any
	if last != nil {
		lastRunAt = sqlite.ISOTime(*last)
	}
	if next := sqlite.NextLiveProbeAt(active, last, now); next != nil && settings.Enabled && available {
		nextRunAt = sqlite.ISOTime(*next)
	}
	return map[string]any{
		"enabled": settings.Enabled, "intervalMinutes": settings.IntervalMinutes,
		"minIntervalMinutes": sqlite.LiveProbeMinMinutes, "maxIntervalMinutes": sqlite.LiveProbeMaxMinutes,
		"available": available, "lastRunAt": lastRunAt, "nextRunAt": nextRunAt,
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
	// The timers are the source schedules (ADR-0003); those SKIP_* disables
	// never run here.
	timers := map[string]any{"scheduled": 0, "nextRunAt": nil}
	if schedules, scheduleErr := a.Store.SourceSchedules(ctx, a.bilibiliFloor()); scheduleErr == nil {
		scheduled := 0
		var next *string
		for _, schedule := range schedules {
			if !schedule.Enabled || a.skipped(schedule.Source) {
				continue
			}
			scheduled++
			if schedule.NextRunAt != nil && (next == nil || *schedule.NextRunAt < *next) {
				next = schedule.NextRunAt
			}
		}
		timers = map[string]any{"scheduled": scheduled, "nextRunAt": next}
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
		"timers":     timers,
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
