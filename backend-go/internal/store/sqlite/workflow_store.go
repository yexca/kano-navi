package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

type workflowScanner interface{ Scan(...any) error }

const runColumns = "id,workflow_id,status,trigger_source,execution_source,created_at,started_at,finished_at,error_code,cancellation_requested_at,recovery_count"

// maxRunSteps bounds the steps a run detail reads.
const maxRunSteps = 16

// scanRun reads runColumns, then any extra columns selected after them.
func scanRun(row workflowScanner, extra ...any) (WorkflowRun, error) {
	var x WorkflowRun
	var sa, fa, ec, ca sql.NullString
	err := row.Scan(append([]any{&x.ID, &x.WorkflowID, &x.Status, &x.TriggerSource, &x.ExecutionSource, &x.CreatedAt, &sa, &fa, &ec, &ca, &x.RecoveryCount}, extra...)...)
	x.Status = workflowStatus(x.Status, false)
	x.TriggerSource = workflowTrigger(x.TriggerSource)
	x.ExecutionSource = workflowTrigger(x.ExecutionSource)
	x.StartedAt = nullableWorkflowString(sa)
	x.FinishedAt = nullableWorkflowString(fa)
	x.ErrorCode = workflowCode(ec)
	x.CancellationRequestedAt = nullableWorkflowString(ca)
	return x, err
}
func (s *Store) ListWorkflowRuns(ctx context.Context, page, size int, status, workflowID string) (WorkflowRunPage, error) {
	if err := s.requireWorkflow(); err != nil {
		return WorkflowRunPage{}, err
	}
	page, size = boundPage(page, size, 100, 25)
	where := []string{"1=1"}
	args := []any{}
	if status != "" {
		if workflowStatus(status, false) != status {
			return WorkflowRunPage{}, ErrWorkflowValidation
		}
		where = append(where, "status=?")
		args = append(args, status)
	}
	if workflowID != "" {
		where = append(where, "workflow_id=?")
		args = append(args, workflowID)
	}
	w := strings.Join(where, " AND ")
	var total int
	if s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM workflow_runs WHERE "+w, args...).Scan(&total) != nil {
		return WorkflowRunPage{}, ErrWorkflowStorage
	}
	page = safePage(page, pageCount(total, size))
	rows, e := s.db.QueryContext(ctx, "SELECT "+runColumns+" FROM workflow_runs WHERE "+w+" ORDER BY julianday(created_at) DESC,id DESC LIMIT ? OFFSET ?", append(args, size, (page-1)*size)...)
	if e != nil {
		return WorkflowRunPage{}, ErrWorkflowStorage
	}
	out := []WorkflowRun{}
	for rows.Next() {
		x, e := scanRun(rows)
		if e != nil {
			rows.Close()
			return WorkflowRunPage{}, ErrWorkflowStorage
		}
		out = append(out, x)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return WorkflowRunPage{}, ErrWorkflowStorage
	}
	// Lists contain headers; details contain bounded steps and attempts.
	return WorkflowRunPage{out, page, size, total, pageCount(total, size), page < pageCount(total, size)}, nil
}

// runConfigColumns selects the lookback days, windows and sync frozen on a
// run, as NULL where a read-only snapshot lacks the column.
func (s *Store) runConfigColumns() string {
	columns := []string{"NULL", "NULL", "NULL"}
	if s.workflowVersion >= 2 {
		columns[0] = "source_lookback_json"
	}
	if s.workflowVersion >= 3 {
		columns[1] = "fetch_windows_json"
	}
	if s.sourceSync {
		columns[2] = "source_sync_json"
	}
	return strings.Join(columns, ",")
}

// decodeRunConfig fills what runConfigColumns read.
func (s *Store) decodeRunConfig(x *WorkflowRun, lookback, windows, sync sql.NullString) error {
	if s.workflowVersion >= 2 && (!lookback.Valid || json.Unmarshal([]byte(lookback.String), &x.SourceLookbackDays) != nil) {
		return ErrWorkflowStorage
	}
	if s.workflowVersion >= 3 && windows.Valid && windows.String != "" && json.Unmarshal([]byte(windows.String), &x.FetchWindows) != nil {
		return ErrWorkflowStorage
	}
	if s.sourceSync {
		// A run keeps only what its definition froze; ad hoc runs and runs
		// made before the column carry none and use each step's defaults.
		var stored map[string]SourceSync
		if !sync.Valid || json.Unmarshal([]byte(sync.String), &stored) != nil {
			return ErrWorkflowStorage
		}
		for source, setting := range stored {
			if setting.Valid() {
				if x.SourceSync == nil {
					x.SourceSync = map[string]SourceSync{}
				}
				x.SourceSync[source] = setting
			}
		}
	}
	return nil
}

func (s *Store) stepColumns() string {
	capabilityColumn := "capability"
	if s.workflowVersion >= 2 {
		capabilityColumn = "COALESCE(adapter_capability,capability)"
	}
	return "id,step_id,step_order,status," + capabilityColumn + ",started_at,finished_at,error_code,retryable"
}

// scanStep reads stepColumns, then any extra columns selected after them.
func scanStep(row workflowScanner, extra ...any) (WorkflowStep, error) {
	var st WorkflowStep
	var sa, fa, ec sql.NullString
	if err := row.Scan(append([]any{&st.ID, &st.StepID, &st.StepOrder, &st.Status, &st.Capability, &sa, &fa, &ec, &st.Retryable}, extra...)...); err != nil {
		return st, err
	}
	st.Status = workflowStatus(st.Status, true)
	if st.Capability != "synthetic" && st.Capability != "x_recent" && st.Capability != "youtube_recent" && st.Capability != "bilibili_recent" && st.Capability != "youtube_live" && st.Capability != "media_download" && st.Capability != "schedule_extract" && st.Capability != "text_translate" && st.Capability != "event_reconcile" {
		st.Capability = "capability_not_migrated"
	}
	st.StartedAt = nullableWorkflowString(sa)
	st.FinishedAt = nullableWorkflowString(fa)
	st.ErrorCode = workflowCode(ec)
	return st, nil
}

func (s *Store) GetWorkflowRun(ctx context.Context, id string) (*WorkflowRun, error) {
	if err := s.requireWorkflow(); err != nil {
		return nil, err
	}
	var lookback, windows, sync sql.NullString
	x, e := scanRun(s.db.QueryRowContext(ctx, "SELECT "+runColumns+","+s.runConfigColumns()+" FROM workflow_runs WHERE id=?", id), &lookback, &windows, &sync)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrWorkflowNotFound
	}
	if e != nil {
		return nil, ErrWorkflowStorage
	}
	if e = s.decodeRunConfig(&x, lookback, windows, sync); e != nil {
		return nil, e
	}
	rows, e := s.db.QueryContext(ctx, `SELECT `+s.stepColumns()+` FROM workflow_run_steps WHERE run_id=? ORDER BY step_order LIMIT ?`, id, maxRunSteps)
	if e != nil {
		return nil, ErrWorkflowStorage
	}
	x.Steps = []WorkflowStep{}
	for rows.Next() {
		st, e := scanStep(rows)
		if e != nil {
			rows.Close()
			return nil, ErrWorkflowStorage
		}
		x.Steps = append(x.Steps, st)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, ErrWorkflowStorage
	}
	for i := range x.Steps {
		st := &x.Steps[i]
		st.Attempts = []WorkflowAttempt{}
		rows, e = s.db.QueryContext(ctx, `SELECT attempt_number,status,error_code,retryable,started_at,finished_at,duration_ms FROM workflow_step_attempts WHERE step_id=? ORDER BY attempt_number DESC LIMIT 100`, st.ID)
		if e != nil {
			return nil, ErrWorkflowStorage
		}
		for rows.Next() {
			var a WorkflowAttempt
			var ec, fa sql.NullString
			var duration sql.NullInt64
			e = rows.Scan(&a.AttemptNumber, &a.Status, &ec, &a.Retryable, &a.StartedAt, &fa, &duration)
			if e != nil {
				rows.Close()
				return nil, ErrWorkflowStorage
			}
			a.Status = workflowStatus(a.Status, true)
			a.ErrorCode = workflowCode(ec)
			a.FinishedAt = nullableWorkflowString(fa)
			a.DurationMS = nullInt(duration)
			st.Attempts = append(st.Attempts, a)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, ErrWorkflowStorage
		}
		for left, right := 0, len(st.Attempts)-1; left < right; left, right = left+1, right-1 {
			st.Attempts[left], st.Attempts[right] = st.Attempts[right], st.Attempts[left]
		}
	}
	return &x, nil
}

// WorkflowRunSummaries reads a page of runs as GetWorkflowRun does, without
// step attempts, in two queries however many runs there are. An ID without a
// run is absent from the result.
func (s *Store) WorkflowRunSummaries(ctx context.Context, ids []string) (map[string]*WorkflowRun, error) {
	if err := s.requireWorkflow(); err != nil {
		return nil, err
	}
	out := make(map[string]*WorkflowRun, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	in := placeholders(len(ids))
	rows, e := s.db.QueryContext(ctx, "SELECT "+runColumns+","+s.runConfigColumns()+" FROM workflow_runs WHERE id IN ("+in+")", args...)
	if e != nil {
		return nil, ErrWorkflowStorage
	}
	for rows.Next() {
		var lookback, windows, sync sql.NullString
		x, e := scanRun(rows, &lookback, &windows, &sync)
		if e == nil {
			e = s.decodeRunConfig(&x, lookback, windows, sync)
		}
		if e != nil {
			rows.Close()
			return nil, ErrWorkflowStorage
		}
		x.Steps = []WorkflowStep{}
		out[x.ID] = &x
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, ErrWorkflowStorage
	}
	rows, e = s.db.QueryContext(ctx, "SELECT "+s.stepColumns()+",run_id FROM workflow_run_steps WHERE run_id IN ("+in+") ORDER BY run_id,step_order", args...)
	if e != nil {
		return nil, ErrWorkflowStorage
	}
	for rows.Next() {
		var runID string
		st, e := scanStep(rows, &runID)
		if e != nil {
			rows.Close()
			return nil, ErrWorkflowStorage
		}
		if x := out[runID]; x != nil && len(x.Steps) < maxRunSteps {
			x.Steps = append(x.Steps, st)
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, ErrWorkflowStorage
	}
	return out, nil
}
