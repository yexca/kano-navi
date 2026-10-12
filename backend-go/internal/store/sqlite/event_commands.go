package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var ErrReadOnly = errors.New("admin_writes_disabled")

type EventInput struct{ ID, Title, Detail, StartsOn, StartsAt, EndsAt, Status, EventType, URL, CancellationStatus, CancellationReason string }

func (s *Store) mutate(ctx context.Context, input EventInput, id string, deleteRow bool) (AdminEvent, error) {
	if s == nil || !s.writable {
		return AdminEvent{}, ErrReadOnly
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminEvent{}, fmt.Errorf("%w: transaction", ErrUnavailable)
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if id == "" {
		id = input.ID
		if id == "" {
			id = "manual-" + now
		}
	}
	if deleteRow {
		res, err := tx.ExecContext(ctx, "UPDATE events SET provenance='manual',manual_locked=1,deleted_at=?,updated_at=? WHERE id=?", now, now, id)
		if err != nil {
			return AdminEvent{}, fmt.Errorf("%w: event", ErrUnavailable)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return AdminEvent{}, sql.ErrNoRows
		}
	} else {
		if input.StartsOn == "" {
			return AdminEvent{}, errors.New("invalid_input")
		}
		if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(input.StartsOn) { return AdminEvent{}, errors.New("invalid_input") }
		var start, end time.Time
		var parseErr error
		if input.StartsAt != "" { start, parseErr = time.Parse(time.RFC3339Nano, input.StartsAt); if parseErr != nil { return AdminEvent{}, errors.New("invalid_input") }; if start.In(time.FixedZone("JST",9*3600)).Format("2006-01-02") != input.StartsOn { return AdminEvent{}, errors.New("invalid_input") } }
		if input.EndsAt != "" { if input.StartsAt=="" { return AdminEvent{}, errors.New("invalid_input") }; end, parseErr=time.Parse(time.RFC3339Nano,input.EndsAt); if parseErr!=nil || end.Before(start) { return AdminEvent{}, errors.New("invalid_input") } }
		if input.URL != "" { parsed, e:=url.Parse(input.URL); if e!=nil || (parsed.Scheme!="http" && parsed.Scheme!="https") { return AdminEvent{}, errors.New("invalid_input") } }
		if input.StartsAt == "" {
			input.EndsAt = ""
		}
		if input.CancellationStatus == "manual_confirmed" && strings.TrimSpace(input.CancellationReason) == "" {
			return AdminEvent{}, errors.New("invalid_input")
		}
		if input.CancellationStatus == "" {
			input.CancellationStatus = "none"
		}
		if input.EventType == "" {
			input.EventType = "event"
		}
		if input.Title == "" {
			return AdminEvent{}, errors.New("invalid_input")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO events(id,source,source_item_id,source_key,title,detail,starts_on,starts_at,ends_at,timezone,time_precision,status,event_type,url,provenance,manual_locked,cancellation_status,cancellation_source,cancellation_reason,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET title=excluded.title,detail=excluded.detail,starts_on=excluded.starts_on,starts_at=excluded.starts_at,ends_at=excluded.ends_at,status=excluded.status,event_type=excluded.event_type,url=excluded.url,provenance='manual',manual_locked=1,deleted_at=NULL,updated_at=excluded.updated_at,cancellation_status=excluded.cancellation_status,cancellation_source=excluded.cancellation_source,cancellation_reason=excluded.cancellation_reason`, id, "manual", id, id, input.Title, input.Detail, input.StartsOn, nullIfEmpty(input.StartsAt), nullIfEmpty(input.EndsAt), "Asia/Tokyo", timePrecision(input.StartsAt), input.Status, input.EventType, nullIfEmpty(input.URL), "manual", 1, input.CancellationStatus, nullIfEmpty(input.CancellationStatus), nullIfEmpty(input.CancellationReason), now, now)
		if err != nil {
			return AdminEvent{}, fmt.Errorf("%w: event", ErrUnavailable)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO event_sources(event_id,source,source_item_id,source_key,url,raw_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, id, "manual", id, "confirmed", nullIfEmpty(input.URL), "{}", now, now)
		if err != nil {
			return AdminEvent{}, fmt.Errorf("%w: source", ErrUnavailable)
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO app_settings(key,value,updated_at) VALUES('dashboard_revision','1',?) ON CONFLICT(key) DO UPDATE SET value=CAST(value AS INTEGER)+1,updated_at=excluded.updated_at`, now)
	if err != nil {
		return AdminEvent{}, fmt.Errorf("%w: revision", ErrUnavailable)
	}
	if err = tx.Commit(); err != nil {
		return AdminEvent{}, fmt.Errorf("%w: commit", ErrUnavailable)
	}
	return s.event(ctx, id)
}
func nullIfEmpty(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}
func timePrecision(v string) string {
	if v == "" {
		return "unknown"
	}
	return "exact"
}
func (s *Store) event(ctx context.Context, id string) (AdminEvent, error) {
	var e AdminEvent
	var deleted, locked int
	err := s.db.QueryRowContext(ctx, `SELECT id,source,source_item_id,source_key,title,detail,starts_on,starts_at,ends_at,timezone,time_precision,status,event_type,url,provenance,manual_locked,cancellation_status,cancellation_source,cancellation_reason,cancellation_evidence,cancellation_source_item_id,cancellation_confidence,cancellation_at,deleted_at,created_at,updated_at FROM events WHERE id=?`, id).Scan(&e.ID, &e.Source, &e.SourceItemID, &e.SourceKey, &e.Title, &e.Detail, &e.StartsOn, &e.StartsAt, &e.EndsAt, &e.Timezone, &e.TimePrecision, &e.Status, &e.EventType, &e.URL, &e.Provenance, &locked, &e.CancellationStatus, &e.CancellationSource, &e.CancellationReason, &e.CancellationEvidence, &e.CancellationSourceItemID, &e.CancellationConfidence, &e.CancellationAt, &e.DeletedAt, &e.CreatedAt, &e.UpdatedAt)
	e.ManualLocked = locked != 0
	_ = deleted
	return e, err
}
func (s *Store) CreateManualEvent(c context.Context, i EventInput) (AdminEvent, error) {
	return s.mutate(c, i, i.ID, false)
}
func (s *Store) UpdateManualEvent(c context.Context, id string, i EventInput) (AdminEvent, error) {
	return s.mutate(c, i, id, false)
}
func (s *Store) DeleteManualEvent(c context.Context, id string) (AdminEvent, error) {
	return s.mutate(c, EventInput{}, id, true)
}

func (s *Store) GetAdminEvent(c context.Context, id string) (AdminEvent, error) { return s.event(c, id) }
