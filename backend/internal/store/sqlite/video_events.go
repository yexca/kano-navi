package sqlite

import (
	"context"
	"database/sql"
	"time"

	ytsource "github.com/yexca/kano-navi/backend/internal/source/youtube"
)

// unclassifiedStreamSeconds is the shortest ended broadcast without a
// premiere verdict that is taken for a stream. Premieres on the channel are
// songs and Shorts, minutes long; a broadcast of half an hour or more is a
// stream.
const unclassifiedStreamSeconds = 30 * 60

// settleVideoEvents derives the YouTube events of the stored videos whose
// start falls in the inclusive Japan dates from..to, inside the reconcile
// transaction, so either source order converges:
//
//   - An automatic, unlocked YouTube event takes its start from its video: a
//     reservation's start, or the publication time of an upload or Short. An
//     X post that links the video names only its date or an announced time.
//   - An ended broadcast with a start but without any event identity gets its
//     reservation event, as the YouTube writer would write it. A broadcast
//     without a premiere verdict, which an unreadable watch page leaves, counts
//     as a stream only when it lasted at least 30 minutes.
//
// Manual locks and tombstones, and every event or evidence row of a video,
// keep the YouTube writer's identity rules: nothing is resurrected or
// written twice. It reports how many events it timed and created.
func settleVideoEvents(ctx context.Context, tx *sql.Tx, from, to, now string) (timed, created int, err error) {
	start, end, err := japanDateBounds(from, to)
	if err != nil {
		return 0, 0, err
	}
	timed, err = timeVideoEvents(ctx, tx, start, end, now)
	if err != nil {
		return 0, 0, err
	}
	created, err = createBroadcastEvents(ctx, tx, start, end, now)
	return timed, created, err
}

// japanDateBounds turns inclusive Japan dates into UTC instants [start, end).
func japanDateBounds(from, to string) (string, string, error) {
	japan := time.FixedZone("JST", 9*3600)
	first, err := time.ParseInLocation("2006-01-02", from, japan)
	if err != nil {
		return "", "", err
	}
	last, err := time.ParseInLocation("2006-01-02", to, japan)
	if err != nil {
		return "", "", err
	}
	return ISOTime(first), ISOTime(last.AddDate(0, 0, 1)), nil
}

func timeVideoEvents(ctx context.Context, tx *sql.Tx, start, end, now string) (int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT e.id,e.starts_on,COALESCE(e.starts_at,''),e.time_precision,COALESCE(e.ends_at,''),COALESCE(v.scheduled_at,v.published_at)
		FROM events e JOIN videos v ON v.id=e.source_item_id AND v.source='youtube'
		WHERE e.source='youtube' AND e.source_key='reservation' AND e.provenance='automatic' AND e.manual_locked=0 AND e.deleted_at IS NULL
		AND julianday(COALESCE(v.scheduled_at,v.published_at))>=julianday(?) AND julianday(COALESCE(v.scheduled_at,v.published_at))<julianday(?)
		ORDER BY e.id`, start, end)
	if err != nil {
		return 0, err
	}
	type change struct{ id, day, at string }
	changes := []change{}
	for rows.Next() {
		var id, day, startsAt, precision, endsAt, anchor string
		if err = rows.Scan(&id, &day, &startsAt, &precision, &endsAt, &anchor); err != nil {
			rows.Close()
			return 0, err
		}
		instant, ok := parseMergeInstant(anchor)
		if !ok {
			continue
		}
		japanDay := instant.In(time.FixedZone("JST", 9*3600)).Format("2006-01-02")
		stored, hasStart := parseMergeInstant(startsAt)
		if hasStart && stored.Equal(instant) && precision == "exact" && day == japanDay {
			continue
		}
		changes = append(changes, change{id: id, day: japanDay, at: anchor})
	}
	if err = rows.Close(); err != nil {
		return 0, err
	}
	if err = rows.Err(); err != nil {
		return 0, err
	}
	for _, c := range changes {
		// An end the old start implied cannot precede the video's start.
		if _, err = tx.ExecContext(ctx, `UPDATE events SET starts_on=?,starts_at=?,time_precision='exact',
			ends_at=CASE WHEN julianday(ends_at)<julianday(?) THEN NULL ELSE ends_at END,updated_at=?
			WHERE id=? AND provenance='automatic' AND manual_locked=0 AND deleted_at IS NULL`, c.day, c.at, c.at, now, c.id); err != nil {
			return 0, err
		}
	}
	return len(changes), nil
}

func createBroadcastEvents(ctx context.Context, tx *sql.Tx, start, end, now string) (int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT v.id,v.title,v.scheduled_at,v.url,v.is_premiere FROM videos v
		WHERE v.source='youtube' AND v.scheduled_at IS NOT NULL AND COALESCE(v.is_upcoming,0)=0 AND v.live_status='ended'
		AND julianday(v.scheduled_at)>=julianday(?) AND julianday(v.scheduled_at)<julianday(?)
		AND (v.is_premiere IS NOT NULL OR COALESCE(v.duration_seconds,0)>=?)
		AND NOT EXISTS(SELECT 1 FROM events e WHERE e.id='youtube-'||v.id OR e.source='youtube' AND e.source_item_id=v.id)
		AND NOT EXISTS(SELECT 1 FROM event_sources s WHERE s.source='youtube' AND s.source_item_id=v.id)
		ORDER BY v.id`, start, end, unclassifiedStreamSeconds)
	if err != nil {
		return 0, err
	}
	videos := []ytsource.Video{}
	for rows.Next() {
		var v ytsource.Video
		var premiere sql.NullBool
		if err = rows.Scan(&v.ID, &v.Title, &v.ScheduledAt, &v.URL, &premiere); err != nil {
			rows.Close()
			return 0, err
		}
		v.Premiere = premiere.Bool
		videos = append(videos, v)
	}
	if err = rows.Close(); err != nil {
		return 0, err
	}
	if err = rows.Err(); err != nil {
		return 0, err
	}
	for _, v := range videos {
		instant, ok := parseMergeInstant(v.ScheduledAt)
		if !ok {
			continue
		}
		typeName, detail, status := youtubeEventFields(v)
		eventID := "youtube-" + v.ID
		if _, err = tx.ExecContext(ctx, `INSERT INTO events(id,source,source_item_id,source_key,title,detail,starts_on,starts_at,timezone,time_precision,status,event_type,url,is_upcoming,provenance,manual_locked,created_at,updated_at,raw_json)
			VALUES(?,'youtube',?,'reservation',?,?,?,?,'Asia/Tokyo','exact',?,?,?,0,'automatic',0,?,?,'{}')`,
			eventID, v.ID, v.Title, detail, instant.In(time.FixedZone("JST", 9*3600)).Format("2006-01-02"), v.ScheduledAt, status, typeName, v.URL, now, now); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO event_sources(event_id,source,source_item_id,source_key,url,raw_json,created_at,updated_at) VALUES(?,'youtube',?,'reservation',?,'{}',?,?)`,
			eventID, v.ID, v.URL, now, now); err != nil {
			return 0, err
		}
	}
	return len(videos), nil
}
