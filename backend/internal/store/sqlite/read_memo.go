package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// readMemo holds the lookups that the owners of one public read share, so a
// projection costs a few queries and one file check per media item instead
// of several queries and path resolutions per owner. It lives in that read's
// context: nothing outlives the read, and a read runs on one goroutine.
type readMemo struct {
	// dirs maps cleaned directory paths to their link-resolved paths.
	dirs map[string]resolution
	// assets maps prefetched normalized source URLs to their media row, or
	// nil when no row exists.
	assets map[string]*mediaAsset
	// postLinks maps postLinkKey(post ID, role) to the post's media links;
	// postsLoaded lists the posts whose links were prefetched.
	postLinks   map[string][]PostMedia
	postsLoaded map[string]bool
}

type readMemoKey struct{}

// withReadMemo returns ctx carrying a memo, reusing one already present.
func withReadMemo(ctx context.Context) context.Context {
	if readMemoFrom(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, readMemoKey{}, &readMemo{
		dirs: map[string]resolution{}, assets: map[string]*mediaAsset{},
		postLinks: map[string][]PostMedia{}, postsLoaded: map[string]bool{},
	})
}

func readMemoFrom(ctx context.Context) *readMemo {
	memo, _ := ctx.Value(readMemoKey{}).(*readMemo)
	return memo
}

// memoChunk bounds the bound parameters of one batched lookup.
const memoChunk = 400

// postLinkKey keys a post's links of one role in readMemo.postLinks.
func postLinkKey(id, role string) string { return id + "\x00" + role }

// prefetchMedia loads the media rows of many source URLs in batched queries.
// resolveMedia then answers them from the memo.
func (s *Store) prefetchMedia(ctx context.Context, sources ...*string) error {
	memo := readMemoFrom(ctx)
	if memo == nil {
		return nil
	}
	wanted := []string{}
	for _, source := range sources {
		if source == nil {
			continue
		}
		v := strings.TrimSpace(*source)
		if v == "" || strings.HasPrefix(v, "/assets/") {
			continue
		}
		norm := normalizeSourceURL(v)
		if _, known := memo.assets[norm]; norm == "" || known {
			continue
		}
		memo.assets[norm] = nil
		wanted = append(wanted, norm)
	}
	for start := 0; start < len(wanted); start += memoChunk {
		chunk := wanted[start:min(len(wanted), start+memoChunk)]
		args := make([]any, len(chunk))
		for i, v := range chunk {
			args[i] = v
		}
		rows, err := s.db.QueryContext(ctx, `SELECT id,source_url,cache_path,mime_type,byte_size,sha256,NULLIF(width,0),NULLIF(height,0),status FROM media_assets WHERE source_url IN (`+placeholders(len(chunk))+`)`, args...)
		if err != nil {
			return dbErr(err)
		}
		for rows.Next() {
			a, scanErr := scanMediaAsset(rows)
			if scanErr != nil {
				_ = rows.Close()
				return scanErr
			}
			memo.assets[a.SourceURL] = &a
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return dbErr(err)
		}
	}
	return nil
}

func scanMediaAsset(row scanner) (mediaAsset, error) {
	var a mediaAsset
	var cp, mime, sha sql.NullString
	var bs, w, h sql.NullInt64
	if err := row.Scan(&a.ID, &a.SourceURL, &cp, &mime, &bs, &sha, &w, &h, &a.Status); err != nil {
		return a, err
	}
	if cp.Valid {
		a.CachePath = &cp.String
	}
	if mime.Valid {
		a.MIMEType = &mime.String
	}
	if bs.Valid {
		a.ByteSize = &bs.Int64
	}
	if sha.Valid {
		a.SHA256 = &sha.String
	}
	if w.Valid {
		a.Width = &w.Int64
	}
	if h.Valid {
		a.Height = &h.Int64
	}
	return a, nil
}

// prefetchPostMedia loads the image and quote-image links of many posts in
// batched queries. queryPostMedia then answers them from the memo.
func (s *Store) prefetchPostMedia(ctx context.Context, ids ...string) error {
	memo := readMemoFrom(ctx)
	if memo == nil {
		return nil
	}
	wanted := []string{}
	for _, id := range ids {
		if !memo.postsLoaded[id] {
			memo.postsLoaded[id] = true
			wanted = append(wanted, id)
		}
	}
	for start := 0; start < len(wanted); start += memoChunk {
		chunk := wanted[start:min(len(wanted), start+memoChunk)]
		args := make([]any, 0, len(chunk)+1)
		args = append(args, roleQuoteImage)
		for _, id := range chunk {
			args = append(args, id)
		}
		rows, err := s.db.QueryContext(ctx, `SELECT ml.owner_id,ml.role,`+postMediaColumns+` FROM media_links ml LEFT JOIN media_assets ma ON ma.id=ml.media_id WHERE ml.owner_type='post' AND ml.role IN ('post-image',?) AND ml.owner_id IN (`+placeholders(len(chunk))+`) ORDER BY ml.owner_id,ml.role,ml.position ASC`, args...)
		if err != nil {
			return dbErr(err)
		}
		for rows.Next() {
			var owner, role string
			media, ok, scanErr := s.scanPostMedia(ctx, rows, &owner, &role)
			if scanErr != nil {
				_ = rows.Close()
				return scanErr
			}
			if ok {
				key := postLinkKey(owner, role)
				memo.postLinks[key] = append(memo.postLinks[key], media)
			}
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return dbErr(err)
		}
	}
	return nil
}

// cacheFileExists reports whether a relative cache path names a regular file
// inside the data root. A regular file needs one Lstat plus its directory's
// resolution, which the read's memo shares; a link resolves in full.
func (s *Store) cacheFileExists(ctx context.Context, relative string) bool {
	if filepath.IsAbs(relative) {
		return false
	}
	candidate := filepath.Join(s.dataRoot, filepath.FromSlash(relative))
	info, err := os.Lstat(candidate)
	if err != nil {
		return false
	}
	if info.Mode().IsRegular() {
		return s.dirInsideDataRoot(ctx, filepath.Dir(candidate))
	}
	if info.Mode()&os.ModeSymlink == 0 || !s.safeDataPath(relative) {
		return false
	}
	info, err = os.Stat(candidate)
	return err == nil && info.Mode().IsRegular()
}

// dirInsideDataRoot reports whether a directory, with its links resolved,
// is the data root or lies inside it.
func (s *Store) dirInsideDataRoot(ctx context.Context, dir string) bool {
	memo := readMemoFrom(ctx)
	rootPath := filepath.Clean(s.dataRoot)
	root := resolveDir(memo, rootPath, rootPath)
	resolved := resolveDir(memo, filepath.Clean(dir), rootPath)
	return root.ok && resolved.ok && (resolved.path == root.path || strings.HasPrefix(resolved.path, root.path+string(os.PathSeparator)))
}

// resolution is a directory's path with its links resolved; ok is false when
// it cannot be resolved.
type resolution struct {
	path string
	ok   bool
}

// resolveDir resolves a directory's links. With a memo, a plain directory
// below the data root extends its parent's resolution by one Lstat; the root
// itself, links, junctions and paths outside the root resolve in full.
func resolveDir(memo *readMemo, dir, root string) resolution {
	if memo == nil {
		return fullResolution(dir)
	}
	if known, ok := memo.dirs[dir]; ok {
		return known
	}
	var r resolution
	if dir != root && strings.HasPrefix(dir, root+string(os.PathSeparator)) {
		if info, err := os.Lstat(dir); err == nil && info.Mode().Type() == os.ModeDir {
			if parent := resolveDir(memo, filepath.Dir(dir), root); parent.ok {
				r = resolution{filepath.Join(parent.path, filepath.Base(dir)), true}
			}
		} else if err == nil {
			r = fullResolution(dir)
		}
	} else {
		r = fullResolution(dir)
	}
	memo.dirs[dir] = r
	return r
}

func fullResolution(path string) resolution {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return resolution{}
	}
	resolved, err = filepath.Abs(resolved)
	return resolution{resolved, err == nil}
}

// eventColumns are scanEvent's columns.
const eventColumns = `id,source,title,detail,starts_on,starts_at,ends_at,timezone,time_precision,status,event_type,url,provenance,manual_locked,cancellation_status,cancellation_source,cancellation_reason,cancellation_evidence,cancellation_at,updated_at`

// queryLinkedEvents finds each video's linked event in one query: the
// non-deleted event that names the video by ID, source evidence or stream
// URL, nearest to its scheduled time, then by event ID.
func (s *Store) queryLinkedEvents(ctx context.Context, videos []Video, now time.Time) (map[string]*DashboardEvent, error) {
	out := map[string]*DashboardEvent{}
	for start := 0; start < len(videos); start += memoChunk / 3 {
		chunk := videos[start:min(len(videos), start+memoChunk/3)]
		args := make([]any, 0, len(chunk)*3)
		for _, v := range chunk {
			args = append(args, v.ID, v.URL, stringValue(v.ScheduledAt))
		}
		rows, err := s.db.QueryContext(ctx, `WITH v(vid,vurl,vat) AS (VALUES `+strings.TrimSuffix(strings.Repeat("(?,?,?),", len(chunk)), ",")+`)
 SELECT vid,`+eventColumns+` FROM (
 SELECT v.vid,e.`+strings.ReplaceAll(eventColumns, ",", ",e.")+`,ROW_NUMBER() OVER (PARTITION BY v.vid ORDER BY ABS(julianday(e.starts_at)-julianday(v.vat)),e.id ASC) AS pick FROM v JOIN events e ON e.deleted_at IS NULL AND (e.id='youtube-'||v.vid OR (e.source='youtube' AND e.source_item_id=v.vid) OR EXISTS (SELECT 1 FROM event_sources es WHERE es.event_id=e.id AND es.source='youtube' AND es.source_item_id=v.vid) OR (e.url IS NOT NULL AND stream_identity(e.url)=stream_identity(v.vurl)))
 ) WHERE pick=1`, args...)
		if err != nil {
			return nil, dbErr(err)
		}
		for rows.Next() {
			var vid string
			e, scanErr := scanEvent(prefixedScanner{rows, &vid}, now)
			if scanErr != nil {
				_ = rows.Close()
				return nil, scanErr
			}
			out[vid] = &e
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, dbErr(err)
		}
	}
	return out, nil
}

// cancelledVideos lists the videos that an operator-confirmed cancellation
// names by ID, source evidence or stream URL.
func (s *Store) cancelledVideos(ctx context.Context, videos []Video) (map[string]bool, error) {
	out := map[string]bool{}
	for start := 0; start < len(videos); start += memoChunk / 2 {
		chunk := videos[start:min(len(videos), start+memoChunk/2)]
		args := make([]any, 0, len(chunk)*2)
		for _, v := range chunk {
			args = append(args, v.ID, v.URL)
		}
		rows, err := s.db.QueryContext(ctx, `WITH v(vid,vurl) AS (VALUES `+strings.TrimSuffix(strings.Repeat("(?,?),", len(chunk)), ",")+`)
 SELECT v.vid FROM v WHERE EXISTS (SELECT 1 FROM events e WHERE e.cancellation_status='manual_confirmed' AND (e.id='youtube-'||v.vid OR (e.source='youtube' AND e.source_item_id=v.vid) OR EXISTS (SELECT 1 FROM event_sources es WHERE es.event_id=e.id AND es.source='youtube' AND es.source_item_id=v.vid) OR (e.url IS NOT NULL AND stream_identity(e.url)=stream_identity(v.vurl))))`, args...)
		if err != nil {
			return nil, dbErr(err)
		}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				_ = rows.Close()
				return nil, dbErr(err)
			}
			out[id] = true
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, dbErr(err)
		}
	}
	return out, nil
}

// prefixedScanner scans a leading column into first and the rest through a
// row scanner shared with unprefixed queries.
type prefixedScanner struct {
	rows  scanner
	first *string
}

func (p prefixedScanner) Scan(dest ...any) error {
	return p.rows.Scan(append([]any{p.first}, dest...)...)
}

// prefetchPostRows loads the media rows and links that mapPost reads for
// these posts.
func (s *Store) prefetchPostRows(ctx context.Context, rows []postRow) error {
	ids := make([]string, 0, len(rows))
	sources := make([]*string, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].id)
		if rows[i].hasMediaURL {
			sources = append(sources, &rows[i].mediaURL)
		}
	}
	if err := s.prefetchMedia(ctx, sources...); err != nil {
		return err
	}
	return s.prefetchPostMedia(ctx, ids...)
}

// resolveVideoThumbnails replaces each video's stored thumbnail URL with its
// resolved media reference.
func (s *Store) resolveVideoThumbnails(ctx context.Context, videos []Video) error {
	sources := make([]*string, len(videos))
	for i := range videos {
		sources[i] = videos[i].ThumbnailURL
	}
	if err := s.prefetchMedia(ctx, sources...); err != nil {
		return err
	}
	for i := range videos {
		ref, err := s.resolveMedia(ctx, videos[i].ThumbnailURL)
		if err != nil {
			return err
		}
		videos[i].ThumbnailID, videos[i].ThumbnailStatus, videos[i].ThumbnailSourceURL = ref.ID, ref.Status, ref.SourceURL
		videos[i].ThumbnailURL = ref.PublicURL
	}
	return nil
}

// prefetchAssetMedia loads the media rows of these assets' images.
func (s *Store) prefetchAssetMedia(ctx context.Context, assets []Asset) error {
	sources := make([]*string, len(assets))
	for i := range assets {
		sources[i] = assets[i].URL
	}
	return s.prefetchMedia(ctx, sources...)
}
