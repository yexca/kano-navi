package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yexca/kano-navi/backend/internal/cdn"
	"github.com/yexca/kano-navi/backend/internal/config"
	"github.com/yexca/kano-navi/backend/internal/publicview"
	"github.com/yexca/kano-navi/backend/internal/source/youtube/websub"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend/internal/webbotauth"
)

type Handler struct {
	store *sqlite.Store
	now   func() time.Time
	admin http.Handler
	// static and mcp are attached only when this process is the single owner
	// of the site, not in read-only mode.
	static http.Handler
	mcp    http.Handler
	// websub receives YouTube hub verifications and signed notifications.
	websub http.Handler
	// publicOrigin is the normalized PUBLIC_ORIGIN or empty; the Atom feed
	// uses it for absolute links.
	publicOrigin string
	// cdnBase publishes the build's assets and cached media from an optional
	// CDN; empty serves every address from this origin.
	cdnBase cdn.Base
	// webBotAuth serves the Web Bot Auth key directory; nil, or a holder
	// without a signer, answers it 404.
	webBotAuth *webbotauth.Holder
	// streams pushes revision changes to /api/dashboard/events.
	streams *revisionHub
	// dashboards holds the prepared /api/dashboard responses.
	dashboards *dashboardCache
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
	// PublicOrigin is the normalized PUBLIC_ORIGIN, or empty.
	PublicOrigin string
	// CDNBase is the normalized CDN_PUBLIC_BASE, or empty to serve the build's
	// assets and cached media from this process.
	CDNBase string
	// WebBotAuth holds the Web Bot Auth signer whose key directory the
	// site serves; nil serves none.
	WebBotAuth *webbotauth.Holder
}

// NewStandaloneHandler serves the whole product from this process.
func NewStandaloneHandler(store *sqlite.Store, now func() time.Time, options Standalone) *Handler {
	if now == nil {
		now = time.Now
	}
	return &Handler{
		store:        store,
		now:          now,
		admin:        options.Admin,
		static:       options.Static,
		mcp:          options.MCP,
		websub:       options.WebSub,
		publicOrigin: options.PublicOrigin,
		cdnBase:      cdn.Base(options.CDNBase),
		webBotAuth:   options.WebBotAuth,
		streams:      newRevisionHub(store.GetDashboardRevision),
		dashboards:   newDashboardCache(),
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
// which read-only mode attaches once its admin configuration is valid.
func NewHandlerWithAdmin(store *sqlite.Store, now func() time.Time, admin http.Handler) *Handler {
	if now == nil {
		now = time.Now
	}
	return &Handler{store: store, now: now, admin: admin, streams: newRevisionHub(store.GetDashboardRevision), dashboards: newDashboardCache()}
}

// WithCDNBase returns the handler with its public reads naming cached media and
// fixed public images on the normalized CDN_PUBLIC_BASE; empty keeps them at
// this origin. Read-only mode uses it, as the standalone handler takes the
// base in its options.
func (h *Handler) WithCDNBase(base string) *Handler {
	h.cdnBase = cdn.Base(base)
	return h
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
	case "/api/visitor-region":
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		h.visitorRegion(w, r)
	case "/api/dashboard/revision":
		w.Header().Set("Vary", "Accept-Encoding")
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		h.revision(w, r)
	case "/api/dashboard/events":
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		h.revisionEvents(w, r)
	case "/api/dashboard":
		w.Header().Set("Vary", "Accept-Encoding")
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		h.dashboard(w, r)
	case "/api/calendar":
		w.Header().Set("Vary", "Accept-Encoding")
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		h.calendar(w, r)
	case "/api/calendar.ics":
		h.calendarFeed(w, r)
	case "/api/feed.xml":
		h.atomFeed(w, r)
	case webbotauth.DirectoryPath:
		// The key directory a site fetches from the Signature-Agent origin
		// to verify the shop source's signed requests.
		if !h.webBotAuth.Serves() && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"}, "")
			return
		}
		h.webBotAuth.ServeDirectory(w, r)
	case "/api/posts":
		w.Header().Set("Vary", "Accept-Encoding")
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		h.posts(w, r)
	case "/api/posts/months":
		w.Header().Set("Vary", "Accept-Encoding")
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		h.postMonths(w, r)
	case "/api/videos":
		w.Header().Set("Vary", "Accept-Encoding")
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		h.videos(w, r)
	case "/api/videos/months":
		w.Header().Set("Vary", "Accept-Encoding")
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		h.videoMonths(w, r)
	case "/api/works/uploads":
		w.Header().Set("Vary", "Accept-Encoding")
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		h.worksUploads(w, r)
	case "/api/works":
		w.Header().Set("Vary", "Accept-Encoding")
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		worksCollection.serve(w, r, h.cdnBase)
	case "/api/works/upload-list":
		w.Header().Set("Vary", "Accept-Encoding")
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		worksUploadListCollection.serve(w, r, h.cdnBase)
	case "/api/journey":
		w.Header().Set("Vary", "Accept-Encoding")
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		journeyCollection.serve(w, r, h.cdnBase)
	case "/api/lineblog":
		w.Header().Set("Vary", "Accept-Encoding")
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		h.lineblogIndex(w, r)
	default:
		switch {
		case strings.HasPrefix(r.URL.Path, lineblogPostsPrefix):
			h.lineblogPost(w, r)
		case strings.HasPrefix(r.URL.Path, "/media/lineblog/"):
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				writeMethodNotAllowed(w, "GET, HEAD")
				return
			}
			lineblogArchive.serve(w, r, h, strings.TrimPrefix(r.URL.Path, "/media/lineblog/"))
		case strings.HasPrefix(r.URL.Path, "/media/works/"):
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				writeMethodNotAllowed(w, "GET, HEAD")
				return
			}
			worksArchive.serve(w, r, h, strings.TrimPrefix(r.URL.Path, "/media/works/"))
		case strings.HasPrefix(r.URL.Path, "/media/journey/"):
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				writeMethodNotAllowed(w, "GET, HEAD")
				return
			}
			journeyArchive.serve(w, r, h, strings.TrimPrefix(r.URL.Path, "/media/journey/"))
		case strings.HasPrefix(r.URL.Path, "/media/profile/"):
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				writeMethodNotAllowed(w, "GET, HEAD")
				return
			}
			h.profileMedia(w, r, strings.TrimPrefix(r.URL.Path, "/media/profile/"))
		case strings.HasPrefix(r.URL.Path, "/media/account-avatar/"):
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				writeMethodNotAllowed(w, "GET, HEAD")
				return
			}
			h.accountAvatarMedia(w, r, strings.TrimPrefix(r.URL.Path, "/media/account-avatar/"))
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
	var entry *dashboardEntry
	var err error
	revision, revisionErr := h.store.GetDashboardRevision(r.Context())
	if revisionErr != nil {
		// Without a revision nothing can be reused; compute as usual.
		entry, err = h.computeDashboard(r.Context(), dashboardKey{days: days})
	} else {
		key := dashboardKey{revision: revision, days: days}
		entry, err = h.dashboards.get(r.Context(), key, h.now, func() (*dashboardEntry, error) {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), dashboardComputeTimeout)
			defer cancel()
			return h.computeDashboard(ctx, key)
		})
	}
	switch {
	case errors.Is(err, errDashboardEncoding):
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"}, "")
	case err != nil:
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database_unavailable"}, "")
	default:
		writePreparedJSON(w, r, entry.body, entry.etag)
	}
}

var errDashboardEncoding = errors.New("dashboard could not be encoded")

// computeDashboard reads and encodes the dashboard for key's post window,
// with its validator and the time it stops being current.
func (h *Handler) computeDashboard(ctx context.Context, key dashboardKey) (*dashboardEntry, error) {
	now := h.now()
	dashboard, err := h.store.GetDashboard(ctx, key.days, now)
	if err != nil {
		return nil, err
	}
	payload := h.public(publicview.Dashboard(dashboard))
	// meta.generatedAt changes on every computation; leaving it out of the
	// validator lets an otherwise unchanged snapshot revalidate.
	validated := make(map[string]any, len(payload))
	for key, value := range payload {
		validated[key] = value
	}
	if meta, ok := payload["meta"].(map[string]any); ok {
		stable := make(map[string]any, len(meta))
		for key, value := range meta {
			if key != "generatedAt" {
				stable[key] = value
			}
		}
		validated["meta"] = stable
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, errDashboardEncoding
	}
	validator, err := json.Marshal(validated)
	if err != nil {
		validator = body
	}
	return &dashboardEntry{key: key, body: body, etag: revalidatedETag(validator), expiresAt: dashboardExpiry(now, dashboard.ValidUntil)}, nil
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
	var page, pageSize int
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
	calendar, err := h.store.GetCalendarPageAt(r.Context(), r.URL.Query().Get("from"), r.URL.Query().Get("to"), page, pageSize, h.now())
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
		// Calendar adds the manual lock marker and the board's trusted live
		// state to the public event, while omitting dashboard-only isUpcoming.
		projected["manualLocked"] = event.ManualLocked
		projected["liveStatus"] = event.LiveStatus
		projected["liveUrl"] = event.LiveURL
		projected["links"] = publicview.PublicEventLinks(event.Links)
		items = append(items, projected)
	}
	images := make([]any, 0, len(calendar.ScheduleImages))
	for _, asset := range calendar.ScheduleImages {
		images = append(images, publicview.PublicAsset(asset))
	}
	writeRevalidatedJSON(w, r, h.public(map[string]any{
		"items": items, "events": items, "page": calendar.Page, "pageSize": calendar.PageSize,
		"total": calendar.Total, "totalPages": calendar.TotalPages, "hasPrevious": calendar.HasPrevious,
		"hasNext": calendar.HasNext, "dayCounts": calendar.DayCounts, "revision": calendar.Revision,
		"scheduleImages": images, "adjacent": map[string]any{"previous": calendar.Previous, "next": calendar.Next},
	}), nil)
}

func numericPrefix(raw string) bool {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "+") || strings.HasPrefix(raw, "-") {
		raw = raw[1:]
	}
	return raw != "" && raw[0] >= '0' && raw[0] <= '9'
}

// publicJSON returns a packaged public read's JSON body with its cached media
// addresses on the configured CDN, if any.
func (h *Handler) publicJSON(body []byte) ([]byte, error) {
	return h.cdnBase.JSON(body)
}

// public returns a public read's object with its cached media addresses on the
// configured CDN, if any. Responses that name no media pass through unchanged.
func (h *Handler) public(value map[string]any) map[string]any {
	if rewritten, ok := h.cdnBase.Value(value).(map[string]any); ok {
		return rewritten
	}
	return value
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

// writeRevalidatedJSON answers a polled public read with a validator over its
// identity JSON (or over validated, a semantically equivalent value without
// per-read fields), so an unchanged snapshot costs a 304 instead of a body.
// The ETag is weak: gzip changes the transferred bytes, and the validator may
// ignore fields that do not change the content.
func writeRevalidatedJSON(w http.ResponseWriter, r *http.Request, value any, validated any) {
	body, err := json.Marshal(value)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"}, "")
		return
	}
	validator := body
	if validated != nil {
		if validator, err = json.Marshal(validated); err != nil {
			validator = body
		}
	}
	writePreparedJSON(w, r, body, revalidatedETag(validator))
}

// revalidatedETag is the weak validator of a public read's validator JSON.
func revalidatedETag(validator []byte) string {
	sum := sha256.Sum256(validator)
	return `W/"` + hex.EncodeToString(sum[:16]) + `"`
}

// writePreparedJSON answers a public read whose JSON body and ETag are
// already made: no-cache, and a 304 when the client holds the ETag.
func writePreparedJSON(w http.ResponseWriter, r *http.Request, body []byte, etag string) {
	w.Header().Set("Cache-Control", "no-cache")
	if notModified(w, r, etag) {
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
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
