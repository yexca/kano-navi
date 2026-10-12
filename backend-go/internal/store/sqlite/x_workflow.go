package sqlite

import (
	"context"
	"time"
)

// EnsureXRecentWorkflow creates the one development workflow definition used
// by the admin trigger. It is additive and does not seed source snapshots.
func (s *Store) EnsureXRecentWorkflow(ctx context.Context) error {
	if s == nil || !s.workflowWritable {
		return ErrWorkflowUnavailable
	}
	now := workflowTime(time.Now().UTC())
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO workflows(id,name,steps_json,source_lookback_json,schedule_enabled,interval_minutes,created_at,updated_at) VALUES('x-recent','X recent sync','["x"]','{"x":7}',0,60,?,?)`, now, now)
	if err != nil {
		return ErrWorkflowStorage
	}
	return nil
}
