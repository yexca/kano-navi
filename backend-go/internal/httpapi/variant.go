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

// knownFixedHash returns the remembered hash of the file at path while its
// size and modification time still match.
func knownFixedHash(path string, info fs.FileInfo) (string, bool) {
	fixedHashesMu.Lock()
	known, ok := fixedHashes[path]
	fixedHashesMu.Unlock()
	if !ok || known.size != info.Size() || !known.modTime.Equal(info.ModTime()) {
		return "", false
	}
	return known.hash, true
}

func rememberFixedHash(path string, info fs.FileInfo, hash string) {
	fixedHashesMu.Lock()
	fixedHashes[path] = fixedHash{size: info.Size(), modTime: info.ModTime(), hash: hash}
	fixedHashesMu.Unlock()
}

// fixedFileHash returns the SHA-256 of the opened fixed file at path, whose
// stat is info. A file not remembered is read to its end and remembered; the
// caller seeks back before serving it.
func fixedFileHash(path string, info fs.FileInfo, file io.Reader) (string, error) {
	if hash, ok := knownFixedHash(path, info); ok {
		return hash, nil
	}
	digest := sha256.New()
	read, err := io.Copy(digest, file)
	if err != nil || read != info.Size() {
		return "", errors.New("fixed file unreadable")
	}
	hash := hex.EncodeToString(digest.Sum(nil))
	rememberFixedHash(path, info, hash)
	return hash, nil
}

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
// serves the original itself. immutable decides from the original's SHA-256
// whether the response may be cached as immutable.
func serveImageVariant(w http.ResponseWriter, r *http.Request, variants variantReader, path string, info fs.FileInfo, open func() (fs.File, error), mime string, width int, immutable func(hash string) bool) bool {
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
	hash, ok := knownFixedHash(path, info)
	if !ok {
		file, err := readFixedImage(open, mime)
		if err != nil {
			return false
		}
		loaded = file
		hash = file.SHA256
		rememberFixedHash(path, info, hash)
	}
	// The cache stores a copy only when the bytes it was made from match
	// hash, so the copy belongs to the original hash names.
	file, err := variants.ReadVariant(r.Context(), hash, width, original)
	if err != nil {
		return false
	}
	w.Header().Set("Content-Type", file.MIMEType)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", mediaCacheControl(immutable(hash)))
	w.Header().Set("ETag", `"`+file.SHA256+`"`)
	// ServeContent answers ranges, HEAD and both conditional validators.
	http.ServeContent(w, r, "", file.LastModified, bytes.NewReader(file.Content))
	return true
}
