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
	rows, err := s.db.QueryContext(ctx, `SELECT id,title,COALESCE(published_at,''),COALESCE(scheduled_at,''),url,COALESCE(thumbnail_url,''),COALESCE(kind,''),is_upcoming FROM videos WHERE source='youtube' ORDER BY is_upcoming DESC,julianday(COALESCE(scheduled_at,published_at)) DESC,id LIMIT 200`)
	if err != nil {
		return out, ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var v ytsource.Video
		if rows.Scan(&v.ID, &v.Title, &v.PublishedAt, &v.ScheduledAt, &v.URL, &v.ThumbnailURL, &v.Kind, &v.IsUpcoming) != nil {
			return out, ErrUnavailable
		}
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
	for _, v := range result.Videos {
		var written sql.Result
		written, err = tx.ExecContext(ctx, `INSERT INTO videos(id,source,title,published_at,scheduled_at,url,thumbnail_url,kind,is_upcoming,raw_json) VALUES(?,'youtube',?,?,?,?,?,?,?,'{}') ON CONFLICT(id) DO UPDATE SET title=excluded.title,published_at=COALESCE(excluded.published_at,videos.published_at),scheduled_at=COALESCE(excluded.scheduled_at,videos.scheduled_at),url=excluded.url,thumbnail_url=excluded.thumbnail_url,kind=excluded.kind,is_upcoming=excluded.is_upcoming WHERE ?=0`, v.ID, v.Title, nullable(v.PublishedAt), nullable(v.ScheduledAt), v.URL, nullable(v.ThumbnailURL), v.Kind, v.IsUpcoming, v.FeedOnly)
		if err != nil {
			return ErrSourceWrite
		}
		if count, e := written.RowsAffected(); e != nil {
			return ErrSourceWrite
		} else if count == 0 {
			continue
		}
		if v.ThumbnailURL != "" {
			if err = replaceMediaLink(ctx, tx, "youtube", v.ThumbnailURL, "video", v.ID, "thumbnail", 0, now); err != nil {
				return ErrSourceWrite
			}
		}
		if v.ScheduledAt != "" {
			start, e := time.Parse(time.RFC3339Nano, v.ScheduledAt)
			if e != nil {
				return ErrSourceWrite
			}
			typeName, detail, status := "stream", "YouTube Live / 预约", "已完成"
			member := strings.Contains(v.Title, "メンバー") || strings.Contains(strings.ToLower(v.Title), "membership") || strings.Contains(v.Title, "限定")
			if member {
				typeName = "member"
				detail = "メンバーシップ限定 / YouTube"
			}
			if v.IsUpcoming {
				status = "已预约"
				if member {
					status = "会员限定"
				}
			}
			// A premiere is a release, so it is never presumed live past its end.
			if v.Premiere {
				typeName, detail = "release", "YouTube Premiere / 预约"
			}
			eventID := "youtube-" + v.ID
			// Reuse legacy evidence identities, including locked rows and deleted
			// rows. A new canonical ID must not resurrect a different old ID.
			var existingID string
			identityErr := tx.QueryRowContext(ctx, `SELECT id FROM events WHERE source='youtube' AND source_item_id=? AND source_key='reservation' UNION ALL SELECT e.id FROM event_sources es JOIN events e ON e.id=es.event_id WHERE es.source='youtube' AND es.source_item_id=? AND es.source_key='reservation' LIMIT 1`, v.ID, v.ID).Scan(&existingID)
			if identityErr != nil && !errors.Is(identityErr, sql.ErrNoRows) {
				return ErrSourceWrite
			}
			if existingID != "" {
				eventID = existingID
			}
			// Manual locks and deletion tombstones survive every source refresh.
			_, err = tx.ExecContext(ctx, `INSERT INTO events(id,source,source_item_id,source_key,title,detail,starts_on,starts_at,ends_at,timezone,time_precision,status,event_type,url,is_upcoming,provenance,manual_locked,created_at,updated_at,raw_json) VALUES(?,'youtube',?,'reservation',?,?,?,?,?,'Asia/Tokyo','exact',?,?,?,?,'automatic',0,?,?,'{}') ON CONFLICT(id) DO UPDATE SET title=excluded.title,detail=excluded.detail,starts_on=excluded.starts_on,starts_at=excluded.starts_at,ends_at=COALESCE(excluded.ends_at,events.ends_at),status=excluded.status,event_type=excluded.event_type,url=excluded.url,is_upcoming=excluded.is_upcoming,updated_at=excluded.updated_at WHERE events.manual_locked=0 AND events.deleted_at IS NULL`, eventID, v.ID, v.Title, detail, start.In(time.FixedZone("JST", 9*3600)).Format("2006-01-02"), v.ScheduledAt, nullable(v.EndedAt), status, typeName, v.URL, v.IsUpcoming, now, now)
			if err != nil {
				return ErrSourceWrite
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO event_sources(event_id,source,source_item_id,source_key,url,raw_json,created_at,updated_at) SELECT ?,'youtube',?,'reservation',?,'{}',?,? WHERE NOT EXISTS(SELECT 1 FROM event_sources WHERE source='youtube' AND source_item_id=? AND source_key='reservation')`, eventID, v.ID, v.URL, now, now, v.ID)
			if err != nil {
				return ErrSourceWrite
			}
		}
	}
	meta, _ := json.Marshal(map[string]any{"streamDetailsCheckedIds": result.CheckedIDs, "feedEntries": result.Discovered, "detailRequested": result.Requested, "inspected": result.Inspected})
	_, err = tx.ExecContext(ctx, `INSERT INTO sync_state(source,account_id,cursor_id,cursor_time,last_success_at,metadata_json) VALUES('youtube',?,?,?,?,?) ON CONFLICT(source,account_id) DO UPDATE SET cursor_id=CASE WHEN julianday(excluded.cursor_time)>COALESCE(julianday(sync_state.cursor_time),0) THEN excluded.cursor_id ELSE sync_state.cursor_id END,cursor_time=CASE WHEN julianday(excluded.cursor_time)>COALESCE(julianday(sync_state.cursor_time),0) THEN excluded.cursor_time ELSE sync_state.cursor_time END,last_success_at=excluded.last_success_at,metadata_json=excluded.metadata_json`, channel, result.NextCursor, result.NextCursorTime, now, string(meta))
	if err != nil {
		return ErrSourceWrite
	}
	queue, _ := json.Marshal(map[string]any{"queue": result.Queue})
	_, err = tx.ExecContext(ctx, `INSERT INTO sync_state(source,account_id,metadata_json) VALUES('youtube-details',?,?) ON CONFLICT(source,account_id) DO UPDATE SET metadata_json=excluded.metadata_json`, channel, string(queue))
	if err != nil {
		return ErrSourceWrite
	}
	if len(result.Videos) > 0 {
		if err = bumpRevision(ctx, tx, now); err != nil {
			return ErrSourceWrite
		}
	}
	if tx.Commit() != nil {
		return ErrSourceWrite
	}
	return nil
}
