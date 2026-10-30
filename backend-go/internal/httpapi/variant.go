package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"sync"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// variantReader makes and caches smaller copies of an image known by its
// SHA-256 (sqlite.Store.ReadVariant).
type variantReader interface {
	ReadVariant(ctx context.Context, hash string, width int, original func() (*sqlite.MediaFile, error)) (*sqlite.MediaFile, error)
}

// maxVariantSourceBytes matches the cached-media bound: a larger fixed image
// is always served whole.
const maxVariantSourceBytes = 10 * 1024 * 1024

// fixedHash is the SHA-256 of a fixed file as it was when last read.
type fixedHash struct {
	size    int64
	modTime time.Time
	hash    string
}

// fixedHashes remembers fixed files' hashes by absolute path, so a cached copy
// is found without reading the original again. A changed size or
// modification time reads the file anew.
var (
	fixedHashesMu sync.Mutex
	fixedHashes   = map[string]fixedHash{}
)

// readFixedImage reads a regular file no larger than maxVariantSourceBytes
// whose type is mime.
func readFixedImage(open func() (fs.File, error), mime string) (*sqlite.MediaFile, error) {
	file, err := open()
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxVariantSourceBytes {
		return nil, errors.New("fixed image unavailable")
	}
	content, err := io.ReadAll(io.LimitReader(file, maxVariantSourceBytes+1))
	if err != nil || int64(len(content)) != info.Size() {
		return nil, errors.New("fixed image unreadable")
	}
	sum := sha256.Sum256(content)
	return &sqlite.MediaFile{Content: content, MIMEType: mime, SHA256: hex.EncodeToString(sum[:]), Size: info.Size(), LastModified: info.ModTime()}, nil
}

// serveImageVariant answers a w request for the fixed image at path with a
// copy no wider than width from the shared variant cache, or with the
// original when no copy can be made. It reports false, having written
// nothing, when the file cannot be read as a variant source; the caller then
// serves the original itself.
func serveImageVariant(w http.ResponseWriter, r *http.Request, variants variantReader, path string, info fs.FileInfo, open func() (fs.File, error), mime string, width int, cacheControl string) bool {
	if variants == nil {
		return false
	}
	var loaded *sqlite.MediaFile
	original := func() (*sqlite.MediaFile, error) {
		if loaded != nil {
			return loaded, nil
		}
		return readFixedImage(open, mime)
	}
	fixedHashesMu.Lock()
	known, ok := fixedHashes[path]
	fixedHashesMu.Unlock()
	if !ok || known.size != info.Size() || !known.modTime.Equal(info.ModTime()) {
		file, err := readFixedImage(open, mime)
		if err != nil {
			return false
		}
		loaded = file
		known = fixedHash{size: info.Size(), modTime: info.ModTime(), hash: file.SHA256}
		fixedHashesMu.Lock()
		fixedHashes[path] = known
		fixedHashesMu.Unlock()
	}
	file, err := variants.ReadVariant(r.Context(), known.hash, width, original)
	if err != nil {
		return false
	}
	w.Header().Set("Content-Type", file.MIMEType)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", cacheControl)
	w.Header().Set("ETag", `"`+file.SHA256+`"`)
	// ServeContent answers ranges, HEAD and both conditional validators.
	http.ServeContent(w, r, "", file.LastModified, bytes.NewReader(file.Content))
	return true
}
