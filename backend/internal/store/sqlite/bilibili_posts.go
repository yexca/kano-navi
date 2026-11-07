package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	bilisource "github.com/yexca/kano-navi/backend/internal/source/bilibili"
)

// Bilibili dynamics are stored as posts with source = 'bilibili' and the
// dynamic's numeric ID as the row ID. Their pictures become media links in
// the order the dynamic shows them, so the cache and the dashboard read them
// like the pictures of an X post. The account handle is the uploader's
// display name, which Bilibili does not address by handle.

// WriteBilibiliDynamics stores one dynamics read together with the account
// state, in one transaction, and reports how many dynamics were new.
func (s *Store) WriteBilibiliDynamics(ctx context.Context, account string, result bilisource.DynamicsResult) (int, error) {
	if s == nil || s.db == nil || !s.workflowWritable {
		return 0, ErrSourceWrite
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, ErrSourceWrite
	}
	defer tx.Rollback()
	if err = fenceSourceWrite(ctx, tx); err != nil {
		return 0, err
	}
	now := workflowTime(time.Now())
	fresh := 0
	for _, dynamic := range result.Dynamics {
		existed, lookupErr := rowExists(ctx, tx, `SELECT 1 FROM posts WHERE id=?`, dynamic.ID)
		if lookupErr != nil {
			return 0, ErrSourceWrite
		}
		stored, writeErr := writeBilibiliPost(ctx, tx, account, dynamic, now)
		if writeErr != nil {
			return 0, ErrSourceWrite
		}
		if !existed && stored {
			fresh++
		}
	}
	state := result.State
	// A read that stored nothing still records when it happened and any
	// backoff it learned, so the next run waits as intended.
	var storedMeta string
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(metadata_json,'{}') FROM sync_state WHERE source='bilibili' AND account_id=?`, account).Scan(&storedMeta); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, ErrSourceWrite
	}
	var stored bilibiliMetadata
	_ = json.Unmarshal([]byte(storedMeta), &stored)
	meta, _ := json.Marshal(mergeBilibiliMetadata(stored, bilibiliMetadata{
		Session: state.Session, SessionAt: formatInstant(state.SessionAt), LastAttemptAt: formatInstant(state.LastAttemptAt),
		LastPostsAt:  formatInstant(state.LastPostsAt),
		BlockedUntil: formatInstant(state.BlockedUntil), Failures: state.Failures, LastError: state.LastError,
	}, time.Now()))
	_, err = tx.ExecContext(ctx, `INSERT INTO sync_state(source,account_id,cursor_id,cursor_time,last_success_at,metadata_json) VALUES('bilibili',?,NULL,NULL,NULL,?)
 ON CONFLICT(source,account_id) DO UPDATE SET last_success_at=COALESCE(excluded.last_success_at,sync_state.last_success_at),metadata_json=excluded.metadata_json`,
		account, string(meta))
	if err != nil {
		return 0, ErrSourceWrite
	}
	if fresh > 0 {
		if err = bumpRevision(ctx, tx, now); err != nil {
			return 0, ErrSourceWrite
		}
	}
	if tx.Commit() != nil {
		return 0, ErrSourceWrite
	}
	return fresh, nil
}

// writeBilibiliPost upserts one dynamic. It reports false when the ID already
// belongs to another source, whose row is left unchanged: a Bilibili dynamic
// ID is unique across accounts, but a stray numeric ID could still collide
// with a stored X post.
func writeBilibiliPost(ctx context.Context, tx *sql.Tx, account string, d bilisource.Dynamic, now string) (bool, error) {
	raw, _ := json.Marshal(map[string]any{"kind": d.Kind, "authorMid": d.AuthorMid, "orig": d.Original})
	media := ""
	if len(d.Images) > 0 {
		media = d.Images[0]
	}
	// The account handle is the uploader's display name; Bilibili does not
	// address anyone by handle, and the numeric ID is what a read asks for.
	handle := d.Author
	if handle == "" {
		handle = account
	}
	written, err := tx.ExecContext(ctx, `INSERT INTO posts(id,source,account_handle,type,label,text,published_at,url,likes,reposts,replies,media_url,media_alt,raw_json) VALUES(?,?,?,?,?,?,?,?,0,0,0,?,NULL,?)
 ON CONFLICT(id) DO UPDATE SET account_handle=excluded.account_handle,text=excluded.text,published_at=excluded.published_at,url=excluded.url,media_url=excluded.media_url,raw_json=excluded.raw_json
 WHERE posts.source='bilibili'`,
		d.ID, "bilibili", handle, d.Kind, nil, d.Text, d.PublishedAt, d.URL, nullable(media), string(raw))
	if err != nil {
		return false, err
	}
	if count, e := written.RowsAffected(); e != nil {
		return false, e
	} else if count != 1 {
		return false, nil
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM media_links WHERE owner_type='post' AND owner_id=? AND role='post-image'`, d.ID); err != nil {
		return false, err
	}
	for i, url := range d.Images {
		if err = replaceMediaLink(ctx, tx, "bilibili", url, "post", d.ID, "post-image", i, now); err != nil {
			return false, err
		}
	}
	return true, nil
}
