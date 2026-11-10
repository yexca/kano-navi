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
	// ErrEventLiveRoomURL refuses a Bilibili live-room link as an entry's
	// link unless the entry is on another platform (other).
	ErrEventLiveRoomURL = errors.New("event_live_room_url")
)

// EventInput is one validated manual schedule entry. An empty string means
// "no value" and is stored as NULL. Timestamps are ISO instants; StartsOn and
// EndsOn are Japan calendar dates. EndsOn is the close date of a sale period
// and is stored for a sale only.
//
// Platforms are the operator's announced platforms (see EventPlatforms);
// PlatformsSet reports that the input names them, so an update without them
// keeps the stored ones.
type EventInput struct {
	ID, Title, Detail, StartsOn, StartsAt, EndsAt, EndsOn, TimePrecision, Status, EventType, Tag, URL, CancellationStatus, CancellationReason string
	Platforms                                                                                                                                 []string
	PlatformsSet                                                                                                                              bool
}

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
	if i.Tag != "" && i.Tag != TagAutomatic && !ValidEventTag(i.Tag) {
		return ErrEventInvalid
	}
	if i.EndsOn != "" {
		if _, err := time.Parse("2006-01-02", i.EndsOn); err != nil || !adminDateRE.MatchString(i.EndsOn) || i.EndsOn < i.StartsOn {
			return ErrEventInvalid
		}
	}
	// An end time needs a start time, except the deadline of a sale period
	// whose opening is not known.
	if i.EndsAt != "" && i.StartsAt == "" && i.EndsOn == "" {
		return ErrEventInvalid
	}
	// A sale's close time always has its close date.
	if i.EndsAt != "" && i.EventType == "sale" && i.EndsOn == "" {
		return ErrEventInvalid
	}
	// A close time falls on its close date in Japan.
	if i.EndsAt != "" && i.EndsOn != "" {
		if end, err := time.Parse(time.RFC3339Nano, i.EndsAt); err != nil || dateInJapan(end) != i.EndsOn {
			return ErrEventInvalid
		}
	}
	if i.CancellationStatus == "manual_confirmed" && strings.TrimSpace(i.CancellationReason) == "" {
		return ErrEventInvalid
	}
	if len(NormalizePlatforms(i.Platforms)) != len(i.Platforms) {
		return ErrEventInvalid
	}
	if (create || i.PlatformsSet) && !i.linkAllowed(i.Platforms) {
		return ErrEventLiveRoomURL
	}
	return nil
}

// linkAllowed reports whether the input's link may be stored with these
// platforms: a live-room link is the same for every broadcast, never an
// entry's link, unless it is someone else's room (other).
func (i EventInput) linkAllowed(platforms []string) bool {
	return !IsBilibiliRoomURL(i.URL) || hasPlatform(platforms, PlatformOther)
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
		"event_type": i.EventType, "tag": nullIfEmpty(i.Tag), "url": nullIfEmpty(i.URL), "provenance": "manual", "manualLocked": true,
	}
	if i.EndsOn != "" {
		value["ends_on"] = i.EndsOn
	}
	if i.PlatformsSet {
		value["platforms"] = NormalizePlatforms(i.Platforms)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

// saleEndsOn is the ends_on column a save writes: the close date for a sale,
// nothing for every other type.
func saleEndsOn(eventType, endsOn string) any {
	if eventType != "sale" {
		return nil
	}
	return nullIfEmpty(endsOn)
}

func nullIfEmpty(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

// manualTagSource marks who decided the tag of a new entry. One without a
// label, or saved as automatic, reads on to its videos' tags like any
// unlabelled row.
func manualTagSource(tag string) any {
	if storedTag(tag) == nil {
		return nil
	}
	return TagSourceManual
}

// storedTag is the tag column an operator's label writes: nothing for no
// label or for TagAutomatic.
func storedTag(tag string) any {
	if strings.TrimSpace(tag) == TagAutomatic {
		return nil
	}
	return nullIfEmpty(tag)
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
	platforms := NormalizePlatforms(input.Platforms)
	if _, err = tx.ExecContext(ctx, `INSERT INTO events(id,source,source_item_id,source_key,title,detail,starts_on,starts_at,ends_at,ends_on,timezone,
 time_precision,status,event_type,tag,tag_source,url,is_upcoming,provenance,manual_locked,cancellation_status,created_at,updated_at,raw_json,platform,platforms,platforms_inferred)
 VALUES(?,'manual',?,?,?,?,?,?,?,?,'Asia/Tokyo',?,?,?,?,?,?,0,'manual',1,'none',?,?,?,?,?,0)`,
		id, id, id, strings.TrimSpace(input.Title), nullIfEmpty(input.Detail), input.StartsOn, nullIfEmpty(input.StartsAt), nullIfEmpty(input.EndsAt), saleEndsOn(eventType, input.EndsOn),
		input.precision(), nullIfEmpty(input.Status), eventType, storedTag(input.Tag), manualTagSource(input.Tag), nullIfEmpty(input.URL), now, now, raw,
		PrimaryPlatform(platforms), EncodePlatforms(platforms)); err != nil {
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
	var title, timezone, eventType, tag, tagSource, status sql.NullString
	var source, reason, evidence, sourceItem, at, storedPlatform, storedList sql.NullString
	var confidence sql.NullFloat64
	err = tx.QueryRowContext(ctx, `SELECT title,timezone,event_type,tag,tag_source,cancellation_status,cancellation_source,cancellation_reason,
 cancellation_evidence,cancellation_source_item_id,cancellation_confidence,cancellation_at,platform,platforms FROM events WHERE id=?`, id).
		Scan(&title, &timezone, &eventType, &tag, &tagSource, &status, &source, &reason, &evidence, &sourceItem, &confidence, &at, &storedPlatform, &storedList)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminEvent{}, sql.ErrNoRows
	}
	if err != nil {
		return AdminEvent{}, fmt.Errorf("%w: event lookup", ErrUnavailable)
	}
	if !input.PlatformsSet && !input.linkAllowed(storedPlatforms(storedList, storedPlatform)) {
		return AdminEvent{}, ErrEventLiveRoomURL
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
	// A save that sends the label back unchanged keeps who decided it, so an
	// unrelated edit or a cancellation decision never turns an automatic
	// label into the operator's. A changed label is the operator's, a cleared
	// one included: the entry then shows no label and holds back the title
	// inference of its videos, so no inference fills it again by itself.
	// TagAutomatic forgets the label, its source and the last attempt, which
	// hands the label back to the automatic layers.
	automatic := strings.TrimSpace(input.Tag) == TagAutomatic
	var newTagSource any = TagSourceManual
	switch {
	case automatic:
		newTagSource = nil
	case strings.TrimSpace(input.Tag) == tag.String:
		newTagSource = nullIfEmpty(tagSource.String)
	}
	args := append([]any{newTitle, nullIfEmpty(input.Detail), input.StartsOn, nullIfEmpty(input.StartsAt), nullIfEmpty(input.EndsAt), saleEndsOn(newType, input.EndsOn), zone,
		input.precision(), nullIfEmpty(input.Status), newType, storedTag(input.Tag), newTagSource, automatic, nullIfEmpty(input.URL)}, cancellation...)
	// The operator's platforms are part of the manual lock; a save without
	// them keeps the stored ones, now the operator's as well.
	platforms := NormalizePlatforms(input.Platforms)
	args = append(args, now, raw, input.PlatformsSet, PrimaryPlatform(platforms), input.PlatformsSet, EncodePlatforms(platforms), id)
	if _, err = tx.ExecContext(ctx, `UPDATE events SET title=?,detail=?,starts_on=?,starts_at=?,ends_at=?,ends_on=?,timezone=?,time_precision=?,status=?,
 event_type=?,tag=?,tag_source=?,tag_attempted_at=CASE WHEN ? THEN NULL ELSE tag_attempted_at END,url=?,provenance='manual',cancellation_status=?,cancellation_source=?,cancellation_reason=?,cancellation_evidence=?,
 cancellation_source_item_id=?,cancellation_confidence=?,cancellation_at=?,manual_locked=1,deleted_at=NULL,updated_at=?,raw_json=?,
 platform=CASE WHEN ? THEN ? ELSE platform END,platforms=CASE WHEN ? THEN ? ELSE platforms END,platforms_inferred=0,
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
// its window and an older duplicate would otherwise stay hidden. An
// operator's merge into it is undone as UnmergeEvent would, so the reconciler
// never merges that event again; other owners' decisions stay.
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
	if _, err = tx.ExecContext(ctx, `UPDATE events SET merged_into=NULL,merge_source=?,merge_reason=?,merged_at=? WHERE merged_into=? AND merge_source=?`,
		MergeSourceManual, "unmerged_from:"+id, now, id, MergeSourceManual); err != nil {
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
	var locked, hiddenByPosts, inferred int
	var platform, platforms, review sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,source,source_item_id,source_key,title,detail,starts_on,starts_at,ends_at,ends_on,timezone,time_precision,status,event_type,tag,tag_source,url,provenance,manual_locked,cancellation_status,cancellation_source,cancellation_reason,cancellation_evidence,cancellation_source_item_id,cancellation_confidence,cancellation_at,deleted_at,created_at,updated_at,merged_into,(SELECT t.title FROM events t WHERE t.id=events.merged_into),merge_source,merge_reason,merged_at,`+eventHiddenByPostsSQL("events")+`,platform,platforms,platforms_inferred,confidence,`+EventAttentionSQL("events", "")+` FROM events WHERE id=?`, id).Scan(&e.ID, &e.Source, &e.SourceItemID, &e.SourceKey, &e.Title, &e.Detail, &e.StartsOn, &e.StartsAt, &e.EndsAt, &e.EndsOn, &e.Timezone, &e.TimePrecision, &e.Status, &e.EventType, &e.Tag, &e.TagSource, &e.URL, &e.Provenance, &locked, &e.CancellationStatus, &e.CancellationSource, &e.CancellationReason, &e.CancellationEvidence, &e.CancellationSourceItemID, &e.CancellationConfidence, &e.CancellationAt, &e.DeletedAt, &e.CreatedAt, &e.UpdatedAt, &e.MergedInto, &e.MergedIntoTitle, &e.MergeSource, &e.MergeReason, &e.MergedAt, &hiddenByPosts, &platform, &platforms, &inferred, &e.Confidence, &review)
	e.ManualLocked = locked != 0
	e.HiddenByPosts = hiddenByPosts != 0
	if err != nil {
		return e, err
	}
	link := ""
	if e.URL != nil {
		link = *e.URL
	}
	e.Platforms, _ = readPlatforms(platforms, platform, link)
	e.PlatformsInferred = inferred != 0
	e.Review = eventReview(review.String, e)
	items := []AdminEvent{e}
	if attachErr := s.attachAdminPlatforms(ctx, items); attachErr != nil {
		return e, fmt.Errorf("%w: event platforms", ErrUnavailable)
	}
	return items[0], nil
}

func (s *Store) GetAdminEvent(ctx context.Context, id string) (AdminEvent, error) {
	return s.event(ctx, id)
}
