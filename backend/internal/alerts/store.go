package alerts

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

func formatTime(t time.Time) string { return sqlite.ISOTime(t) }

// incident is one rule holding for one subject, from when it was first seen
// until it no longer holds.
type incident struct {
	ID         int64          `json:"id"`
	Rule       string         `json:"rule"`
	Subject    string         `json:"subject"`
	Severity   Severity       `json:"severity"`
	Status     string         `json:"status"`
	Detail     map[string]any `json:"detail"`
	OpenedAt   string         `json:"openedAt"`
	LastSeenAt string         `json:"lastSeenAt"`
	ResolvedAt *string        `json:"resolvedAt"`
	// AcknowledgedAt is when the operator acknowledged the open incident;
	// an acknowledged incident leaves the console's badge count.
	AcknowledgedAt *string `json:"acknowledgedAt"`
}

func (i incident) key() string { return i.Rule + "\x00" + i.Subject }

type rowScanner interface{ Scan(dest ...any) error }

const incidentColumns = `id,rule,subject,severity,status,detail_json,opened_at,last_seen_at,resolved_at,acknowledged_at`

func scanIncident(row rowScanner) (incident, error) {
	var out incident
	var detail string
	var resolved, acknowledged sql.NullString
	if err := row.Scan(&out.ID, &out.Rule, &out.Subject, &out.Severity, &out.Status, &detail, &out.OpenedAt, &out.LastSeenAt, &resolved, &acknowledged); err != nil {
		return incident{}, err
	}
	out.Detail = map[string]any{}
	_ = json.Unmarshal([]byte(detail), &out.Detail)
	if resolved.Valid {
		out.ResolvedAt = &resolved.String
	}
	if acknowledged.Valid {
		out.AcknowledgedAt = &acknowledged.String
	}
	return out, nil
}

// acknowledge sets or clears an open incident's acknowledgement. It reports
// false when no open incident has that id. A resolved incident keeps its
// acknowledgement as it was; the same problem returning opens a new,
// unacknowledged incident.
func acknowledge(ctx context.Context, db *sql.DB, id int64, on bool, now time.Time) (bool, error) {
	var at any
	if on {
		at = formatTime(now)
	}
	result, err := db.ExecContext(ctx, `UPDATE alert_incidents SET acknowledged_at=CASE WHEN ? IS NULL THEN NULL ELSE COALESCE(acknowledged_at,?) END WHERE id=? AND status='open'`, at, at, id)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return changed == 1, nil
}

type rowsQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func openIncidents(ctx context.Context, db rowsQueryer) ([]incident, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+incidentColumns+` FROM alert_incidents WHERE status='open' ORDER BY julianday(opened_at) DESC,id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []incident{}
	for rows.Next() {
		item, err := scanIncident(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func encodeDetail(detail map[string]any) string {
	if detail == nil {
		return "{}"
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

// outcome is one rule's evaluation: whether it ran, and what it found.
type outcome struct {
	evaluated bool
	findings  []Finding
}

// apply records one pass in a single transaction: findings open incidents
// or refresh them, incidents of an evaluated rule that no longer hold are
// resolved, and incidents of a rule turned off are resolved without a
// delivery and their pending deliveries dropped. channels lists the channels that receive deliveries; nil queues
// none.
func apply(ctx context.Context, db *sql.DB, settings Settings, outcomes map[string]outcome, channels []string, now time.Time) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	open, err := openIncidents(ctx, tx)
	if err != nil {
		return err
	}
	at := formatTime(now)
	existing := map[string]incident{}
	for _, item := range open {
		existing[item.key()] = item
	}
	seen := map[string]bool{}
	for _, rule := range Catalog {
		result := outcomes[rule.Code]
		if !result.evaluated {
			continue
		}
		setting := settings.Rules[rule.Code]
		for _, finding := range result.findings {
			key := finding.Rule + "\x00" + finding.Subject
			if seen[key] {
				continue
			}
			seen[key] = true
			if current, ok := existing[key]; ok {
				// The severity follows the rule's current setting.
				if _, err = tx.ExecContext(ctx, `UPDATE alert_incidents SET last_seen_at=?,detail_json=?,severity=? WHERE id=?`, at, encodeDetail(finding.Detail), string(setting.Severity), current.ID); err != nil {
					return err
				}
				continue
			}
			inserted, err := tx.ExecContext(ctx, `INSERT INTO alert_incidents(rule,subject,severity,status,detail_json,opened_at,last_seen_at) VALUES(?,?,?,'open',?,?,?)`,
				finding.Rule, finding.Subject, string(setting.Severity), encodeDetail(finding.Detail), at, at)
			if err != nil {
				return err
			}
			id, err := inserted.LastInsertId()
			if err != nil {
				return err
			}
			if err = queue(ctx, tx, id, KindOpened, channels, at); err != nil {
				return err
			}
		}
	}
	for key, item := range existing {
		if seen[key] {
			continue
		}
		setting, known := settings.Rules[item.Rule]
		result := outcomes[item.Rule]
		off := !known || !setting.Enabled
		if !off && !result.evaluated {
			// The rule could not be evaluated this pass.
			continue
		}
		if _, err = tx.ExecContext(ctx, `UPDATE alert_incidents SET status='resolved',resolved_at=? WHERE id=?`, at, item.ID); err != nil {
			return err
		}
		if off {
			// A rule turned off is silenced: what it still had queued is
			// not sent either.
			if _, err = tx.ExecContext(ctx, `UPDATE alert_deliveries SET status='dropped',last_error='rule_off',finished_at=? WHERE incident_id=? AND status='pending'`, at, item.ID); err != nil {
				return err
			}
		}
		if !off && setting.NotifyResolved {
			if err = queue(ctx, tx, item.ID, KindResolved, channels, at); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func queue(ctx context.Context, tx *sql.Tx, incidentID int64, kind string, channels []string, at string) error {
	for _, channel := range channels {
		if _, err := tx.ExecContext(ctx, `INSERT INTO alert_deliveries(incident_id,channel_id,kind,status,attempts,next_attempt_at,created_at) VALUES(?,?,?,'pending',0,?,?)`,
			incidentID, channel, kind, at, at); err != nil {
			return err
		}
	}
	return nil
}

// pending is a due delivery with its incident.
type pending struct {
	ID        int64
	ChannelID string
	Kind      string
	Attempts  int
	CreatedAt string
	Incident  *incident
}

func dueDeliveries(ctx context.Context, db *sql.DB, now time.Time, limit int) ([]pending, error) {
	rows, err := db.QueryContext(ctx, `SELECT d.id,d.channel_id,d.kind,d.attempts,d.created_at,
		i.id,i.rule,i.subject,i.severity,i.status,i.detail_json,i.opened_at,i.last_seen_at,i.resolved_at
		FROM alert_deliveries d JOIN alert_incidents i ON i.id=d.incident_id
		WHERE d.status='pending' AND julianday(d.next_attempt_at) <= julianday(?)
		ORDER BY julianday(d.next_attempt_at),d.id LIMIT ?`, formatTime(now), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []pending{}
	for rows.Next() {
		var item pending
		var linked incident
		var detail string
		var resolved sql.NullString
		if err = rows.Scan(&item.ID, &item.ChannelID, &item.Kind, &item.Attempts, &item.CreatedAt,
			&linked.ID, &linked.Rule, &linked.Subject, &linked.Severity, &linked.Status, &detail, &linked.OpenedAt, &linked.LastSeenAt, &resolved); err != nil {
			return nil, err
		}
		linked.Detail = map[string]any{}
		_ = json.Unmarshal([]byte(detail), &linked.Detail)
		if resolved.Valid {
			linked.ResolvedAt = &resolved.String
		}
		item.Incident = &linked
		out = append(out, item)
	}
	return out, rows.Err()
}

// settle records an attempt's result: sent, retried later, failed or
// dropped. code is "" for a sent delivery.
func settle(ctx context.Context, db *sql.DB, id int64, attempts int, status, code string, next, now time.Time) error {
	var lastError any
	if code != "" {
		lastError = code
	}
	var finished any
	if status != "pending" {
		finished = formatTime(now)
	}
	_, err := db.ExecContext(ctx, `UPDATE alert_deliveries SET status=?,attempts=?,last_error=?,next_attempt_at=?,finished_at=? WHERE id=? AND status='pending'`,
		status, attempts, lastError, formatTime(next), finished, id)
	return err
}

// recordTest stores a channel test's result in the delivery history.
func recordTest(ctx context.Context, db *sql.DB, channel, code string, now time.Time) error {
	status := "sent"
	var lastError any
	if code != "" {
		status, lastError = "failed", code
	}
	at := formatTime(now)
	_, err := db.ExecContext(ctx, `INSERT INTO alert_deliveries(incident_id,channel_id,kind,status,attempts,next_attempt_at,last_error,created_at,finished_at) VALUES(NULL,?,'test',?,1,?,?,?,?)`,
		channel, status, at, lastError, at, at)
	return err
}

// Delivery is the console's view of one delivery.
type Delivery struct {
	ID         int64   `json:"id"`
	ChannelID  string  `json:"channelId"`
	Kind       string  `json:"kind"`
	Status     string  `json:"status"`
	Attempts   int     `json:"attempts"`
	LastError  *string `json:"lastError"`
	CreatedAt  string  `json:"createdAt"`
	FinishedAt *string `json:"finishedAt"`
	Rule       *string `json:"rule"`
	Subject    *string `json:"subject"`
	Severity   *string `json:"severity"`
}

func listDeliveries(ctx context.Context, db rowsQueryer, limit int) ([]Delivery, error) {
	rows, err := db.QueryContext(ctx, `SELECT d.id,d.channel_id,d.kind,d.status,d.attempts,d.last_error,d.created_at,d.finished_at,i.rule,i.subject,i.severity
		FROM alert_deliveries d LEFT JOIN alert_incidents i ON i.id=d.incident_id
		ORDER BY julianday(d.created_at) DESC,d.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Delivery{}
	for rows.Next() {
		var item Delivery
		var lastError, finished, rule, subject, severity sql.NullString
		if err = rows.Scan(&item.ID, &item.ChannelID, &item.Kind, &item.Status, &item.Attempts, &lastError, &item.CreatedAt, &finished, &rule, &subject, &severity); err != nil {
			return nil, err
		}
		item.LastError, item.FinishedAt = optional(lastError), optional(finished)
		item.Rule, item.Subject, item.Severity = optional(rule), optional(subject), optional(severity)
		out = append(out, item)
	}
	return out, rows.Err()
}

func optional(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

// prune deletes finished deliveries and resolved incidents past the run
// retention windows: sent deliveries follow the succeeded window, failed
// and dropped ones and resolved incidents the failed window. 0 keeps them.
func prune(ctx context.Context, db *sql.DB, retention sqlite.RunRetention, now time.Time) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	cutoff := func(days int) string { return formatTime(now.Add(-time.Duration(days) * 24 * time.Hour)) }
	if retention.SucceededDays > 0 {
		if _, err = tx.ExecContext(ctx, `DELETE FROM alert_deliveries WHERE status='sent' AND julianday(COALESCE(finished_at,created_at)) < julianday(?)`, cutoff(retention.SucceededDays)); err != nil {
			return err
		}
	}
	if retention.FailedDays > 0 {
		before := cutoff(retention.FailedDays)
		if _, err = tx.ExecContext(ctx, `DELETE FROM alert_deliveries WHERE status IN ('failed','dropped') AND julianday(COALESCE(finished_at,created_at)) < julianday(?)`, before); err != nil {
			return err
		}
		// A sent delivery follows the succeeded window alone, so it keeps
		// its incident until that window prunes it.
		if _, err = tx.ExecContext(ctx, `DELETE FROM alert_deliveries WHERE status IN ('failed','dropped') AND incident_id IN (SELECT id FROM alert_incidents WHERE status='resolved' AND julianday(resolved_at) < julianday(?))`, before); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM alert_incidents WHERE status='resolved' AND julianday(resolved_at) < julianday(?)
			AND id NOT IN (SELECT incident_id FROM alert_deliveries WHERE incident_id IS NOT NULL)`, before); err != nil {
			return err
		}
	}
	return tx.Commit()
}
