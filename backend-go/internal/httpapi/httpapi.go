package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/config"
	"github.com/yexca/kano-navi/backend-go/internal/publicview"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

type Handler struct {
	store *sqlite.Store
	now   func() time.Time
}

func NewHandler(store *sqlite.Store) http.Handler {
	return NewHandlerWithClock(store, time.Now)
}

func NewHandlerWithClock(store *sqlite.Store, now func() time.Time) http.Handler {
	if now == nil {
		now = time.Now
	}
	return &Handler{store: store, now: now}
}

func NewServer(handler http.Handler, cfg config.Config) *http.Server {
	return &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/health":
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		h.health(w, r)
	case "/api/dashboard/revision":
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		h.revision(w, r)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "api_not_found"}, "")
	}
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	run, err := h.store.GetLatestSync(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database_unavailable"}, "")
		return
	}
	w.Header().Set("Vary", "Accept-Encoding")
	writeJSON(w, http.StatusOK, publicview.Health(h.now(), run), "")
}

func (h *Handler) revision(w http.ResponseWriter, r *http.Request) {
	revision, err := h.store.GetDashboardRevision(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database_unavailable"}, "")
		return
	}
	var since *int64
	if raw := r.URL.Query().Get("since"); raw != "" {
		if parsed, ok := parseInt10(raw); ok {
			since = &parsed
		}
	}
	w.Header().Set("Vary", "Accept-Encoding")
	writeJSON(w, http.StatusOK, publicview.Revision(revision, since), "no-store")
}

func writeMethodNotAllowed(w http.ResponseWriter) {
	w.Header().Set("Allow", http.MethodGet)
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"}, "")
}

func writeJSON(w http.ResponseWriter, status int, value any, cacheControl string) {
	if cacheControl != "" {
		w.Header().Set("Cache-Control", cacheControl)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	body, err := json.Marshal(value)
	if err != nil {
		// All response values are fixed structs. Keep this fallback stable if a
		// future edit accidentally makes one non-serializable.
		status = http.StatusInternalServerError
		body = []byte(`{"error":"internal_error"}`)
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func parseInt10(raw string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	sign := int64(1)
	if raw[0] == '+' || raw[0] == '-' {
		if raw[0] == '-' {
			sign = -1
		}
		raw = raw[1:]
	}
	if raw == "" || raw[0] < '0' || raw[0] > '9' {
		return 0, false
	}
	var value int64
	for i := 0; i < len(raw); i++ {
		if raw[i] < '0' || raw[i] > '9' {
			break
		}
		digit := int64(raw[i] - '0')
		if value > (1<<63-1-digit)/10 {
			return 0, false
		}
		value = value*10 + digit
	}
	return value * sign, true
}

// Shutdown is a small testable wrapper around http.Server.Shutdown.
func Shutdown(ctx context.Context, server *http.Server) error {
	if server == nil {
		return errors.New("server is nil")
	}
	return server.Shutdown(ctx)
}
