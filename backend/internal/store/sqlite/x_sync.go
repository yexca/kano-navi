package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	xsource "github.com/yexca/kano-navi/backend/internal/source/x"
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
	// A post read from a timeline or API page that is already stored keeps
	// the text, media and schedule candidate it has, as a fetch window leaves
	// it.
	kept := map[string]bool{}
	for _, p := range result.Posts {
		if result.FromListing[p.ID] {
			var exists int
			lookupErr := tx.QueryRowContext(ctx, `SELECT 1 FROM posts WHERE id=?`, p.ID).Scan(&exists)
			if lookupErr == nil {
				kept[p.ID] = true
				continue
			}
			if !errors.Is(lookupErr, sql.ErrNoRows) {
				return ErrSourceWrite
			}
		}
		if stored, writeErr := writeXPost(ctx, tx, account, p, now); writeErr != nil || !stored {
			return ErrSourceWrite
		}
	}
	// A requested refresh is done once its detail request was made; the
	// answer, if usable, was written above.
	for _, id := range result.Refreshed {
		if _, err = tx.ExecContext(ctx, `UPDATE posts SET refresh_requested_at=NULL WHERE id=? AND lower(account_handle)=lower(?)`, id, account); err != nil {
			return ErrSourceWrite
		}
	}
	for _, c := range result.ScheduleCandidates {
		if c.MediaURL == "" || c.SourceURL == "" || kept[c.PostID] {
			continue
		}
		if err = writeScheduleCandidate(ctx, tx, c, now); err != nil {
			return ErrSourceWrite
		}
	}
	// A refresh-only run leaves the incremental state as it is.
	if !result.RefreshOnly {
		if err = writeXState(ctx, tx, account, result, success, now); err != nil {
			return ErrSourceWrite
		}
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

// writeXState replaces the account's incremental sync state: the pending
// queue, its retries, the timeline gap and a cursor that only moves forward.
func writeXState(ctx context.Context, tx *sql.Tx, account string, result xsource.Result, success bool, now string) error {
	var oldMetadata string
	_ = tx.QueryRowContext(ctx, "SELECT COALESCE(metadata_json,'') FROM sync_state WHERE source='x' AND account_id=?", account).Scan(&oldMetadata)
	pending := append([]string(nil), result.PendingIDs...)
	var old struct {
		Pending []any                           `json:"pendingStatusIds"`
		Retries map[string]xsource.PendingRetry `json:"pendingRetries"`
	}
	if oldMetadata != "" {
		// A field of another shape is skipped and the rest still read.
		_ = json.Unmarshal([]byte(oldMetadata), &old)
		// A dropped ID is not taken back from the earlier queue.
		seen := map[string]bool{}
		for _, id := range pending {
			seen[id] = true
		}
		for _, dropped := range result.Dropped {
			seen[dropped.ID] = true
		}
		for _, value := range old.Pending {
			if id, ok := value.(string); ok && !seen[id] {
				pending = append(pending, id)
				seen[id] = true
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
	// Failure counts are kept only for IDs still pending; the run's state
	// replaces the earlier one.
	retries := map[string]xsource.PendingRetry{}
	for _, id := range pending {
		if retry, ok := result.PendingRetries[id]; ok {
			retries[id] = retry
		} else if retry, ok = old.Retries[id]; ok {
			retries[id] = retry
		}
	}
	metadata := map[string]any{"handle": account, "discovered": result.DiscoveredCount, "requested": result.RequestedCount, "pendingStatusIds": pending}
	if len(retries) > 0 {
		metadata["pendingRetries"] = retries
	}
	if result.Discovery != "" {
		metadata["discovery"] = result.Discovery
	}
	if result.Gap != nil {
		metadata["timelineGap"] = result.Gap
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
	_, err := tx.ExecContext(ctx, `INSERT INTO sync_state(source,account_id,cursor_id,cursor_time,last_success_at,metadata_json) VALUES('x',?,?,?,?,?) ON CONFLICT(source,account_id) DO UPDATE SET cursor_id=excluded.cursor_id,cursor_time=excluded.cursor_time,last_success_at=CASE WHEN excluded.last_success_at IS NULL THEN sync_state.last_success_at ELSE excluded.last_success_at END,metadata_json=excluded.metadata_json`, account, nullable(cursorID), nullable(cursorTime), func() any {
		if success {
			return now
		}
		return nil
	}(), string(metaBytes))
	return err
}

// writeXPost stores one post with its media candidates. It reports false when
// the ID already belongs to another account, whose row is left unchanged.
func writeXPost(ctx context.Context, tx *sql.Tx, account string, p xsource.Post, now string) (bool, error) {
	raw, _ := json.Marshal(p.Raw)
	media := ""
	if len(p.MediaURLs) > 0 {
		media = p.MediaURLs[0]
	}
	written, err := tx.ExecContext(ctx, `INSERT INTO posts(id,source,account_handle,type,label,text,published_at,url,likes,reposts,replies,media_url,media_alt,raw_json,quote_json) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET account_handle=excluded.account_handle,type=excluded.type,label=excluded.label,text=excluded.text,published_at=excluded.published_at,url=excluded.url,likes=excluded.likes,reposts=excluded.reposts,replies=excluded.replies,media_url=excluded.media_url,media_alt=excluded.media_alt,raw_json=excluded.raw_json,quote_json=excluded.quote_json WHERE lower(posts.account_handle)=lower(excluded.account_handle) OR posts.account_handle IS NULL`, p.ID, "x", account, p.Type, p.Label, p.Text, p.PublishedAt, p.URL, p.Likes, p.Reposts, p.Replies, nullable(media), nullable(p.MediaAlt), string(raw), quoteJSON(p.Quote))
	if err != nil {
		return false, err
	}
	if count, e := written.RowsAffected(); e != nil {
		return false, e
	} else if count != 1 {
		return false, nil
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM media_links WHERE owner_type='post' AND owner_id=? AND role IN ('post-image',?)`, p.ID, roleQuoteImage); err != nil {
		return false, err
	}
	for i, u := range p.MediaURLs {
		if err = writeMediaCandidate(ctx, tx, p.ID, u, account, i, now); err != nil {
			return false, err
		}
	}
	if p.Quote != nil {
		if err = writeQuoteMedia(ctx, tx, p.ID, p.Quote.MediaURLs, now); err != nil {
			return false, err
		}
	}
	return true, nil
}

// roleQuoteImage links a quoted status's images to the quoting post, apart
// from its own post-image links, which alone feed schedule extraction.
const roleQuoteImage = "quote-image"

// quoteJSON is the stored quote projection; its images are media links.
func quoteJSON(q *xsource.Quote) any {
	if q == nil {
		return nil
	}
	body, err := json.Marshal(q)
	if err != nil {
		return nil
	}
	return string(body)
}

// writeQuoteMedia registers a quoted status's images as pending candidates.
// A quoted image the cache cannot identify is left out rather than failing
// the post.
func writeQuoteMedia(ctx context.Context, tx *sql.Tx, postID string, urls []string, now string) error {
	position := 0
	for _, u := range urls {
		if _, _, err := mediaIdentity(u); err != nil {
			continue
		}
		if err := replaceMediaLink(ctx, tx, "x", u, "post", postID, roleQuoteImage, position, now); err != nil {
			return err
		}
		position++
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
	// PendingRetries is the failure state of pending IDs, from
	// metadata_json.pendingRetries; a row without it has none.
	PendingRetries map[string]xsource.PendingRetry
	KnownIDs       map[string]bool
	// CursorID is the newest successfully stored status of the account.
	CursorID string
	// Gap is the timeline stretch a capped run left unread, or nil.
	Gap *xsource.Gap
	// GapCeilings holds, for each gap floor, the oldest stored status above
	// it: everything between the two is unread, which an X API read closes
	// exactly with since_id and until_id.
	GapCeilings map[string]string
	// RefreshIDs are the account's posts an operator asked to read again,
	// oldest request first.
	RefreshIDs []string
}

// maxRefreshIDs bounds the refreshes one run reads for an account; the
// rest wait for the next run.
const maxRefreshIDs = 20

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
		Pending []string                        `json:"pendingStatusIds"`
		Retries map[string]xsource.PendingRetry `json:"pendingRetries"`
		Gap     *xsource.Gap                    `json:"timelineGap"`
	}
	_ = json.Unmarshal([]byte(meta), &data)
	out.PendingIDs, out.PendingRetries = data.Pending, data.Retries
	if data.Gap != nil && data.Gap.Floor != "" {
		out.Gap = data.Gap
		out.GapCeilings = map[string]string{}
		for gap := data.Gap; gap != nil; gap = gap.Older {
			// Status IDs are decimal and time ordered; compare them by length,
			// then digits.
			var ceiling string
			err = s.db.QueryRowContext(ctx, `SELECT id FROM posts WHERE source='x' AND lower(account_handle)=lower(?) AND (length(id)>length(?) OR (length(id)=length(?) AND id>?)) ORDER BY length(id),id LIMIT 1`, account, gap.Floor, gap.Floor, gap.Floor).Scan(&ceiling)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return out, ErrSourceWrite
			}
			if ceiling != "" {
				out.GapCeilings[gap.Floor] = ceiling
			}
		}
	}
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
	rows.Close()
	refresh, err := s.db.QueryContext(ctx, `SELECT id FROM posts WHERE source='x' AND lower(account_handle)=lower(?) AND refresh_requested_at IS NOT NULL ORDER BY refresh_requested_at,id LIMIT ?`, account, maxRefreshIDs)
	if err != nil {
		return out, ErrSourceWrite
	}
	defer refresh.Close()
	for refresh.Next() {
		var id string
		if refresh.Scan(&id) != nil {
			return out, ErrSourceWrite
		}
		out.RefreshIDs = append(out.RefreshIDs, id)
	}
	if refresh.Err() != nil {
		return out, ErrSourceWrite
	}
	return out, nil
}
