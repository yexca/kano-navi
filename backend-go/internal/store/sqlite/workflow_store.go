package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const definitionColumns = "id,name,steps_json,source_lookback_json,schedule_enabled,interval_minutes,last_run_at,last_job_id,last_status,next_run_at,created_at,updated_at"

type workflowScanner interface{ Scan(...any) error }

func scanDefinition(row workflowScanner) (WorkflowDefinition, error) {
	var x WorkflowDefinition
	var steps, lookback string
	var enabled int
	var lr, lj, ls, nr sql.NullString
	err := row.Scan(&x.ID, &x.Name, &steps, &lookback, &enabled, &x.IntervalMinutes, &lr, &lj, &ls, &nr, &x.CreatedAt, &x.UpdatedAt)
	if err != nil {
		return x, err
	}
	if json.Unmarshal([]byte(steps), &x.Steps) != nil {
		return x, ErrWorkflowValidation
	}
	x.SourceLookbackDays = map[string]int{"x": 7, "youtube": 14}
	var days map[string]int
	if json.Unmarshal([]byte(lookback), &days) == nil {
		for _, k := range []string{"x", "youtube"} {
			if n, ok := days[k]; ok && n >= 1 && n <= 365 {
				x.SourceLookbackDays[k] = n
			}
		}
	}
	x.ScheduleEnabled = enabled != 0
	x.IntervalMinutes = clampInterval(x.IntervalMinutes)
	x.LastRunAt = nullableWorkflowString(lr)
	x.LastJobID = nullableWorkflowString(lj)
	x.LastStatus = nullableWorkflowString(ls)
	x.NextRunAt = nullableWorkflowString(nr)
	return x, nil
}
func clampInterval(v int) int {
	if v < 15 {
		return 15
	}
	if v > 10080 {
		return 10080
	}
	return v
}
func (s *Store) GetWorkflow(ctx context.Context, id string) (*WorkflowDefinition, error) {
	var n int
	if s == nil || s.db == nil {
		return nil, ErrWorkflowStorage
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='workflows'`).Scan(&n); err != nil {
		return nil, ErrWorkflowStorage
	}
	if n == 0 {
		return nil, ErrWorkflowUnavailable
	}
	x, err := scanDefinition(s.db.QueryRowContext(ctx, "SELECT "+definitionColumns+" FROM workflows WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWorkflowNotFound
	}
	if err != nil {
		return nil, ErrWorkflowStorage
	}
	return &x, nil
}
func (s *Store) ListWorkflows(ctx context.Context, page, size int) (WorkflowPage, error) {
	if s == nil || s.db == nil {
		return WorkflowPage{}, ErrWorkflowStorage
	}
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM workflows").Scan(&total); err != nil {
		return WorkflowPage{}, ErrWorkflowUnavailable
	}
	page, size = boundPage(page, size, 100, 25)
	page = safePage(page, pageCount(total, size))
	rows, err := s.db.QueryContext(ctx, "SELECT "+definitionColumns+" FROM workflows ORDER BY created_at,id LIMIT ? OFFSET ?", size, (page-1)*size)
	if err != nil {
		return WorkflowPage{}, ErrWorkflowStorage
	}
	defer rows.Close()
	out := []WorkflowDefinition{}
	for rows.Next() {
		x, e := scanDefinition(rows)
		if e != nil {
			return WorkflowPage{}, ErrWorkflowStorage
		}
		out = append(out, x)
	}
	if rows.Err() != nil {
		return WorkflowPage{}, ErrWorkflowStorage
	}
	return WorkflowPage{out, page, size, total, pageCount(total, size), page < pageCount(total, size)}, nil
}
func (s *Store) ListDueWorkflows(ctx context.Context, now time.Time) ([]WorkflowDefinition, error) {
	if err := s.requireWorkflow(); err != nil {
		return nil, err
	}
	rows, e := s.db.QueryContext(ctx, "SELECT "+definitionColumns+" FROM workflows WHERE schedule_enabled=1 AND next_run_at IS NOT NULL AND julianday(next_run_at)<=julianday(?) ORDER BY julianday(next_run_at),id LIMIT 1", workflowTime(now))
	if e != nil {
		return nil, ErrWorkflowStorage
	}
	defer rows.Close()
	out := []WorkflowDefinition{}
	for rows.Next() {
		x, e := scanDefinition(rows)
		if e != nil {
			return nil, ErrWorkflowStorage
		}
		out = append(out, x)
	}
	if rows.Err() != nil {
		return nil, ErrWorkflowStorage
	}
	return out, nil
}

const runColumns = "id,workflow_id,status,trigger_source,execution_source,created_at,started_at,finished_at,error_code,cancellation_requested_at,recovery_count"

func scanRun(row workflowScanner) (WorkflowRun, error) {
	var x WorkflowRun
	var sa, fa, ec, ca sql.NullString
	err := row.Scan(&x.ID, &x.WorkflowID, &x.Status, &x.TriggerSource, &x.ExecutionSource, &x.CreatedAt, &sa, &fa, &ec, &ca, &x.RecoveryCount)
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
func (s *Store) GetWorkflowRun(ctx context.Context, id string) (*WorkflowRun, error) {
	if err := s.requireWorkflow(); err != nil {
		return nil, err
	}
	x, e := scanRun(s.db.QueryRowContext(ctx, "SELECT "+runColumns+" FROM workflow_runs WHERE id=?", id))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrWorkflowNotFound
	}
	if e != nil {
		return nil, ErrWorkflowStorage
	}
	capabilityColumn := "capability"
	if s.workflowVersion >= 2 {
		var lookback string
		if s.db.QueryRowContext(ctx, `SELECT source_lookback_json FROM workflow_runs WHERE id=?`, id).Scan(&lookback) != nil {
			return nil, ErrWorkflowStorage
		}
		if json.Unmarshal([]byte(lookback), &x.SourceLookbackDays) != nil {
			return nil, ErrWorkflowStorage
		}
		capabilityColumn = "COALESCE(adapter_capability,capability)"
	}
	if s.workflowVersion >= 3 {
		var windows sql.NullString
		if s.db.QueryRowContext(ctx, `SELECT fetch_windows_json FROM workflow_runs WHERE id=?`, id).Scan(&windows) != nil {
			return nil, ErrWorkflowStorage
		}
		if windows.Valid && windows.String != "" && json.Unmarshal([]byte(windows.String), &x.FetchWindows) != nil {
			return nil, ErrWorkflowStorage
		}
	}
	rows, e := s.db.QueryContext(ctx, `SELECT id,step_id,step_order,status,`+capabilityColumn+`,started_at,finished_at,error_code,retryable FROM workflow_run_steps WHERE run_id=? ORDER BY step_order LIMIT 16`, id)
	if e != nil {
		return nil, ErrWorkflowStorage
	}
	x.Steps = []WorkflowStep{}
	for rows.Next() {
		var st WorkflowStep
		var sa, fa, ec sql.NullString
		e = rows.Scan(&st.ID, &st.StepID, &st.StepOrder, &st.Status, &st.Capability, &sa, &fa, &ec, &st.Retryable)
		if e != nil {
			rows.Close()
			return nil, ErrWorkflowStorage
		}
		st.Status = workflowStatus(st.Status, true)
		if st.Capability != "synthetic" && st.Capability != "x_recent" && st.Capability != "youtube_recent" && st.Capability != "youtube_live" && st.Capability != "media_download" && st.Capability != "schedule_extract" && st.Capability != "event_reconcile" {
			st.Capability = "capability_not_migrated"
		}
		st.StartedAt = nullableWorkflowString(sa)
		st.FinishedAt = nullableWorkflowString(fa)
		st.ErrorCode = workflowCode(ec)
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
