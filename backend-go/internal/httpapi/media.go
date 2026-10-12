package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

func (h *Handler) media(w http.ResponseWriter, r *http.Request, id string) {
	version, present, valid := requestedVersion(r)
	if present && !valid {
		h.writeMediaError(w, sqlite.ErrMediaVersionNotFound, false, r.Method == http.MethodHead)
		return
	}
	file, err := h.store.ReadMedia(r.Context(), id, version)
	if err != nil {
		h.writeMediaError(w, err, false, r.Method == http.MethodHead)
		return
	}
	h.writeMediaHeaders(w, file.MIMEType, file.SHA256, file.Size, file.LastModified, version != "")
	if mediaFresh(r, file.SHA256, file.LastModified) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	start, end, partial, rangeOK := parseRange(r.Header.Get("Range"), file.Size)
	if !rangeOK {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", file.Size))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	if partial {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, file.Size))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		if r.Method != http.MethodHead {
			_, _ = w.Write(file.Content[start : end+1])
		}
		return
	}
	w.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(file.Content)
	}
}

func (h *Handler) profileMedia(w http.ResponseWriter, r *http.Request, slot string) {
	slot = strings.ToLower(slot)
	if slot != "avatar" && slot != "banner" {
		h.writeMediaError(w, sqlite.ErrMediaNotFound, true, r.Method == http.MethodHead)
		return
	}
	version, present, valid := requestedVersion(r)
	if present && !valid {
		h.writeMediaError(w, sqlite.ErrMediaVersionNotFound, true, r.Method == http.MethodHead)
		return
	}
	file, err := h.store.ReadProfileMedia(r.Context(), slot, version)
	if err != nil {
		h.writeMediaError(w, err, true, r.Method == http.MethodHead)
		return
	}
	w.Header().Set("Content-Type", file.MIMEType)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", mediaCacheControl(version != ""))
	w.Header().Set("ETag", `"`+file.SHA256+`"`)
	w.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))
	if mediaFresh(r, file.SHA256, time.Time{}) {
		w.Header().Del("Content-Length")
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(file.Content)
	}
}

func requestedVersion(r *http.Request) (string, bool, bool) {
	values, present := r.URL.Query()["v"]
	if !present {
		return "", false, true
	}
	if len(values) != 1 || values[0] == "" {
		return "", true, false
	}
	return values[0], true, true
}

func (h *Handler) writeMediaError(w http.ResponseWriter, err error, profile, head bool) {
	if errors.Is(err, sqlite.ErrUnavailable) || errors.Is(err, sqlite.ErrSchema) {
		if head {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database_unavailable"}, "")
		}
		return
	}
	code := "media_not_found"
	if profile {
		switch {
		case errors.Is(err, sqlite.ErrMediaNotReady):
			code = "profile_media_not_ready"
		case errors.Is(err, sqlite.ErrMediaVersionNotFound):
			code = "profile_media_version_not_found"
		case errors.Is(err, sqlite.ErrMediaNotFound):
			code = "profile_media_not_found"
		}
	} else {
		switch {
		case errors.Is(err, sqlite.ErrMediaNotReady):
			code = "media_not_ready"
		case errors.Is(err, sqlite.ErrMediaVersionNotFound):
			code = "media_version_not_found"
		}
	}
	if head {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": code}, "")
}

func (h *Handler) writeMediaHeaders(w http.ResponseWriter, mime, hash string, size int64, modified time.Time, immutable bool) {
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", mediaCacheControl(immutable))
	w.Header().Set("ETag", `"`+hash+`"`)
	if !modified.IsZero() {
		w.Header().Set("Last-Modified", modified.UTC().Format(http.TimeFormat))
	}
	w.Header().Set("Accept-Ranges", "bytes")
}

func mediaCacheControl(immutable bool) string {
	if immutable {
		return "public, max-age=31536000, immutable"
	}
	return "public, max-age=0, must-revalidate"
}

func mediaFresh(r *http.Request, hash string, modified time.Time) bool {
	if raw := r.Header.Get("If-None-Match"); raw != "" {
		// Match the validator after proxies have optionally stripped weak
		// markers or surrounding whitespace.
		if strings.Contains(strings.ToLower(raw), strings.ToLower(hash)) {
			return true
		}
		for _, candidate := range strings.Split(raw, ",") {
			candidate = strings.TrimSpace(strings.TrimPrefix(candidate, "W/"))
			if candidate == `"`+hash+`"` || candidate == "*" {
				return true
			}
		}
	}
	if !modified.IsZero() {
		if raw := r.Header.Get("If-Modified-Since"); raw != "" {
			if since, err := http.ParseTime(raw); err == nil && !modified.Truncate(time.Second).After(since) {
				return true
			}
		}
	}
	return false
}

func parseRange(raw string, size int64) (int64, int64, bool, bool) {
	if raw == "" {
		return 0, size - 1, false, true
	}
	if !strings.HasPrefix(raw, "bytes=") || strings.Contains(raw[6:], ",") {
		return 0, 0, false, false
	}
	part := strings.TrimSpace(raw[6:])
	pieces := strings.SplitN(part, "-", 2)
	if len(pieces) != 2 {
		return 0, 0, false, false
	}
	if pieces[0] == "" {
		n, err := strconv.ParseInt(strings.TrimSpace(pieces[1]), 10, 64)
		if err != nil || n <= 0 || size <= 0 {
			return 0, 0, false, false
		}
		if n > size {
			n = size
		}
		return size - n, size - 1, true, true
	}
	start, err := strconv.ParseInt(strings.TrimSpace(pieces[0]), 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, false, false
	}
	end := size - 1
	if strings.TrimSpace(pieces[1]) != "" {
		end, err = strconv.ParseInt(strings.TrimSpace(pieces[1]), 10, 64)
		if err != nil || end < start {
			return 0, 0, false, false
		}
		if end >= size {
			end = size - 1
		}
	}
	return start, end, true, true
}
