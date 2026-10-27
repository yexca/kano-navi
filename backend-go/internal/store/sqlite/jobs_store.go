package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// Ad-hoc workflow IDs name the kind of work a run does. Every new run is ad
// hoc; older runs of retired saved workflows keep IDs that match
// ^[a-z0-9][a-z0-9_-]*$, so the prefix cannot collide.
const (
	AdhocSyncWorkflow  = "@sync"
	AdhocScanWorkflow  = "@scan"
	AdhocFetchWorkflow = "@fetch"
	// AdhocTranslateWorkflow is an operator's incremental translation.
	AdhocTranslateWorkflow = "@translate"
)

func IsAdhocWorkflow(id string) bool { return strings.HasPrefix(id, "@") }

// JobRun is the sync_runs history row that belongs to one job.
type JobRun struct {
	ID          int64          `json:"id"`
	Source      string         `json:"source"`
	StartedAt   string         `json:"startedAt"`
	FinishedAt  *string        `json:"finishedAt"`
	Status      string         `json:"status"`
	Message     *string        `json:"message"`
	Counts      map[string]any `json:"counts"`
	TriggeredBy *string        `json:"triggeredBy"`
	JobID       *string        `json:"jobId"`
}

// LabelSyncRun records how the history row should be presented: the run
// source (workflow, manual or schedule) and who started it.
func (s *Store) LabelSyncRun(ctx context.Context, jobID, source, triggeredBy string) error {
	if s == nil || !s.workflowWritable {
		return ErrWorkflowUnavailable
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE sync_runs SET source=?,triggered_by=? WHERE job_id=?`, source, triggeredBy, jobID); err != nil {
		return ErrWorkflowStorage
	}
	return nil
}

// RecordStepResult merges one step's counters into the job's history row so
// the operator sees what each step did. Earlier keys are preserved.
func (s *Store) RecordStepResult(ctx context.Context, jobID, key string, value map[string]any) error {
	if s == nil || !s.workflowWritable {
		return ErrWorkflowUnavailable
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrWorkflowStorage
	}
	defer tx.Rollback()
	var raw sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT counts_json FROM sync_runs WHERE job_id=? ORDER BY id DESC LIMIT 1`, jobID).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return ErrWorkflowStorage
	}
	counts := map[string]any{}
	if raw.Valid {
		_ = json.Unmarshal([]byte(raw.String), &counts)
		if counts == nil {
			counts = map[string]any{}
		}
	}
	counts[key] = value
	encoded, err := json.Marshal(counts)
	if err != nil {
		return ErrWorkflowValidation
	}
	if _, err = tx.ExecContext(ctx, `UPDATE sync_runs SET counts_json=? WHERE job_id=?`, string(encoded), jobID); err != nil {
		return ErrWorkflowStorage
	}
	if err = tx.Commit(); err != nil {
		return ErrWorkflowStorage
	}
	return nil
}

func (s *Store) SyncRunForJob(ctx context.Context, jobID string) (*JobRun, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	var run JobRun
	var finished, message, counts, triggered, job sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,source,started_at,finished_at,status,message,counts_json,triggered_by,job_id FROM sync_runs WHERE job_id=? ORDER BY id DESC LIMIT 1`, jobID).
		Scan(&run.ID, &run.Source, &run.StartedAt, &finished, &run.Status, &message, &counts, &triggered, &job)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrUnavailable
	}
	run.FinishedAt = nullString(finished)
	run.Message = sanitizeRunMessage(message)
	run.TriggeredBy = nullString(triggered)
	run.JobID = nullString(job)
	run.Counts = sanitizeCounts(counts.String)
	return &run, nil
}

// OldestSourceTimestamp is the earliest stored record for a source, which is
// where a "before" backfill window ends.
func (s *Store) OldestSourceTimestamp(ctx context.Context, source, account string) (*string, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	var row *sql.Row
	switch source {
	case "x":
		row = s.db.QueryRowContext(ctx, `SELECT published_at FROM posts WHERE source='x' AND lower(account_handle)=lower(?) AND julianday(published_at) IS NOT NULL ORDER BY julianday(published_at) ASC LIMIT 1`, account)
	case "youtube":
		row = s.db.QueryRowContext(ctx, `SELECT published_at FROM videos WHERE source='youtube' AND julianday(published_at) IS NOT NULL ORDER BY julianday(published_at) ASC LIMIT 1`)
	default:
		return nil, ErrWorkflowValidation
	}
	var oldest sql.NullString
	if err := row.Scan(&oldest); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, ErrUnavailable
	}
	return nullString(oldest), nil
}
