package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	bilisource "github.com/yexca/kano-navi/backend/internal/source/bilibili"
)

// Bilibili uploads are stored as videos with source = 'bilibili' and their BV
// ID as the row ID. The account's read bookkeeping, including the anonymous
// device identity and any backoff, is kept in sync_state (source 'bilibili')
// and is never returned by an API.

type bilibiliMetadata struct {
	Session       bilisource.Session `json:"session"`
	SessionAt     string             `json:"sessionAt,omitempty"`
	LastAttemptAt string             `json:"lastAttemptAt,omitempty"`
	BlockedUntil  string             `json:"blockedUntil,omitempty"`
	Failures      int                `json:"failures,omitempty"`
	LastError     string             `json:"lastError,omitempty"`
}

func parseInstant(value string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, value)
	return t
}

func formatInstant(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return workflowTime(t)
}

// BilibiliState reads the bookkeeping of one account; an account never read
// has the zero state.
func (s *Store) BilibiliState(ctx context.Context, account string) (bilisource.State, error) {
	var lastSuccess, meta string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(last_success_at,''),COALESCE(metadata_json,'{}') FROM sync_state WHERE source='bilibili' AND account_id=?`, account).Scan(&lastSuccess, &meta)
	if errors.Is(err, sql.ErrNoRows) {
		return bilisource.State{}, nil
	}
	if err != nil {
		return bilisource.State{}, ErrUnavailable
	}
	var data bilibiliMetadata
	_ = json.Unmarshal([]byte(meta), &data)
	return bilisource.State{
		Session: data.Session, SessionAt: parseInstant(data.SessionAt),
		LastAttemptAt: parseInstant(data.LastAttemptAt), LastSuccessAt: parseInstant(lastSuccess),
		BlockedUntil: parseInstant(data.BlockedUntil), Failures: data.Failures, LastError: data.LastError,
	}, nil
}

// BilibiliStatus is the console's view of one account: when it was read, any
// backoff and the stored uploads. It never includes the device identity.
type BilibiliStatus struct {
	Account       string  `json:"account"`
	Videos        int     `json:"videos"`
	LastAttemptAt *string `json:"lastAttemptAt"`
	LastSuccessAt *string `json:"lastSuccessAt"`
	BlockedUntil  *string `json:"blockedUntil"`
	LastError     *string `json:"lastError"`
}

func (s *Store) BilibiliStatus(ctx context.Context, account string) (BilibiliStatus, error) {
	state, err := s.BilibiliState(ctx, account)
	if err != nil {
		return BilibiliStatus{}, err
	}
	out := BilibiliStatus{Account: account}
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM videos WHERE source='bilibili'`).Scan(&out.Videos); err != nil {
		return BilibiliStatus{}, ErrUnavailable
	}
	optional := func(value string) *string {
		if value == "" {
			return nil
		}
		return &value
	}
	out.LastAttemptAt, out.LastSuccessAt = optional(formatInstant(state.LastAttemptAt)), optional(formatInstant(state.LastSuccessAt))
	out.BlockedUntil, out.LastError = optional(formatInstant(state.BlockedUntil)), optional(state.LastError)
	return out, nil
}

// WriteBilibiliRecent stores what one list read returned together with the
// account state, in one transaction. A failed or skipped read stores only the
// state, so known uploads survive. It reports whether a video changed.
func (s *Store) WriteBilibiliRecent(ctx context.Context, account string, result bilisource.Result) (bool, error) {
	return s.writeBilibili(ctx, account, result.Videos, result.State)
}

// WriteBilibiliVideo stores one upload an operator asked for and the state.
func (s *Store) WriteBilibiliVideo(ctx context.Context, account string, video bilisource.Video, state bilisource.State) (bool, error) {
	return s.writeBilibili(ctx, account, []bilisource.Video{video}, state)
}

// SaveBilibiliState stores the bookkeeping of a read that stored nothing,
// such as a failed operator request.
func (s *Store) SaveBilibiliState(ctx context.Context, account string, state bilisource.State) error {
	_, err := s.writeBilibili(ctx, account, nil, state)
	return err
}

func (s *Store) writeBilibili(ctx context.Context, account string, videos []bilisource.Video, state bilisource.State) (bool, error) {
	if s == nil || !s.workflowWritable {
		return false, ErrSourceWrite
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, ErrSourceWrite
	}
	defer tx.Rollback()
	if err = fenceSourceWrite(ctx, tx); err != nil {
		return false, err
	}
	now := workflowTime(time.Now())
	changed, err := writeBilibiliVideos(ctx, tx, videos, now)
	if err != nil {
		return false, err
	}
	newest, newestAt := "", ""
	for _, v := range videos {
		if newestAt == "" || parseInstant(v.PublishedAt).After(parseInstant(newestAt)) {
			newest, newestAt = v.BVID, v.PublishedAt
		}
	}
	// Operator reads run outside the workflow queue, so another read may have
	// stored a captcha block since this one read the state.
	var storedMeta string
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(metadata_json,'{}') FROM sync_state WHERE source='bilibili' AND account_id=?`, account).Scan(&storedMeta); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, ErrSourceWrite
	}
	var stored bilibiliMetadata
	_ = json.Unmarshal([]byte(storedMeta), &stored)
	meta, _ := json.Marshal(mergeBilibiliMetadata(stored, bilibiliMetadata{
		Session: state.Session, SessionAt: formatInstant(state.SessionAt), LastAttemptAt: formatInstant(state.LastAttemptAt),
		BlockedUntil: formatInstant(state.BlockedUntil), Failures: state.Failures, LastError: state.LastError,
	}, time.Now()))
	// The cursor only names the newest upload seen; reads are not incremental.
	_, err = tx.ExecContext(ctx, `INSERT INTO sync_state(source,account_id,cursor_id,cursor_time,last_success_at,metadata_json) VALUES('bilibili',?,NULLIF(?,''),NULLIF(?,''),NULLIF(?,''),?)
 ON CONFLICT(source,account_id) DO UPDATE SET
  cursor_id=CASE WHEN julianday(excluded.cursor_time)>COALESCE(julianday(sync_state.cursor_time),0) THEN excluded.cursor_id ELSE sync_state.cursor_id END,
  cursor_time=CASE WHEN julianday(excluded.cursor_time)>COALESCE(julianday(sync_state.cursor_time),0) THEN excluded.cursor_time ELSE sync_state.cursor_time END,
  last_success_at=COALESCE(excluded.last_success_at,sync_state.last_success_at),metadata_json=excluded.metadata_json`,
		account, newest, newestAt, formatInstant(state.LastSuccessAt), string(meta))
	if err != nil {
		return false, ErrSourceWrite
	}
	if changed {
		if err = bumpRevision(ctx, tx, now); err != nil {
			return false, ErrSourceWrite
		}
	}
	if tx.Commit() != nil {
		return false, ErrSourceWrite
	}
	return changed, nil
}

// mergeBilibiliMetadata keeps a block that is still active and later than
// the incoming one, with its failure bookkeeping: no request starts while a
// block lasts, so such a block was stored by a concurrent read that this
// write must not erase. The latest attempt is kept as well.
func mergeBilibiliMetadata(stored, incoming bilibiliMetadata, now time.Time) bilibiliMetadata {
	storedBlock := parseInstant(stored.BlockedUntil)
	if storedBlock.After(now) && storedBlock.After(parseInstant(incoming.BlockedUntil)) {
		incoming.BlockedUntil, incoming.Failures, incoming.LastError = stored.BlockedUntil, stored.Failures, stored.LastError
	}
	if parseInstant(stored.LastAttemptAt).After(parseInstant(incoming.LastAttemptAt)) {
		incoming.LastAttemptAt = stored.LastAttemptAt
	}
	return incoming
}

// writeBilibiliVideos upserts uploads. A list read is a complete description
// of an upload, so it replaces the stored platform fields and keeps the
// replaced title and cover as history. Operator corrections survive.
func writeBilibiliVideos(ctx context.Context, tx *sql.Tx, videos []bilisource.Video, now string) (bool, error) {
	changed := false
	for _, v := range videos {
		var before string
		if err := tx.QueryRowContext(ctx, youtubeVideoState, v.BVID).Scan(&before); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return false, ErrSourceWrite
		}
		prior, existed, err := readVideoSource(ctx, tx, v.BVID)
		if err != nil {
			return false, ErrSourceWrite
		}
		var duration any
		if v.DurationSeconds > 0 {
			duration = v.DurationSeconds
		}
		raw, _ := json.Marshal(map[string]any{"aid": v.AID, "description": v.Description})
		written, err := tx.ExecContext(ctx, `INSERT INTO videos(id,source,title,published_at,scheduled_at,url,thumbnail_url,kind,is_upcoming,live_checked_at,duration_seconds,raw_json) VALUES(?,'bilibili',?,?,NULL,?,?,?,0,?,?,?)
 ON CONFLICT(id) DO UPDATE SET title=excluded.title,published_at=excluded.published_at,url=excluded.url,thumbnail_url=COALESCE(excluded.thumbnail_url,videos.thumbnail_url),kind=excluded.kind,live_checked_at=excluded.live_checked_at,duration_seconds=COALESCE(excluded.duration_seconds,videos.duration_seconds),raw_json=excluded.raw_json,refresh_requested_at=NULL
 WHERE videos.source='bilibili'`,
			v.BVID, v.Title, v.PublishedAt, v.URL, nullable(v.ThumbnailURL), v.Kind, now, duration, string(raw))
		if err != nil {
			return false, ErrSourceWrite
		}
		if count, e := written.RowsAffected(); e != nil {
			return false, ErrSourceWrite
		} else if count == 0 {
			continue
		}
		if v.ThumbnailURL != "" {
			if err = replaceMediaLink(ctx, tx, "bilibili", v.ThumbnailURL, "video", v.BVID, "thumbnail", 0, now); err != nil {
				return false, ErrSourceWrite
			}
		}
		if existed && (prior.Title != v.Title || v.ThumbnailURL != "" && prior.ThumbnailURL != v.ThumbnailURL) {
			if err = recordVideoRevision(ctx, tx, v.BVID, prior, revisionSync, now); err != nil {
				return false, ErrSourceWrite
			}
		}
		// A merged upload's title is its YouTube video's Bilibili translation.
		translated, err := refreshMergeTranslations(ctx, tx, v.BVID, now)
		if err != nil {
			return false, ErrSourceWrite
		}
		var after string
		if err = tx.QueryRowContext(ctx, youtubeVideoState, v.BVID).Scan(&after); err != nil {
			return false, ErrSourceWrite
		}
		changed = changed || translated || after != before
	}
	return changed, nil
}
