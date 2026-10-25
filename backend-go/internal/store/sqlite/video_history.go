package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Video history keeps what a platform reported before a creator replaced it:
// video_revisions holds the superseded title and thumbnail of a video, and
// media_versions keeps the earlier files of a thumbnail URL whose bytes
// changed. Old files stay in the content-addressed cache, so an operator can
// pin any recorded version. Operator overrides are separate columns on
// videos and are not revisions.

// publicVideoScope selects the videos public readers (the board, the video
// archive and MCP) list. Bilibili uploads stay in the console, so the board
// never lists one cover twice; an upload merged into the YouTube copy of the
// same work reaches readers as that video's alternate (video_merge.go).
const publicVideoScope = "source = 'youtube'"

// Revision origins: a detail check that reported a new title or thumbnail
// URL, or a thumbnail revalidation that downloaded new bytes for the same URL.
const (
	revisionSync  = "sync"
	revisionMedia = "media"
)

// videoSource is what the platform last reported for one video.
type videoSource struct {
	Title, ThumbnailURL, Kind string
	MediaID, SHA256           sql.NullString
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// readVideoSource reads the stored platform fields and the ready file behind
// the linked thumbnail. It reports false for an unknown video.
func readVideoSource(ctx context.Context, q queryRower, id string) (videoSource, bool, error) {
	var v videoSource
	err := q.QueryRowContext(ctx, `SELECT v.title,COALESCE(v.thumbnail_url,''),COALESCE(v.kind,''),
 (SELECT ml.media_id FROM media_links ml WHERE ml.owner_type='video' AND ml.owner_id=v.id AND ml.role='thumbnail' ORDER BY ml.position LIMIT 1),
 (SELECT a.sha256 FROM media_links ml JOIN media_assets a ON a.id=ml.media_id WHERE ml.owner_type='video' AND ml.owner_id=v.id AND ml.role='thumbnail' AND a.status='ready' ORDER BY ml.position LIMIT 1)
 FROM videos v WHERE v.id=?`, id).Scan(&v.Title, &v.ThumbnailURL, &v.Kind, &v.MediaID, &v.SHA256)
	if errors.Is(err, sql.ErrNoRows) {
		return v, false, nil
	}
	return v, err == nil, err
}

// recordVideoRevision stores before as the state origin replaced at now.
func recordVideoRevision(ctx context.Context, tx *sql.Tx, videoID string, before videoSource, origin, now string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO video_revisions(video_id,origin,title,thumbnail_url,thumbnail_media_id,thumbnail_sha256,kind,replaced_at) VALUES(?,?,?,?,?,?,?,?)`,
		videoID, origin, before.Title, nullable(before.ThumbnailURL), before.MediaID, before.SHA256, nullable(before.Kind), now)
	return err
}

// recordMediaVersion keeps the ready file a revalidation is about to replace
// and records the thumbnail it was for every video that links it. It runs
// inside the media write, before the asset row changes.
func recordMediaVersion(ctx context.Context, tx *sql.Tx, mediaID, now string) error {
	var hash, cachePath, mime sql.NullString
	var size sql.NullInt64
	var fetched sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT sha256,cache_path,mime_type,byte_size,fetched_at FROM media_assets WHERE id=? AND status='ready'`, mediaID).Scan(&hash, &cachePath, &mime, &size, &fetched)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (!hash.Valid || !safeHash(hash.String) || !cachePath.Valid || !mime.Valid) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO media_versions(media_id,sha256,cache_path,mime_type,byte_size,fetched_at,replaced_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(media_id,sha256) DO UPDATE SET replaced_at=excluded.replaced_at`,
		mediaID, hash.String, cachePath.String, mime.String, size, fetched, now); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT owner_id FROM media_links WHERE media_id=? AND owner_type='video' AND role='thumbnail'`, mediaID)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		before, ok, readErr := readVideoSource(ctx, tx, id)
		if readErr != nil {
			return readErr
		}
		if ok {
			if err = recordVideoRevision(ctx, tx, id, before, revisionMedia, now); err != nil {
				return err
			}
		}
	}
	return nil
}

// VideoRevision is one superseded platform state, newest first. Thumbnail is
// the local URL of the file shown then, or null when it is not cached.
type VideoRevision struct {
	ID              int64   `json:"id"`
	Origin          string  `json:"origin"`
	Title           string  `json:"title"`
	Kind            *string `json:"kind"`
	ThumbnailURL    *string `json:"thumbnailUrl"`
	ThumbnailStatus string  `json:"thumbnailStatus"`
	ReplacedAt      string  `json:"replacedAt"`
}

// VideoThumbnail is a cached thumbnail file an operator may pin: the current
// platform file, an earlier file of the same URL, or the file of an earlier
// URL.
type VideoThumbnail struct {
	MediaID   string  `json:"mediaId"`
	SHA256    string  `json:"sha256"`
	URL       string  `json:"url"`
	FetchedAt *string `json:"fetchedAt"`
	// Current is the platform's thumbnail now; Selected is the operator's pin.
	Current  bool `json:"current"`
	Selected bool `json:"selected"`
}

// VideoHistory is the operator view of one video's changes.
type VideoHistory struct {
	Video      AdminVideo       `json:"video"`
	Revisions  []VideoRevision  `json:"revisions"`
	Thumbnails []VideoThumbnail `json:"thumbnails"`
}

// maxVideoRevisions bounds the history one response lists.
const maxVideoRevisions = 50

// VideoHistory lists a video's superseded states and the thumbnail files an
// operator may pin. It returns sql.ErrNoRows for an unknown video.
func (s *Store) VideoHistory(ctx context.Context, id string) (VideoHistory, error) {
	video, err := s.AdminVideoByID(ctx, id)
	if err != nil {
		return VideoHistory{}, err
	}
	out := VideoHistory{Video: video, Revisions: []VideoRevision{}, Thumbnails: []VideoThumbnail{}}
	rows, err := s.db.QueryContext(ctx, `SELECT id,origin,COALESCE(title,''),kind,thumbnail_media_id,thumbnail_sha256,replaced_at FROM video_revisions WHERE video_id=? ORDER BY id DESC LIMIT ?`, id, maxVideoRevisions)
	if err != nil {
		return VideoHistory{}, fmt.Errorf("%w: video history unavailable", ErrUnavailable)
	}
	type pinned struct{ media, hash sql.NullString }
	files := []pinned{}
	for rows.Next() {
		var r VideoRevision
		var kind, media, hash sql.NullString
		if err = rows.Scan(&r.ID, &r.Origin, &r.Title, &kind, &media, &hash, &r.ReplacedAt); err != nil {
			rows.Close()
			return VideoHistory{}, fmt.Errorf("%w: video history unavailable", ErrUnavailable)
		}
		r.Kind = nullString(kind)
		out.Revisions = append(out.Revisions, r)
		files = append(files, pinned{media, hash})
	}
	rows.Close()
	if rows.Err() != nil {
		return VideoHistory{}, fmt.Errorf("%w: video history unavailable", ErrUnavailable)
	}
	for i := range out.Revisions {
		out.Revisions[i].ThumbnailStatus = "missing"
		if url, ok := s.mediaVersionURL(ctx, files[i].media.String, files[i].hash.String); ok {
			out.Revisions[i].ThumbnailURL, out.Revisions[i].ThumbnailStatus = &url, "ready"
		}
	}
	if out.Thumbnails, err = s.videoThumbnails(ctx, id); err != nil {
		return VideoHistory{}, fmt.Errorf("%w: video history unavailable", ErrUnavailable)
	}
	return out, nil
}

// videoThumbnails lists the servable thumbnail files of a video, current
// first and then newest replaced first, without duplicates.
func (s *Store) videoThumbnails(ctx context.Context, id string) ([]VideoThumbnail, error) {
	var manualMedia, manualHash sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT manual_thumbnail_media_id,manual_thumbnail_sha256 FROM videos WHERE id=?`, id).Scan(&manualMedia, &manualHash); err != nil {
		return nil, err
	}
	// Candidates: the linked asset's current file, earlier files of every
	// asset this video has linked, and the files its revisions recorded.
	rows, err := s.db.QueryContext(ctx, `
 SELECT a.id,a.sha256,a.fetched_at,1,0 FROM media_links ml JOIN media_assets a ON a.id=ml.media_id
  WHERE ml.owner_type='video' AND ml.owner_id=?1 AND ml.role='thumbnail' AND a.status='ready' AND a.sha256 IS NOT NULL
 UNION ALL
 SELECT a.id,a.sha256,a.fetched_at,0,1 FROM media_assets a
  WHERE a.status='ready' AND a.sha256 IS NOT NULL AND a.id IN (SELECT thumbnail_media_id FROM video_revisions WHERE video_id=?1)
 UNION ALL
 SELECT mv.media_id,mv.sha256,mv.fetched_at,0,2 FROM media_versions mv
  WHERE mv.media_id IN (SELECT ml.media_id FROM media_links ml WHERE ml.owner_type='video' AND ml.owner_id=?1 AND ml.role='thumbnail'
   UNION SELECT thumbnail_media_id FROM video_revisions WHERE video_id=?1)
 ORDER BY 5,3 DESC`, id)
	if err != nil {
		return nil, err
	}
	type candidate struct {
		media, hash string
		fetched     sql.NullString
		current     bool
	}
	candidates := []candidate{}
	for rows.Next() {
		var c candidate
		var current, rank int
		if err = rows.Scan(&c.media, &c.hash, &c.fetched, &current, &rank); err != nil {
			rows.Close()
			return nil, err
		}
		c.current = current == 1
		candidates = append(candidates, c)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	// The same bytes under another URL are one image.
	out := []VideoThumbnail{}
	seen := map[string]bool{}
	for _, c := range candidates {
		if seen[c.hash] {
			continue
		}
		url, ok := s.mediaVersionURL(ctx, c.media, c.hash)
		if !ok {
			continue
		}
		seen[c.hash] = true
		out = append(out, VideoThumbnail{
			MediaID: c.media, SHA256: c.hash, URL: url, FetchedAt: nullString(c.fetched), Current: c.current,
			Selected: manualMedia.String == c.media && manualHash.String == c.hash,
		})
	}
	return out, nil
}

// pinVideoThumbnails shows each video's operator-pinned thumbnail in place of
// the platform's while the pinned file is still cached.
func (s *Store) pinVideoThumbnails(ctx context.Context, videos []Video) error {
	if len(videos) == 0 {
		return nil
	}
	args := make([]any, len(videos))
	for i := range videos {
		args[i] = videos[i].ID
	}
	rows, err := s.db.QueryContext(ctx, `SELECT v.id,v.manual_thumbnail_media_id,v.manual_thumbnail_sha256,a.source_url FROM videos v
 LEFT JOIN media_assets a ON a.id=v.manual_thumbnail_media_id
 WHERE v.manual_thumbnail_media_id IS NOT NULL AND v.manual_thumbnail_sha256 IS NOT NULL AND v.id IN (?`+strings.Repeat(",?", len(videos)-1)+`)`, args...)
	if err != nil {
		return dbErr(err)
	}
	type pin struct {
		media, hash string
		source      sql.NullString
	}
	pins := map[string]pin{}
	for rows.Next() {
		var id string
		var p pin
		if err = rows.Scan(&id, &p.media, &p.hash, &p.source); err != nil {
			rows.Close()
			return dbErr(err)
		}
		pins[id] = p
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return dbErr(err)
	}
	for i := range videos {
		p, ok := pins[videos[i].ID]
		if !ok {
			continue
		}
		if url, servable := s.mediaVersionURL(ctx, p.media, p.hash); servable {
			media := p.media
			videos[i].ThumbnailURL, videos[i].ThumbnailID, videos[i].ThumbnailStatus = &url, &media, "ready"
			videos[i].ThumbnailSourceURL = nullString(p.source)
		}
	}
	return nil
}

// mediaVersionURL is the local URL of one cached file of an asset, either its
// current ready file or a recorded earlier version, when the file is present.
func (s *Store) mediaVersionURL(ctx context.Context, mediaID, hash string) (string, bool) {
	if !safeHash(mediaID) || !safeHash(hash) {
		return "", false
	}
	var cachePath string
	err := s.db.QueryRowContext(ctx, `SELECT cache_path FROM media_assets WHERE id=?1 AND status='ready' AND sha256=?2 AND cache_path IS NOT NULL
 UNION ALL SELECT cache_path FROM media_versions WHERE media_id=?1 AND sha256=?2 LIMIT 1`, mediaID, hash).Scan(&cachePath)
	if err != nil || !s.cacheFileExists(cachePath) {
		return "", false
	}
	return "/media/" + mediaID + "?v=" + hash, true
}
