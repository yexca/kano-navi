package schedule

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
)

// applyExtraction writes one accepted model result in a single transaction:
// cancellation targets are resolved first, the post's automatic events are
// replaced, and the cancellation overlay is applied last.
func (e *Extractor) applyExtraction(ctx context.Context, postID string, extractionID int64, result normalizedExtraction) error {
	cancellations := []normalizedEvent{}
	scheduled := []normalizedEvent{}
	for _, event := range result.events {
		if !event.cancellationSuspected {
			scheduled = append(scheduled, event)
		}
		if result.action == "cancel" || event.cancellationSuspected {
			cancellations = append(cancellations, event)
		}
	}
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	targets, err := findCancellationTargets(ctx, tx, cancellations)
	if err != nil {
		return err
	}
	// Cancellation evidence must not retire a source's existing schedules.
	if result.classification == "schedule" && result.action != "cancel" && (len(scheduled) > 0 || len(cancellations) == 0) {
		if err = e.replaceAutomaticEvents(ctx, tx, postID, extractionID, scheduled, targets); err != nil {
			return err
		}
	}
	if len(cancellations) > 0 || result.action == "cancel" {
		reason := result.reason
		if reason == "" {
			reason = result.evidence
		}
		evidence := result.evidence
		if evidence == "" {
			parts := make([]string, len(cancellations))
			for i, event := range cancellations {
				parts[i] = event.evidence
			}
			evidence = strings.Join(parts, "; ")
		}
		if err = e.applyCancellations(ctx, tx, postID, reason, evidence, result.confidence, targets); err != nil {
			return err
		}
	}
	return tx.Commit()
}

var cancellationTitleNoise = regexp.MustCompile(jsSpaceClass + `|[「」『』【】()（）［］\[\]・:：,.，。!?！？]`)

func normalizedCancellationTitle(value string) string {
	return cancellationTitleNoise.ReplaceAllString(jsToLower(value), "")
}

// findCancellationTargets resolves each cancellation candidate to at most one
// active event: same Japan date, same instant when both carry one, then an
// exact title match or the only event of that day. Ambiguity selects nothing.
func findCancellationTargets(ctx context.Context, q queryer, candidates []normalizedEvent) ([]string, error) {
	type activeEvent struct {
		id       string
		title    string
		startsOn string
		startsAt string
	}
	rows, err := q.QueryContext(ctx, `SELECT id, title, starts_on, starts_at FROM events WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	active := []activeEvent{}
	for rows.Next() {
		var event activeEvent
		var title, startsOn, startsAt sql.NullString
		if err = rows.Scan(&event.id, &title, &startsOn, &startsAt); err != nil {
			rows.Close()
			return nil, err
		}
		event.title, event.startsOn, event.startsAt = title.String, startsOn.String, startsAt.String
		active = append(active, event)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	targets := []string{}
	for _, candidate := range candidates {
		title := normalizedCancellationTitle(candidate.title)
		var instant int64
		hasInstant := false
		if candidate.startsAt != nil {
			if parsed, ok := parseInstant(*candidate.startsAt); ok {
				instant, hasInstant = parsed.UnixMilli(), true
			}
		}
		sameDate := []activeEvent{}
		for _, event := range active {
			if event.startsOn != candidate.startsOn {
				continue
			}
			// Stored times mix `Z` and `+09:00` offsets, so compare instants.
			parsed, ok := parseInstant(event.startsAt)
			if hasInstant && ok && parsed.UnixMilli() != instant {
				continue
			}
			sameDate = append(sameDate, event)
		}
		if len(sameDate) == 0 {
			continue
		}
		exact := []activeEvent{}
		if title != "" {
			for _, event := range sameDate {
				if normalizedCancellationTitle(event.title) == title {
					exact = append(exact, event)
				}
			}
		}
		switch {
		case len(exact) == 1:
			targets = append(targets, exact[0].id)
		case len(sameDate) == 1:
			targets = append(targets, sameDate[0].id)
		}
	}
	return targets, nil
}

const upsertEventSQL = `INSERT INTO events (
		id, source, source_item_id, source_key, title, detail, starts_on, starts_at,
		ends_at, timezone, time_precision, status, event_type, url, is_upcoming,
		provenance, manual_locked, confidence, extraction_id,
		cancellation_status, cancellation_source, cancellation_reason,
		cancellation_evidence, cancellation_source_item_id,
		cancellation_confidence, cancellation_at, deleted_at,
		created_at, updated_at, raw_json
	) VALUES (
		?, ?, ?, ?, ?, ?, ?, ?, ?, 'Asia/Tokyo', ?, ?, ?, ?, ?,
		'automatic', 0, ?, ?, 'none', NULL, NULL, NULL, NULL, NULL, NULL, NULL,
		?, ?, ?
	)
	ON CONFLICT(id) DO UPDATE SET
		source=excluded.source, source_item_id=excluded.source_item_id,
		source_key=excluded.source_key, title=excluded.title, detail=excluded.detail,
		starts_on=excluded.starts_on, starts_at=excluded.starts_at,
		ends_at=excluded.ends_at, timezone=excluded.timezone,
		time_precision=excluded.time_precision, status=excluded.status,
		event_type=excluded.event_type, url=excluded.url,
		is_upcoming=excluded.is_upcoming, provenance=excluded.provenance,
		manual_locked=excluded.manual_locked, confidence=excluded.confidence,
		extraction_id=excluded.extraction_id, deleted_at=excluded.deleted_at,
		cancellation_status=CASE WHEN events.cancellation_status IN ('llm_suspected', 'manual_confirmed')
			THEN events.cancellation_status ELSE excluded.cancellation_status END,
		cancellation_source=CASE WHEN events.cancellation_status IN ('llm_suspected', 'manual_confirmed')
			THEN events.cancellation_source ELSE excluded.cancellation_source END,
		cancellation_reason=CASE WHEN events.cancellation_status IN ('llm_suspected', 'manual_confirmed')
			THEN events.cancellation_reason ELSE excluded.cancellation_reason END,
		cancellation_evidence=CASE WHEN events.cancellation_status IN ('llm_suspected', 'manual_confirmed')
			THEN events.cancellation_evidence ELSE excluded.cancellation_evidence END,
		cancellation_source_item_id=CASE WHEN events.cancellation_status IN ('llm_suspected', 'manual_confirmed')
			THEN events.cancellation_source_item_id ELSE excluded.cancellation_source_item_id END,
		cancellation_confidence=CASE WHEN events.cancellation_status IN ('llm_suspected', 'manual_confirmed')
			THEN events.cancellation_confidence ELSE excluded.cancellation_confidence END,
		cancellation_at=CASE WHEN events.cancellation_status IN ('llm_suspected', 'manual_confirmed')
			THEN events.cancellation_at ELSE excluded.cancellation_at END,
		updated_at=excluded.updated_at, raw_json=excluded.raw_json
	WHERE events.manual_locked = 0`

func nullableText(value *string) any {
	if value == nil || *value == "" {
		return nil
	}
	return *value
}

// replaceAutomaticEvents mirrors replaceAutomaticEventsForSource for the X
// source scope. The upsert is guarded by manual_locked = 0, so an operator's
// edit or tombstone keeps its fields while the source evidence row is still
// attached to it; events the post no longer lists are retired only when they
// are automatic, unlocked and have no other live source.
func (e *Extractor) replaceAutomaticEvents(ctx context.Context, tx *sql.Tx, postID string, extractionID int64, events []normalizedEvent, preserve []string) error {
	const source = "x"
	timestamp := e.nowISO()
	activeKeys := map[string]bool{}
	for _, event := range events {
		activeKeys[event.sourceKey] = true
		eventID := event.id
		if event.canonicalSource == source {
			// Source evidence survives manual edits and tombstones. Reuse its ID
			// across posts as well as across the old and new ID schemes.
			var previous string
			err := tx.QueryRowContext(ctx, `SELECT s.event_id FROM event_sources s
				JOIN events e ON e.id=s.event_id
				WHERE s.source=? AND s.source_key=? AND e.source='x' AND e.source_key=?
				ORDER BY (s.source_item_id=?) DESC, s.id ASC LIMIT 1`,
				source, event.sourceKey, event.canonicalSourceKey, postID).Scan(&previous)
			if err == nil {
				eventID = previous
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		raw := event.rawObject()
		raw.set("id", eventID)
		raw.set("source_key", event.canonicalSourceKey)
		raw.set("source", event.canonicalSource)
		raw.set("source_item_id", event.canonicalSourceItemID)
		raw.set("provenance", "automatic")
		raw.set("manual_locked", 0)
		raw.set("extraction_id", extractionID)
		raw.set("deleted_at", nil)
		// The persisted source identity wins over a newly generated ID, including
		// legacy date-only IDs and locked/deleted rows. Never insert a second row
		// for a tuple already protected by the unique source index.
		var existing string
		err := tx.QueryRowContext(ctx, `SELECT id FROM events WHERE source=? AND source_item_id=? AND source_key=?`,
			event.canonicalSource, event.canonicalSourceItemID, event.canonicalSourceKey).Scan(&existing)
		if err == nil {
			eventID = existing
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		upcoming := 0
		if event.isUpcoming {
			upcoming = 1
		}
		now := e.nowISO()
		if _, err = tx.ExecContext(ctx, upsertEventSQL,
			eventID, event.canonicalSource, event.canonicalSourceItemID, event.canonicalSourceKey,
			event.title, nullableText(event.detail), event.startsOn, nullableText(event.startsAt),
			nullableText(event.endsAt), event.timePrecision, nullable(event.status), event.eventType,
			nullable(event.url), upcoming, event.confidence, extractionID, now, now, stringify(raw)); err != nil {
			return err
		}
		evidence := event.rawObject()
		evidence.set("source", source)
		evidence.set("source_item_id", postID)
		if _, err = tx.ExecContext(ctx, `INSERT INTO event_sources (
				event_id, source, source_item_id, source_key, url, raw_json, deleted_at,
				created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, NULL, ?, ?)
			ON CONFLICT(source, source_item_id, source_key) DO UPDATE SET
				event_id=excluded.event_id,
				url=COALESCE(excluded.url, event_sources.url),
				raw_json=COALESCE(excluded.raw_json, event_sources.raw_json),
				deleted_at=NULL,
				updated_at=excluded.updated_at`,
			eventID, source, postID, event.sourceKey, nullable(event.url), stringify(evidence), timestamp, timestamp); err != nil {
			return err
		}
	}

	type sourceRow struct {
		id        int64
		eventID   string
		sourceKey string
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, event_id, source_key FROM event_sources
		WHERE source = ? AND source_item_id = ? AND deleted_at IS NULL`, source, postID)
	if err != nil {
		return err
	}
	previous := []sourceRow{}
	for rows.Next() {
		var row sourceRow
		if err = rows.Scan(&row.id, &row.eventID, &row.sourceKey); err != nil {
			rows.Close()
			return err
		}
		previous = append(previous, row)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	preserved := map[string]bool{}
	for _, id := range preserve {
		preserved[id] = true
	}
	for _, row := range previous {
		if activeKeys[row.sourceKey] || preserved[row.eventID] {
			continue
		}
		if _, err = tx.ExecContext(ctx, `UPDATE event_sources SET deleted_at=?, updated_at=? WHERE id=?`, timestamp, timestamp, row.id); err != nil {
			return err
		}
		var remaining int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_sources WHERE event_id=? AND deleted_at IS NULL`, row.eventID).Scan(&remaining); err != nil {
			return err
		}
		if remaining == 0 {
			if _, err = tx.ExecContext(ctx, `UPDATE events SET deleted_at=?, updated_at=?
				WHERE id=? AND provenance='automatic' AND manual_locked=0`, timestamp, timestamp, row.eventID); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyCancellations records the model's cancellation judgement as an
// overlay. It never changes schedule fields and never replaces an operator's
// confirmed cancellation; locked events can still be annotated.
func (e *Extractor) applyCancellations(ctx context.Context, tx *sql.Tx, postID, reason, evidence string, confidence float64, targets []string) error {
	timestamp := e.nowISO()
	if reason == "" {
		reason = "LLM 判断该日程可能已取消"
	}
	var storedConfidence any
	if finite(confidence) {
		storedConfidence = confidence
	}
	seen := map[string]bool{}
	for _, eventID := range targets {
		eventID = jsTrim(eventID)
		if eventID == "" || seen[eventID] {
			continue
		}
		seen[eventID] = true
		if _, err := tx.ExecContext(ctx, `UPDATE events SET
				cancellation_status='llm_suspected', cancellation_source='llm',
				cancellation_reason=?, cancellation_evidence=?,
				cancellation_source_item_id=?, cancellation_confidence=?,
				cancellation_at=?, updated_at=?
			WHERE id=? AND deleted_at IS NULL
				AND COALESCE(cancellation_status, 'none') != 'manual_confirmed'`,
			sliceUTF16(reason, 500), nullable(sliceUTF16(evidence, 1000)), nullable(postID),
			storedConfidence, timestamp, timestamp, eventID); err != nil {
			return err
		}
	}
	return nil
}
