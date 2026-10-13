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

// AdminModule owns a group of /api/admin routes. The router has already
// authenticated the session and applied the origin guard. path is the request
// path without the "/api/admin" prefix. ServeAdmin returns false when the
// path belongs to another module.
type AdminModule interface {
	ServeAdmin(w http.ResponseWriter, r *http.Request, path string) bool
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
