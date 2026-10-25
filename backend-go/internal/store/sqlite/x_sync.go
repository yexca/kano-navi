package sqlite

import (
	"context"
	"database/sql"
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
	if err = fenceSourceWrite(ctx, tx); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, p := range result.Posts {
		if stored, writeErr := writeXPost(ctx, tx, account, p, now); writeErr != nil || !stored {
			return ErrSourceWrite
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
	if len(pending) > 1000 {
		pending = pending[:1000]
	}
	metadata := map[string]any{"handle": account, "discovered": result.DiscoveredCount, "requested": result.RequestedCount, "pendingStatusIds": pending}
	if result.Discovery != "" {
		metadata["discovery"] = result.Discovery
	}
	metaBytes, _ := json.Marshal(metadata)
	var oldID, oldTime string
	_ = tx.QueryRowContext(ctx, "SELECT COALESCE(cursor_id,''),COALESCE(cursor_time,'') FROM sync_state WHERE source='x' AND account_id=?", account).Scan(&oldID, &oldTime)
	cursorID, cursorTime := oldID, oldTime
	oldInstant, _ := time.Parse(time.RFC3339Nano, oldTime)
	newInstant, _ := time.Parse(time.RFC3339Nano, result.NextCursorTime)
	if success && result.NextCursor != "" && (oldTime == "" || newInstant.After(oldInstant)) {
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
	if len(result.Posts) > 0 {
		if err = bumpRevision(ctx, tx, now); err != nil {
			return ErrSourceWrite
		}
	}
	if err = tx.Commit(); err != nil {
		return ErrSourceWrite
	}
	return nil
}

// writeXPost stores one post with its media candidates. It reports false when
// the ID already belongs to another account, whose row is left unchanged.
func writeXPost(ctx context.Context, tx *sql.Tx, account string, p xsource.Post, now string) (bool, error) {
	raw, _ := json.Marshal(p.Raw)
	media := ""
	if len(p.MediaURLs) > 0 {
		media = p.MediaURLs[0]
	}
	written, err := tx.ExecContext(ctx, `INSERT INTO posts(id,source,account_handle,type,label,text,published_at,url,likes,reposts,replies,media_url,media_alt,raw_json) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET account_handle=excluded.account_handle,type=excluded.type,label=excluded.label,text=excluded.text,published_at=excluded.published_at,url=excluded.url,likes=excluded.likes,reposts=excluded.reposts,replies=excluded.replies,media_url=excluded.media_url,media_alt=excluded.media_alt,raw_json=excluded.raw_json WHERE lower(posts.account_handle)=lower(excluded.account_handle) OR posts.account_handle IS NULL`, p.ID, "x", account, p.Type, p.Label, p.Text, p.PublishedAt, p.URL, p.Likes, p.Reposts, p.Replies, nullable(media), nullable(p.MediaAlt), string(raw))
	if err != nil {
		return false, err
	}
	if count, e := written.RowsAffected(); e != nil {
		return false, e
	} else if count != 1 {
		return false, nil
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM media_links WHERE owner_type='post' AND owner_id=? AND role='post-image'`, p.ID); err != nil {
		return false, err
	}
	for i, u := range p.MediaURLs {
		if err = writeMediaCandidate(ctx, tx, p.ID, u, account, i, now); err != nil {
			return false, err
		}
	}
	return true, nil
}
func nullable(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}
func writeMediaCandidate(ctx context.Context, tx *sql.Tx, postID, url, account string, pos int, now string) error {
	return replaceMediaLink(ctx, tx, "x", url, "post", postID, "post-image", pos, now)
}
func writeScheduleCandidate(ctx context.Context, tx *sql.Tx, c xsource.ScheduleCandidate, now string) error {
	id := "schedule-x-" + c.PostID
	var oldURL string
	lookupErr := tx.QueryRowContext(ctx, `SELECT url FROM assets WHERE id=?`, id).Scan(&oldURL)
	if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
		return lookupErr
	}
	if oldURL != "" && oldURL != c.MediaURL {
		if _, e := tx.ExecContext(ctx, `UPDATE schedule_asset_reviews SET llm_status='pending',updated_at=? WHERE asset_id=?`, now, id); e != nil {
			return e
		}
	}
	if _, e := tx.ExecContext(ctx, `INSERT INTO assets(id,kind,url,source_url,source_account,updated_at,raw_json) VALUES(?, 'schedule', ?, ?, ?, ?, NULL) ON CONFLICT(id) DO UPDATE SET asset_version=assets.asset_version+CASE WHEN assets.url!=excluded.url THEN 1 ELSE 0 END,url=excluded.url,source_url=excluded.source_url,source_account=excluded.source_account,updated_at=excluded.updated_at`, id, c.MediaURL, c.SourceURL, c.AccountHandle, now); e != nil {
		return e
	}
	if e := replaceMediaLink(ctx, tx, "x", c.MediaURL, "asset", id, "schedule-image", 0, now); e != nil {
		return e
	}
	_, e := tx.ExecContext(ctx, `INSERT INTO schedule_asset_reviews(asset_id,llm_status,manual_status,updated_at) VALUES(?, 'pending','unreviewed',?) ON CONFLICT(asset_id) DO UPDATE SET updated_at=excluded.updated_at`, id, now)
	return e
}

type XRecentState struct {
	PendingIDs []string
	KnownIDs   map[string]bool
	// CursorID is the newest successfully stored status of the account.
	CursorID string
}

func (s *Store) XAccountStart(ctx context.Context, handles []string) (int, error) {
	var next string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key='go_x_next_account'`).Scan(&next)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, ErrSourceWrite
	}
	for i, handle := range handles {
		if strings.EqualFold(next, handle) {
			return i, nil
		}
	}
	return 0, nil
}
func (s *Store) SetXNextAccount(ctx context.Context, handle string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrSourceWrite
	}
	defer tx.Rollback()
	if err = fenceSourceWrite(ctx, tx); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO app_settings(key,value,updated_at) VALUES('go_x_next_account',?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, handle, workflowTime(time.Now()))
	if err != nil || tx.Commit() != nil {
		return ErrSourceWrite
	}
	return nil
}

func (s *Store) XRecentState(ctx context.Context, account string) (XRecentState, error) {
	out := XRecentState{KnownIDs: map[string]bool{}}
	var meta string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(metadata_json,'{}'),COALESCE(cursor_id,'') FROM sync_state WHERE source='x' AND account_id=?`, account).Scan(&meta, &out.CursorID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, ErrSourceWrite
	}
	var data struct {
		Pending []string `json:"pendingStatusIds"`
	}
	_ = json.Unmarshal([]byte(meta), &data)
	out.PendingIDs = data.Pending
	if len(out.PendingIDs) > 1000 {
		out.PendingIDs = out.PendingIDs[:1000]
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM posts WHERE source='x' AND lower(account_handle)=lower(?) ORDER BY julianday(published_at) DESC,id LIMIT 200`, account)
	if err != nil {
		return out, ErrSourceWrite
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			return out, ErrSourceWrite
		}
		out.KnownIDs[id] = true
	}
	if rows.Err() != nil {
		return out, ErrSourceWrite
	}
	return out, nil
}
