package adminapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/yexca/kano-navi/backend-go/internal/httpx"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// --- lists ------------------------------------------------------------------

func (a *api) listEvents(w http.ResponseWriter, r *http.Request) {
	page, err := a.Store.ListAdminEvents(r.Context(), sqlite.AdminEventFilter{
		Page:           queryInt(r, "page", 1),
		PageSize:       queryInt(r, "pageSize", 25),
		Ascending:      query(r, "order") == "asc",
		IncludeDeleted: queryFlag(r, "deleted"),
		Search:         query(r, "search"),
		Provenance:     query(r, "provenance"),
		Source:         query(r, "source"),
		Cancellation:   query(r, "cancellation"),
		From:           query(r, "from"),
		To:             query(r, "to"),
	})
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}

func (a *api) listPosts(w http.ResponseWriter, r *http.Request) {
	page, err := a.Store.ListAdminPosts(r.Context(), sqlite.AdminPostFilter{
		Page:      queryInt(r, "page", 1),
		PageSize:  queryInt(r, "pageSize", 50),
		Source:    query(r, "source"),
		Account:   query(r, "account"),
		From:      query(r, "from"),
		To:        query(r, "to"),
		Search:    query(r, "search"),
		Media:     query(r, "media"),
		LLM:       query(r, "llm"),
		Reprocess: queryFlag(r, "reprocess"),
	})
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}

func (a *api) listVideos(w http.ResponseWriter, r *http.Request) {
	page, err := a.Store.ListAdminVideos(r.Context(), sqlite.AdminVideoFilter{
		Page:      queryInt(r, "page", 1),
		PageSize:  queryInt(r, "pageSize", 50),
		Status:    query(r, "status"),
		Kind:      query(r, "kind"),
		Thumbnail: query(r, "thumbnail"),
	})
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}

// --- event writes -----------------------------------------------------------

// Every event write is manual: it locks the entry against automatic
// extraction, and a delete leaves a durable tombstone.

func (a *api) createEvent(w http.ResponseWriter, r *http.Request) {
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	input, err := eventInput(body)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	event, err := a.Store.CreateManualEvent(r.Context(), input)
	if err != nil {
		writeEventError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"event": event})
}

func (a *api) updateEvent(w http.ResponseWriter, r *http.Request) {
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	input, err := eventInput(body)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	event, err := a.Store.UpdateManualEvent(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeEventError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"event": event})
}

// confirmEvent keeps the stored values unless the request overrides them.
func (a *api) confirmEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := a.Store.GetAdminEvent(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) || err == nil && existing.DeletedAt != nil {
		httpx.WriteCode(w, http.StatusNotFound, "event_not_found")
		return
	}
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	merged := map[string]json.RawMessage{}
	if encoded, encodeErr := json.Marshal(existing); encodeErr == nil {
		_ = json.Unmarshal(encoded, &merged)
	}
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	for key, value := range body {
		merged[key] = value
	}
	input, err := eventInput(merged)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	event, err := a.Store.UpdateManualEvent(r.Context(), id, input)
	if err != nil {
		writeEventError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"event": event})
}

func (a *api) deleteEvent(w http.ResponseWriter, r *http.Request) {
	if _, err := a.Store.DeleteManualEvent(r.Context(), r.PathValue("id")); err != nil {
		writeEventError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeEventError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sqlite.ErrReadOnly):
		httpx.ReadOnly(w)
	case errors.Is(err, sql.ErrNoRows):
		httpx.WriteCode(w, http.StatusNotFound, "event_not_found")
	case errors.Is(err, sqlite.ErrEventExists):
		httpx.WriteError(w, httpx.Invalid("event already exists"))
	case errors.Is(err, sqlite.ErrEventInvalid):
		httpx.WriteError(w, httpx.Invalid("event is invalid"))
	default:
		httpx.WriteError(w, err)
	}
}

// --- event validation -------------------------------------------------------

// textField reads a trimmed scalar; null and absent are empty.
func textField(raw json.RawMessage) string {
	if jsonNull(raw) {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}
	return strings.TrimSpace(string(raw))
}

func eventText(raw json.RawMessage, name string, required bool, limit int) (string, error) {
	value := textField(raw)
	if required && value == "" {
		return "", httpx.Invalid("%s is required", name)
	}
	if len(utf16.Encode([]rune(value))) > limit {
		return "", httpx.Invalid("%s is too long", name)
	}
	return value, nil
}

// eventInstant normalizes an ISO instant to the stored UTC millisecond form.
func eventInstant(raw json.RawMessage, name string) (string, time.Time, error) {
	value := textField(raw)
	if value == "" {
		return "", time.Time{}, nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04Z07:00"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return sqlite.ISOTime(parsed), parsed, nil
		}
	}
	return "", time.Time{}, httpx.Invalid("%s is invalid", name)
}

func eventURL(raw json.RawMessage) (string, error) {
	value, err := eventText(raw, "url", false, 2000)
	if err != nil || value == "" {
		return "", err
	}
	parsed, parseErr := url.Parse(value)
	if parseErr != nil || parsed.Host == "" {
		return "", httpx.Invalid("url is invalid")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", httpx.Invalid("url must use HTTP or HTTPS")
	}
	parsed.Scheme = scheme
	parsed.Host = strings.ToLower(parsed.Host)
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	return parsed.String(), nil
}

func oneOf(value, fallback, name string, allowed ...string) (string, error) {
	if value == "" {
		return fallback, nil
	}
	for _, candidate := range allowed {
		if value == candidate {
			return value, nil
		}
	}
	return "", httpx.Invalid("%s is invalid", name)
}

// eventInput validates one manual schedule entry.
func eventInput(body map[string]json.RawMessage) (sqlite.EventInput, error) {
	var input sqlite.EventInput
	var err error
	if input.StartsOn, err = eventText(body["startsOn"], "startsOn", true, 10); err != nil {
		return input, err
	}
	if day, parseErr := time.Parse("2006-01-02", input.StartsOn); parseErr != nil || day.Format("2006-01-02") != input.StartsOn {
		return input, httpx.Invalid("startsOn is invalid")
	}
	var startsAt, endsAt time.Time
	if input.StartsAt, startsAt, err = eventInstant(body["startsAt"], "startsAt"); err != nil {
		return input, err
	}
	if input.StartsAt != "" && startsAt.In(japanZone).Format("2006-01-02") != input.StartsOn {
		return input, httpx.Invalid("startsAt must fall on startsOn in Asia/Tokyo")
	}
	if input.EndsAt, endsAt, err = eventInstant(body["endsAt"], "endsAt"); err != nil {
		return input, err
	}
	if input.EndsAt != "" && input.StartsAt == "" {
		return input, httpx.Invalid("endsAt requires startsAt")
	}
	if input.EndsAt != "" && endsAt.Before(startsAt) {
		return input, httpx.Invalid("endsAt must not be earlier than startsAt")
	}
	if input.EventType, err = oneOf(textField(body["eventType"]), "event", "eventType", "event", "stream", "member", "release", "appearance"); err != nil {
		return input, err
	}
	// A date-only entry never acquires a time precision.
	input.TimePrecision = "unknown"
	if input.StartsAt != "" {
		if input.TimePrecision, err = oneOf(textField(body["timePrecision"]), "exact", "timePrecision", "exact", "approximate", "unknown"); err != nil {
			return input, err
		}
	}
	// llm_suspected only means "keep the current LLM overlay"; the store
	// ignores it unless the event already carries that overlay.
	if input.CancellationStatus, err = oneOf(textField(body["cancellationStatus"]), "none", "cancellationStatus", "none", "llm_suspected", "manual_confirmed"); err != nil {
		return input, err
	}
	if input.Title, err = eventText(body["title"], "title", true, 240); err != nil {
		return input, err
	}
	if input.Detail, err = eventText(body["detail"], "detail", false, 500); err != nil {
		return input, err
	}
	if input.Status, err = eventText(body["status"], "status", false, 80); err != nil {
		return input, err
	}
	if input.CancellationStatus == "manual_confirmed" {
		if input.CancellationReason, err = eventText(body["cancellationReason"], "cancellationReason", true, 500); err != nil {
			return input, err
		}
	}
	if input.URL, err = eventURL(body["url"]); err != nil {
		return input, err
	}
	return input, nil
}
