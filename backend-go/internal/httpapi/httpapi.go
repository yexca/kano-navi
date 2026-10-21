package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/config"
	"github.com/yexca/kano-navi/backend-go/internal/publicview"
	"github.com/yexca/kano-navi/backend-go/internal/source/youtube/websub"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

type Handler struct {
	store *sqlite.Store
	now   func() time.Time
	admin http.Handler
	// static and mcp are attached only when this process is the single owner
	// of the site instead of a read-only sidecar.
	static http.Handler
	mcp    http.Handler
	// websub receives YouTube hub verifications and signed notifications.
	websub http.Handler
}

// Standalone carries everything the single-process owner serves beyond the
// public reads: the operator API, MCP and the built frontend.
type Standalone struct {
	// Admin serves /api/admin (internal/adminapi).
	Admin  http.Handler
	Static http.Handler
	MCP    http.Handler
	// WebSub answers the YouTube hub; it reports the route as absent while
	// push is disabled in /admin.
	WebSub http.Handler
}

// NewStandaloneHandler serves the whole product from this process.
func NewStandaloneHandler(store *sqlite.Store, now func() time.Time, options Standalone) http.Handler {
	if now == nil {
		now = time.Now
	}
	return &Handler{
		store:  store,
		now:    now,
		admin:  options.Admin,
		static: options.Static,
		mcp:    options.MCP,
		websub: options.WebSub,
	}
}

// NewHandler serves the public reads only; /api/admin answers unavailable.
func NewHandler(store *sqlite.Store) http.Handler {
	return NewHandlerWithClock(store, time.Now)
}

func NewHandlerWithClock(store *sqlite.Store, now func() time.Time) http.Handler {
	return NewHandlerWithAdmin(store, now, nil)
}

// NewHandlerWithAdmin serves the public reads plus an operator API handler,
// which the read-only sidecar attaches once its admin configuration is valid.
func NewHandlerWithAdmin(store *sqlite.Store, now func() time.Time, admin http.Handler) http.Handler {
	if now == nil {
		now = time.Now
	}
	return &Handler{store: store, now: now, admin: admin}
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
	if r.URL.Path == "/api/admin" || strings.HasPrefix(r.URL.Path, "/api/admin/") {
		if h.admin == nil {
			w.Header().Set("Cache-Control", "no-store, no-transform")
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "admin_unavailable"}, "")
			return
		}
		h.admin.ServeHTTP(w, r)
		return
	}
	switch r.URL.Path {
	case "/api/health":
		w.Header().Set("Vary", "Accept-Encoding")
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		h.health(w, r)
	case "/api/dashboard/revision":
		w.Header().Set("Vary", "Accept-Encoding")
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		h.revision(w, r)
	case "/api/dashboard":
		w.Header().Set("Vary", "Accept-Encoding")
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		h.dashboard(w, r)
	case "/api/calendar":
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		h.calendar(w, r)
	default:
		switch {
		case strings.HasPrefix(r.URL.Path, "/media/profile/"):
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				writeMethodNotAllowed(w, "GET, HEAD")
				return
			}
			h.profileMedia(w, r, strings.TrimPrefix(r.URL.Path, "/media/profile/"))
		case strings.HasPrefix(r.URL.Path, "/media/"):
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				writeMethodNotAllowed(w, "GET, HEAD")
				return
			}
			h.media(w, r, strings.TrimPrefix(r.URL.Path, "/media/"))
		case strings.HasPrefix(r.URL.Path, "/media"):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "media_not_found"}, "")
		case h.websub != nil && r.URL.Path == websub.CallbackPath:
			h.websub.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api":
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "api_not_found"}, "")
		case h.mcp != nil && (r.URL.Path == "/mcp" || strings.HasPrefix(r.URL.Path, "/mcp/")):
			h.mcp.ServeHTTP(w, r)
		case h.static != nil && r.URL.Path != "/mcp" && !strings.HasPrefix(r.URL.Path, "/mcp/"):
			h.static.ServeHTTP(w, r)
		default:
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "api_not_found"}, "")
		}
	}
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	run, err := h.store.GetLatestSync(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database_unavailable"}, "")
		return
	}
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
	writeJSON(w, http.StatusOK, publicview.Revision(revision, since), "no-store")
}

func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) {
	days := 3
	if raw, ok := r.URL.Query()["days"]; ok && len(raw) > 0 {
		if parsed, valid := parseInt10(raw[0]); valid {
			days = int(parsed)
		} else if numericPrefix(raw[0]) {
			// JavaScript Number.parseInt accepts arbitrarily large finite
			// decimal prefixes; clamping only needs their sign here.
			if strings.HasPrefix(strings.TrimSpace(raw[0]), "-") {
				days = 1
			} else {
				days = 30
			}
		}
	}
	if days < 1 {
		days = 1
	}
	if days > 30 {
		days = 30
	}
	dashboard, err := h.store.GetDashboard(r.Context(), days, h.now())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database_unavailable"}, "")
		return
	}
	writeJSON(w, http.StatusOK, publicview.Dashboard(dashboard), "no-store")
}

func queryCalendarNumber(r *http.Request, key string, fallback float64) float64 {
	raw, ok := r.URL.Query()[key]
	if !ok || len(raw) == 0 || strings.TrimSpace(raw[0]) == "" {
		return fallback
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(raw[0]), 64)
	if err != nil || value == 0 {
		return fallback
	}
	return value
}

func (h *Handler) calendar(w http.ResponseWriter, r *http.Request) {
	pageValue := queryCalendarNumber(r, "page", 1)
	pageSizeValue := queryCalendarNumber(r, "pageSize", 100)
	page, pageSize := 1, 100
	maxInt := float64(^uint(0) >> 1)
	if pageValue < maxInt {
		page = int(pageValue)
		if page == 0 {
			page = 1
		}
	} else {
		page = int(^uint(0) >> 1)
	}
	if pageSizeValue < maxInt {
		pageSize = int(pageSizeValue)
		if pageSize == 0 {
			// listAdminEventsPage uses its 25-item default when a non-zero
			// fractional Number truncates to zero.
			pageSize = 25
		}
	} else {
		pageSize = int(^uint(0) >> 1)
	}
	calendar, err := h.store.GetCalendarPage(r.Context(), r.URL.Query().Get("from"), r.URL.Query().Get("to"), page, pageSize)
	if err != nil {
		if errors.Is(err, sqlite.ErrInvalidCalendarRange) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_calendar_range"}, "")
			return
		}
		if errors.Is(err, sqlite.ErrUnavailable) || errors.Is(err, sqlite.ErrSchema) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database_unavailable"}, "")
			return
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database_unavailable"}, "")
		return
	}
	items := make([]any, 0, len(calendar.Items))
	for _, event := range calendar.Items {
		projected := publicview.PublicEvent(event, false)
		// Calendar exposes the same manual lock marker as TypeScript's
		// publicEvent(event, ["manualLocked"]), while omitting dashboard-only
		// isUpcoming.
		projected["manualLocked"] = event.ManualLocked
		items = append(items, projected)
	}
	images := make([]any, 0, len(calendar.ScheduleImages))
	for _, asset := range calendar.ScheduleImages {
		images = append(images, publicview.PublicAsset(asset))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "events": items, "page": calendar.Page, "pageSize": calendar.PageSize,
		"total": calendar.Total, "totalPages": calendar.TotalPages, "hasPrevious": calendar.HasPrevious,
		"hasNext": calendar.HasNext, "dayCounts": calendar.DayCounts, "revision": calendar.Revision,
		"scheduleImages": images, "adjacent": map[string]any{"previous": calendar.Previous, "next": calendar.Next},
	}, "no-store")
}

func numericPrefix(raw string) bool {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "+") || strings.HasPrefix(raw, "-") {
		raw = raw[1:]
	}
	return raw != "" && raw[0] >= '0' && raw[0] <= '9'
}

func writeMethodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
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
