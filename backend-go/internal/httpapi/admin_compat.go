package httpapi

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/httpx"
	"github.com/yexca/kano-navi/backend-go/internal/jobs"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

const (
	workflowMinIntervalMinutes = 15
	workflowMaxIntervalMinutes = 7 * 24 * 60
)

// Compat makes this process the owner of the operator console: every route
// the React admin calls is answered here with the established wire shapes.
type Compat struct {
	Jobs    jobs.Manager
	Modules []httpx.AdminModule
	// SchedulerStatus reports the in-process workflow timer.
	SchedulerStatus func() map[string]any
	// SkippedSteps lists steps disabled for the process through SKIP_* flags.
	SkippedSteps []string
	XHandles     []string
	// HistoryAvailable reports whether a source can search past windows.
	HistoryAvailable func(source string) bool
	// HistoryCredential reports whether the source API credential is present.
	HistoryCredential func(source string) bool
}

var workflowStepGroups = []map[string]string{
	{"id": "x", "group": "source"},
	{"id": "youtube", "group": "source"},
	{"id": "media", "group": "process"},
	{"id": "schedule", "group": "process"},
}

func (h *adminHandler) serveCompat(w http.ResponseWriter, r *http.Request, path string) bool {
	c := h.compat
	parts := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case path == "/workflows" && r.Method == http.MethodGet:
		h.compatWrite(w, r, http.StatusOK, nil)
	case path == "/workflows" && r.Method == http.MethodPost:
		h.compatSaveWorkflow(w, r, "")
	case len(parts) == 2 && parts[0] == "workflows" && r.Method == http.MethodPut:
		h.compatSaveWorkflow(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "workflows" && r.Method == http.MethodDelete:
		err := h.store.DeleteWorkflow(r.Context(), parts[1])
		switch {
		case errors.Is(err, sqlite.ErrWorkflowNotFound):
			httpx.WriteCode(w, http.StatusNotFound, "workflow_not_found")
		case errors.Is(err, sqlite.ErrWorkflowBusy):
			httpx.WriteCode(w, http.StatusConflict, "workflow_busy")
		case err != nil:
			workflowUnavailable(w, err)
		default:
			h.compatWrite(w, r, http.StatusOK, nil)
		}
	case len(parts) == 3 && parts[0] == "workflows" && parts[2] == "run" && r.Method == http.MethodPost:
		result, ok := h.compatStart(w, r, jobs.KindWorkflow, jobs.Options{WorkflowID: parts[1]})
		if !ok {
			return true
		}
		if result.Reason != nil && *result.Reason == jobs.ReasonWorkflowNotFound {
			httpx.WriteCode(w, http.StatusNotFound, "workflow_not_found")
			return true
		}
		h.compatWrite(w, r, startStatus(result), map[string]any{"accepted": result.Accepted, "reason": result.Reason, "job": result.Job})
	case path == "/sources/fetch" && r.Method == http.MethodGet:
		status, err := h.sourceFetchStatus(r)
		if err != nil {
			httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
			return true
		}
		httpx.WriteJSON(w, http.StatusOK, status)
	case len(parts) == 3 && parts[0] == "sources" && parts[2] == "fetch" && r.Method == http.MethodPost:
		h.compatFetchSource(w, r, parts[1])
	case path == "/sync/jobs" && r.Method == http.MethodGet:
		if c.Jobs == nil {
			httpx.WriteJSON(w, http.StatusOK, map[string]any{"jobs": []any{}})
			return true
		}
		list, err := c.Jobs.List(r.Context(), queryInt(r, "limit", 20))
		if err != nil {
			httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
			return true
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"jobs": list})
	case len(parts) == 3 && parts[0] == "sync" && parts[1] == "jobs" && r.Method == http.MethodGet:
		var job *jobs.Job
		if c.Jobs != nil {
			found, err := c.Jobs.Get(r.Context(), parts[2])
			if err != nil {
				httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
				return true
			}
			job = found
		}
		if job == nil {
			httpx.WriteCode(w, http.StatusNotFound, "sync_job_not_found")
			return true
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"job": job})
	case (path == "/sync" || path == "/sync/start") && r.Method == http.MethodPost:
		if result, ok := h.compatStart(w, r, jobs.KindSync, jobs.Options{}); ok {
			httpx.WriteJSON(w, startStatus(result), result)
		}
	case path == "/scan/automatic" && r.Method == http.MethodPost:
		if result, ok := h.compatStart(w, r, jobs.KindScan, jobs.Options{}); ok {
			httpx.WriteJSON(w, startStatus(result), result)
		}
	default:
		for _, module := range c.Modules {
			if module != nil && module.ServeAdmin(w, r, path) {
				return true
			}
		}
		return false
	}
	return true
}

func startStatus(result jobs.StartResult) int {
	if result.Accepted {
		return http.StatusAccepted
	}
	return http.StatusConflict
}

func (h *adminHandler) compatStart(w http.ResponseWriter, r *http.Request, kind string, options jobs.Options) (jobs.StartResult, bool) {
	if h.compat.Jobs == nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "sync_jobs_unavailable")
		return jobs.StartResult{}, false
	}
	result, err := h.compat.Jobs.Start(r.Context(), kind, "admin", options)
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "sync_jobs_unavailable")
		return jobs.StartResult{}, false
	}
	return result, true
}

// compatWrite answers with the workflow console state, optionally merged with
// route-specific fields such as the saved workflow or a start result.
func (h *adminHandler) compatWrite(w http.ResponseWriter, r *http.Request, status int, extra map[string]any) {
	page, err := h.store.ListWorkflows(r.Context(), 1, 100)
	if err != nil {
		workflowUnavailable(w, err)
		return
	}
	for i := range page.Workflows {
		page.Workflows[i].LastStatus = consoleWorkflowStatus(page.Workflows[i].LastStatus)
	}
	scheduler := map[string]any{"running": false, "tickSeconds": nil}
	if h.compat.SchedulerStatus != nil {
		scheduler = h.compat.SchedulerStatus()
	}
	skipped := h.compat.SkippedSteps
	if skipped == nil {
		skipped = []string{}
	}
	var active *jobs.Job
	if h.compat.Jobs != nil {
		active, _ = h.compat.Jobs.Active(r.Context())
	}
	payload := map[string]any{
		"workflows":            page.Workflows,
		"steps":                workflowStepGroups,
		"skippedByEnvironment": skipped,
		"limits":               map[string]int{"minIntervalMinutes": workflowMinIntervalMinutes, "maxIntervalMinutes": workflowMaxIntervalMinutes},
		"scheduler":            scheduler,
		"activeJob":            active,
	}
	for key, value := range extra {
		payload[key] = value
	}
	httpx.WriteJSON(w, status, payload)
}

// consoleWorkflowStatus maps durable run states to the console's vocabulary.
func consoleWorkflowStatus(status *string) *string {
	if status == nil {
		return nil
	}
	value := *status
	switch value {
	case "succeeded":
		value = "success"
	case "cancelled", "blocked":
		value = "failed"
	}
	return &value
}

func (h *adminHandler) compatSaveWorkflow(w http.ResponseWriter, r *http.Request, id string) {
	create := id == ""
	input := sqlite.WorkflowDefinition{IntervalMinutes: 60}
	var existing *sqlite.WorkflowDefinition
	if !create {
		found, err := h.store.GetWorkflow(r.Context(), id)
		if errors.Is(err, sqlite.ErrWorkflowNotFound) {
			httpx.WriteCode(w, http.StatusNotFound, "workflow_not_found")
			return
		}
		if err != nil {
			workflowUnavailable(w, err)
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
	saved, err := h.store.SaveWorkflow(r.Context(), input, create)
	if err != nil {
		if errors.Is(err, sqlite.ErrWorkflowValidation) {
			httpx.WriteError(w, httpx.Invalid("workflow is invalid"))
			return
		}
		workflowUnavailable(w, err)
		return
	}
	saved.LastStatus = consoleWorkflowStatus(saved.LastStatus)
	status := http.StatusOK
	if create {
		status = http.StatusCreated
	}
	h.compatWrite(w, r, status, map[string]any{"workflow": saved})
}

func jsonNull(raw json.RawMessage) bool {
	return len(raw) == 0 || strings.TrimSpace(string(raw)) == "null"
}

// jsText mirrors String(value).trim() for the scalar JSON types.
func jsText(raw json.RawMessage) string {
	if jsonNull(raw) {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}
	return strings.TrimSpace(string(raw))
}

func jsBoolean(raw json.RawMessage, name string) (bool, error) {
	var value bool
	if json.Unmarshal(raw, &value) == nil {
		return value, nil
	}
	switch strings.ToLower(jsText(raw)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	}
	return false, httpx.Invalid("%s must be a boolean", name)
}

func jsInteger(raw json.RawMessage, name string, minimum, maximum, fallback int) (int, error) {
	text := jsText(raw)
	if text == "" {
		return fallback, nil
	}
	number, err := strconv.ParseFloat(text, 64)
	if err != nil || math.Trunc(number) != number || number < float64(minimum) || number > float64(maximum) {
		return 0, httpx.Invalid("%s must be an integer between %d and %d", name, minimum, maximum)
	}
	return int(number), nil
}

func workflowInput(body map[string]json.RawMessage, existing *sqlite.WorkflowDefinition, input *sqlite.WorkflowDefinition) error {
	if raw, ok := body["name"]; ok && !jsonNull(raw) {
		input.Name = jsText(raw)
	}
	if input.Name == "" {
		return httpx.Invalid("name is required")
	}
	if len([]rune(input.Name)) > 80 {
		return httpx.Invalid("name is too long")
	}
	if raw, ok := body["steps"]; ok && !jsonNull(raw) {
		var values []any
		if json.Unmarshal(raw, &values) != nil {
			return httpx.Invalid("steps must be an array")
		}
		distinct := map[string]bool{}
		names := make([]string, 0, len(values))
		for _, value := range values {
			name, _ := value.(string)
			name = strings.ToLower(strings.TrimSpace(name))
			if !distinct[name] {
				distinct[name] = true
				names = append(names, name)
			}
		}
		steps := jobs.NormalizeSteps(names)
		if len(steps) != len(distinct) {
			return httpx.Invalid("steps contains an unsupported value")
		}
		input.Steps = steps
	} else if existing == nil {
		return httpx.Invalid("steps must be an array")
	}
	if len(input.Steps) == 0 {
		return httpx.Invalid("steps must not be empty")
	}
	if raw, ok := body["scheduleEnabled"]; ok && !jsonNull(raw) {
		enabled, err := jsBoolean(raw, "scheduleEnabled")
		if err != nil {
			return err
		}
		input.ScheduleEnabled = enabled
	}
	interval, err := jsInteger(body["intervalMinutes"], "intervalMinutes", workflowMinIntervalMinutes, workflowMaxIntervalMinutes, input.IntervalMinutes)
	if err != nil {
		return err
	}
	input.IntervalMinutes = interval
	if raw, ok := body["sourceLookbackDays"]; ok && !jsonNull(raw) {
		var days map[string]json.RawMessage
		if json.Unmarshal(raw, &days) != nil {
			return httpx.Invalid("sourceLookbackDays must contain x and/or youtube")
		}
		lookback := map[string]int{"x": 7, "youtube": 14}
		for source, value := range days {
			if source != "x" && source != "youtube" {
				return httpx.Invalid("sourceLookbackDays must contain x and/or youtube")
			}
			if jsonNull(value) {
				continue
			}
			var number float64
			if json.Unmarshal(value, &number) != nil || math.Trunc(number) != number || number < 1 || number > 365 {
				return httpx.Invalid("days must be an integer from 1 to 365")
			}
			lookback[source] = int(number)
		}
		input.SourceLookbackDays = lookback
	} else if input.SourceLookbackDays == nil {
		input.SourceLookbackDays = map[string]int{"x": 7, "youtube": 14}
	}
	return nil
}

func (h *adminHandler) sourceFetchStatus(r *http.Request) (map[string]any, error) {
	available := func(source string) bool {
		return h.compat.HistoryAvailable != nil && h.compat.HistoryAvailable(source)
	}
	accounts := []map[string]any{}
	for _, account := range h.compat.XHandles {
		oldest, err := h.store.OldestSourceTimestamp(r.Context(), "x", account)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, map[string]any{"account": account, "oldest": oldest})
	}
	oldest, err := h.store.OldestSourceTimestamp(r.Context(), "youtube", "")
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"x":       map[string]any{"historyAvailable": available("x"), "accounts": accounts},
		"youtube": map[string]any{"historyAvailable": available("youtube"), "oldest": oldest},
	}, nil
}

var fetchDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// japanMidnight parses an inclusive Japan calendar date.
func japanMidnight(raw json.RawMessage) (time.Time, error) {
	var value string
	if json.Unmarshal(raw, &value) != nil || !fetchDatePattern.MatchString(value) {
		return time.Time{}, httpx.Invalid("dates must use YYYY-MM-DD")
	}
	parsed, err := time.ParseInLocation("2006-01-02", value, time.FixedZone("JST", 9*3600))
	if err != nil {
		return time.Time{}, httpx.Invalid("date is invalid")
	}
	return parsed, nil
}

// normalizeFetchWindow validates {mode, days} or {mode, startDate, endDate}.
func normalizeFetchWindow(raw json.RawMessage) (map[string]any, error) {
	var window map[string]json.RawMessage
	if jsonNull(raw) || json.Unmarshal(raw, &window) != nil || window == nil {
		return nil, httpx.Invalid("fetch window is required")
	}
	var mode string
	_ = json.Unmarshal(window["mode"], &mode)
	switch mode {
	case "recent", "before":
		var days float64
		if jsonNull(window["days"]) || json.Unmarshal(window["days"], &days) != nil || math.Trunc(days) != days || days < 1 || days > 365 {
			return nil, httpx.Invalid("days must be an integer from 1 to 365")
		}
		return map[string]any{"mode": mode, "days": int(days)}, nil
	case "range":
		start, err := japanMidnight(window["startDate"])
		if err != nil {
			return nil, err
		}
		end, err := japanMidnight(window["endDate"])
		if err != nil {
			return nil, err
		}
		end = end.Add(24 * time.Hour)
		if !end.After(start) || end.Sub(start) > 365*24*time.Hour {
			return nil, httpx.Invalid("date range must cover 1 to 365 days")
		}
		return map[string]any{"mode": mode, "startDate": start.Format("2006-01-02"), "endDate": end.Add(-24 * time.Hour).Format("2006-01-02")}, nil
	}
	return nil, httpx.Invalid("unsupported fetch mode")
}

func (h *adminHandler) compatFetchSource(w http.ResponseWriter, r *http.Request, source string) {
	if source != "x" && source != "youtube" {
		httpx.WriteError(w, httpx.Invalid("unsupported source"))
		return
	}
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	window, err := normalizeFetchWindow(body["window"])
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if window["mode"] != "recent" {
		credential := h.compat.HistoryCredential != nil && h.compat.HistoryCredential(source)
		available := h.compat.HistoryAvailable != nil && h.compat.HistoryAvailable(source)
		switch {
		case !credential && source == "x":
			httpx.WriteError(w, httpx.Invalid("X_API_BEARER_TOKEN is required for history search"))
			return
		case !credential:
			httpx.WriteError(w, httpx.Invalid("YOUTUBE_API_KEY is required for history search"))
			return
		case !available:
			httpx.WriteError(w, httpx.Invalid("history search is not available in this backend yet"))
			return
		}
	}
	result, ok := h.compatStart(w, r, jobs.KindWorkflow, jobs.Options{
		Steps:        []string{source, "media"},
		FetchWindows: map[string]any{source: window},
	})
	if ok {
		httpx.WriteJSON(w, startStatus(result), result)
	}
}
