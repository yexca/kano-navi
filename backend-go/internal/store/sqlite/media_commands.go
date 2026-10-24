package sqlite

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/mediaformat"
)

// PendingMedia is an image the media step requests. ETag and LastModified are
// set only for a ready file that is being revalidated.
type PendingMedia struct{ ID, Source, SourceURL, ETag, LastModified string }

const (
	// mediaRecheckAfter spaces retries of a transient failure and
	// revalidations of a thumbnail that may still be replaced.
	mediaRecheckAfter = 30 * time.Minute
	// mediaLiveWindow is how long after its start or publication a video's
	// thumbnail is revalidated; an upcoming stream's is revalidated until then.
	mediaLiveWindow = 24 * time.Hour
)

func (s *Store) PendingMedia(ctx context.Context, limit int) ([]PendingMedia, error) {
	if s == nil || !s.workflowWritable {
		return nil, ErrSourceWrite
	}
	if limit < 1 || limit > 100 {
		return nil, ErrSourceWrite
	}
	now := time.Now()
	recheck, live := workflowTime(now.Add(-mediaRecheckAfter)), workflowTime(now.Add(-mediaLiveWindow))
	// Never-checked assets come first, newest owner first, so the latest video
	// or post the board shows is not left behind by a same-sync tie on
	// created_at. Each owner lookup is a primary-key probe via media_links_media_idx.
	// Transient download failures are retried, and a ready YouTube thumbnail of
	// an upcoming or recent video is revalidated because its URL stays the same
	// when the creator replaces the image.
	rows, err := s.db.QueryContext(ctx, `SELECT id,source,source_url,etag,last_modified FROM (
		SELECT a.id,a.source,a.source_url,a.last_checked_at,a.created_at,
		CASE WHEN a.status='ready' THEN COALESCE(a.etag,'') ELSE '' END AS etag,
		CASE WHEN a.status='ready' THEN COALESCE(a.last_modified,'') ELSE '' END AS last_modified,(
			SELECT MAX(COALESCE(
				(SELECT julianday(COALESCE(v.scheduled_at,v.published_at)) FROM videos v WHERE ml.owner_type='video' AND v.id=ml.owner_id),
				(SELECT julianday(p.published_at) FROM posts p WHERE ml.owner_type='post' AND p.id=ml.owner_id),
				(SELECT julianday(p.published_at) FROM posts p WHERE ml.owner_type='asset' AND ml.owner_id GLOB 'schedule-x-*' AND p.id=substr(ml.owner_id,12))
			)) FROM media_links ml WHERE ml.media_id=a.id
		) AS owner_instant
		FROM media_assets a WHERE a.source IN('x','youtube') AND length(a.id)=64 AND a.id NOT GLOB '*[^a-f0-9]*' AND (
			a.status='pending'
			OR (a.status='failed' AND a.last_error IN('media_timeout','media_unavailable','media_http_error','media_body_unavailable')
				AND julianday(COALESCE(a.last_checked_at,a.created_at))<=julianday(?))
			OR (a.status='ready' AND a.source='youtube' AND julianday(COALESCE(a.last_checked_at,a.fetched_at,a.created_at))<=julianday(?)
				AND EXISTS (SELECT 1 FROM media_links ml JOIN videos v ON v.id=ml.owner_id WHERE ml.media_id=a.id AND ml.owner_type='video' AND ml.role='thumbnail'
					AND (v.is_upcoming=1 OR julianday(COALESCE(v.scheduled_at,v.published_at))>=julianday(?))))
		)
	) ORDER BY last_checked_at IS NOT NULL,COALESCE(owner_instant,0) DESC,COALESCE(last_checked_at,created_at),id LIMIT ?`, recheck, recheck, live, limit)
	if err != nil {
		return nil, ErrSourceWrite
	}
	defer rows.Close()
	out := []PendingMedia{}
	for rows.Next() {
		var a PendingMedia
		if rows.Scan(&a.ID, &a.Source, &a.SourceURL, &a.ETag, &a.LastModified) != nil {
			return nil, ErrSourceWrite
		}
		out = append(out, a)
	}
	if rows.Err() != nil {
		return nil, ErrSourceWrite
	}
	return out, nil
}

// Successful writes publish an immutable content-addressed file and metadata
// under the same source lease. Failures retain a previously ready snapshot.
func (s *Store) SaveMediaDownload(ctx context.Context, asset PendingMedia, content []byte, mime, etag, lastModified, status, code string) error {
	if s == nil || !s.workflowWritable || !safeHash(asset.ID) || (asset.Source != "x" && asset.Source != "youtube") {
		return ErrSourceWrite
	}
	if status != "ready" && status != "failed" && status != "missing" {
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
	if status != "ready" {
		_, err = tx.ExecContext(ctx, `UPDATE media_assets SET status=CASE WHEN status='ready' THEN status ELSE ? END,last_error=?,last_checked_at=?,updated_at=? WHERE id=? AND source_url=?`, status, code, now, now, asset.ID, asset.SourceURL)
	} else {
		if len(content) == 0 || len(content) > maxCachedMediaBytes || mediaformat.Sniff(content) != mime || mediaformat.Extension(mime) == "" {
			return ErrSourceWrite
		}
		h := sha256.Sum256(content)
		hash := hex.EncodeToString(h[:])
		relative := asset.Source + "/sha256/" + hash[:2] + "/" + hash + "." + mediaformat.Extension(mime)
		root, e := os.OpenRoot(s.dataRoot)
		if e != nil {
			return ErrSourceWrite
		}
		defer root.Close()
		dir := filepath.Dir(filepath.FromSlash(relative))
		if root.MkdirAll(dir, 0755) != nil {
			return ErrSourceWrite
		}
		// Root operations confine all resolution (including symlink races) to
		// data/. A temporary file in the target directory is atomically renamed.
		b := make([]byte, 16)
		if _, e = rand.Read(b); e != nil {
			return ErrSourceWrite
		}
		temp := filepath.Join(dir, ".download-"+hex.EncodeToString(b))
		f, e := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return ErrSourceWrite
		}
		defer root.Remove(temp)
		_, writeErr := f.Write(content)
		syncErr := f.Sync()
		closeErr := f.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil {
			return ErrSourceWrite
		}
		// An existing immutable file is reusable only after its hash agrees.
		existing, e := root.Open(filepath.FromSlash(relative))
		if e == nil {
			info, statErr := existing.Stat()
			var actual []byte
			if statErr == nil && info.Mode().IsRegular() && info.Size() == int64(len(content)) {
				actual, _ = io.ReadAll(io.LimitReader(existing, maxCachedMediaBytes+1))
			}
			existing.Close()
			if sha256.Sum256(actual) != h {
				return ErrSourceWrite
			}
		} else if errors.Is(e, os.ErrNotExist) {
			if root.Rename(temp, filepath.FromSlash(relative)) != nil {
				return ErrSourceWrite
			}
		} else {
			return ErrSourceWrite
		}
		// A revalidation that downloads the same bytes changes nothing the board
		// shows, so only a new version advances the revision.
		var previous string
		if err = tx.QueryRowContext(ctx, `SELECT CASE WHEN status='ready' THEN COALESCE(sha256,'') ELSE '' END FROM media_assets WHERE id=? AND source_url=?`, asset.ID, asset.SourceURL).Scan(&previous); err != nil {
			return ErrSourceWrite
		}
		_, err = tx.ExecContext(ctx, `UPDATE media_assets SET status='ready',cache_path=?,mime_type=?,byte_size=?,sha256=?,etag=?,last_modified=?,last_error=NULL,fetched_at=?,last_checked_at=?,updated_at=? WHERE id=? AND source_url=?`, relative, mime, len(content), hash, nullable(etag), nullable(lastModified), now, now, now, asset.ID, asset.SourceURL)
		if err == nil && previous != hash {
			err = bumpRevision(ctx, tx, now)
		}
	}
	if err != nil || tx.Commit() != nil {
		return ErrSourceWrite
	}
	return nil
}

// MarkMediaUnchanged records a revalidation the server answered with 304: the
// ready file stays current and the check time moves on.
func (s *Store) MarkMediaUnchanged(ctx context.Context, asset PendingMedia) error {
	if s == nil || !s.workflowWritable || !safeHash(asset.ID) {
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
	if _, err = tx.ExecContext(ctx, `UPDATE media_assets SET last_error=NULL,last_checked_at=?,updated_at=? WHERE id=? AND source_url=? AND status='ready'`, now, now, asset.ID, asset.SourceURL); err != nil || tx.Commit() != nil {
		return ErrSourceWrite
	}
	return nil
}
