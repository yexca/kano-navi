package sqlite

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// SaveWorkflow preserves run history and timer metadata. Run inputs have their
// own snapshot and are never changed by editing the saved definition.
func (s *Store) SaveWorkflow(ctx context.Context, input WorkflowDefinition, create bool) (*WorkflowDefinition, error) {
	if !s.workflowWritable {
		return nil, ErrWorkflowUnavailable
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len([]rune(input.Name)) > 80 || len(input.Steps) == 0 {
		return nil, ErrWorkflowValidation
	}
	seen := map[string]bool{}
	for _, step := range input.Steps {
		if step != "x" && step != "youtube" && step != "bilibili" && step != "media" && step != "schedule" && step != "translate" && step != "reconcile" {
			return nil, ErrWorkflowValidation
		}
		seen[step] = true
	}
	input.Steps = []string{}
	for _, step := range []string{"x", "youtube", "bilibili", "media", "schedule", "translate", "reconcile"} {
		if seen[step] {
			input.Steps = append(input.Steps, step)
		}
	}
	days := map[string]int{"x": 7, "youtube": 14}
	for key, value := range input.SourceLookbackDays {
		if (key != "x" && key != "youtube") || value < 1 || value > 365 {
			return nil, ErrWorkflowValidation
		}
		days[key] = value
	}
	steps, _ := json.Marshal(input.Steps)
	lookback, _ := json.Marshal(days)
	input.IntervalMinutes = clampInterval(input.IntervalMinutes)
	if create {
		id, err := newWorkflowRunID()
		if err != nil {
			return nil, err
		}
		input.ID = "workflow-" + id
	}
	now := time.Now()
	timestamp := workflowTime(now)
	var next any
	if input.ScheduleEnabled {
		next = workflowTime(now.Add(time.Duration(input.IntervalMinutes) * time.Minute))
	}
	tx, err := s.workflowTx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if create {
		_, err = tx.ExecContext(ctx, `INSERT INTO workflows(id,name,steps_json,source_lookback_json,schedule_enabled,interval_minutes,next_run_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, input.ID, input.Name, string(steps), string(lookback), input.ScheduleEnabled, input.IntervalMinutes, next, timestamp, timestamp)
	} else {
		res, e := tx.ExecContext(ctx, `UPDATE workflows SET name=?,steps_json=?,source_lookback_json=?,schedule_enabled=?,interval_minutes=?,next_run_at=CASE WHEN ?=0 THEN NULL WHEN schedule_enabled=1 AND interval_minutes=? THEN next_run_at ELSE ? END,updated_at=? WHERE id=?`, input.Name, string(steps), string(lookback), input.ScheduleEnabled, input.IntervalMinutes, input.ScheduleEnabled, input.IntervalMinutes, next, timestamp, input.ID)
		err = e
		if err == nil {
			n, e := res.RowsAffected()
			if e != nil {
				return nil, ErrWorkflowStorage
			}
			if n == 0 {
				return nil, ErrWorkflowNotFound
			}
		}
	}
	if err != nil || tx.Commit() != nil {
		return nil, ErrWorkflowStorage
	}
	return s.GetWorkflow(ctx, input.ID)
}
func (s *Store) DeleteWorkflow(ctx context.Context, id string) error {
	tx, err := s.workflowTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM workflows WHERE id=? AND NOT EXISTS(SELECT 1 FROM workflow_runs WHERE workflow_id=? AND status IN('queued','running'))`, id, id)
	if err != nil {
		return ErrWorkflowStorage
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var count int
		if tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM workflows WHERE id=?`, id).Scan(&count) != nil {
			return ErrWorkflowStorage
		}
		if count > 0 {
			return ErrWorkflowBusy
		}
		return ErrWorkflowNotFound
	}
	if tx.Commit() != nil {
		return ErrWorkflowStorage
	}
	return nil
}
