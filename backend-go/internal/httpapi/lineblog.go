package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

// lineblogRoot is the data directory holding the recovered LINE BLOG media,
// written by scripts/package-lineblog.ts.
const lineblogRoot = "lineblog"

// lineblogName is an archive media name: 12 hex digits of a SHA-256 and the
// extension that fixes its type.
var lineblogName = regexp.MustCompile(`^([0-9a-f]{12})\.(jpg|png|mp4)$`)

var lineblogTypes = map[string]string{
	"jpg": "image/jpeg",
	"png": "image/png",
	"mp4": "video/mp4",
}

// lineblogMedia serves one recovered LINE BLOG file from the data directory.
// Only a regular file directly inside it with an archive name is served;
// names are opaque hashes, so the rest of the path never reaches the disk.
func (h *Handler) lineblogMedia(w http.ResponseWriter, r *http.Request, name string) {
	match := lineblogName.FindStringSubmatch(name)
	root := ""
	if h.store != nil {
		root = h.store.DataRoot()
	}
	if match == nil || root == "" {
		writeLineblogMissing(w, r)
		return
	}
	dir, err := os.OpenRoot(filepath.Join(root, lineblogRoot))
	if err != nil {
		writeLineblogMissing(w, r)
		return
	}
	defer dir.Close()
	info, err := dir.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		writeLineblogMissing(w, r)
		return
	}
	file, err := dir.Open(name)
	if err != nil {
		writeLineblogMissing(w, r)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", lineblogTypes[match[2]])
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", mediaCacheControl(false))
	w.Header().Set("ETag", `"`+match[1]+`-`+strconv.FormatInt(info.Size(), 10)+`"`)
	// ServeContent answers ranges, HEAD and both conditional validators.
	http.ServeContent(w, r, name, info.ModTime(), file)
}

func writeLineblogMissing(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "media_not_found"}, "")
}
