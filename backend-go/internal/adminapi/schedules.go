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
		out = append(out, a.scheduleEntry(schedule))
	}
	return out, nil
}

func (a *api) scheduleEntry(schedule sqlite.SourceSchedule) map[string]any {
	low, high := sqlite.SourceScheduleBounds(schedule.Source, a.bilibiliFloor())
	entry := map[string]any{
		"source": schedule.Source, "enabled": schedule.Enabled, "intervalMinutes": schedule.IntervalMinutes,
		"minIntervalMinutes": low, "maxIntervalMinutes": high, "sync": schedule.Sync,
		"nextRunAt": schedule.NextRunAt, "lastRun": schedule.LastRun, "skipped": a.skipped(schedule.Source),
	}
	if a.skipped(schedule.Source) {
		entry["reason"] = "environment"
	}
	return entry
}

// updateSourceSchedule accepts any subset of { enabled, intervalMinutes,
// sync }; omitted fields keep their value.
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
	httpx.WriteJSON(w, http.StatusOK, a.scheduleEntry(saved))
}

// sourceStepsOf lists the source steps of a workflow, whose timers belong to
// the source schedules.
func sourceStepsOf(steps []string) []string {
	out := []string{}
	for _, step := range steps {
		if slices.Contains(sqlite.ScheduleSources, step) {
			out = append(out, step)
		}
	}
	return out
}

// timerRefusal is the validation error for a timer on a workflow that reads
// a source: the source schedule runs it instead.
func timerRefusal(sources []string) error {
	noun := "schedule"
	if len(sources) > 1 {
		noun = "schedules"
	}
	return httpx.Invalid("a workflow with a source step runs manually; enable the %s source %s on the Sources page instead", strings.Join(sources, " and "), noun)
}
