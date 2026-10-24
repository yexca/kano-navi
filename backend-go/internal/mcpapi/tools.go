package mcpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/yexca/kano-navi/backend-go/internal/jobs"
	"github.com/yexca/kano-navi/backend-go/internal/publicview"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// controlTools need the control bearer token. They are also absent from the
// tool registry of a request that did not present it.
var controlTools = map[string]bool{
	"dashboard_request_reload": true,
	"sync_start":               true,
	"scan_run_automatic":       true,
}

type argumentKind int

const (
	integerArgument argumentKind = iota
	stringArgument
	enumArgument
)

// argument describes one optional tool input and its bounds. Unknown inputs
// are ignored rather than rejected.
type argument struct {
	name      string
	kind      argumentKind
	hasMin    bool
	min       float64
	hasMax    bool
	max       float64
	maxLength int
	pattern   *regexp.Regexp
	options   []string
}

type toolHandler func(h *handler, ctx context.Context, arguments map[string]any) (any, error)

type tool struct {
	name        string
	title       string
	description string
	inputSchema string
	annotations string
	arguments   []argument
	run         toolHandler
}

var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// jsonSchemaDialect is the dialect identifier every listed input schema has
// always declared. It is an identifier only; nothing ever fetches it.
const jsonSchemaDialect = "http://json-schema.org/draft-07/schema#"

const (
	emptyInputSchema   = `{"$schema":"` + jsonSchemaDialect + `","type":"object","properties":{}}`
	readOnlyAnnotation = `{"readOnlyHint":true,"openWorldHint":false}`
)

func boundedInteger(name string, minimum, maximum float64) argument {
	return argument{name: name, kind: integerArgument, hasMin: true, min: minimum, hasMax: maximum > 0, max: maximum}
}

// tools lists the public tools first, then the control tools, in the order
// clients have always seen them.
var tools = []tool{
	{
		name:        "dashboard_read",
		title:       "Read dashboard",
		description: "Read the public dashboard snapshot without triggering sync.",
		inputSchema: `{"type":"object","properties":{"days":{"type":"integer","minimum":1,"maximum":30}},"additionalProperties":false,"$schema":"` + jsonSchemaDialect + `"}`,
		annotations: readOnlyAnnotation,
		arguments:   []argument{boundedInteger("days", 1, 30)},
		run:         (*handler).dashboardRead,
	},
	{
		name:        "schedule_list",
		title:       "List schedules",
		description: "List schedule records with read-only filters and pagination.",
		inputSchema: `{"type":"object","properties":{"page":{"type":"integer","minimum":1},"pageSize":{"type":"integer","minimum":1,"maximum":100},"search":{"type":"string","maxLength":120},"provenance":{"type":"string","enum":["automatic","manual"]},"source":{"type":"string","maxLength":40},"from":{"type":"string","pattern":"^\\d{4}-\\d{2}-\\d{2}$"},"to":{"type":"string","pattern":"^\\d{4}-\\d{2}-\\d{2}$"}},"additionalProperties":false,"$schema":"` + jsonSchemaDialect + `"}`,
		annotations: readOnlyAnnotation,
		arguments: []argument{
			boundedInteger("page", 1, 0),
			boundedInteger("pageSize", 1, 100),
			{name: "search", kind: stringArgument, maxLength: 120},
			{name: "provenance", kind: enumArgument, options: []string{"automatic", "manual"}},
			{name: "source", kind: stringArgument, maxLength: 40},
			{name: "from", kind: stringArgument, pattern: datePattern},
			{name: "to", kind: stringArgument, pattern: datePattern},
		},
		run: (*handler).scheduleList,
	},
	{
		name:        "posts_list",
		title:       "List posts",
		description: "Read recent public posts from the stored snapshot.",
		inputSchema: `{"type":"object","properties":{"days":{"type":"integer","minimum":1,"maximum":30},"limit":{"type":"integer","minimum":1,"maximum":100}},"additionalProperties":false,"$schema":"` + jsonSchemaDialect + `"}`,
		annotations: readOnlyAnnotation,
		arguments:   []argument{boundedInteger("days", 1, 30), boundedInteger("limit", 1, 100)},
		run:         (*handler).postsList,
	},
	{
		name:        "health_read",
		title:       "Read health",
		description: "Read service health and the latest synchronization summary.",
		inputSchema: emptyInputSchema,
		annotations: readOnlyAnnotation,
		run:         (*handler).healthRead,
	},
	{
		// Status is a read-only observation, so it stays available to public
		// clients. The mutation controls below still require the bearer token.
		name:        "sync_status",
		title:       "Read synchronization status",
		description: "Read the current synchronization job status without starting a job.",
		inputSchema: `{"type":"object","properties":{"jobId":{"type":"string","maxLength":100},"limit":{"type":"integer","minimum":1,"maximum":100}},"additionalProperties":false,"$schema":"` + jsonSchemaDialect + `"}`,
		annotations: readOnlyAnnotation,
		arguments: []argument{
			{name: "jobId", kind: stringArgument, maxLength: 100},
			boundedInteger("limit", 1, 100),
		},
		run: (*handler).syncStatus,
	},
	{
		name:        "dashboard_request_reload",
		title:       "Request dashboard reload",
		description: "Advance the public dashboard revision so connected pages reload their stored data. This does not fetch external sources.",
		inputSchema: emptyInputSchema,
		annotations: `{"destructiveHint":false,"openWorldHint":false}`,
		run:         (*handler).dashboardRequestReload,
	},
	{
		name:        "sync_start",
		title:       "Start synchronization",
		description: "Start one asynchronous server-side synchronization job. Existing snapshots are retained when a source fails.",
		inputSchema: emptyInputSchema,
		annotations: `{"destructiveHint":false,"openWorldHint":true}`,
		run: func(h *handler, ctx context.Context, _ map[string]any) (any, error) {
			return h.startJob(ctx, jobs.KindSync)
		},
	},
	{
		name:        "scan_run_automatic",
		title:       "Run automatic schedule scan",
		description: "Run keyword and visual schedule extraction asynchronously. It never confirms, edits, deletes, or selects human-managed records.",
		inputSchema: emptyInputSchema,
		annotations: `{"destructiveHint":false,"openWorldHint":true}`,
		run: func(h *handler, ctx context.Context, _ map[string]any) (any, error) {
			return h.startJob(ctx, jobs.KindScan)
		},
	},
}

type listedTool struct {
	Name        string          `json:"name"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations json.RawMessage `json:"annotations"`
	Execution   toolExecution   `json:"execution"`
}

type toolExecution struct {
	TaskSupport string `json:"taskSupport"`
}

type toolList struct {
	Tools []listedTool `json:"tools"`
}

func available(candidate tool, controlAuthorized bool) bool {
	return controlAuthorized || !controlTools[candidate.name]
}

func listedTools(controlAuthorized bool) []listedTool {
	listed := make([]listedTool, 0, len(tools))
	for _, candidate := range tools {
		if !available(candidate, controlAuthorized) {
			continue
		}
		listed = append(listed, listedTool{
			Name:        candidate.name,
			Title:       candidate.title,
			Description: candidate.description,
			InputSchema: json.RawMessage(candidate.inputSchema),
			Annotations: json.RawMessage(candidate.annotations),
			Execution:   toolExecution{TaskSupport: "forbidden"},
		})
	}
	return listed
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolResult struct {
	Content           []textContent   `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError,omitempty"`
}

func toolError(text string) toolResult {
	return toolResult{Content: []textContent{{Type: "text", Text: text}}, IsError: true}
}

// toolFailure is the only kind of failure text a tool may report. Store,
// queue and encoder errors are never serialized.
type toolFailure string

func (f toolFailure) Error() string { return string(f) }

const (
	failureSnapshot  toolFailure = "Snapshot unavailable"
	failureReload    toolFailure = "Dashboard reload unavailable"
	failureJobs      toolFailure = "Job manager unavailable"
	failureJobStart  toolFailure = "Job could not be started"
	failureJobStatus toolFailure = "Job status unavailable"
)

// callTool runs one tool. Unknown tools, invalid arguments and execution
// failures are reported as isError results rather than protocol errors.
func (h *handler) callTool(ctx context.Context, name string, arguments map[string]any, controlAuthorized bool) toolResult {
	var selected *tool
	for index := range tools {
		if tools[index].name == name && available(tools[index], controlAuthorized) {
			selected = &tools[index]
			break
		}
	}
	if selected == nil {
		return toolError("MCP error -32602: Tool " + name + " not found")
	}
	if issues := validateArguments(selected.arguments, arguments); len(issues) > 0 {
		return toolError("MCP error -32602: Input validation error: Invalid arguments for tool " + name + ": " + strings.Join(issues, "\n"))
	}
	value, err := selected.run(h, ctx, arguments)
	if err != nil {
		var failure toolFailure
		if errors.As(err, &failure) {
			return toolError(string(failure))
		}
		return toolError(string(failureSnapshot))
	}
	encoded, err := encodeJSON(value)
	if err != nil {
		return toolError(string(failureSnapshot))
	}
	return toolResult{
		Content:           []textContent{{Type: "text", Text: string(encoded)}},
		StructuredContent: encoded,
	}
}

func receivedType(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case json.Number:
		return "number"
	case bool:
		return "boolean"
	case []any:
		return "array"
	default:
		return "object"
	}
}

func formatBound(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func numberValue(number json.Number) float64 {
	// Out-of-range literals parse to an infinity, which then fails the
	// integer and bound checks like any other non-integer.
	value, _ := strconv.ParseFloat(number.String(), 64)
	return value
}

// validateArguments returns one line per violated constraint, in declaration
// order, phrased like the validation messages clients already receive.
func validateArguments(declared []argument, arguments map[string]any) []string {
	var issues []string
	report := func(name, text string) { issues = append(issues, text+" at "+name) }
	for _, spec := range declared {
		value, present := arguments[spec.name]
		if !present {
			continue
		}
		switch spec.kind {
		case integerArgument:
			number, ok := value.(json.Number)
			if !ok {
				report(spec.name, "Expected number, received "+receivedType(value))
				continue
			}
			parsed := numberValue(number)
			if math.IsInf(parsed, 0) || parsed != math.Trunc(parsed) {
				report(spec.name, "Expected integer, received float")
			}
			if spec.hasMin && parsed < spec.min {
				report(spec.name, "Number must be greater than or equal to "+formatBound(spec.min))
			}
			if spec.hasMax && parsed > spec.max {
				report(spec.name, "Number must be less than or equal to "+formatBound(spec.max))
			}
		case stringArgument:
			text, ok := value.(string)
			if !ok {
				report(spec.name, "Expected string, received "+receivedType(value))
				continue
			}
			if spec.maxLength > 0 && len(utf16.Encode([]rune(text))) > spec.maxLength {
				report(spec.name, "String must contain at most "+strconv.Itoa(spec.maxLength)+" character(s)")
			}
			if spec.pattern != nil && !spec.pattern.MatchString(text) {
				report(spec.name, "Invalid")
			}
		case enumArgument:
			expected := "'" + strings.Join(spec.options, "' | '") + "'"
			text, ok := value.(string)
			if !ok {
				report(spec.name, "Expected "+expected+", received "+receivedType(value))
				continue
			}
			known := false
			for _, option := range spec.options {
				known = known || option == text
			}
			if !known {
				report(spec.name, "Invalid enum value. Expected "+expected+", received '"+text+"'")
			}
		}
	}
	return issues
}

// integerArgumentValue reads a validated integer input, or the fallback when
// it is absent.
func integerArgumentValue(arguments map[string]any, name string, fallback int) int {
	number, ok := arguments[name].(json.Number)
	if !ok {
		return fallback
	}
	value := numberValue(number)
	if value > math.MaxInt32 {
		return math.MaxInt32
	}
	if value == 0 {
		return fallback
	}
	return int(value)
}

func stringArgumentValue(arguments map[string]any, name string) string {
	text, _ := arguments[name].(string)
	return text
}

// publicDashboard is the dashboard projection without the visitor-only event
// markers (manualLocked, isUpcoming) that the HTTP board adds.
func (h *handler) publicDashboard(ctx context.Context, days int) (map[string]any, *sqlite.Dashboard, error) {
	if h.store == nil {
		return nil, nil, failureSnapshot
	}
	dashboard, err := h.store.GetDashboard(ctx, days, h.now())
	if err != nil || dashboard == nil {
		return nil, nil, failureSnapshot
	}
	projection := publicview.Dashboard(dashboard)
	events := make([]any, 0, len(dashboard.Events))
	for _, event := range dashboard.Events {
		events = append(events, publicview.PublicEvent(event, false))
	}
	projection["events"] = events
	if summary, ok := projection["summary"].(map[string]any); ok {
		if dashboard.Summary.NextEvent != nil {
			summary["nextEvent"] = publicview.PublicEvent(*dashboard.Summary.NextEvent, false)
		} else {
			summary["nextEvent"] = nil
		}
	}
	return projection, dashboard, nil
}

func (h *handler) dashboardRead(ctx context.Context, arguments map[string]any) (any, error) {
	projection, _, err := h.publicDashboard(ctx, integerArgumentValue(arguments, "days", 3))
	if err != nil {
		return nil, err
	}
	return projection, nil
}

type postsResult struct {
	Posts       []any  `json:"posts"`
	Days        int    `json:"days"`
	GeneratedAt string `json:"generatedAt"`
}

func (h *handler) postsList(ctx context.Context, arguments map[string]any) (any, error) {
	projection, dashboard, err := h.publicDashboard(ctx, integerArgumentValue(arguments, "days", 3))
	if err != nil {
		return nil, err
	}
	posts, _ := projection["posts"].([]any)
	if posts == nil {
		posts = []any{}
	}
	if limit := integerArgumentValue(arguments, "limit", 50); len(posts) > limit {
		posts = posts[:limit]
	}
	return postsResult{Posts: posts, Days: dashboard.Meta.PostWindowDays, GeneratedAt: dashboard.Meta.GeneratedAt}, nil
}

// publicEvent keeps only the event facts a visitor needs. Deleted tombstones,
// source identities, extraction IDs, confidence and lock flags are operator
// metadata and never leave through MCP.
type publicEvent struct {
	ID                   string  `json:"id"`
	Source               string  `json:"source"`
	Title                string  `json:"title"`
	Detail               *string `json:"detail"`
	StartsOn             string  `json:"startsOn"`
	StartsAt             *string `json:"startsAt"`
	EndsAt               *string `json:"endsAt"`
	Timezone             string  `json:"timezone"`
	TimePrecision        string  `json:"timePrecision"`
	Status               *string `json:"status"`
	EventType            *string `json:"eventType"`
	URL                  *string `json:"url"`
	Provenance           string  `json:"provenance"`
	CancellationStatus   string  `json:"cancellationStatus"`
	CancellationSource   *string `json:"cancellationSource"`
	CancellationReason   *string `json:"cancellationReason"`
	CancellationEvidence *string `json:"cancellationEvidence"`
	CancellationAt       *string `json:"cancellationAt"`
}

type schedulePage struct {
	Items       []publicEvent `json:"items"`
	Events      []publicEvent `json:"events"`
	Page        int           `json:"page"`
	PageSize    int           `json:"pageSize"`
	Total       int           `json:"total"`
	TotalPages  int           `json:"totalPages"`
	HasPrevious bool          `json:"hasPrevious"`
	HasNext     bool          `json:"hasNext"`
}

func (h *handler) scheduleList(ctx context.Context, arguments map[string]any) (any, error) {
	if h.store == nil {
		return nil, failureSnapshot
	}
	// Newest first, never including deleted tombstones or merged duplicates.
	page, err := h.store.ListAdminEvents(ctx, sqlite.AdminEventFilter{
		Page:       integerArgumentValue(arguments, "page", 1),
		PageSize:   integerArgumentValue(arguments, "pageSize", 25),
		Search:     stringArgumentValue(arguments, "search"),
		Provenance: stringArgumentValue(arguments, "provenance"),
		Source:     stringArgumentValue(arguments, "source"),
		From:       stringArgumentValue(arguments, "from"),
		To:         stringArgumentValue(arguments, "to"),
		Merge:      "visible",
	})
	if err != nil {
		return nil, failureSnapshot
	}
	items := make([]publicEvent, 0, len(page.Items))
	for _, event := range page.Items {
		items = append(items, publicEvent{
			ID: event.ID, Source: event.Source, Title: event.Title, Detail: event.Detail,
			StartsOn: event.StartsOn, StartsAt: event.StartsAt, EndsAt: event.EndsAt,
			Timezone: event.Timezone, TimePrecision: event.TimePrecision, Status: event.Status,
			EventType: event.EventType, URL: event.URL, Provenance: event.Provenance,
			CancellationStatus: event.CancellationStatus, CancellationSource: event.CancellationSource,
			CancellationReason: event.CancellationReason, CancellationEvidence: event.CancellationEvidence,
			CancellationAt: event.CancellationAt,
		})
	}
	return schedulePage{
		Items: items, Events: items, Page: page.Page, PageSize: page.PageSize, Total: page.Total,
		TotalPages: page.TotalPages, HasPrevious: page.Page > 1, HasNext: page.Page < page.TotalPages,
	}, nil
}

type healthResult struct {
	OK       bool                              `json:"ok"`
	Service  string                            `json:"service"`
	Now      string                            `json:"now"`
	Revision int64                             `json:"revision"`
	LastSync *publicview.PublicSyncRunResponse `json:"lastSync"`
}

func (h *handler) healthRead(ctx context.Context, _ map[string]any) (any, error) {
	if h.store == nil {
		return nil, failureSnapshot
	}
	revision, err := h.store.GetDashboardRevision(ctx)
	if err != nil {
		return nil, failureSnapshot
	}
	run, err := h.store.GetLatestSync(ctx)
	if err != nil {
		return nil, failureSnapshot
	}
	health := publicview.Health(h.now(), run)
	return healthResult{OK: health.OK, Service: health.Service, Now: health.Now, Revision: revision, LastSync: health.LastSync}, nil
}

// publicJob is the whole public view of a job or a stored run: no trigger,
// counters, messages, errors or results.
type publicJob struct {
	ID         any     `json:"id"`
	Kind       string  `json:"kind"`
	Status     string  `json:"status"`
	CreatedAt  *string `json:"createdAt,omitempty"`
	StartedAt  *string `json:"startedAt"`
	FinishedAt *string `json:"finishedAt"`
}

func publicManagedJob(job jobs.Job) publicJob {
	kind := job.Kind
	if kind == "" {
		kind = jobs.KindSync
	}
	createdAt := job.CreatedAt
	return publicJob{ID: job.ID, Kind: kind, Status: job.Status, CreatedAt: &createdAt, StartedAt: job.StartedAt, FinishedAt: job.FinishedAt}
}

func publicRunJob(id int64, source, status, startedAt string, finishedAt *string) publicJob {
	kind := jobs.KindSync
	if source == "schedule" {
		kind = jobs.KindScan
	}
	return publicJob{ID: id, Kind: kind, Status: status, StartedAt: &startedAt, FinishedAt: finishedAt}
}

type jobResult struct {
	Job *publicJob `json:"job"`
}

type jobsResult struct {
	Jobs []publicJob `json:"jobs"`
}

var runIDPattern = regexp.MustCompile(`^[0-9]+$`)

// storedRun finds a run by numeric id or by job id, newest first.
func (h *handler) storedRun(ctx context.Context, idOrJobID string) (*publicJob, error) {
	value := strings.TrimSpace(idOrJobID)
	if value == "" || h.store == nil || h.store.DB() == nil {
		return nil, nil
	}
	numericID := int64(-1)
	if runIDPattern.MatchString(value) {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			numericID = parsed
		}
	}
	var (
		id         int64
		source     string
		startedAt  string
		finishedAt sql.NullString
		status     string
	)
	// QueryRowContext releases the single pooled connection after Scan.
	err := h.store.DB().QueryRowContext(ctx, `
		SELECT id, source, started_at, finished_at, status
		FROM sync_runs
		WHERE id = ? OR job_id = ?
		ORDER BY id DESC LIMIT 1`, numericID, value).Scan(&id, &source, &startedAt, &finishedAt, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, failureJobStatus
	}
	var finished *string
	if finishedAt.Valid {
		finished = &finishedAt.String
	}
	job := publicRunJob(id, source, status, startedAt, finished)
	return &job, nil
}

func (h *handler) syncStatus(ctx context.Context, arguments map[string]any) (any, error) {
	if jobID := stringArgumentValue(arguments, "jobId"); jobID != "" {
		if h.jobs != nil {
			job, err := h.jobs.Get(ctx, jobID)
			if err != nil {
				return nil, failureJobStatus
			}
			if job != nil {
				projected := publicManagedJob(*job)
				return jobResult{Job: &projected}, nil
			}
		}
		stored, err := h.storedRun(ctx, jobID)
		if err != nil {
			return nil, err
		}
		return jobResult{Job: stored}, nil
	}

	limit := integerArgumentValue(arguments, "limit", 20)
	if h.jobs != nil {
		listed, err := h.jobs.List(ctx, limit)
		if err != nil {
			return nil, failureJobStatus
		}
		if len(listed) > 0 {
			projected := make([]publicJob, 0, len(listed))
			for _, job := range listed {
				projected = append(projected, publicManagedJob(job))
			}
			return jobsResult{Jobs: projected}, nil
		}
	}
	if h.store == nil {
		return nil, failureJobStatus
	}
	runs, err := h.store.ListAdminRunsPage(ctx, 1, limit, "", "")
	if err != nil {
		return nil, failureJobStatus
	}
	projected := make([]publicJob, 0, len(runs.Runs))
	for _, run := range runs.Runs {
		projected = append(projected, publicRunJob(run.ID, run.Source, run.Status, run.StartedAt, run.FinishedAt))
	}
	return jobsResult{Jobs: projected}, nil
}

type reloadResult struct {
	Revision int64 `json:"revision"`
}

func (h *handler) dashboardRequestReload(ctx context.Context, _ map[string]any) (any, error) {
	if h.store == nil {
		return nil, failureReload
	}
	if err := h.store.BumpDashboardRevision(ctx); err != nil {
		return nil, failureReload
	}
	revision, err := h.store.GetDashboardRevision(ctx)
	if err != nil {
		return nil, failureReload
	}
	return reloadResult{Revision: revision}, nil
}

// controlJob keeps the member names of a started job. The diagnostic members
// are always null: raw errors, per-source counters and run rows stay behind
// the admin boundary even for a holder of the control token.
type controlJob struct {
	ID           string            `json:"id"`
	Kind         string            `json:"kind"`
	Status       string            `json:"status"`
	CreatedAt    string            `json:"createdAt"`
	StartedAt    *string           `json:"startedAt"`
	FinishedAt   *string           `json:"finishedAt"`
	TriggeredBy  string            `json:"triggeredBy"`
	WorkflowID   *string           `json:"workflowId"`
	WorkflowName *string           `json:"workflowName"`
	Steps        []string          `json:"steps"`
	FetchWindows map[string]any    `json:"fetchWindows"`
	Progress     map[string]string `json:"progress"`
	Error        *string           `json:"error"`
	Result       map[string]any    `json:"result"`
	Run          map[string]any    `json:"run"`
}

type startResult struct {
	Accepted bool        `json:"accepted"`
	Reason   *string     `json:"reason"`
	Job      *controlJob `json:"job"`
}

// startJob reserves the single-flight slot for a sync or scan. A busy queue is
// a normal result with accepted=false and reason="already_running".
func (h *handler) startJob(ctx context.Context, kind string) (any, error) {
	if h.jobs == nil {
		return nil, failureJobs
	}
	// The job outlives this request; only the reservation belongs to it.
	started, err := h.jobs.Start(context.WithoutCancel(ctx), kind, "mcp", jobs.Options{})
	if err != nil {
		return nil, failureJobStart
	}
	result := startResult{Accepted: started.Accepted, Reason: started.Reason}
	if job := started.Job; job != nil {
		result.Job = &controlJob{
			ID: job.ID, Kind: job.Kind, Status: job.Status, CreatedAt: job.CreatedAt,
			StartedAt: job.StartedAt, FinishedAt: job.FinishedAt, TriggeredBy: job.TriggeredBy,
			WorkflowID: job.WorkflowID, WorkflowName: job.WorkflowName, Steps: job.Steps,
			FetchWindows: job.FetchWindows, Progress: job.Progress,
		}
	}
	return result, nil
}
