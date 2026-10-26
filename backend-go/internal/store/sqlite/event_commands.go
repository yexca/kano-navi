package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrReadOnly     = errors.New("admin_writes_disabled")
	ErrEventExists  = errors.New("event already exists")
	ErrEventInvalid = errors.New("invalid_input")
)

// EventInput is one validated manual schedule entry. An empty string means
// "no value" and is stored as NULL. Timestamps are ISO instants; StartsOn is
// the Japan calendar date.
type EventInput struct{ ID, Title, Detail, StartsOn, StartsAt, EndsAt, TimePrecision, Status, EventType, URL, CancellationStatus, CancellationReason string }

func (i EventInput) validate(create bool) error {
	if !adminDateRE.MatchString(i.StartsOn) {
		return ErrEventInvalid
	}
	if _, err := time.Parse("2006-01-02", i.StartsOn); err != nil {
		return ErrEventInvalid
	}
	if create && strings.TrimSpace(i.Title) == "" {
		return ErrEventInvalid
	}
	if i.EndsAt != "" && i.StartsAt == "" {
		return ErrEventInvalid
	}
	if i.CancellationStatus == "manual_confirmed" && strings.TrimSpace(i.CancellationReason) == "" {
		return ErrEventInvalid
	}
	return nil
}

func (i EventInput) precision() string {
	switch i.TimePrecision {
	case "exact", "approximate", "unknown":
		return i.TimePrecision
	}
	if i.StartsAt != "" {
		return "exact"
	}
	return "unknown"
}

// raw keeps the operator's input beside the row, as the original store did.
func (i EventInput) raw() string {
	value := map[string]any{
		"title": i.Title, "detail": nullIfEmpty(i.Detail), "starts_on": i.StartsOn, "starts_at": nullIfEmpty(i.StartsAt),
		"ends_at": nullIfEmpty(i.EndsAt), "timezone": "Asia/Tokyo", "time_precision": i.precision(), "status": nullIfEmpty(i.Status),
		"cancellation_status": i.CancellationStatus, "cancellation_reason": nullIfEmpty(i.CancellationReason),
		"event_type": i.EventType, "url": nullIfEmpty(i.URL), "provenance": "manual", "manualLocked": true,
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func nullIfEmpty(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

func newEventID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("%w: identifier", ErrUnavailable)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("manual-%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func (s *Store) eventTx(ctx context.Context) (*sql.Tx, error) {
	if s == nil || !s.writable {
		return nil, ErrReadOnly
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: transaction", ErrUnavailable)
	}
	return tx, nil
}

// upsertEventSource records which source identity produced or confirmed an
// event. A repeated identity moves to the event and clears its tombstone.
func upsertEventSource(ctx context.Context, tx *sql.Tx, eventID, source, sourceItemID, sourceKey string, url any, raw, now string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO event_sources(event_id,source,source_item_id,source_key,url,raw_json,deleted_at,created_at,updated_at)
 VALUES(?,?,?,?,?,?,NULL,?,?)
 ON CONFLICT(source,source_item_id,source_key) DO UPDATE SET event_id=excluded.event_id,
 url=COALESCE(excluded.url,event_sources.url),raw_json=COALESCE(excluded.raw_json,event_sources.raw_json),
 deleted_at=NULL,updated_at=excluded.updated_at`, eventID, source, sourceItemID, sourceKey, url, raw, now, now)
	return err
}

// CreateManualEvent inserts a locked manual schedule entry.
func (s *Store) CreateManualEvent(ctx context.Context, input EventInput) (AdminEvent, error) {
	tx, err := s.eventTx(ctx)
	if err != nil {
		return AdminEvent{}, err
	}
	defer tx.Rollback()
	if err = input.validate(true); err != nil {
		return AdminEvent{}, err
	}
	id := input.ID
	if id == "" {
		if id, err = newEventID(); err != nil {
			return AdminEvent{}, err
		}
	}
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE id=? OR (source='manual' AND source_item_id=? AND source_key=?)`, id, id, id).Scan(&exists); err != nil {
		return AdminEvent{}, fmt.Errorf("%w: event lookup", ErrUnavailable)
	}
	if exists > 0 {
		return AdminEvent{}, ErrEventExists
	}
	eventType := input.EventType
	if eventType == "" {
		eventType = "event"
	}
	now, raw := ISOTime(time.Now()), input.raw()
	if _, err = tx.ExecContext(ctx, `INSERT INTO events(id,source,source_item_id,source_key,title,detail,starts_on,starts_at,ends_at,timezone,
 time_precision,status,event_type,url,is_upcoming,provenance,manual_locked,cancellation_status,created_at,updated_at,raw_json)
 VALUES(?,'manual',?,?,?,?,?,?,?,'Asia/Tokyo',?,?,?,?,0,'manual',1,'none',?,?,?)`,
		id, id, id, strings.TrimSpace(input.Title), nullIfEmpty(input.Detail), input.StartsOn, nullIfEmpty(input.StartsAt), nullIfEmpty(input.EndsAt),
		input.precision(), nullIfEmpty(input.Status), eventType, nullIfEmpty(input.URL), now, now, raw); err != nil {
		return AdminEvent{}, fmt.Errorf("%w: event", ErrUnavailable)
	}
	if err = upsertEventSource(ctx, tx, id, "manual", id, id, nullIfEmpty(input.URL), raw, now); err != nil {
		return AdminEvent{}, fmt.Errorf("%w: source", ErrUnavailable)
	}
	if input.CancellationStatus == "manual_confirmed" {
		if _, err = tx.ExecContext(ctx, `UPDATE events SET cancellation_status='manual_confirmed',cancellation_source='manual',cancellation_reason=?,
 cancellation_evidence=NULL,cancellation_source_item_id=NULL,cancellation_confidence=NULL,cancellation_at=?,updated_at=? WHERE id=?`,
			strings.TrimSpace(input.CancellationReason), now, now, id); err != nil {
			return AdminEvent{}, fmt.Errorf("%w: cancellation", ErrUnavailable)
		}
	}
	if err = bumpRevision(ctx, tx, now); err != nil {
		return AdminEvent{}, fmt.Errorf("%w: revision", ErrUnavailable)
	}
	if err = tx.Commit(); err != nil {
		return AdminEvent{}, fmt.Errorf("%w: commit", ErrUnavailable)
	}
	return s.event(ctx, id)
}

// UpdateManualEvent saves an operator edit or confirmation. The row becomes
// manually locked and any tombstone is cleared; its source identity is kept so
// later automatic extraction recognizes and leaves it alone.
//
// The save decides the cancellation explicitly: llm_suspected keeps an
// unresolved LLM overlay untouched, none dismisses it, and manual_confirmed
// replaces it with the operator's reason while keeping the original decision
// time of an already confirmed cancellation.
func (s *Store) UpdateManualEvent(ctx context.Context, id string, input EventInput) (AdminEvent, error) {
	tx, err := s.eventTx(ctx)
	if err != nil {
		return AdminEvent{}, err
	}
	defer tx.Rollback()
	if err = input.validate(false); err != nil {
		return AdminEvent{}, err
	}
	var title, timezone, eventType, status sql.NullString
	var source, reason, evidence, sourceItem, at sql.NullString
	var confidence sql.NullFloat64
	err = tx.QueryRowContext(ctx, `SELECT title,timezone,event_type,cancellation_status,cancellation_source,cancellation_reason,
 cancellation_evidence,cancellation_source_item_id,cancellation_confidence,cancellation_at FROM events WHERE id=?`, id).
		Scan(&title, &timezone, &eventType, &status, &source, &reason, &evidence, &sourceItem, &confidence, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminEvent{}, sql.ErrNoRows
	}
	if err != nil {
		return AdminEvent{}, fmt.Errorf("%w: event lookup", ErrUnavailable)
	}
	now, raw := ISOTime(time.Now()), input.raw()
	cancellation := []any{"none", nil, nil, nil, nil, nil, nil}
	switch {
	case input.CancellationStatus == "manual_confirmed":
		decided := any(now)
		if status.String == "manual_confirmed" && at.Valid && at.String != "" {
			decided = at.String
		}
		cancellation = []any{"manual_confirmed", "manual", nullIfEmpty(input.CancellationReason), nil, nil, nil, decided}
	case input.CancellationStatus == "llm_suspected" && status.String == "llm_suspected":
		cancellation = []any{"llm_suspected", source, reason, evidence, sourceItem, confidence, at}
	}
	newTitle := strings.TrimSpace(input.Title)
	if newTitle == "" {
		newTitle = title.String
	}
	newType := input.EventType
	if newType == "" {
		newType = eventType.String
	}
	zone := timezone.String
	if zone == "" {
		zone = "Asia/Tokyo"
	}
	args := append([]any{newTitle, nullIfEmpty(input.Detail), input.StartsOn, nullIfEmpty(input.StartsAt), nullIfEmpty(input.EndsAt), zone,
		input.precision(), nullIfEmpty(input.Status), newType, nullIfEmpty(input.URL)}, cancellation...)
	args = append(args, now, raw, id)
	if _, err = tx.ExecContext(ctx, `UPDATE events SET title=?,detail=?,starts_on=?,starts_at=?,ends_at=?,timezone=?,time_precision=?,status=?,
 event_type=?,url=?,provenance='manual',cancellation_status=?,cancellation_source=?,cancellation_reason=?,cancellation_evidence=?,
 cancellation_source_item_id=?,cancellation_confidence=?,cancellation_at=?,manual_locked=1,deleted_at=NULL,updated_at=?,raw_json=?,
 merged_into=CASE WHEN merge_source='rule' THEN NULL ELSE merged_into END,merge_reason=CASE WHEN merge_source='rule' THEN NULL ELSE merge_reason END,
 merged_at=CASE WHEN merge_source='rule' THEN NULL ELSE merged_at END,merge_source=CASE WHEN merge_source='rule' THEN NULL ELSE merge_source END WHERE id=?`, args...); err != nil {
		return AdminEvent{}, fmt.Errorf("%w: event", ErrUnavailable)
	}
	if err = upsertEventSource(ctx, tx, id, "manual", id, "confirmed", nullIfEmpty(input.URL), raw, now); err != nil {
		return AdminEvent{}, fmt.Errorf("%w: source", ErrUnavailable)
	}
	if err = bumpRevision(ctx, tx, now); err != nil {
		return AdminEvent{}, fmt.Errorf("%w: revision", ErrUnavailable)
	}
	if err = tx.Commit(); err != nil {
		return AdminEvent{}, fmt.Errorf("%w: commit", ErrUnavailable)
	}
	return s.event(ctx, id)
}

// DeleteManualEvent leaves a locked tombstone so synchronization cannot
// resurrect the entry. A missing event reports sql.ErrNoRows. Rule merges
// into the event are released with it, since the reconciler only revisits
// its window and an older duplicate would otherwise stay hidden; other
// owners' decisions stay.
func (s *Store) DeleteManualEvent(ctx context.Context, id string) (AdminEvent, error) {
	tx, err := s.eventTx(ctx)
	if err != nil {
		return AdminEvent{}, err
	}
	defer tx.Rollback()
	now := ISOTime(time.Now())
	result, err := tx.ExecContext(ctx, `UPDATE events SET provenance='manual',manual_locked=1,deleted_at=?,updated_at=? WHERE id=?`, now, now, id)
	if err != nil {
		return AdminEvent{}, fmt.Errorf("%w: event", ErrUnavailable)
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		return AdminEvent{}, sql.ErrNoRows
	}
	if _, err = tx.ExecContext(ctx, `UPDATE events SET merged_into=NULL,merge_source=NULL,merge_reason=NULL,merged_at=NULL WHERE merged_into=? AND merge_source=?`, id, MergeSourceRule); err != nil {
		return AdminEvent{}, fmt.Errorf("%w: merges", ErrUnavailable)
	}
	if err = bumpRevision(ctx, tx, now); err != nil {
		return AdminEvent{}, fmt.Errorf("%w: revision", ErrUnavailable)
	}
	if err = tx.Commit(); err != nil {
		return AdminEvent{}, fmt.Errorf("%w: commit", ErrUnavailable)
	}
	return s.event(ctx, id)
}

func (s *Store) event(ctx context.Context, id string) (AdminEvent, error) {
	var e AdminEvent
	var locked int
	err := s.db.QueryRowContext(ctx, `SELECT id,source,source_item_id,source_key,title,detail,starts_on,starts_at,ends_at,timezone,time_precision,status,event_type,url,provenance,manual_locked,cancellation_status,cancellation_source,cancellation_reason,cancellation_evidence,cancellation_source_item_id,cancellation_confidence,cancellation_at,deleted_at,created_at,updated_at,merged_into,(SELECT t.title FROM events t WHERE t.id=events.merged_into),merge_source,merge_reason,merged_at FROM events WHERE id=?`, id).Scan(&e.ID, &e.Source, &e.SourceItemID, &e.SourceKey, &e.Title, &e.Detail, &e.StartsOn, &e.StartsAt, &e.EndsAt, &e.Timezone, &e.TimePrecision, &e.Status, &e.EventType, &e.URL, &e.Provenance, &locked, &e.CancellationStatus, &e.CancellationSource, &e.CancellationReason, &e.CancellationEvidence, &e.CancellationSourceItemID, &e.CancellationConfidence, &e.CancellationAt, &e.DeletedAt, &e.CreatedAt, &e.UpdatedAt, &e.MergedInto, &e.MergedIntoTitle, &e.MergeSource, &e.MergeReason, &e.MergedAt)
	e.ManualLocked = locked != 0
	return e, err
}

func (s *Store) GetAdminEvent(ctx context.Context, id string) (AdminEvent, error) {
	return s.event(ctx, id)
}
