package sqlite

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	driver "modernc.org/sqlite"
)

func workflowSQLError(err error) error {
	if err == nil {
		return nil
	}
	var se *driver.Error
	if errors.As(err, &se) {
		switch se.Code() & 255 {
		case 5, 6, 19:
			return ErrWorkflowBusy
		}
	}
	return ErrWorkflowStorage
}
func workflowKey(v string) any {
	if v == "" {
		return nil
	}
	h := sha256.Sum256([]byte(v))
	return hex.EncodeToString(h[:])
}
func newWorkflowRunID() (string, error) {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", ErrWorkflowStorage
	}
	return hex.EncodeToString(b), nil
}
func (s *Store) CreateWorkflowRun(ctx context.Context, definition WorkflowDefinition, trigger, key string, synthetic map[string]bool, now time.Time) (*WorkflowRun, error) {
	if trigger != "operator" && trigger != "scheduler" && trigger != "recovery" || len(definition.Steps) == 0 || len(definition.Steps) > 16 {
		return nil, ErrWorkflowValidation
	}
	id, e := newWorkflowRunID()
	if e != nil {
		return nil, e
	}
	tx, e := s.workflowTx(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	// INSERT obtains the write lock before any read/claim decision.
	result, e := tx.ExecContext(ctx, `INSERT INTO workflow_runs(id,workflow_id,status,trigger_source,execution_source,idempotency_key,created_at)
 SELECT ?,?,'queued',?,?,?,? WHERE NOT EXISTS(SELECT 1 FROM workflow_runs WHERE status IN ('queued','running'))
 AND NOT EXISTS(SELECT 1 FROM workflow_runs WHERE workflow_id=? AND idempotency_key=?)`,
		id, definition.ID, trigger, trigger, workflowKey(key), workflowTime(now), definition.ID, workflowKey(key))
	if e != nil {
		return nil, workflowSQLError(e)
	}
	n, e := result.RowsAffected()
	if e != nil {
		return nil, ErrWorkflowStorage
	}
	if n == 0 {
		var existing string
		if key != "" {
			if e = tx.QueryRowContext(ctx, `SELECT id FROM workflow_runs WHERE workflow_id=? AND idempotency_key=?`, definition.ID, workflowKey(key)).Scan(&existing); e == nil {
				tx.Rollback()
				return s.GetWorkflowRun(ctx, existing)
			}
			if !errors.Is(e, sql.ErrNoRows) {
				return nil, ErrWorkflowStorage
			}
		}
		return nil, ErrWorkflowBusy
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO sync_runs(source,started_at,status,message,counts_json,triggered_by,job_id) VALUES('workflow',?,'running','','{}',?,?)`, workflowTime(now), trigger, id); e != nil {
		return nil, ErrWorkflowStorage
	}
	lookback, marshalErr := json.Marshal(definition.SourceLookbackDays)
	if marshalErr != nil {
		return nil, ErrWorkflowValidation
	}
	if _, e = tx.ExecContext(ctx, `UPDATE workflow_runs SET source_lookback_json=? WHERE id=?`, string(lookback), id); e != nil {
		return nil, ErrWorkflowStorage
	}
	if len(definition.SourceSync) > 0 {
		sync, syncErr := json.Marshal(definition.SourceSync)
		if syncErr != nil || !s.sourceSync {
			return nil, ErrWorkflowValidation
		}
		if _, e = tx.ExecContext(ctx, `UPDATE workflow_runs SET source_sync_json=? WHERE id=?`, string(sync), id); e != nil {
			return nil, ErrWorkflowStorage
		}
	}
	if len(definition.FetchWindows) > 0 {
		windows, windowErr := json.Marshal(definition.FetchWindows)
		if windowErr != nil || s.workflowVersion < 3 {
			return nil, ErrWorkflowValidation
		}
		if _, e = tx.ExecContext(ctx, `UPDATE workflow_runs SET fetch_windows_json=? WHERE id=?`, string(windows), id); e != nil {
			return nil, ErrWorkflowStorage
		}
	}
	var stepSchema string
	_ = tx.QueryRowContext(ctx, `SELECT COALESCE(sql,'') FROM sqlite_master WHERE type='table' AND name='workflow_run_steps'`).Scan(&stepSchema)
	supportsXRecent := strings.Contains(stepSchema, "x_recent")
	for order, step := range definition.Steps {
		capability := "capability_not_migrated"
		if synthetic[step] {
			capability = "synthetic"
		} else if enabled, declared := synthetic[step]; step == "x" && supportsXRecent && declared && !enabled {
			capability = "x_recent"
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO workflow_run_steps(run_id,step_id,step_order,status,capability) VALUES(?,?,?,'pending',?)`, id, step, order, capability); e != nil {
			return nil, ErrWorkflowStorage
		}
		adapter := ""
		if enabled, declared := synthetic[step]; declared && !enabled {
			switch step {
			case "x":
				adapter = "x_recent"
			case "youtube":
				adapter = "youtube_recent"
			case "bilibili":
				adapter = "bilibili_recent"
			case "live":
				adapter = "youtube_live"
			case "media":
				adapter = "media_download"
			case "schedule":
				adapter = "schedule_extract"
			case "translate":
				adapter = "text_translate"
			case "reconcile":
				adapter = "event_reconcile"
			}
		}
		if adapter != "" {
			if _, e = tx.ExecContext(ctx, `UPDATE workflow_run_steps SET adapter_capability=? WHERE run_id=? AND step_id=?`, adapter, id, step); e != nil {
				return nil, ErrWorkflowStorage
			}
		}
	}
	if e = tx.Commit(); e != nil {
		return nil, ErrWorkflowStorage
	}
	return s.GetWorkflowRun(ctx, id)
}
func (s *Store) ClaimWorkflowRun(ctx context.Context, id, owner string, now time.Time, ttl time.Duration) (WorkflowLease, error) {
	if owner == "" || ttl <= 0 {
		return WorkflowLease{}, ErrWorkflowValidation
	}
	tx, e := s.workflowTx(ctx)
	if e != nil {
		return WorkflowLease{}, e
	}
	defer tx.Rollback()
	result, e := tx.ExecContext(ctx, `UPDATE workflow_runs SET status='running',started_at=COALESCE(started_at,?) WHERE id=? AND status='queued'
 AND NOT EXISTS(SELECT 1 FROM workflow_leases)`, workflowTime(now), id)
	if e != nil {
		return WorkflowLease{}, workflowSQLError(e)
	}
	if e = affected(result, nil); e != nil {
		return WorkflowLease{}, ErrWorkflowBusy
	}
	var workflowID string
	var generation int
	if e = tx.QueryRowContext(ctx, `SELECT workflow_id,recovery_count FROM workflow_runs WHERE id=?`, id).Scan(&workflowID, &generation); e != nil {
		return WorkflowLease{}, ErrWorkflowStorage
	}
	lease := WorkflowLease{id, owner, generation + 1, now.Add(ttl)}
	if _, e = tx.ExecContext(ctx, `INSERT INTO workflow_leases VALUES('global',?,?,?,?,?)`, id, owner, workflowTime(now), workflowTime(lease.ExpiresAt), lease.Generation); e != nil {
		return WorkflowLease{}, workflowSQLError(e)
	}
	var interval, enabled int
	e = tx.QueryRowContext(ctx, `SELECT interval_minutes,schedule_enabled FROM workflows WHERE id=?`, workflowID).Scan(&interval, &enabled)
	// Operator jobs (full sync, scan, one-source fetch) run from an unsaved
	// definition, so there is no saved workflow row to plan or annotate.
	if e != nil && !(errors.Is(e, sql.ErrNoRows) && IsAdhocWorkflow(workflowID)) {
		return WorkflowLease{}, ErrWorkflowStorage
	}
	if e == nil {
		var next any
		if enabled != 0 {
			next = workflowTime(now.Add(time.Duration(clampInterval(interval)) * time.Minute))
		}
		if _, e = tx.ExecContext(ctx, `UPDATE workflows SET last_run_at=?,last_job_id=?,last_status='running',next_run_at=?,updated_at=? WHERE id=?`, workflowTime(now), id, next, workflowTime(now), workflowID); e != nil {
			return WorkflowLease{}, ErrWorkflowStorage
		}
	}
	// A source schedule plans its next run from the claim, as a saved
	// workflow timer does.
	if source, scheduled := SourceScheduleOf(workflowID); scheduled {
		if e = planSourceSchedule(ctx, tx, source, now); e != nil {
			return WorkflowLease{}, e
		}
	}
	if e = tx.Commit(); e != nil {
		return WorkflowLease{}, ErrWorkflowStorage
	}
	return lease, nil
}
func (s *Store) RenewWorkflowLease(ctx context.Context, l WorkflowLease, now time.Time, ttl time.Duration) error {
	if s == nil || !s.workflowWritable {
		return ErrWorkflowUnavailable
	}
	return affected(s.db.ExecContext(ctx, `UPDATE workflow_leases SET expires_at=? WHERE scope='global' AND run_id=? AND owner_id=? AND generation=? AND julianday(expires_at)>julianday(?)`,
		workflowTime(now.Add(ttl)), l.RunID, l.OwnerID, l.Generation, workflowTime(now)))
}
func fenceWorkflow(ctx context.Context, tx *sql.Tx, l WorkflowLease, now time.Time) error {
	// No-op write takes the SQLite write lock before validating a lease.
	return affected(tx.ExecContext(ctx, `UPDATE workflow_leases SET acquired_at=acquired_at WHERE scope='global' AND run_id=? AND owner_id=? AND generation=? AND julianday(expires_at)>julianday(?)`, l.RunID, l.OwnerID, l.Generation, workflowTime(now)))
}
func (s *Store) StartWorkflowStep(ctx context.Context, l WorkflowLease, stepRow int64, now time.Time) (int, error) {
	tx, e := s.workflowTx(ctx)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	if e = fenceWorkflow(ctx, tx, l, now); e != nil {
		return 0, e
	}
	var cancel sql.NullString
	if e = tx.QueryRowContext(ctx, `SELECT cancellation_requested_at FROM workflow_runs WHERE id=?`, l.RunID).Scan(&cancel); e != nil {
		return 0, ErrWorkflowStorage
	}
	if cancel.Valid {
		return 0, ErrWorkflowValidation
	}
	if e = affected(tx.ExecContext(ctx, `UPDATE workflow_run_steps SET status='running',started_at=COALESCE(started_at,?),finished_at=NULL,error_code=NULL,retryable=0 WHERE id=? AND run_id=? AND status='pending'`, workflowTime(now), stepRow, l.RunID)); e != nil {
		return 0, e
	}
	var attempt int
	if e = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(attempt_number),0)+1 FROM workflow_step_attempts WHERE step_id=?`, stepRow).Scan(&attempt); e != nil {
		return 0, ErrWorkflowStorage
	}
	if attempt > 100 {
		return 0, ErrWorkflowValidation
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO workflow_step_attempts(step_id,attempt_number,status,started_at) VALUES(?,?,'running',?)`, stepRow, attempt, workflowTime(now)); e != nil {
		return 0, ErrWorkflowStorage
	}
	if e = tx.Commit(); e != nil {
		return 0, ErrWorkflowStorage
	}
	return attempt, nil
}
func (s *Store) FinishWorkflowStep(ctx context.Context, l WorkflowLease, stepRow int64, attempt int, out WorkflowOutcome, retry bool, now time.Time) error {
	if workflowStatus(out.Status, true) != out.Status || out.Status == "pending" || out.Status == "running" {
		return ErrWorkflowValidation
	}
	if out.Code != "" {
		code := workflowCode(sql.NullString{String: out.Code, Valid: true})
		out.Code = *code
	}
	tx, e := s.workflowTx(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = fenceWorkflow(ctx, tx, l, now); e != nil {
		return e
	}
	var code any
	if out.Code != "" {
		code = out.Code
	}
	if e = affected(tx.ExecContext(ctx, `UPDATE workflow_step_attempts SET status=?,error_code=?,retryable=?,finished_at=?,duration_ms=MAX(0,CAST(ROUND((julianday(?)-julianday(started_at))*86400000) AS INTEGER))
 WHERE step_id=? AND attempt_number=? AND status='running' AND step_id IN(SELECT id FROM workflow_run_steps WHERE run_id=?)`,
		out.Status, code, out.Retryable, workflowTime(now), workflowTime(now), stepRow, attempt, l.RunID)); e != nil {
		return e
	}
	status := out.Status
	if retry && out.Retryable {
		status = "pending"
	}
	if e = affected(tx.ExecContext(ctx, `UPDATE workflow_run_steps SET status=?,error_code=?,retryable=?,finished_at=? WHERE id=? AND run_id=? AND status='running'`, status, code, out.Retryable, workflowTime(now), stepRow, l.RunID)); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return ErrWorkflowStorage
	}
	return nil
}
func (s *Store) FinishWorkflowRun(ctx context.Context, l WorkflowLease, status, code string, now time.Time) error {
	if workflowStatus(status, false) != status || status == "queued" || status == "running" {
		return ErrWorkflowValidation
	}
	tx, e := s.workflowTx(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = fenceWorkflow(ctx, tx, l, now); e != nil {
		return e
	}
	var ec any
	if code != "" {
		ec = *workflowCode(sql.NullString{String: code, Valid: true})
	}
	// Pending dependent steps are blocked; cancellation has its own durable state.
	tail := "blocked"
	if status == "cancelled" {
		tail = "cancelled"
	}
	if status == "failed" || status == "cancelled" || status == "blocked" {
		if _, e = tx.ExecContext(ctx, `UPDATE workflow_step_attempts SET status=?,error_code=?,finished_at=?,duration_ms=MAX(0,CAST(ROUND((julianday(?)-julianday(started_at))*86400000) AS INTEGER)) WHERE status='running' AND step_id IN(SELECT id FROM workflow_run_steps WHERE run_id=?)`, status, ec, workflowTime(now), workflowTime(now), l.RunID); e != nil {
			return ErrWorkflowStorage
		}
		if _, e = tx.ExecContext(ctx, `UPDATE workflow_run_steps SET status=?,error_code=?,finished_at=? WHERE run_id=? AND status='running'`, status, ec, workflowTime(now), l.RunID); e != nil {
			return ErrWorkflowStorage
		}
	}
	if _, e = tx.ExecContext(ctx, `UPDATE workflow_run_steps SET status=?,error_code=?,finished_at=? WHERE run_id=? AND status='pending'`, tail, ec, workflowTime(now), l.RunID); e != nil {
		return ErrWorkflowStorage
	}
	if e = affected(tx.ExecContext(ctx, `UPDATE workflow_runs SET status=?,error_code=?,finished_at=? WHERE id=? AND status='running'`, status, ec, workflowTime(now), l.RunID)); e != nil {
		return e
	}
	syncStatus := status
	if status == "succeeded" {
		syncStatus = "success"
	}
	if status == "cancelled" || status == "blocked" {
		syncStatus = "failed"
	}
	if _, e = tx.ExecContext(ctx, `UPDATE sync_runs SET status=?,finished_at=?,message=? WHERE job_id=? AND status='running'`, syncStatus, workflowTime(now), ec, l.RunID); e != nil {
		return ErrWorkflowStorage
	}
	if e = bumpRevision(ctx, tx, workflowTime(now)); e != nil {
		return ErrWorkflowStorage
	}
	if _, e = tx.ExecContext(ctx, `UPDATE workflows SET last_status=?,updated_at=? WHERE last_job_id=?`, status, workflowTime(now), l.RunID); e != nil {
		return ErrWorkflowStorage
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM workflow_leases WHERE run_id=? AND owner_id=? AND generation=?`, l.RunID, l.OwnerID, l.Generation); e != nil {
		return ErrWorkflowStorage
	}
	if e = tx.Commit(); e != nil {
		return ErrWorkflowStorage
	}
	return nil
}
func (s *Store) RequestWorkflowCancellation(ctx context.Context, id string, now time.Time) error {
	tx, e := s.workflowTx(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	result, e := tx.ExecContext(ctx, `UPDATE workflow_runs SET cancellation_requested_at=COALESCE(cancellation_requested_at,?) WHERE id=? AND status IN('queued','running')`, workflowTime(now), id)
	if e != nil {
		return ErrWorkflowStorage
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return ErrWorkflowNotFound
	}
	if _, e = tx.ExecContext(ctx, `UPDATE workflow_run_steps SET status='cancelled',error_code='cancelled',finished_at=? WHERE run_id=? AND status='pending' AND EXISTS(SELECT 1 FROM workflow_runs WHERE id=? AND status='queued')`, workflowTime(now), id, id); e != nil {
		return ErrWorkflowStorage
	}
	if _, e = tx.ExecContext(ctx, `UPDATE workflow_runs SET status='cancelled',error_code='cancelled',finished_at=? WHERE id=? AND status='queued'`, workflowTime(now), id); e != nil {
		return ErrWorkflowStorage
	}
	cancelledRun, e := tx.ExecContext(ctx, `UPDATE sync_runs SET status='failed',message='cancelled',finished_at=? WHERE job_id=? AND EXISTS(SELECT 1 FROM workflow_runs WHERE id=? AND status='cancelled')`, workflowTime(now), id, id)
	if e != nil {
		return ErrWorkflowStorage
	}
	if n, _ := cancelledRun.RowsAffected(); n > 0 {
		if e = bumpRevision(ctx, tx, workflowTime(now)); e != nil {
			return ErrWorkflowStorage
		}
	}
	if e = tx.Commit(); e != nil {
		return ErrWorkflowStorage
	}
	return nil
}
func (s *Store) WorkflowCancellationRequested(ctx context.Context, id string) (bool, error) {
	var v sql.NullString
	if s == nil || s.db == nil {
		return false, ErrWorkflowStorage
	}
	if e := s.db.QueryRowContext(ctx, `SELECT cancellation_requested_at FROM workflow_runs WHERE id=?`, id).Scan(&v); e != nil {
		return false, ErrWorkflowStorage
	}
	return v.Valid, nil
}
func (s *Store) RetryWorkflowRun(ctx context.Context, id string, now time.Time) error {
	tx, e := s.workflowTx(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	result, e := tx.ExecContext(ctx, `UPDATE workflow_runs SET status='queued',finished_at=NULL,error_code=NULL,cancellation_requested_at=NULL,execution_source='recovery',recovery_count=recovery_count+1
 WHERE id=? AND status IN('failed','partial','blocked') AND NOT EXISTS(SELECT 1 FROM workflow_runs WHERE status IN('queued','running'))`, id)
	if e != nil {
		return workflowSQLError(e)
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return ErrWorkflowBusy
	}
	var nretry, npermanent int
	if e = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(status='failed' AND retryable=1),0),COALESCE(SUM(status='failed' AND retryable=0 OR status='blocked' AND error_code='capability_not_migrated'),0) FROM workflow_run_steps WHERE run_id=?`, id).Scan(&nretry, &npermanent); e != nil {
		return ErrWorkflowStorage
	}
	if nretry == 0 || npermanent > 0 {
		return ErrWorkflowValidation
	}
	if _, e = tx.ExecContext(ctx, `UPDATE workflow_run_steps SET status='pending',finished_at=NULL WHERE run_id=? AND (status='failed' AND retryable=1 OR status='blocked')`, id); e != nil {
		return ErrWorkflowStorage
	}
	if _, e = tx.ExecContext(ctx, `UPDATE sync_runs SET status='running',message='',finished_at=NULL WHERE job_id=?`, id); e != nil {
		return ErrWorkflowStorage
	}
	if e = tx.Commit(); e != nil {
		return ErrWorkflowStorage
	}
	return nil
}
func (s *Store) RecoverWorkflowRuns(ctx context.Context, now time.Time) (int, error) {
	tx, e := s.workflowTx(ctx)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	// Serialize against heartbeat/claim before inspecting expired ownership.
	if _, e = tx.ExecContext(ctx, `UPDATE workflow_leases SET acquired_at=acquired_at WHERE scope='global'`); e != nil {
		return 0, ErrWorkflowStorage
	}
	rows, e := tx.QueryContext(ctx, `SELECT r.id FROM workflow_runs r LEFT JOIN workflow_leases l ON l.run_id=r.id WHERE r.status='running' AND (l.run_id IS NULL OR julianday(l.expires_at)<=julianday(?))`, workflowTime(now))
	if e != nil {
		return 0, ErrWorkflowStorage
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			rows.Close()
			return 0, ErrWorkflowStorage
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return 0, ErrWorkflowStorage
	}
	for _, id := range ids {
		if _, e = tx.ExecContext(ctx, `UPDATE workflow_step_attempts SET status='failed',error_code='lease_expired',retryable=1,finished_at=?,duration_ms=MAX(0,CAST(ROUND((julianday(?)-julianday(started_at))*86400000) AS INTEGER)) WHERE status='running' AND step_id IN(SELECT id FROM workflow_run_steps WHERE run_id=?)`, workflowTime(now), workflowTime(now), id); e != nil {
			return 0, ErrWorkflowStorage
		}
		if _, e = tx.ExecContext(ctx, `UPDATE workflow_run_steps SET status='pending',error_code='lease_expired',retryable=1,finished_at=NULL WHERE run_id=? AND status='running'`, id); e != nil {
			return 0, ErrWorkflowStorage
		}
		if _, e = tx.ExecContext(ctx, `UPDATE workflow_runs SET status='queued',execution_source='recovery',error_code='lease_expired',recovery_count=recovery_count+1 WHERE id=?`, id); e != nil {
			return 0, ErrWorkflowStorage
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM workflow_leases WHERE run_id=?`, id); e != nil {
			return 0, ErrWorkflowStorage
		}
	}
	if e = tx.Commit(); e != nil {
		return 0, ErrWorkflowStorage
	}
	return len(ids), nil
}
func (s *Store) NextQueuedWorkflowRun(ctx context.Context) (*WorkflowRun, error) {
	var id string
	e := s.db.QueryRowContext(ctx, `SELECT id FROM workflow_runs WHERE status='queued' ORDER BY created_at,id LIMIT 1`).Scan(&id)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, ErrWorkflowStorage
	}
	return s.GetWorkflowRun(ctx, id)
}
