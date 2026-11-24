package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	bilisource "github.com/yexca/kano-navi/backend/internal/source/bilibili"
	xsource "github.com/yexca/kano-navi/backend/internal/source/x"
)

// Bilibili dynamics are posts with source = 'bilibili', their ID prefixed
// with bilibili- (bilisource.PostPrefix) so that they never meet an X status
// ID in posts.id, account_handle the account's mid and url the opus page. A
// forward quotes the dynamic it forwards the way an X post quotes a status.
// The listing's bookkeeping (its own attempt clock, outcome and the WBI keys)
// is kept in the metadata of the account's 'bilibili' sync_state row, beside
// the upload read's, because both share the captcha backoff stored there.

// bilibiliPostLabel is the label every dynamic gets: the English label of a
// daily X post, never localized text.
const bilibiliPostLabel = xsource.LabelDaily

// rolePostEmoji links a dynamic's sticker pictures to its post, one link per
// distinct picture, and roleQuoteEmoji those of the dynamic a forward
// forwards to the forwarding post. Neither is an image of the post: the board
// shows them inside the original wording (see stickerSegments), and
// extraction never reads them.
const (
	rolePostEmoji  = "post-emoji"
	roleQuoteEmoji = "quote-emoji"
)

// WriteBilibiliDynamics stores what one listing read returned together with
// the account state, in one transaction: the dynamics as posts and the video
// dynamics as uploads, written as the upload list writes them (the upload
// read's newest cursor stays its own). A failed or skipped read stores only
// the state, so known posts and uploads survive. It reports whether a public
// post or video changed, and advances the dashboard revision only then.
func (s *Store) WriteBilibiliDynamics(ctx context.Context, account string, result bilisource.DynamicsResult) (bool, error) {
	return s.writeBilibiliPosts(ctx, account, result.Dynamics, result.Uploads, result.State)
}

// WriteBilibiliDynamic stores one dynamic an operator asked for and the state.
func (s *Store) WriteBilibiliDynamic(ctx context.Context, account string, d bilisource.Dynamic, state bilisource.State) (bool, error) {
	return s.writeBilibiliPosts(ctx, account, []bilisource.Dynamic{d}, nil, state)
}

func (s *Store) writeBilibiliPosts(ctx context.Context, account string, dynamics []bilisource.Dynamic, uploads []bilisource.Video, state bilisource.State) (bool, error) {
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
	changed := false
	for _, d := range dynamics {
		visible, err := writeBilibiliPost(ctx, tx, account, d, now)
		if err != nil {
			return false, ErrSourceWrite
		}
		changed = changed || visible
	}
	written, err := writeBilibiliVideos(ctx, tx, uploads, now)
	if err != nil {
		return false, err
	}
	changed = changed || written
	if err = writeBilibiliState(ctx, tx, account, state, "", ""); err != nil {
		return false, err
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

// bilibiliPostState is what the board shows of a dynamic: its fields, its
// images in order, its quote, the quote's images, both sets of stickers and,
// for a dynamic with stickers, the nodes they are placed by (its own and its
// quote's), so a sticker that moves within unchanged text counts too.
const bilibiliPostState = `SELECT p.text||char(31)||p.published_at||char(31)||p.url||char(31)||COALESCE(p.media_url,'')||char(31)||COALESCE(p.hidden_at,'')||char(31)||
 CASE WHEN json_valid(p.raw_json) AND instr(p.raw_json,'` + bilisource.EmojiNode + `')>0 THEN COALESCE(json_extract(p.raw_json,'$.nodes'),'')||char(30)||COALESCE(json_extract(p.raw_json,'$.orig.nodes'),'') ELSE '' END||char(31)||
 COALESCE((SELECT group_concat(ma.source_url,char(30)) FROM (SELECT media_id FROM media_links WHERE owner_type='post' AND owner_id=p.id AND role='post-image' ORDER BY position) ml JOIN media_assets ma ON ma.id=ml.media_id),'')||char(31)||
 COALESCE(p.quote_json,'')||char(31)||
 COALESCE((SELECT group_concat(ma.source_url,char(30)) FROM (SELECT media_id FROM media_links WHERE owner_type='post' AND owner_id=p.id AND role='quote-image' ORDER BY position) ml JOIN media_assets ma ON ma.id=ml.media_id),'')||char(31)||
 COALESCE((SELECT group_concat(ma.source_url,char(30)) FROM (SELECT media_id FROM media_links WHERE owner_type='post' AND owner_id=p.id AND role='post-emoji' ORDER BY position) ml JOIN media_assets ma ON ma.id=ml.media_id),'')||char(31)||
 COALESCE((SELECT group_concat(ma.source_url,char(30)) FROM (SELECT media_id FROM media_links WHERE owner_type='post' AND owner_id=p.id AND role='quote-emoji' ORDER BY position) ml JOIN media_assets ma ON ma.id=ml.media_id),'')
 FROM posts p WHERE p.id=?`

// writeBilibiliPost upserts one dynamic, its image and sticker links and,
// for a forward, its quote: the projection in quote_json, the forwarded
// dynamic's pictures as quote-image links, its stickers as quote-emoji links
// and its pool row (see writeBilibiliStatus). A row of
// another source with the same ID is left alone, and so is a stored forward
// when an operator's single read returns the dynamic without its quote,
// because only the listing gives a forward its quote. A new forward without
// wording of its own whose dynamic is unavailable is not stored; a stored one
// takes the unavailable quote and loses its quote-image links, while the
// dynamic's pool row stays. Operator hides survive. It reports whether what
// the board shows changed.
func writeBilibiliPost(ctx context.Context, tx *sql.Tx, account string, d bilisource.Dynamic, now string) (bool, error) {
	id := d.PostID()
	var before string
	err := tx.QueryRowContext(ctx, bilibiliPostState, id).Scan(&before)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	raw, _ := json.Marshal(d.Raw)
	// A new forward with no wording of its own whose dynamic is gone would
	// be an empty card; a stored one is rewritten to show the dynamic gone.
	// Sticker captions are wording too, even without a cached picture.
	if errors.Is(err, sql.ErrNoRows) && d.Quote != nil && d.Quote.Unavailable && d.Text == "" {
		var own stickerSource
		if json.Unmarshal(raw, &own) != nil || !hasSticker(own.Nodes) {
			return false, nil
		}
	}
	media := ""
	if len(d.Images) > 0 {
		media = d.Images[0]
	}
	written, err := tx.ExecContext(ctx, `INSERT INTO posts(id,source,account_handle,type,label,text,published_at,url,likes,reposts,replies,media_url,media_alt,raw_json,quote_json) VALUES(?,'bilibili',?,'daily',?,?,?,?,0,0,0,?,NULL,?,?)
 ON CONFLICT(id) DO UPDATE SET account_handle=excluded.account_handle,text=excluded.text,published_at=excluded.published_at,url=excluded.url,media_url=excluded.media_url,raw_json=excluded.raw_json,quote_json=excluded.quote_json
 WHERE posts.source='bilibili' AND (excluded.quote_json IS NOT NULL OR NOT (json_valid(posts.raw_json) AND json_extract(posts.raw_json,'$.type')=?))`,
		id, account, bilibiliPostLabel, d.Text, d.PublishedAt, d.URL(), nullable(media), string(raw), bilibiliQuoteJSON(d.Quote), bilisource.ForwardType)
	if err != nil {
		return false, err
	}
	if count, e := written.RowsAffected(); e != nil {
		return false, e
	} else if count != 1 {
		return false, nil
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM media_links WHERE owner_type='post' AND owner_id=? AND role IN ('post-image',?,?,?)`, id, roleQuoteImage, rolePostEmoji, roleQuoteEmoji); err != nil {
		return false, err
	}
	for i, u := range d.Images {
		if err = replaceMediaLink(ctx, tx, "bilibili", u, "post", id, "post-image", i, now); err != nil {
			return false, err
		}
	}
	if err = linkBilibiliMedia(ctx, tx, id, rolePostEmoji, d.Stickers, now); err != nil {
		return false, err
	}
	if d.Quote != nil {
		if err = linkBilibiliMedia(ctx, tx, id, roleQuoteImage, d.Quote.MediaURLs, now); err != nil {
			return false, err
		}
		if err = linkBilibiliMedia(ctx, tx, id, roleQuoteEmoji, d.Quote.Stickers, now); err != nil {
			return false, err
		}
		if err = writeBilibiliStatus(ctx, tx, d.Quote, d.PublishedAt, now); err != nil {
			return false, err
		}
	}
	var after string
	if err = tx.QueryRowContext(ctx, bilibiliPostState, id).Scan(&after); err != nil {
		return false, err
	}
	return after != before, nil
}

// linkBilibiliMedia links pictures to a post under role in order, skipping
// any that is not a media URL. A picture is one asset whichever posts and
// roles link it.
func linkBilibiliMedia(ctx context.Context, tx *sql.Tx, postID, role string, urls []string, now string) error {
	position := 0
	for _, u := range urls {
		if _, _, err := mediaIdentity(u); err != nil {
			continue
		}
		if err := replaceMediaLink(ctx, tx, "bilibili", u, "post", postID, role, position, now); err != nil {
			return err
		}
		position++
	}
	return nil
}

// bilibiliQuoteJSON is a forward's stored quote projection, NULL for any
// other dynamic. Its pictures are media links, never part of it.
func bilibiliQuoteJSON(q *bilisource.Quote) any {
	if q == nil {
		return nil
	}
	body, err := json.Marshal(q)
	if err != nil {
		return nil
	}
	return string(body)
}

// The Bilibili status pool (bilibili_statuses) holds every dynamic an own
// forward forwards, whatever its author, so that it is translated once
// however often it is forwarded. Like x_statuses it is read only by the
// translate step and the translation routes: a pooled dynamic reaches the
// board only as the quote projection on the forwarding post and the
// translations keyed by its post ID, and both the pool row and the
// projection come from the same bilisource.Quote.

// writeBilibiliStatus stores the dynamic a forward forwards in the pool, in
// the forward's transaction, and records forwardedAt, the forward's
// publication time, when it is newer than the one stored. An unavailable
// quote stores nothing.
func writeBilibiliStatus(ctx context.Context, tx *sql.Tx, q *bilisource.Quote, forwardedAt, now string) error {
	if q == nil || q.Unavailable || q.ID == "" || q.AuthorHandle == "" {
		return nil
	}
	media := q.MediaURLs
	if media == nil {
		media = []string{}
	}
	raw, _ := json.Marshal(map[string]any{"id": q.ID, "text": q.Text, "publishedAt": q.PublishedAt, "url": q.URL,
		"author": map[string]any{"mid": q.AuthorHandle, "name": q.AuthorName}, "mediaURLs": media})
	_, err := tx.ExecContext(ctx, `INSERT INTO bilibili_statuses(id,author_mid,author_name,text,published_at,url,raw_json,forwarded_at,first_seen_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET author_mid=excluded.author_mid,author_name=excluded.author_name,text=excluded.text,
			published_at=COALESCE(excluded.published_at,bilibili_statuses.published_at),url=excluded.url,raw_json=excluded.raw_json,
			forwarded_at=CASE WHEN bilibili_statuses.forwarded_at IS NULL OR julianday(excluded.forwarded_at)>julianday(bilibili_statuses.forwarded_at) THEN excluded.forwarded_at ELSE bilibili_statuses.forwarded_at END,
			updated_at=excluded.updated_at`,
		q.ID, q.AuthorHandle, nullable(q.AuthorName), q.Text, nullable(q.PublishedAt), q.URL, string(raw), forwardedAt, now, now)
	return err
}

// writeBilibiliState stores the account state in its 'bilibili' sync_state
// row. newest and newestAt move the cursor of the upload read forward.
func writeBilibiliState(ctx context.Context, tx *sql.Tx, account string, state bilisource.State, newest, newestAt string) error {
	// Operator reads run outside the workflow queue, so another read may have
	// stored a captcha block since this one read the state.
	var storedMeta string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(metadata_json,'{}') FROM sync_state WHERE source='bilibili' AND account_id=?`, account).Scan(&storedMeta); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ErrSourceWrite
	}
	var stored bilibiliMetadata
	_ = json.Unmarshal([]byte(storedMeta), &stored)
	meta, _ := json.Marshal(mergeBilibiliMetadata(stored, bilibiliMetadataOf(state), time.Now()))
	// The cursor only names the newest upload seen; reads are not incremental.
	_, err := tx.ExecContext(ctx, `INSERT INTO sync_state(source,account_id,cursor_id,cursor_time,last_success_at,metadata_json) VALUES('bilibili',?,NULLIF(?,''),NULLIF(?,''),NULLIF(?,''),?)
 ON CONFLICT(source,account_id) DO UPDATE SET
  cursor_id=CASE WHEN julianday(excluded.cursor_time)>COALESCE(julianday(sync_state.cursor_time),0) THEN excluded.cursor_id ELSE sync_state.cursor_id END,
  cursor_time=CASE WHEN julianday(excluded.cursor_time)>COALESCE(julianday(sync_state.cursor_time),0) THEN excluded.cursor_time ELSE sync_state.cursor_time END,
  last_success_at=COALESCE(excluded.last_success_at,sync_state.last_success_at),metadata_json=excluded.metadata_json`,
		account, newest, newestAt, formatInstant(state.LastSuccessAt), string(meta))
	if err != nil {
		return ErrSourceWrite
	}
	return nil
}
