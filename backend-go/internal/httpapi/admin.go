package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/yexca/kano-navi/backend-go/internal/adminauth"
	"github.com/yexca/kano-navi/backend-go/internal/jsonbody"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// adminHandler keeps reads available in the canary; event writes are an
// explicitly gated development-only slice. TypeScript remains the production
// owner of every mutation, job queue, source fetch and model operation.
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
		if path == "/events" || strings.HasPrefix(path, "/events/") {
			h.auth.OriginGuard(h.auth.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.mutation(w, r, path) }))).ServeHTTP(w, r)
			return
		}
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
			writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "capability_not_migrated"}, "")
		case "/activity":
			h.activity(w, r)
		case "/overview":
			h.overview(w, r)
		default:
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "admin_not_found"}, "")
		}
	})).ServeHTTP(w, r)
}

func (h *adminHandler) mutation(w http.ResponseWriter, r *http.Request, path string) {
	if r.Method == http.MethodDelete && strings.HasPrefix(path, "/events/") {
		e, err := h.store.DeleteManualEvent(r.Context(), strings.TrimPrefix(path, "/events/"))
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, 404, map[string]string{"error": "event_not_found"}, "")
			return
		}
		if errors.Is(err, sqlite.ErrReadOnly) {
			writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "capability_not_migrated"}, "")
			return
		}
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "internal_error"}, "")
			return
		}
		writeJSON(w, 204, nil, "")
		_ = e
		return
	}
	confirm := r.Method == http.MethodPost && strings.HasSuffix(path, "/confirm")
	var body map[string]json.RawMessage
	var err error
	if confirm && r.ContentLength == 0 {
		body = map[string]json.RawMessage{}
	} else {
		body, err = jsonbody.Object(w, r)
	}
	if err != nil {
		status := 400
		if errors.Is(err, jsonbody.ErrLarge) {
			status = 413
		}
		writeJSON(w, status, map[string]string{"error": err.Error()}, "")
		return
	}
	var i sqlite.EventInput
	decode := func(k string, p *string) {
		if v, ok := body[k]; ok {
			_ = json.Unmarshal(v, p)
		}
	}
	decode("id", &i.ID)
	decode("title", &i.Title)
	decode("detail", &i.Detail)
	decode("startsOn", &i.StartsOn)
	decode("startsAt", &i.StartsAt)
	decode("endsAt", &i.EndsAt)
	decode("timePrecision", &i.TimePrecision)
	decode("status", &i.Status)
	decode("eventType", &i.EventType)
	decode("url", &i.URL)
	decode("cancellationStatus", &i.CancellationStatus)
	decode("cancellationReason", &i.CancellationReason)
	var e sqlite.AdminEvent
	if confirm && len(body) == 0 {
		current, getErr := h.store.GetAdminEvent(r.Context(), strings.TrimSuffix(strings.TrimPrefix(path, "/events/"), "/confirm"))
		if getErr != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "event_not_found"}, "")
			return
		}
		i = sqlite.EventInput{Title: current.Title, Detail: valueString(current.Detail), StartsOn: current.StartsOn, StartsAt: valueString(current.StartsAt), EndsAt: valueString(current.EndsAt), TimePrecision: current.TimePrecision, Status: valueString(current.Status), EventType: valueString(current.EventType), URL: valueString(current.URL), CancellationStatus: current.CancellationStatus, CancellationReason: valueString(current.CancellationReason)}
	}
	if r.Method == http.MethodPost && path == "/events" {
		e, err = h.store.CreateManualEvent(r.Context(), i)
	} else if r.Method == http.MethodPut && strings.HasPrefix(path, "/events/") {
		e, err = h.store.UpdateManualEvent(r.Context(), strings.TrimPrefix(path, "/events/"), i)
	} else if confirm {
		e, err = h.store.UpdateManualEvent(r.Context(), strings.TrimSuffix(strings.TrimPrefix(path, "/events/"), "/confirm"), i)
	} else {
		writeMethodNotAllowed(w, "GET")
		return
	}
	if err != nil {
		if errors.Is(err, sqlite.ErrReadOnly) {
			writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "capability_not_migrated"}, "")
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, 404, map[string]string{"error": "event_not_found"}, "")
			return
		}
		writeJSON(w, 400, map[string]string{"error": "invalid_input"}, "")
		return
	}
	status := 200
	if r.Method == http.MethodPost && !confirm {
		status = 201
	}
	writeJSON(w, status, map[string]any{"event": e}, "")
}

func valueString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
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
	result, err := h.store.ListAdminVideos(r.Context(), queryInt(r, "page", 1), queryInt(r, "pageSize", queryInt(r, "limit", 100)), r.URL.Query().Get("status"), r.URL.Query().Get("kind"), r.URL.Query().Get("featured"), firstQuery(r, "thumbnailStatus", "thumbnail"), firstQuery(r, "eventId", "event"), !hasPageQuery(r))
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
	result, err := h.store.ListAdminEventsPage(r.Context(), queryInt(r, "page", 1), queryInt(r, "pageSize", 25), queryBool(r, "ascending"), queryBool(r, "includeDeleted"), r.URL.Query().Get("search"), r.URL.Query().Get("provenance"), r.URL.Query().Get("source"), r.URL.Query().Get("from"), r.URL.Query().Get("to"), !hasEventPageQuery(r))
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database_unavailable"}, "")
		return
	}
	if !hasEventPageQuery(r) {
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
	writeJSON(w, http.StatusOK, map[string]any{"runs": result.Runs, "capabilities": map[string]string{"jobs": "capability_not_migrated", "workflows": "capability_not_migrated"}}, "")
}

func (h *adminHandler) overview(w http.ResponseWriter, r *http.Request) {
	result, err := h.store.ListAdminRunsPage(r.Context(), 1, queryInt(r, "pageSize", 12), "", "")
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database_unavailable"}, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": result.Runs, "capabilities": map[string]string{"jobs": "capability_not_migrated", "workflows": "capability_not_migrated"}}, "")
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

func hasEventPageQuery(r *http.Request) bool {
	for _, key := range []string{"page", "pageSize", "search", "provenance", "source", "from", "to"} {
		if _, ok := r.URL.Query()[key]; ok {
			return true
		}
	}
	return false
}
