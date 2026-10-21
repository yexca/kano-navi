// Package httpx holds the small response helpers shared by admin feature
// modules so each one reports errors with the same stable wire shape.
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/yexca/kano-navi/backend-go/internal/jsonbody"
)

// AdminPrefix is the mount point of the operator API.
const AdminPrefix = "/api/admin"

// Route is one operator endpoint. Path is relative to AdminPrefix and may use
// http.ServeMux wildcards such as {id}; handlers read them with PathValue.
type Route struct {
	Method  string
	Path    string
	Handler http.HandlerFunc
}

// AdminModule contributes a group of /api/admin routes. The router has
// already authenticated the session and applied the origin guard.
type AdminModule interface {
	AdminRoutes() []Route
}

// NewAdminMux registers routes under AdminPrefix. Unknown paths and methods
// answer with the JSON error shape instead of the plain-text mux defaults.
func NewAdminMux(groups ...[]Route) http.Handler {
	mux := http.NewServeMux()
	for _, routes := range groups {
		for _, route := range routes {
			mux.HandleFunc(route.Method+" "+AdminPrefix+route.Path, route.Handler)
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler, pattern := mux.Handler(r)
		if pattern != "" {
			mux.ServeHTTP(w, r)
			return
		}
		// The fallback reports 404 or 405 (with Allow); keep its status and
		// Allow header but answer in JSON.
		probe := &statusProbe{header: http.Header{}}
		handler.ServeHTTP(probe, r)
		if probe.status == http.StatusMethodNotAllowed {
			MethodNotAllowed(w, probe.header.Get("Allow"))
			return
		}
		WriteCode(w, http.StatusNotFound, "not_found")
	})
}

type statusProbe struct {
	header http.Header
	status int
}

func (p *statusProbe) Header() http.Header         { return p.header }
func (p *statusProbe) Write(b []byte) (int, error) { return len(b), nil }
func (p *statusProbe) WriteHeader(status int)      { p.status = status }

// ReadOnly reports a write against a database opened without write access.
func ReadOnly(w http.ResponseWriter) {
	WriteCode(w, http.StatusServiceUnavailable, "read_only")
}

// InputError is an operator-facing validation failure. Its message is safe to
// return: it never contains stored secrets, SQL, paths or provider payloads.
type InputError struct{ Message string }

func (e *InputError) Error() string { return e.Message }

func Invalid(format string, args ...any) *InputError {
	return &InputError{Message: fmt.Sprintf(format, args...)}
}

func WriteJSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		status = http.StatusInternalServerError
		body = []byte(`{"error":"internal_error"}`)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func WriteCode(w http.ResponseWriter, status int, code string) {
	WriteJSON(w, status, map[string]string{"error": code})
}

func MethodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	WriteCode(w, http.StatusMethodNotAllowed, "method_not_allowed")
}

// WriteError maps a handler error to the admin wire contract: validation
// failures keep their message, everything else is a fixed code.
func WriteError(w http.ResponseWriter, err error) {
	var input *InputError
	switch {
	case errors.As(err, &input):
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_input", "message": input.Message})
	case errors.Is(err, jsonbody.ErrLarge):
		WriteCode(w, http.StatusRequestEntityTooLarge, "request_too_large")
	case errors.Is(err, jsonbody.ErrInvalid):
		WriteCode(w, http.StatusBadRequest, "invalid_json")
	default:
		WriteCode(w, http.StatusInternalServerError, "internal_error")
	}
}

// Body reads a bounded JSON object. An absent body is an empty object, which
// matches express.json leaving request.body as {}.
func Body(w http.ResponseWriter, r *http.Request) (map[string]json.RawMessage, error) {
	if r.Body == nil || r.Body == http.NoBody || r.ContentLength == 0 {
		return map[string]json.RawMessage{}, nil
	}
	return jsonbody.Object(w, r)
}
