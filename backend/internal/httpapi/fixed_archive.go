package httpapi

import (
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/yexca/kano-navi/backend/internal/variants"
)

// fixedArchive is a data directory of packaged files served by name. A name
// matches name, whose first group is the start of a SHA-256 and whose second
// is the extension that fixes the file's type in types.
type fixedArchive struct {
	dir   string
	name  *regexp.Regexp
	types map[string]string
}

// lineblogArchive holds the recovered LINE BLOG media, written by
// scripts/package-lineblog.ts.
var lineblogArchive = fixedArchive{
	dir:  "lineblog",
	name: variants.LineblogName,
	types: map[string]string{
		"jpg": "image/jpeg",
		"png": "image/png",
		"mp4": "video/mp4",
	},
}

// worksArchive holds the discography cover images, written by
// scripts/package-works.ts.
var worksArchive = fixedArchive{
	dir:  "works",
	name: variants.WorksName,
	types: map[string]string{
		"jpg": "image/jpeg",
		"png": "image/png",
	},
}

// journeyArchive holds the Journey page's images, written by
// scripts/package-history-media.ts.
var journeyArchive = fixedArchive{
	dir:  "journey",
	name: variants.JourneyName,
	types: map[string]string{
		"jpg": "image/jpeg",
		"png": "image/png",
	},
}

// serve answers one archive file from the data directory. Only a regular
// file directly inside the archive with an archive name is served; names are
// opaque hashes, so the rest of the path never reaches the disk. An image
// asked for with w gets a copy from the shared variant cache; a video
// ignores w. An image and its copies are immutable once its bytes are
// checked against its name.
func (a fixedArchive) serve(w http.ResponseWriter, r *http.Request, h *Handler, name string) {
	match := a.name.FindStringSubmatch(name)
	root := ""
	if h.store != nil {
		root = h.store.DataRoot()
	}
	width, widthPresent, widthValid := requestedWidth(r)
	if match == nil || root == "" || widthPresent && !widthValid {
		writeArchiveMissing(w, r)
		return
	}
	dir, err := os.OpenRoot(filepath.Join(root, a.dir))
	if err != nil {
		writeArchiveMissing(w, r)
		return
	}
	defer dir.Close()
	info, err := dir.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		writeArchiveMissing(w, r)
		return
	}
	// An image's name is the start of its bytes' SHA-256, so an image whose
	// bytes match it never changes; a video is named after its watch URL.
	contentType := a.types[match[2]]
	image := strings.HasPrefix(contentType, "image/")
	named := func(hash string) bool { return image && strings.HasPrefix(hash, match[1]) }
	path := filepath.Join(root, a.dir, name)
	if widthPresent && image {
		open := func() (fs.File, error) { return dir.Open(name) }
		if serveImageVariant(w, r, h.store, path, info, open, width, named) {
			return
		}
	}
	file, err := dir.Open(name)
	if err != nil {
		writeArchiveMissing(w, r)
		return
	}
	defer file.Close()
	if info, err = file.Stat(); err != nil || !info.Mode().IsRegular() {
		writeArchiveMissing(w, r)
		return
	}
	immutable := false
	if image {
		hash, err := fixedFileHash(r.Context(), h.store, path, info, file)
		if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
			writeArchiveMissing(w, r)
			return
		}
		immutable = err == nil && named(hash)
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", mediaCacheControl(immutable))
	// The size and modification time change with any repackaged file, so a
	// corrected file of the same size is not answered 304.
	w.Header().Set("ETag", `"`+match[1]+`-`+strconv.FormatInt(info.Size(), 10)+`-`+strconv.FormatInt(info.ModTime().UnixNano(), 36)+`"`)
	// ServeContent answers ranges, HEAD and both conditional validators.
	serveContent(w, r, name, info.ModTime(), file)
}

func writeArchiveMissing(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "media_not_found"}, "")
}
