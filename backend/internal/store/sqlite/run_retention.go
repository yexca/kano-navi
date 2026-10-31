package sqlite

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"
)

// Run history retention: finished runs older than their window are deleted
// with their steps, attempts and sync_runs rows. 0 days keeps them forever.
const (
	RunRetentionDefaultSucceededDays = 30
	RunRetentionDefaultFailedDays    = 90
	RunRetentionMaxDays              = 3650

	settingRunRetentionSucceeded = "run_retention_succeeded_days"
	settingRunRetentionFailed    = "run_retention_failed_days"

	runPruneBatch = 200
)

// RunRetention is how long finished runs stay in the history. SucceededDays
// covers succeeded and cancelled runs; FailedDays covers failed, partial and
// blocked runs.
type RunRetention struct {
	SucceededDays int `json:"succeededDays"`
	FailedDays    int `json:"failedDays"`
}

func validRetentionDays(n int) bool { return n >= 0 && n <= RunRetentionMaxDays }

// RunRetention reads the operator settings; absent or invalid values use the
// defaults.
func (s *Store) RunRetention(ctx context.Context) (RunRetention, error) {
	out := RunRetention{SucceededDays: RunRetentionDefaultSucceededDays, FailedDays: RunRetentionDefaultFailedDays}
	rows, err := s.db.QueryContext(ctx, `SELECT key,value FROM app_settings WHERE key IN (?,?)`, settingRunRetentionSucceeded, settingRunRetentionFailed)
	if err != nil {
		return out, dbErr(err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err = rows.Scan(&key, &value); err != nil {
			return out, dbErr(err)
		}
		n, e := strconv.Atoi(value)
		if e != nil || !validRetentionDays(n) {
			continue
		}
		switch key {
		case settingRunRetentionSucceeded:
			out.SucceededDays = n
		case settingRunRetentionFailed:
			out.FailedDays = n
		}
	}
	if err = rows.Err(); err != nil {
		return out, dbErr(err)
	}
	return out, nil
}

// SaveRunRetention stores validated operator settings.
func (s *Store) SaveRunRetention(ctx context.Context, settings RunRetention) error {
	if !s.Writable() {
		return ErrReadOnly
	}
	if !validRetentionDays(settings.SucceededDays) || !validRetentionDays(settings.FailedDays) {
		return ErrWorkflowValidation
	}
	now := workflowTime(time.Now())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrUnavailable
	}
	defer tx.Rollback()
	for key, value := range map[string]int{settingRunRetentionSucceeded: settings.SucceededDays, settingRunRetentionFailed: settings.FailedDays} {
		if _, err = tx.ExecContext(ctx, `INSERT INTO app_settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, key, strconv.Itoa(value), now); err != nil {
			return ErrUnavailable
		}
	}
	if tx.Commit() != nil {
		return ErrUnavailable
	}
	return nil
}

// PruneRuns deletes finished runs past their retention window, in batches,
// and returns how many runs and unlinked sync_runs rows it deleted. Queued,
// running and leased runs are never touched. The newest run of each workflow
// ID (live-probe pacing and the source schedules read it) and the newest
// successful sync (the board's freshness reads it) are kept whatever their
// age.
func (s *Store) PruneRuns(ctx context.Context, settings RunRetention, now time.Time) (int, error) {
	total := 0
	for _, prune := range []func(context.Context, *sql.Tx, RunRetention, time.Time) (int, error){pruneWorkflowRuns, pruneUnlinkedSyncRuns} {
		for {
			n, err := s.pruneBatch(ctx, prune, settings, now)
			total += n
			if err != nil {
				return total, err
			}
			if n < runPruneBatch {
				break
			}
		}
	}
	return total, nil
}

func (s *Store) pruneBatch(ctx context.Context, prune func(context.Context, *sql.Tx, RunRetention, time.Time) (int, error), settings RunRetention, now time.Time) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	tx, err := s.workflowTx(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	n, err := prune(ctx, tx, settings, now)
	if err != nil {
		return 0, err
	}
	if tx.Commit() != nil {
		return 0, ErrWorkflowStorage
	}
	return n, nil
}

// retentionFilter is the age condition on a status column and finish time
// for each window that is not 0 days.
func retentionFilter(settings RunRetention, now time.Time, status, at string, succeeded, failed []string) (string, []any) {
	var clauses []string
	var args []any
	for _, window := range []struct {
		days     int
		statuses []string
	}{{settings.SucceededDays, succeeded}, {settings.FailedDays, failed}} {
		if window.days == 0 {
			continue
		}
		clauses = append(clauses, status+" IN ("+placeholders(len(window.statuses))+") AND julianday("+at+")<julianday(?)")
		for _, v := range window.statuses {
			args = append(args, v)
		}
		args = append(args, workflowTime(now.AddDate(0, 0, -window.days)))
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return "(" + strings.Join(clauses, " OR ") + ")", args
}

// newestSuccessfulSync is the sync_runs row the board's freshness reads.
const newestSuccessfulSync = `(SELECT id FROM sync_runs WHERE status IN ('success','partial') AND finished_at IS NOT NULL ORDER BY finished_at DESC,id DESC LIMIT 1)`

func pruneWorkflowRuns(ctx context.Context, tx *sql.Tx, settings RunRetention, now time.Time) (int, error) {
	filter, args := retentionFilter(settings, now, "r.status", "COALESCE(r.finished_at,r.created_at)",
		[]string{"succeeded", "cancelled"}, []string{"failed", "partial", "blocked"})
	if filter == "" {
		return 0, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT r.id FROM workflow_runs r
 WHERE `+filter+`
 AND NOT EXISTS(SELECT 1 FROM workflow_leases l WHERE l.run_id=r.id)
 AND r.id NOT IN(SELECT id FROM (SELECT id,ROW_NUMBER() OVER (PARTITION BY workflow_id ORDER BY julianday(created_at) DESC,id DESC) AS n FROM workflow_runs) WHERE n=1)
 AND r.id NOT IN(SELECT job_id FROM sync_runs WHERE id=`+newestSuccessfulSync+` AND job_id IS NOT NULL)
 LIMIT ?`, append(args, runPruneBatch)...)
	if err != nil {
		return 0, ErrWorkflowStorage
	}
	var ids []any
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, ErrWorkflowStorage
		}
		ids = append(ids, id)
	}
	if rows.Close() != nil || rows.Err() != nil {
		return 0, ErrWorkflowStorage
	}
	if len(ids) == 0 {
		return 0, nil
	}
	in := placeholders(len(ids))
	for _, statement := range []string{
		`DELETE FROM workflow_step_attempts WHERE step_id IN(SELECT id FROM workflow_run_steps WHERE run_id IN(` + in + `))`,
		`DELETE FROM workflow_run_steps WHERE run_id IN(` + in + `)`,
		`DELETE FROM sync_runs WHERE job_id IN(` + in + `)`,
		`DELETE FROM workflow_runs WHERE id IN(` + in + `)`,
	} {
		if _, err = tx.ExecContext(ctx, statement, ids...); err != nil {
			return 0, ErrWorkflowStorage
		}
	}
	return len(ids), nil
}

// pruneUnlinkedSyncRuns applies the same windows to sync_runs rows that
// belong to no stored run.
func pruneUnlinkedSyncRuns(ctx context.Context, tx *sql.Tx, settings RunRetention, now time.Time) (int, error) {
	filter, args := retentionFilter(settings, now, "status", "COALESCE(finished_at,started_at)",
		[]string{"success"}, []string{"failed", "partial"})
	if filter == "" {
		return 0, nil
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM sync_runs WHERE id IN(SELECT id FROM sync_runs
 WHERE `+filter+`
 AND (job_id IS NULL OR job_id NOT IN(SELECT id FROM workflow_runs))
 AND id IS NOT `+newestSuccessfulSync+`
 LIMIT ?)`, append(args, runPruneBatch)...)
	if err != nil {
		return 0, ErrWorkflowStorage
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, ErrWorkflowStorage
	}
	return int(n), nil
}
