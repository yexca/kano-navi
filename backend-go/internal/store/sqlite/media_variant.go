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
	"strconv"

	"github.com/yexca/kano-navi/backend-go/internal/mediaformat"
	"github.com/yexca/kano-navi/backend-go/internal/mediavariant"
)

// variantRoot holds resized copies under data/cache/variants. It is a derived
// cache: originals are never changed and a removed directory is rebuilt on
// demand.
const variantRoot = "cache/variants"

// variantKeepOriginal marks a width whose copy would not be smaller (or the
// format is served whole), so the original is served without decoding again.
const variantKeepOriginal = "orig"

// resizeSlots bounds concurrent decodes: a 40-megapixel image needs about
// 160 MB as RGBA.
var resizeSlots = make(chan struct{}, 2)

// ReadMediaVariant returns a copy of a ready asset no wider than width, made
// once and kept in the variant cache. It returns the original file whenever
// a smaller copy cannot be made or stored, so callers never fail on a
// variant alone.
func (s *Store) ReadMediaVariant(ctx context.Context, id, version string, width int) (*MediaFile, error) {
	if !mediavariant.ValidWidth(width) {
		return nil, ErrMediaNotFound
	}
	record, err := s.lookupMedia(ctx, id, version)
	if err != nil {
		return nil, err
	}
	name := variantName(record.hash, width)
	if file, keepOriginal := s.readVariant(name); file != nil {
		file.LastModified = record.lastModified(file.LastModified)
		return file, nil
	} else if keepOriginal {
		return s.readMediaRecord(record)
	}
	original, err := s.readMediaRecord(record)
	if err != nil {
		return nil, err
	}
	select {
	case resizeSlots <- struct{}{}:
	case <-ctx.Done():
		return original, nil
	}
	content, mime, resizeErr := mediavariant.Resize(original.Content, width)
	<-resizeSlots
	if resizeErr != nil {
		s.writeVariant(name+"."+variantKeepOriginal, nil)
		return original, nil
	}
	s.writeVariant(name+"."+mediaformat.Extension(mime), content)
	file := variantFile(content, mime)
	file.LastModified = original.LastModified
	return file, nil
}

func variantName(hash string, width int) string {
	return filepath.Join(hash[:2], hash+"-"+strconv.Itoa(width))
}

func variantFile(content []byte, mime string) *MediaFile {
	sum := sha256.Sum256(content)
	return &MediaFile{Content: content, MIMEType: mime, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(content))}
}

// readVariant finds a stored copy, or reports that the original is kept for
// this width. Any unreadable or mistyped entry counts as absent.
func (s *Store) readVariant(name string) (*MediaFile, bool) {
	root, err := os.OpenRoot(filepath.Join(s.dataRoot, variantRoot))
	if err != nil {
		return nil, false
	}
	defer root.Close()
	if _, err = root.Stat(name + "." + variantKeepOriginal); err == nil {
		return nil, true
	}
	for _, mime := range []string{"image/jpeg", "image/png"} {
		file, openErr := root.Open(name + "." + mediaformat.Extension(mime))
		if openErr != nil {
			continue
		}
		info, statErr := file.Stat()
		var content []byte
		if statErr == nil && info.Mode().IsRegular() && info.Size() > 0 && info.Size() <= maxCachedMediaBytes {
			content, _ = io.ReadAll(io.LimitReader(file, maxCachedMediaBytes+1))
		}
		file.Close()
		if int64(len(content)) != info.Size() || mediaformat.Sniff(content) != mime {
			continue
		}
		out := variantFile(content, mime)
		out.LastModified = info.ModTime()
		return out, false
	}
	return nil, false
}

// writeVariant stores a copy atomically. A read-only data directory or any
// other failure only means the copy is made again next time.
func (s *Store) writeVariant(name string, content []byte) {
	base, err := os.OpenRoot(s.dataRoot)
	if err != nil {
		return
	}
	defer base.Close()
	dir := filepath.Join(filepath.FromSlash(variantRoot), filepath.Dir(name))
	if base.MkdirAll(dir, 0755) != nil {
		return
	}
	suffix := make([]byte, 16)
	if _, err = rand.Read(suffix); err != nil {
		return
	}
	temp := filepath.Join(dir, ".variant-"+hex.EncodeToString(suffix))
	file, err := base.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return
	}
	defer base.Remove(temp)
	_, writeErr := file.Write(content)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return
	}
	target := filepath.Join(filepath.FromSlash(variantRoot), name)
	if err = base.Rename(temp, target); err != nil && !errors.Is(err, os.ErrExist) {
		return
	}
}
