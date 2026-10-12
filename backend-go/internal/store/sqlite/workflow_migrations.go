package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const WorkflowSchemaVersion = 1

var ErrWorkflowSchema = errors.New("workflow schema invalid")

// DDL, version recording and column validation all commit together.
func migrateWorkflowSchema(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return ErrWorkflowSchema
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS workflow_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL, result TEXT NOT NULL CHECK(result='applied'))`,
		`CREATE TABLE IF NOT EXISTS workflows(id TEXT PRIMARY KEY, name TEXT NOT NULL, steps_json TEXT NOT NULL,
 source_lookback_json TEXT NOT NULL DEFAULT '{}', schedule_enabled INTEGER NOT NULL DEFAULT 0,
 interval_minutes INTEGER NOT NULL DEFAULT 60, last_run_at TEXT,last_job_id TEXT,last_status TEXT,
 next_run_at TEXT,created_at TEXT NOT NULL,updated_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS workflow_runs(id TEXT PRIMARY KEY,workflow_id TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('queued','running','succeeded','partial','failed','cancelled','blocked')),
 trigger_source TEXT NOT NULL CHECK(trigger_source IN ('operator','scheduler','recovery')),
 execution_source TEXT NOT NULL CHECK(execution_source IN ('operator','scheduler','recovery')),
 idempotency_key TEXT,created_at TEXT NOT NULL,started_at TEXT,finished_at TEXT,error_code TEXT,
 cancellation_requested_at TEXT,recovery_count INTEGER NOT NULL DEFAULT 0,UNIQUE(workflow_id,idempotency_key))`,
		`CREATE TABLE IF NOT EXISTS workflow_run_steps(id INTEGER PRIMARY KEY AUTOINCREMENT,
 run_id TEXT NOT NULL REFERENCES workflow_runs(id),step_id TEXT NOT NULL,step_order INTEGER NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('pending','running','succeeded','partial','failed','cancelled','blocked')),
 capability TEXT NOT NULL CHECK(capability IN ('synthetic','x_recent','capability_not_migrated')),
 started_at TEXT,finished_at TEXT,error_code TEXT,retryable INTEGER NOT NULL DEFAULT 0,
 UNIQUE(run_id,step_id),UNIQUE(run_id,step_order))`,
		`CREATE TABLE IF NOT EXISTS workflow_step_attempts(id INTEGER PRIMARY KEY AUTOINCREMENT,
 step_id INTEGER NOT NULL REFERENCES workflow_run_steps(id),attempt_number INTEGER NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('running','succeeded','partial','failed','cancelled','blocked')),
 error_code TEXT,retryable INTEGER NOT NULL DEFAULT 0,started_at TEXT NOT NULL,finished_at TEXT,
 duration_ms INTEGER,UNIQUE(step_id,attempt_number))`,
		`CREATE TABLE IF NOT EXISTS workflow_leases(scope TEXT PRIMARY KEY CHECK(scope='global'),
 run_id TEXT NOT NULL UNIQUE REFERENCES workflow_runs(id),owner_id TEXT NOT NULL,
 acquired_at TEXT NOT NULL,expires_at TEXT NOT NULL,generation INTEGER NOT NULL)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS workflow_active_idx ON workflow_runs((1)) WHERE status IN ('queued','running')`,
		`CREATE INDEX IF NOT EXISTS workflow_runs_created_idx ON workflow_runs(created_at DESC,id DESC)`,
		`CREATE INDEX IF NOT EXISTS workflow_steps_run_idx ON workflow_run_steps(run_id,step_order)`,
		`CREATE INDEX IF NOT EXISTS workflow_attempts_step_idx ON workflow_step_attempts(step_id,attempt_number)`,
	}
	if _, err = tx.ExecContext(ctx, statements[0]); err != nil {
		return ErrWorkflowSchema
	}
	var version int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM workflow_migrations`).Scan(&version); err != nil || version > WorkflowSchemaVersion {
		return ErrWorkflowSchema
	}
	for _, statement := range statements[1:] {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			return ErrWorkflowSchema
		}
	}
	rows, err := tx.QueryContext(ctx, "PRAGMA table_info(workflows)")
	if err != nil {
		return ErrWorkflowSchema
	}
	found := false
	for rows.Next() {
		var cid, nn, pk int
		var name, typ string
		var def any
		if err = rows.Scan(&cid, &name, &typ, &nn, &def, &pk); err != nil {
			rows.Close()
			return ErrWorkflowSchema
		}
		if name == "source_lookback_json" {
			found = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrWorkflowSchema
	}
	if !found {
		if _, err = tx.ExecContext(ctx, `ALTER TABLE workflows ADD COLUMN source_lookback_json TEXT NOT NULL DEFAULT '{}'`); err != nil {
			return ErrWorkflowSchema
		}
	}
	if err = verifyWorkflowSchema(ctx, tx); err != nil {
		return err
	}
	if version == 0 {
		if _, err = tx.ExecContext(ctx, `INSERT INTO workflow_migrations VALUES(?,?,'applied')`, WorkflowSchemaVersion, workflowTime(time.Now())); err != nil {
			return ErrWorkflowSchema
		}
	}
	if err = tx.Commit(); err != nil {
		return ErrWorkflowSchema
	}
	return nil
}

type workflowQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func verifyWorkflowSchema(ctx context.Context, q workflowQuerier) error {
	checks := []string{
		`SELECT id,name,steps_json,source_lookback_json,schedule_enabled,interval_minutes,last_run_at,last_job_id,last_status,next_run_at,created_at,updated_at FROM workflows LIMIT 0`,
		`SELECT id,workflow_id,status,trigger_source,execution_source,idempotency_key,created_at,started_at,finished_at,error_code,cancellation_requested_at,recovery_count FROM workflow_runs LIMIT 0`,
		`SELECT id,run_id,step_id,step_order,status,capability,started_at,finished_at,error_code,retryable FROM workflow_run_steps LIMIT 0`,
		`SELECT id,step_id,attempt_number,status,error_code,retryable,started_at,finished_at,duration_ms FROM workflow_step_attempts LIMIT 0`,
		`SELECT scope,run_id,owner_id,acquired_at,expires_at,generation FROM workflow_leases LIMIT 0`,
		`SELECT version,applied_at,result FROM workflow_migrations LIMIT 0`,
	}
	for _, query := range checks {
		rows, e := q.QueryContext(ctx, query)
		if e != nil {
			return ErrWorkflowSchema
		}
		rows.Close()
	}
	return nil
}

// Separate writable capability: workflow migration never enables event commands.
func OpenWorkflowCore(filename string, development bool) (*Store, error) {
	if !development {
		return nil, fmt.Errorf("%w: development required", ErrWorkflowSchema)
	}
	s, err := OpenAdminWrites(filename)
	if err != nil {
		return nil, err
	}
	s.writable = false
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = migrateWorkflowSchema(ctx, s.db); err != nil {
		s.Close()
		return nil, err
	}
	s.workflowWritable = true
	return s, nil
}
