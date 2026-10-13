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

type PendingMedia struct{ ID, Source, SourceURL string }

func (s *Store) PendingMedia(ctx context.Context, limit int) ([]PendingMedia, error) {
	if s == nil || !s.workflowWritable {
		return nil, ErrSourceWrite
	}
	if limit < 1 || limit > 100 {
		return nil, ErrSourceWrite
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,source,source_url FROM media_assets WHERE status='pending' AND source IN('x','youtube') AND length(id)=64 AND id NOT GLOB '*[^a-f0-9]*' ORDER BY COALESCE(last_checked_at,created_at),id LIMIT ?`, limit)
	if err != nil {
		return nil, ErrSourceWrite
	}
	defer rows.Close()
	out := []PendingMedia{}
	for rows.Next() {
		var a PendingMedia
		if rows.Scan(&a.ID, &a.Source, &a.SourceURL) != nil {
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
		_, err = tx.ExecContext(ctx, `UPDATE media_assets SET status='ready',cache_path=?,mime_type=?,byte_size=?,sha256=?,etag=?,last_modified=?,last_error=NULL,fetched_at=?,last_checked_at=?,updated_at=? WHERE id=? AND source_url=?`, relative, mime, len(content), hash, nullable(etag), nullable(lastModified), now, now, now, asset.ID, asset.SourceURL)
		if err == nil {
			err = bumpRevision(ctx, tx, now)
		}
	}
	if err != nil || tx.Commit() != nil {
		return ErrSourceWrite
	}
	return nil
}
