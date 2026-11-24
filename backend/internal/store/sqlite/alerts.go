package sqlite

import "context"

// OpenAlertIncidents counts the open operator alerts nobody has acknowledged,
// for the console's navigation badge. internal/alerts owns the alert tables.
func (s *Store) OpenAlertIncidents(ctx context.Context) (int, error) {
	if s == nil || s.db == nil {
		return 0, ErrUnavailable
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM alert_incidents WHERE status='open' AND acknowledged_at IS NULL`).Scan(&count); err != nil {
		return 0, dbErr(err)
	}
	return count, nil
}
