package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	ytsource "github.com/yexca/kano-navi/backend-go/internal/source/youtube"
)

func (s *Store) YoutubeRecentState(ctx context.Context, channel string) (ytsource.State, error) {
	out := ytsource.State{Videos: map[string]ytsource.Video{}}
	var meta string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(cursor_id,''),COALESCE(cursor_time,''),COALESCE(metadata_json,'{}') FROM sync_state WHERE source='youtube' AND account_id=?`, channel).Scan(&out.CursorID, &out.CursorTime, &meta)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, ErrUnavailable
	}
	var data struct {
		Checked []string `json:"streamDetailsCheckedIds"`
	}
	_ = json.Unmarshal([]byte(meta), &data)
	out.CheckedIDs = data.Checked
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(metadata_json,'{}') FROM sync_state WHERE source='youtube-details' AND account_id=?`, channel).Scan(&meta)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, ErrUnavailable
	}
	var queue struct {
		IDs []string `json:"queue"`
	}
	_ = json.Unmarshal([]byte(meta), &queue)
	out.Queue = queue.IDs
	// Feed/streams are bounded, but a large history must not be loaded just to
	// refresh the small active reservation set.
	rows, err := s.db.QueryContext(ctx, `SELECT id,title,COALESCE(published_at,''),COALESCE(scheduled_at,''),url,COALESCE(thumbnail_url,''),COALESCE(kind,''),is_upcoming,is_premiere FROM videos WHERE source='youtube' ORDER BY is_upcoming DESC,julianday(COALESCE(scheduled_at,published_at)) DESC,id LIMIT 200`)
	if err != nil {
		return out, ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var v ytsource.Video
		var premiere sql.NullBool
		if rows.Scan(&v.ID, &v.Title, &v.PublishedAt, &v.ScheduledAt, &v.URL, &v.ThumbnailURL, &v.Kind, &v.IsUpcoming, &premiere) != nil {
			return out, ErrUnavailable
		}
		v.Premiere, v.PremiereKnown = premiere.Bool, premiere.Valid
		out.Videos[v.ID] = v
		t, _ := time.Parse(time.RFC3339Nano, v.ScheduledAt)
		if v.IsUpcoming || !t.IsZero() && t.After(time.Now().Add(-24*time.Hour)) {
			if len(out.ActiveIDs) < 30 {
				out.ActiveIDs = append(out.ActiveIDs, v.ID)
			}
		}
	}
	if rows.Err() != nil {
		return out, ErrUnavailable
	}
	rows.Close()
	if out.RefreshIDs, out.FollowUpIDs, err = s.youtubeRefreshWork(ctx, time.Now(), out.Videos); err != nil {
		return out, err
	}
	return out, nil
}
func (s *Store) WriteYoutubeRecent(ctx context.Context, channel string, result ytsource.Result) error {
	if s == nil || !s.workflowWritable {
		return ErrSourceWrite
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrSourceWrite
	}
	defer tx.Rollback()
	if err = fenceSourceWrite(ctx, tx); err != nil {
		return err
	}
	now := workflowTime(time.Now())
	if _, err = writeYoutubeVideos(ctx, tx, result.Videos, now); err != nil {
		return err
	}
	shorts, err := writeYoutubeShorts(ctx, tx, result.Shorts)
	if err != nil {
		return err
	}
	meta, _ := json.Marshal(map[string]any{"streamDetailsCheckedIds": result.CheckedIDs, "feedEntries": result.Discovered, "detailRequested": result.Requested, "inspected": result.Inspected})
	_, err = tx.ExecContext(ctx, `INSERT INTO sync_state(source,account_id,cursor_id,cursor_time,last_success_at,metadata_json) VALUES('youtube',?,?,?,?,?) ON CONFLICT(source,account_id) DO UPDATE SET cursor_id=CASE WHEN julianday(excluded.cursor_time)>COALESCE(julianday(sync_state.cursor_time),0) THEN excluded.cursor_id ELSE sync_state.cursor_id END,cursor_time=CASE WHEN julianday(excluded.cursor_time)>COALESCE(julianday(sync_state.cursor_time),0) THEN excluded.cursor_time ELSE sync_state.cursor_time END,last_success_at=excluded.last_success_at,metadata_json=excluded.metadata_json`, channel, result.NextCursor, result.NextCursorTime, now, string(meta))
	if err != nil {
		return ErrSourceWrite
	}
	if err = writeYoutubeQueue(ctx, tx, channel, result.Queue); err != nil {
		return err
	}
	if len(result.Videos) > 0 || shorts {
		if err = bumpRevision(ctx, tx, now); err != nil {
			return ErrSourceWrite
		}
	}
	if tx.Commit() != nil {
		return ErrSourceWrite
	}
	return nil
}

// SaveYoutubeQueue stores only the rotated detail queue of a run that failed,
// so its cursor, metadata and snapshots stay as they were.
func (s *Store) SaveYoutubeQueue(ctx context.Context, channel string, queue []string) error {
	if s == nil || !s.workflowWritable {
		return ErrSourceWrite
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrSourceWrite
	}
	defer tx.Rollback()
	if err = fenceSourceWrite(ctx, tx); err != nil {
		return err
	}
	if err = writeYoutubeQueue(ctx, tx, channel, queue); err != nil {
		return err
	}
	if tx.Commit() != nil {
		return ErrSourceWrite
	}
	return nil
}

func writeYoutubeQueue(ctx context.Context, tx *sql.Tx, channel string, queue []string) error {
	encoded, _ := json.Marshal(map[string]any{"queue": queue})
	if _, err := tx.ExecContext(ctx, `INSERT INTO sync_state(source,account_id,metadata_json) VALUES('youtube-details',?,?) ON CONFLICT(source,account_id) DO UPDATE SET metadata_json=excluded.metadata_json`, channel, string(encoded)); err != nil {
		return ErrSourceWrite
	}
	return nil
}

// writeYoutubeShorts records the feed's format verdict on stored YouTube
// rows, after writeYoutubeVideos has added the new ones. Only the feed writes
// is_short, so a watch-page read never clears it, and a row the feed never
// listed with a format link stays unknown. It reports whether a row changed.
func writeYoutubeShorts(ctx context.Context, tx *sql.Tx, shorts map[string]bool) (bool, error) {
	changed := false
	for id, short := range shorts {
		written, err := tx.ExecContext(ctx, `UPDATE videos SET is_short=? WHERE id=? AND source='youtube' AND is_short IS NOT ?`, short, id, short)
		if err != nil {
			return false, ErrSourceWrite
		}
		count, err := written.RowsAffected()
		if err != nil {
			return false, ErrSourceWrite
		}
		changed = changed || count > 0
	}
	return changed, nil
}

// youtubeVideoState is the public part of a video row, compared to tell
// whether a write changed what the board shows.
const youtubeVideoState = `SELECT title||char(31)||COALESCE(published_at,'')||char(31)||COALESCE(scheduled_at,'')||char(31)||url||char(31)||COALESCE(thumbnail_url,'')||char(31)||COALESCE(kind,'')||char(31)||COALESCE(is_upcoming,0)||char(31)||COALESCE(live_status,'') FROM videos WHERE id=?`

// youtubeEventFields derives a reservation event's type, detail and status.
// The status is a code the board translates (scheduled, live or ended) and the
// detail names the YouTube format; members-only is carried by the type. A
// broadcast the watch page reports as live is never "ended".
func youtubeEventFields(v ytsource.Video) (typeName, detail, status string) {
	typeName, detail, status = "stream", "YouTube Live", "ended"
	if strings.Contains(v.Title, "メンバー") || strings.Contains(strings.ToLower(v.Title), "membership") || strings.Contains(v.Title, "限定") {
		typeName = "member"
	}
	switch {
	case v.LiveNow:
		status = "live"
	case v.IsUpcoming:
		status = "scheduled"
	}
	// A premiere is a release, so it is never presumed live past its end.
	if v.Premiere {
		typeName, detail = "release", "YouTube Premiere"
	}
	return typeName, detail, status
}

// writeYoutubeVideos upserts checked videos and their reservation events. A
// feed-only entry never replaces a stored video. Manual locks and deletion
// tombstones survive. It reports whether a public video or event field
// changed.
func writeYoutubeVideos(ctx context.Context, tx *sql.Tx, videos []ytsource.Video, now string) (bool, error) {
	changed := false
	for _, v := range videos {
		var before string
		if err := tx.QueryRowContext(ctx, youtubeVideoState, v.ID).Scan(&before); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return false, ErrSourceWrite
		}
		prior, existed, err := readVideoSource(ctx, tx, v.ID)
		if err != nil {
			return false, ErrSourceWrite
		}
		// A check that cannot tell a premiere from a stream keeps the stored
		// verdict, which a later check that can tell replaces.
		var storedPremiere sql.NullBool
		if err = tx.QueryRowContext(ctx, `SELECT is_premiere FROM videos WHERE id=?`, v.ID).Scan(&storedPremiere); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return false, ErrSourceWrite
		}
		var premiere any
		if v.PremiereKnown {
			premiere = v.Premiere
		} else if storedPremiere.Valid {
			v.Premiere = storedPremiere.Bool
		}
		// A detail check records the broadcast state the page reported, or
		// clears it when the page names none, and answers an operator's
		// refresh request.
		var checkedAt, duration any
		if !v.FeedOnly {
			checkedAt = now
		}
		if v.DurationSeconds > 0 {
			duration = v.DurationSeconds
		}
		written, err := tx.ExecContext(ctx, `INSERT INTO videos(id,source,title,published_at,scheduled_at,url,thumbnail_url,kind,is_upcoming,live_status,live_checked_at,duration_seconds,is_premiere,raw_json) VALUES(?,'youtube',?,?,?,?,?,?,?,?,?,?,?,'{}') ON CONFLICT(id) DO UPDATE SET title=excluded.title,published_at=COALESCE(excluded.published_at,videos.published_at),scheduled_at=COALESCE(excluded.scheduled_at,videos.scheduled_at),url=excluded.url,thumbnail_url=excluded.thumbnail_url,kind=excluded.kind,is_upcoming=excluded.is_upcoming,live_status=excluded.live_status,live_checked_at=excluded.live_checked_at,duration_seconds=COALESCE(excluded.duration_seconds,videos.duration_seconds),is_premiere=COALESCE(excluded.is_premiere,videos.is_premiere),refresh_requested_at=CASE WHEN excluded.live_checked_at IS NULL THEN videos.refresh_requested_at END WHERE ?=0`, v.ID, v.Title, nullable(v.PublishedAt), nullable(v.ScheduledAt), v.URL, nullable(v.ThumbnailURL), v.Kind, v.IsUpcoming, nullable(v.LiveStatus), checkedAt, duration, premiere, v.FeedOnly)
		if err != nil {
			return false, ErrSourceWrite
		}
		if count, e := written.RowsAffected(); e != nil {
			return false, ErrSourceWrite
		} else if count == 0 {
			continue
		}
		var after string
		if err = tx.QueryRowContext(ctx, youtubeVideoState, v.ID).Scan(&after); err != nil {
			return false, ErrSourceWrite
		}
		changed = changed || after != before
		if v.ThumbnailURL != "" {
			if err = replaceMediaLink(ctx, tx, "youtube", v.ThumbnailURL, "video", v.ID, "thumbnail", 0, now); err != nil {
				return false, ErrSourceWrite
			}
		}
		// A creator's new title or thumbnail keeps the replaced one as history.
		// Another size or host of the same image is not a replacement.
		if existed && (prior.Title != v.Title || prior.ThumbnailURL != v.ThumbnailURL && !ytsource.SameImage(prior.ThumbnailURL, v.ThumbnailURL)) {
			if err = recordVideoRevision(ctx, tx, v.ID, prior, revisionSync, now); err != nil {
				return false, ErrSourceWrite
			}
		}
		if v.ScheduledAt != "" {
			start, e := time.Parse(time.RFC3339Nano, v.ScheduledAt)
			if e != nil {
				return false, ErrSourceWrite
			}
			typeName, detail, status := youtubeEventFields(v)
			eventID := "youtube-" + v.ID
			// Reuse legacy evidence identities, including locked rows and deleted
			// rows. A new canonical ID must not resurrect a different old ID.
			var existingID string
			identityErr := tx.QueryRowContext(ctx, `SELECT id FROM events WHERE source='youtube' AND source_item_id=? AND source_key='reservation' UNION ALL SELECT e.id FROM event_sources es JOIN events e ON e.id=es.event_id WHERE es.source='youtube' AND es.source_item_id=? AND es.source_key='reservation' LIMIT 1`, v.ID, v.ID).Scan(&existingID)
			if identityErr != nil && !errors.Is(identityErr, sql.ErrNoRows) {
				return false, ErrSourceWrite
			}
			if existingID != "" {
				eventID = existingID
			}
			// An answer that could not call the broadcast a premiere or a stream
			// keeps the stored event's type and detail, and creates none.
			if v.Unclassified && !storedPremiere.Valid {
				err = tx.QueryRowContext(ctx, `SELECT event_type,COALESCE(detail,'') FROM events WHERE id=?`, eventID).Scan(&typeName, &detail)
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				if err != nil {
					return false, ErrSourceWrite
				}
			}
			// Manual locks and deletion tombstones survive every source refresh.
			var event sql.Result
			event, err = tx.ExecContext(ctx, `INSERT INTO events(id,source,source_item_id,source_key,title,detail,starts_on,starts_at,ends_at,timezone,time_precision,status,event_type,url,is_upcoming,provenance,manual_locked,created_at,updated_at,raw_json) VALUES(?,'youtube',?,'reservation',?,?,?,?,?,'Asia/Tokyo','exact',?,?,?,?,'automatic',0,?,?,'{}') ON CONFLICT(id) DO UPDATE SET title=excluded.title,detail=excluded.detail,starts_on=excluded.starts_on,starts_at=excluded.starts_at,ends_at=COALESCE(excluded.ends_at,events.ends_at),status=excluded.status,event_type=excluded.event_type,url=excluded.url,is_upcoming=excluded.is_upcoming,updated_at=excluded.updated_at WHERE events.manual_locked=0 AND events.deleted_at IS NULL AND (events.title IS NOT excluded.title OR events.detail IS NOT excluded.detail OR events.starts_on IS NOT excluded.starts_on OR events.starts_at IS NOT excluded.starts_at OR events.ends_at IS NOT COALESCE(excluded.ends_at,events.ends_at) OR events.status IS NOT excluded.status OR events.event_type IS NOT excluded.event_type OR events.url IS NOT excluded.url OR events.is_upcoming IS NOT excluded.is_upcoming)`, eventID, v.ID, v.Title, detail, start.In(time.FixedZone("JST", 9*3600)).Format("2006-01-02"), v.ScheduledAt, nullable(v.EndedAt), status, typeName, v.URL, v.IsUpcoming, now, now)
			if err != nil {
				return false, ErrSourceWrite
			}
			if count, e := event.RowsAffected(); e != nil {
				return false, ErrSourceWrite
			} else if count > 0 {
				changed = true
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO event_sources(event_id,source,source_item_id,source_key,url,raw_json,created_at,updated_at) SELECT ?,'youtube',?,'reservation',?,'{}',?,? WHERE NOT EXISTS(SELECT 1 FROM event_sources WHERE source='youtube' AND source_item_id=? AND source_key='reservation')`, eventID, v.ID, v.URL, now, now, v.ID)
			if err != nil {
				return false, ErrSourceWrite
			}
		}
	}
	return changed, nil
}
