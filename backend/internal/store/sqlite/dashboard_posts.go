package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"time"

	bilisource "github.com/yexca/kano-navi/backend/internal/source/bilibili"
)

type postRow struct {
	id, source, accountHandle, typ, label, text, publishedAt, url, mediaURL, mediaAlt string
	likes, reposts, replies                                                           int64
	hasAccount, hasLabel, hasMediaURL, hasMediaAlt                                    bool
	// quoteJSON is NULL in public reads when an operator hid the quote.
	quoteJSON sql.NullString
	// promoted marks a pooled status promoted into posts.
	promoted bool
	// stickerRaw is the raw_json of a Bilibili dynamic that has emoji
	// nodes, the source of its sticker segments; NULL for any other post.
	stickerRaw sql.NullString
}

// postColumns are a public post read's columns of the unaliased posts table,
// in postRow's scan order (see scanPostRow).
const postColumns = `id,source,account_handle,type,label,text,published_at,url,likes,reposts,replies,media_url,media_alt,CASE WHEN quote_hidden_at IS NULL THEN quote_json END,promoted_at IS NOT NULL,
 CASE WHEN source='bilibili' AND instr(raw_json,'` + bilisource.EmojiNode + `')>0 THEN raw_json END`

// scanPostRow reads one row of postColumns.
func scanPostRow(row scanner) (postRow, error) {
	var r postRow
	var account, label, mu, ma sql.NullString
	if err := row.Scan(&r.id, &r.source, &account, &r.typ, &label, &r.text, &r.publishedAt, &r.url, &r.likes, &r.reposts, &r.replies, &mu, &ma, &r.quoteJSON, &r.promoted, &r.stickerRaw); err != nil {
		return r, err
	}
	r.accountHandle, r.hasAccount = account.String, account.Valid
	r.label, r.hasLabel = label.String, label.Valid
	r.mediaURL, r.hasMediaURL = mu.String, mu.Valid
	r.mediaAlt, r.hasMediaAlt = ma.String, ma.Valid
	return r, nil
}

// wallPostSQL holds for a post of the unaliased posts table that the post
// wall and the archive list: not deleted or hidden, and not a promoted post that a shown
// own post's shown quote already carries. The second is a display
// rule computed at read time; the quote card returns the post to the wall
// when the quoting post or its quote is hidden.
const wallPostSQL = `deleted_at IS NULL AND hidden_at IS NULL AND NOT (promoted_at IS NOT NULL AND EXISTS (SELECT 1 FROM posts wq WHERE wq.source='x' AND wq.promoted_at IS NULL AND wq.deleted_at IS NULL AND wq.hidden_at IS NULL AND wq.quote_hidden_at IS NULL AND json_valid(wq.quote_json) AND json_extract(wq.quote_json,'$.id')=posts.id))`

// dashboardAccountPosts is how many of each account's newest posts the board
// always lists, however old, so every account's filter fills the wall's whole
// rows on its own (two rows of up to four columns; see
// src/dashboard/post-rows.ts), like the per-kind limits of the video shelf.
const dashboardAccountPosts = 8

// queryPosts lists the newest 100 posts published since the window start
// and, for every account, the newest dashboardAccountPosts of its posts.
func (s *Store) queryPosts(ctx context.Context, since string) ([]postRow, error) {
	instant := "julianday(published_at)"
	// The per-account newest posts are the own accounts' only: a promoted
	// post appears in the combined wall, never as an account of its own.
	rows, err := s.db.QueryContext(ctx, `SELECT `+postColumns+` FROM posts WHERE id IN (
		SELECT id FROM (SELECT id FROM posts WHERE `+wallPostSQL+` AND `+instant+` >= julianday(?) ORDER BY `+instant+` DESC, id ASC LIMIT 100)
		UNION SELECT id FROM (SELECT id, ROW_NUMBER() OVER (PARTITION BY lower(COALESCE(account_handle, '')) ORDER BY `+instant+` DESC, id ASC) AS n FROM posts WHERE deleted_at IS NULL AND hidden_at IS NULL AND promoted_at IS NULL) WHERE n <= ?
	) ORDER BY `+instant+` DESC, id ASC`, since, dashboardAccountPosts)
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	out := make([]postRow, 0, 100)
	for rows.Next() {
		r, err := scanPostRow(rows)
		if err != nil {
			return nil, dbErr(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, dbErr(err)
	}
	return out, nil
}

func (s *Store) queryLatestPost(ctx context.Context) (*postRow, error) {
	// The latest post is an own account's: a promoted post never takes it.
	r, err := scanPostRow(s.db.QueryRowContext(ctx, `SELECT `+postColumns+` FROM posts WHERE deleted_at IS NULL AND hidden_at IS NULL AND promoted_at IS NULL ORDER BY julianday(published_at) DESC,id ASC LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, dbErr(err)
	}
	return &r, nil
}

func (s *Store) queryPostCount(ctx context.Context, since string) (int64, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM posts WHERE `+wallPostSQL+` AND julianday(published_at) >= julianday(?)`, since).Scan(&count); err != nil {
		return 0, dbErr(err)
	}
	return count, nil
}

const dashboardVideoCountWindowDays = 7

// queryRecentVideoCount counts public videos published in the profile card's
// recent window. It stays separate from queryVideos, whose bounded result is
// the homepage's display pool.
func (s *Store) queryRecentVideoCount(ctx context.Context, now time.Time) (int64, error) {
	since := now.Add(-time.Duration(dashboardVideoCountWindowDays) * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	until := now.UTC().Format(time.RFC3339Nano)
	var count int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM videos WHERE `+publicVideoScope+` AND published_at IS NOT NULL AND julianday(published_at) >= julianday(?) AND julianday(published_at) <= julianday(?)`, since, until).Scan(&count); err != nil {
		return 0, dbErr(err)
	}
	return count, nil
}

func (s *Store) mapPost(ctx context.Context, r postRow) (Post, error) {
	p := Post{ID: r.id, Source: r.source, Type: r.typ, Text: r.text, PublishedAt: r.publishedAt, URL: r.url, Likes: r.likes, Reposts: r.reposts, Replies: r.replies, Promoted: r.promoted, RepostedBy: []string{}}
	if r.promoted && r.source == "x" {
		reposters, err := s.queryRepostedBy(ctx, r.id)
		if err != nil {
			return p, err
		}
		p.RepostedBy = reposters
	}
	if r.hasAccount {
		p.AccountHandle = strPtr(r.accountHandle)
	} else if h := xHandleFromURL(r.url); h != "" {
		p.AccountHandle = &h
	}
	if r.hasLabel {
		p.Label = strPtr(r.label)
	}
	if r.hasMediaURL {
		p.MediaURL = strPtr(r.mediaURL)
	}
	if r.hasMediaAlt {
		p.MediaAlt = strPtr(r.mediaAlt)
	}
	if p.AccountHandle != nil && r.source == "x" {
		u := "https://x.com/" + *p.AccountHandle
		p.AccountURL = &u
	}
	// A Bilibili dynamic's account is the mid of its space.
	if p.AccountHandle != nil && r.source == "bilibili" && bilibiliMid.MatchString(*p.AccountHandle) {
		u := "https://space.bilibili.com/" + *p.AccountHandle
		p.AccountURL = &u
	}
	ref, err := s.resolveMedia(ctx, p.MediaURL)
	if err != nil {
		return p, err
	}
	p.MediaID, p.MediaStatus, p.MediaSourceURL = ref.ID, ref.Status, ref.SourceURL
	if p.MediaURL != nil {
		p.MediaURL = ref.PublicURL
		p.Media = []PostMedia{{ID: ref.ID, URL: ref.PublicURL, Status: ref.Status, SourceURL: ref.SourceURL, Alt: p.MediaAlt, Position: 0, Width: ref.Width, Height: ref.Height, Kind: mediaKindPhoto}}
	}
	links, err := s.queryPostMedia(ctx, r.id, "post-image")
	if err != nil {
		return p, err
	}
	if len(links) > 0 {
		p.Media = links
		p.MediaID, p.MediaURL, p.MediaStatus, p.MediaSourceURL = links[0].ID, links[0].URL, links[0].Status, links[0].SourceURL
	}
	if p.Quote, err = s.mapQuote(ctx, r.source, r.id, r.quoteJSON); err != nil {
		return p, err
	}
	if r.source == "bilibili" && r.stickerRaw.Valid {
		if err = s.mapStickers(ctx, &p, r.stickerRaw.String); err != nil {
			return p, err
		}
	}
	return p, nil
}

// queryRepostedBy lists the own accounts that reposted a promoted post, first
// sighting first. x_reposts keeps the account in lower case; each takes the
// spelling of its own stored posts when it has any.
func (s *Store) queryRepostedBy(ctx context.Context, id string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT COALESCE((SELECT o.account_handle FROM posts o
			WHERE o.source='x' AND o.promoted_at IS NULL AND lower(o.account_handle)=r.account_handle LIMIT 1), r.account_handle)
		FROM x_reposts r WHERE r.status_id=? ORDER BY julianday(r.first_seen_at), r.account_handle`, id)
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var account string
		if err = rows.Scan(&account); err != nil {
			return nil, dbErr(err)
		}
		out = append(out, account)
	}
	return out, dbErr(rows.Err())
}

var (
	bilibiliMid      = regexp.MustCompile(`^[1-9][0-9]{0,15}$`)
	quoteIDPattern   = regexp.MustCompile(`^\d{10,22}$`)
	quotePathPattern = regexp.MustCompile(`^/[A-Za-z0-9_]{1,30}/status/(\d{10,22})$`)
	// A Bilibili quote names a dynamic by its post ID and links to its opus
	// page, or to the video page of a forwarded upload.
	bilibiliQuoteIDPattern = regexp.MustCompile(`^bilibili-([1-9][0-9]{8,24})$`)
	bilibiliOpusPath       = regexp.MustCompile(`^/opus/([1-9][0-9]{8,24})$`)
	bilibiliVideoPath      = regexp.MustCompile(`^/video/BV1[1-9A-HJ-NP-Za-km-z]{9}$`)
)

// quoteLinksTo reports whether a stored quote URL is exactly the x.com page
// of the status id.
func quoteLinksTo(raw, id string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "x.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	m := quotePathPattern.FindStringSubmatch(u.Path)
	return m != nil && m[1] == id
}

// bilibiliQuoteLinksTo reports whether a stored Bilibili quote URL is exactly
// the opus page of the dynamic id, or a video page when video is allowed.
func bilibiliQuoteLinksTo(raw, id string, video bool) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "www.bilibili.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if m := bilibiliOpusPath.FindStringSubmatch(u.Path); m != nil {
		return m[1] == id
	}
	return video && bilibiliVideoPath.MatchString(u.Path)
}

// validQuote reports whether a stored projection is a quote the post of
// source and postID may show: an X post quotes an x.com status, a Bilibili
// post (a forward) a Bilibili dynamic. A forward of a dynamic whose ID the
// listing did not give is unavailable and links to the forward itself.
func validQuote(source, postID string, q PostQuote) bool {
	if source != "bilibili" {
		return quoteIDPattern.MatchString(q.ID) && quoteLinksTo(q.URL, q.ID)
	}
	if q.ID == "" {
		own := bilibiliQuoteIDPattern.FindStringSubmatch(postID)
		return q.Unavailable && own != nil && bilibiliQuoteLinksTo(q.URL, own[1], false)
	}
	m := bilibiliQuoteIDPattern.FindStringSubmatch(q.ID)
	return m != nil && bilibiliQuoteLinksTo(q.URL, m[1], !q.Unavailable)
}

// mapQuote reads a post's stored quote. A projection that is not a valid
// quote for the post (see validQuote) is left out; its images are the post's
// quote-image links whose cached files are ready.
func (s *Store) mapQuote(ctx context.Context, source, postID string, stored sql.NullString) (*PostQuote, error) {
	if !stored.Valid || stored.String == "" {
		return nil, nil
	}
	var q PostQuote
	if json.Unmarshal([]byte(stored.String), &q) != nil || !validQuote(source, postID, q) {
		return nil, nil
	}
	q.Media = []PostMedia{}
	if q.Unavailable {
		q.AuthorHandle, q.AuthorName, q.Text, q.PublishedAt = "", "", "", ""
		return &q, nil
	}
	links, err := s.queryPostMedia(ctx, postID, roleQuoteImage)
	if err != nil {
		return nil, err
	}
	for _, link := range links {
		if link.Status == "ready" && link.ID != nil && link.URL != nil {
			q.Media = append(q.Media, link)
		}
	}
	return &q, nil
}

// postMediaColumns are scanPostMedia's columns of a media link joined with
// its asset.
const postMediaColumns = `ml.media_id,ml.position,ml.alt,ma.source_url,ma.cache_path,ma.mime_type,ma.byte_size,ma.sha256,NULLIF(ma.width,0),NULLIF(ma.height,0),ma.status,COALESCE(ml.kind,'photo')`

func (s *Store) queryPostMedia(ctx context.Context, id, role string) ([]PostMedia, error) {
	if memo := readMemoFrom(ctx); memo != nil && memo.postsLoaded[id] {
		return append([]PostMedia{}, memo.postLinks[postLinkKey(id, role)]...), nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+postMediaColumns+` FROM media_links ml LEFT JOIN media_assets ma ON ma.id=ml.media_id WHERE ml.owner_type='post' AND ml.owner_id=? AND ml.role=? ORDER BY ml.position ASC`, id, role)
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	out := []PostMedia{}
	for rows.Next() {
		media, ok, err := s.scanPostMedia(ctx, rows)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, media)
		}
	}
	return out, rows.Err()
}

// scanPostMedia reads one postMediaColumns row, after any leading columns
// passed in lead. A link without a usable asset reports false.
func (s *Store) scanPostMedia(ctx context.Context, row scanner, lead ...any) (PostMedia, bool, error) {
	var mid string
	var pos int64
	var alt, src, cp, mime, sha sql.NullString
	var bs, w, h sql.NullInt64
	var status sql.NullString
	var kind string
	if err := row.Scan(append(lead, &mid, &pos, &alt, &src, &cp, &mime, &bs, &sha, &w, &h, &status, &kind)...); err != nil {
		return PostMedia{}, false, dbErr(err)
	}
	if !src.Valid || !status.Valid {
		return PostMedia{}, false, nil
	}
	asset := mediaAsset{ID: mid, SourceURL: src.String, Status: status.String}
	if cp.Valid {
		asset.CachePath = &cp.String
	}
	if mime.Valid {
		asset.MIMEType = &mime.String
	}
	if bs.Valid {
		asset.ByteSize = &bs.Int64
	}
	if sha.Valid {
		asset.SHA256 = &sha.String
	}
	if w.Valid {
		asset.Width = &w.Int64
	}
	if h.Valid {
		asset.Height = &h.Int64
	}
	ref := s.mediaReference(ctx, asset)
	if ref == nil {
		return PostMedia{}, false, nil
	}
	var ap *string
	if alt.Valid {
		ap = &alt.String
	}
	return PostMedia{ID: ref.ID, URL: ref.PublicURL, Status: ref.Status, SourceURL: ref.SourceURL, Alt: ap, Position: pos, Width: ref.Width, Height: ref.Height, Kind: kind}, true, nil
}
