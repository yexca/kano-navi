package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	xsource "github.com/yexca/kano-navi/backend-go/internal/source/x"
)

var ErrSourceWrite = errors.New("source write unavailable")

// WriteXRecent commits posts, pending media candidates, schedule candidates and
// incremental state atomically. It never touches historical/window cursors.
func (s *Store) WriteXRecent(ctx context.Context, account string, result xsource.Result, success bool) error {
	if s == nil || s.db == nil || !s.workflowWritable {
		return ErrSourceWrite
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrSourceWrite
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, p := range result.Posts {
		raw, _ := json.Marshal(p.Raw)
		media := ""
		if len(p.MediaURLs) > 0 {
			media = p.MediaURLs[0]
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO posts(id,source,account_handle,type,label,text,published_at,url,likes,reposts,replies,media_url,media_alt,raw_json) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET account_handle=excluded.account_handle,type=excluded.type,label=excluded.label,text=excluded.text,published_at=excluded.published_at,url=excluded.url,likes=excluded.likes,reposts=excluded.reposts,replies=excluded.replies,media_url=excluded.media_url,media_alt=excluded.media_alt,raw_json=excluded.raw_json WHERE lower(posts.account_handle)=lower(excluded.account_handle) OR posts.account_handle IS NULL`, p.ID, "x", account, p.Type, p.Label, p.Text, p.PublishedAt, p.URL, p.Likes, p.Reposts, p.Replies, nullable(media), nullable(p.MediaAlt), string(raw))
		if err != nil {
			return ErrSourceWrite
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM media_links WHERE owner_type='post' AND owner_id=? AND role='post-image'`, p.ID); err != nil {
			return ErrSourceWrite
		}
		for i, u := range p.MediaURLs {
			if err = writeMediaCandidate(ctx, tx, p.ID, u, account, i, now); err != nil {
				return ErrSourceWrite
			}
		}
	}
	for _, c := range result.ScheduleCandidates {
		if c.MediaURL == "" || c.SourceURL == "" {
			continue
		}
		if err = writeScheduleCandidate(ctx, tx, c, now); err != nil {
			return ErrSourceWrite
		}
	}
	var oldMetadata string
	_ = tx.QueryRowContext(ctx, "SELECT COALESCE(metadata_json,'') FROM sync_state WHERE source='x' AND account_id=?", account).Scan(&oldMetadata)
	pending := append([]string(nil), result.PendingIDs...)
	if oldMetadata != "" {
		var old map[string]any
		if json.Unmarshal([]byte(oldMetadata), &old) == nil {
			if values, ok := old["pendingStatusIds"].([]any); ok {
				seen := map[string]bool{}
				for _, id := range pending {
					seen[id] = true
				}
				for _, value := range values {
					if id, ok := value.(string); ok && !seen[id] {
						pending = append(pending, id)
						seen[id] = true
					}
				}
			}
		}
	}
	for _, p := range result.Posts {
		for i, id := range pending {
			if id == p.ID {
				pending = append(pending[:i], pending[i+1:]...)
				break
			}
		}
	}
	metadata := map[string]any{"handle": account, "discovered": result.DiscoveredCount, "requested": result.RequestedCount, "pendingStatusIds": pending}
	metaBytes, _ := json.Marshal(metadata)
	var oldID, oldTime string
	_ = tx.QueryRowContext(ctx, "SELECT COALESCE(cursor_id,''),COALESCE(cursor_time,'') FROM sync_state WHERE source='x' AND account_id=?", account).Scan(&oldID, &oldTime)
	cursorID, cursorTime := oldID, oldTime
	if success && result.NextCursor != "" && (oldTime == "" || result.NextCursorTime > oldTime) {
		cursorID, cursorTime = result.NextCursor, result.NextCursorTime
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sync_state(source,account_id,cursor_id,cursor_time,last_success_at,metadata_json) VALUES('x',?,?,?,?,?) ON CONFLICT(source,account_id) DO UPDATE SET cursor_id=excluded.cursor_id,cursor_time=excluded.cursor_time,last_success_at=CASE WHEN excluded.last_success_at IS NULL THEN sync_state.last_success_at ELSE excluded.last_success_at END,metadata_json=excluded.metadata_json`, account, nullable(cursorID), nullable(cursorTime), func() any {
		if success {
			return now
		}
		return nil
	}(), string(metaBytes))
	if err != nil {
		return ErrSourceWrite
	}
	if err = tx.Commit(); err != nil {
		return ErrSourceWrite
	}
	return nil
}
func nullable(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}
func writeMediaCandidate(ctx context.Context, tx *sql.Tx, postID, url, account string, pos int, now string) error {
	h := sha256.Sum256([]byte(url))
	id := "x-" + hex.EncodeToString(h[:16])
	if _, e := tx.ExecContext(ctx, `INSERT INTO media_assets(id,source,source_url,status,created_at,updated_at) VALUES(?,?,?,'pending',?,?) ON CONFLICT(id) DO UPDATE SET last_seen_at=excluded.updated_at,updated_at=excluded.updated_at`, id, "x", url, now, now); e != nil {
		return e
	}
	_, e := tx.ExecContext(ctx, `INSERT OR IGNORE INTO media_links(media_id,owner_type,owner_id,role,position,alt) VALUES(?, 'post', ?, 'post-image', ?, NULL)`, id, postID, pos)
	return e
}
func writeScheduleCandidate(ctx context.Context, tx *sql.Tx, c xsource.ScheduleCandidate, now string) error {
	id := "schedule-x-" + c.PostID
	if _, e := tx.ExecContext(ctx, `INSERT INTO assets(id,kind,url,source_url,source_account,updated_at,raw_json) VALUES(?, 'schedule', ?, ?, ?, ?, NULL) ON CONFLICT(id) DO UPDATE SET url=excluded.url,source_url=excluded.source_url,source_account=excluded.source_account,updated_at=excluded.updated_at`, id, c.MediaURL, c.SourceURL, c.AccountHandle, now); e != nil {
		return e
	}
	h := sha256.Sum256([]byte(c.MediaURL))
	mediaID := "x-" + hex.EncodeToString(h[:16])
	if _, e := tx.ExecContext(ctx, `INSERT INTO media_assets(id,source,source_url,status,created_at,updated_at) VALUES(?,?,?,'pending',?,?) ON CONFLICT(id) DO UPDATE SET last_seen_at=excluded.updated_at,updated_at=excluded.updated_at`, mediaID, "x", c.MediaURL, now, now); e != nil {
		return e
	}
	if _, e := tx.ExecContext(ctx, `INSERT OR IGNORE INTO media_links(media_id,owner_type,owner_id,role,position,alt) VALUES(?, 'asset', ?, 'schedule-image', 0, NULL)`, mediaID, id); e != nil {
		return e
	}
	_, e := tx.ExecContext(ctx, `INSERT INTO schedule_asset_reviews(asset_id,llm_status,manual_status,updated_at) VALUES(?, 'pending','unreviewed',?) ON CONFLICT(asset_id) DO UPDATE SET updated_at=excluded.updated_at`, id, now)
	return e
}
