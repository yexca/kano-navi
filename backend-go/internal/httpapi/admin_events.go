package httpapi

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

var japanZone = time.FixedZone("JST", 9*3600)

// eventField reads the first non-null spelling of a field; operators and
// older clients send either camelCase or snake_case.
func eventField(body map[string]json.RawMessage, names ...string) json.RawMessage {
	for _, name := range names {
		if raw, ok := body[name]; ok && !jsonNull(raw) {
			return raw
		}
	}
	return nil
}

func eventText(raw json.RawMessage, name string, required bool, max int) (string, error) {
	value := jsText(raw)
	if required && value == "" {
		return "", httpx.Invalid("%s is required", name)
	}
	if len(utf16.Encode([]rune(value))) > max {
		return "", httpx.Invalid("%s is too long", name)
	}
	return value, nil
}

// eventInstant normalizes an ISO instant to the stored UTC millisecond form.
func eventInstant(raw json.RawMessage, name string) (string, time.Time, error) {
	value := jsText(raw)
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

// eventInput validates one manual schedule entry with the operator-facing
// messages the console already shows.
func eventInput(body map[string]json.RawMessage) (sqlite.EventInput, error) {
	var input sqlite.EventInput
	var err error
	if input.StartsOn, err = eventText(eventField(body, "startsOn", "starts_on"), "startsOn", true, 10); err != nil {
		return input, err
	}
	if day, parseErr := time.Parse("2006-01-02", input.StartsOn); parseErr != nil || day.Format("2006-01-02") != input.StartsOn {
		return input, httpx.Invalid("startsOn is invalid")
	}
	var startsAt, endsAt time.Time
	if input.StartsAt, startsAt, err = eventInstant(eventField(body, "startsAt", "starts_at"), "startsAt"); err != nil {
		return input, err
	}
	if input.StartsAt != "" && startsAt.In(japanZone).Format("2006-01-02") != input.StartsOn {
		return input, httpx.Invalid("startsAt must fall on startsOn in Asia/Tokyo")
	}
	if input.EndsAt, endsAt, err = eventInstant(eventField(body, "endsAt", "ends_at"), "endsAt"); err != nil {
		return input, err
	}
	if input.EndsAt != "" && input.StartsAt == "" {
		return input, httpx.Invalid("endsAt requires startsAt")
	}
	if input.EndsAt != "" && endsAt.Before(startsAt) {
		return input, httpx.Invalid("endsAt must not be earlier than startsAt")
	}
	if input.EventType, err = eventText(eventField(body, "eventType", "event_type"), "eventType", false, 32); err != nil {
		return input, err
	}
	if input.EventType == "" {
		input.EventType = "event"
	}
	switch input.EventType {
	case "event", "stream", "member", "release", "appearance":
	default:
		return input, httpx.Invalid("eventType is invalid")
	}
	input.TimePrecision = "unknown"
	if input.StartsAt != "" {
		if input.TimePrecision, err = eventText(eventField(body, "timePrecision", "time_precision"), "timePrecision", false, 20); err != nil {
			return input, err
		}
		if input.TimePrecision == "" {
			input.TimePrecision = "exact"
		}
	}
	switch input.TimePrecision {
	case "exact", "approximate", "unknown":
	default:
		return input, httpx.Invalid("timePrecision is invalid")
	}
	if input.CancellationStatus, err = eventText(eventField(body, "cancellationStatus", "cancellation_status"), "cancellationStatus", false, 32); err != nil {
		return input, err
	}
	if input.CancellationStatus == "" {
		input.CancellationStatus = "none"
	}
	// llm_suspected only means "keep the current LLM overlay"; the store
	// ignores it unless the event already carries that overlay.
	switch input.CancellationStatus {
	case "none", "llm_suspected", "manual_confirmed":
	default:
		return input, httpx.Invalid("cancellationStatus is invalid")
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
		if input.CancellationReason, err = eventText(eventField(body, "cancellationReason", "cancellation_reason"), "cancellationReason", true, 500); err != nil {
			return input, err
		}
	}
	if input.URL, err = eventURL(body["url"]); err != nil {
		return input, err
	}
	return input, nil
}

func (h *adminHandler) writeEventError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sqlite.ErrReadOnly):
		httpx.WriteCode(w, http.StatusNotImplemented, "capability_not_migrated")
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

// mutation handles manual schedule creation, edits, confirmation and
// deletion. Each of them locks the entry against automatic extraction.
func (h *adminHandler) mutation(w http.ResponseWriter, r *http.Request, path string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case r.Method == http.MethodDelete && len(parts) == 2:
		if _, err := h.store.DeleteManualEvent(r.Context(), parts[1]); err != nil {
			h.writeEventError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && len(parts) == 1:
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
		event, err := h.store.CreateManualEvent(r.Context(), input)
		if err != nil {
			h.writeEventError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, map[string]any{"event": event})
	case r.Method == http.MethodPut && len(parts) == 2:
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
		event, err := h.store.UpdateManualEvent(r.Context(), parts[1], input)
		if err != nil {
			h.writeEventError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"event": event})
	case r.Method == http.MethodPost && len(parts) == 3 && parts[2] == "confirm":
		// Confirming keeps the stored values unless the request overrides them.
		existing, err := h.store.GetAdminEvent(r.Context(), parts[1])
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
		event, err := h.store.UpdateManualEvent(r.Context(), parts[1], input)
		if err != nil {
			h.writeEventError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"event": event})
	default:
		writeMethodNotAllowed(w, "GET")
	}
}
