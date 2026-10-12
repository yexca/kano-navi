package httpapi

import (
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/yexca/kano-navi/backend-go/internal/adminauth"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// adminHandler is intentionally read-only. The TypeScript service remains the
// owner of every admin mutation, job queue, source fetch and model operation.
type adminHandler struct {
	store *sqlite.Store
	auth  *adminauth.Auth
}

func newAdminHandler(store *sqlite.Store, auth *adminauth.Auth) http.Handler {
	return &adminHandler{store: store, auth: auth}
}

func (h *adminHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, no-transform")
	if h.auth == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "admin_unavailable"}, "")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/admin")
	if path == "" {
		path = "/"
	}
	switch path {
	case "/session":
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		h.auth.Session(w, r)
		return
	case "/login":
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		h.auth.OriginGuard(http.HandlerFunc(h.auth.Login)).ServeHTTP(w, r)
		return
	case "/logout":
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		h.auth.OriginGuard(http.HandlerFunc(h.auth.Logout)).ServeHTTP(w, r)
		return
	}
	if r.Method != http.MethodGet {
		// Do not expose a mutation surface in the sidecar. Origin protection is
		// evaluated before the stable method response for unsafe requests.
		h.auth.OriginGuard(h.auth.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeMethodNotAllowed(w, http.MethodGet) }))).ServeHTTP(w, r)
		return
	}
	h.auth.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch path {
		case "/videos":
			h.videos(w, r)
		case "/posts/llm":
			h.posts(w, r)
		case "/events":
			h.events(w, r)
		case "/sync/runs":
			h.runs(w, r)
		case "/sync/jobs":
			writeJSON(w, http.StatusOK, map[string]any{"jobs": []any{}}, "")
		case "/activity":
			h.activity(w, r)
		case "/overview":
			h.overview(w, r)
		default:
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "admin_not_found"}, "")
		}
	})).ServeHTTP(w, r)
}

func queryInt(r *http.Request, name string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || !math.IsInf(value, 0) && math.IsNaN(value) || value < 1 {
		return fallback
	}
	maxInt := int(^uint(0) >> 1)
	if value >= float64(maxInt) {
		return maxInt
	}
	return int(math.Trunc(value))
}
func queryBool(r *http.Request, name string) bool {
	value := strings.ToLower(strings.TrimSpace(r.URL.Query().Get(name)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func (h *adminHandler) posts(w http.ResponseWriter, r *http.Request) {
	page := queryInt(r, "page", 1)
	pageSize := queryInt(r, "pageSize", queryInt(r, "limit", 50))
	result, err := h.store.ListAdminPosts(r.Context(), page, pageSize, r.URL.Query().Get("source"), firstQuery(r, "account", "accountHandle", "handle"), firstQuery(r, "from", "dateFrom", "startDate"), firstQuery(r, "to", "dateTo", "endDate"), r.URL.Query().Get("search"), firstQuery(r, "mediaStatus", "media"), firstQuery(r, "llmStatus", "status"), queryBool(r, "reprocessRequested"))
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database_unavailable"}, "")
		return
	}
	payload := map[string]any{"posts": result.Posts}
	if hasPageQuery(r) {
		payload["page"] = result.Page
		payload["pageSize"] = result.PageSize
		payload["total"] = result.Total
		payload["totalPages"] = result.TotalPages
		payload["hasPrevious"] = result.HasPrevious
		payload["hasNext"] = result.HasNext
	}
	writeJSON(w, http.StatusOK, payload, "")
}

func (h *adminHandler) videos(w http.ResponseWriter, r *http.Request) {
	result, err := h.store.ListAdminVideos(r.Context(), queryInt(r, "page", 1), queryInt(r, "pageSize", queryInt(r, "limit", 100)), r.URL.Query().Get("status"), r.URL.Query().Get("kind"), r.URL.Query().Get("featured"), firstQuery(r, "thumbnailStatus", "thumbnail"), firstQuery(r, "eventId", "event"))
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database_unavailable"}, "")
		return
	}
	payload := map[string]any{"videos": result.Videos}
	if hasPageQuery(r) {
		payload["page"] = result.Page
		payload["pageSize"] = result.PageSize
		payload["total"] = result.Total
		payload["totalPages"] = result.TotalPages
		payload["hasPrevious"] = result.HasPrevious
		payload["hasNext"] = result.HasNext
		if result.FeaturedVideoID != nil {
			payload["featuredVideoId"] = *result.FeaturedVideoID
		} else {
			payload["featuredVideoId"] = nil
		}
	}
	writeJSON(w, http.StatusOK, payload, "")
}

func (h *adminHandler) events(w http.ResponseWriter, r *http.Request) {
	result, err := h.store.ListAdminEventsPage(r.Context(), queryInt(r, "page", 1), queryInt(r, "pageSize", 25), queryBool(r, "ascending"), queryBool(r, "includeDeleted"), r.URL.Query().Get("search"), r.URL.Query().Get("provenance"), r.URL.Query().Get("source"), r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database_unavailable"}, "")
		return
	}
	if !hasPageQuery(r) {
		writeJSON(w, http.StatusOK, map[string]any{"events": result.Events}, "")
		return
	}
	writeJSON(w, http.StatusOK, result, "")
}

func (h *adminHandler) runs(w http.ResponseWriter, r *http.Request) {
	result, err := h.store.ListAdminRunsPage(r.Context(), queryInt(r, "page", 1), queryInt(r, "pageSize", queryInt(r, "limit", 20)), r.URL.Query().Get("jobId"), r.URL.Query().Get("status"))
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database_unavailable"}, "")
		return
	}
	if !hasPageQuery(r) {
		writeJSON(w, http.StatusOK, map[string]any{"runs": result.Runs}, "")
		return
	}
	writeJSON(w, http.StatusOK, result, "")
}

func (h *adminHandler) activity(w http.ResponseWriter, r *http.Request) {
	result, err := h.store.ListAdminRunsPage(r.Context(), 1, queryInt(r, "pageSize", 12), "", "")
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database_unavailable"}, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": []any{}, "runs": result.Runs, "workflows": []any{}}, "")
}

func (h *adminHandler) overview(w http.ResponseWriter, r *http.Request) {
	result, err := h.store.ListAdminRunsPage(r.Context(), 1, queryInt(r, "pageSize", 12), "", "")
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database_unavailable"}, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": result.Runs, "jobs": []any{}, "workflows": []any{}}, "")
}

func firstQuery(r *http.Request, names ...string) string {
	for _, name := range names {
		if value := r.URL.Query().Get(name); value != "" {
			return value
		}
	}
	return ""
}

func hasPageQuery(r *http.Request) bool {
	_, page := r.URL.Query()["page"]
	_, pageSize := r.URL.Query()["pageSize"]
	return page || pageSize
}
