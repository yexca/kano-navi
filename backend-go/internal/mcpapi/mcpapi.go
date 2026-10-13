// Package mcpapi serves the stateless Model Context Protocol endpoint at /mcp.
//
// It is a hand-written Streamable HTTP server in JSON response mode: every
// POST carries one JSON-RPC message or batch and receives one JSON body. No
// session, cookie or server-initiated stream exists, so credentials and
// authorization decisions stay request-scoped.
//
// Public tools read sanitized projections of the stored snapshot. Control
// tools need the separate MCP control token and can only advance the dashboard
// revision or start automatic jobs; manual confirmation, editing, deletion,
// media selection, SQL, file access and URL proxying are not reachable here.
package mcpapi

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/jobs"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// bodyLimit mirrors the global 32 KiB JSON body limit of the original service.
const bodyLimit = 32 << 10

// maxBatchSize mirrors the transport limit on one JSON-RPC batch.
const maxBatchSize = 100

// Options configures the endpoint.
type Options struct {
	// Enabled is false when MCP_ENABLED is "0"; every request then gets 404.
	Enabled bool
	// ControlToken is MCP_CONTROL_TOKEN. An empty token leaves the control
	// tools unavailable to everyone.
	ControlToken string
	// Jobs starts and observes automatic jobs. A nil manager keeps status
	// reads on the stored run history and makes job starts fail as tool errors.
	Jobs jobs.Manager
	// Now is the clock used for health and dashboard windows.
	Now func() time.Time
}

type handler struct {
	store        *sqlite.Store
	enabled      bool
	controlToken string
	jobs         jobs.Manager
	now          func() time.Time
}

// New returns the handler for "/mcp" and "/mcp/". It also accepts "/" and ""
// so that it can be mounted behind http.StripPrefix.
func New(store *sqlite.Store, opts Options) http.Handler {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &handler{
		store:        store,
		enabled:      opts.Enabled,
		controlToken: opts.ControlToken,
		jobs:         opts.Jobs,
		now:          now,
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	header := w.Header()
	header.Set("Cache-Control", "no-store, no-transform")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Vary", "Authorization")
	if !h.enabled {
		writeCode(w, http.StatusNotFound, "mcp_disabled")
		return
	}
	if r.Method == http.MethodOptions {
		header.Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	switch r.URL.Path {
	case "/mcp", "/mcp/", "/", "":
	default:
		writeCode(w, http.StatusNotFound, "mcp_not_found")
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		header.Set("Allow", http.MethodPost)
		writeCode(w, http.StatusMethodNotAllowed, "mcp_get_not_supported")
	case http.MethodDelete:
		header.Set("Allow", http.MethodPost)
		writeCode(w, http.StatusMethodNotAllowed, "mcp_delete_not_supported")
	case http.MethodPost:
		h.post(w, r)
	default:
		header.Set("Allow", http.MethodPost)
		writeCode(w, http.StatusMethodNotAllowed, "method_not_allowed")
	}
}

func (h *handler) post(w http.ResponseWriter, r *http.Request) {
	jsonBody := isJSONContentType(r.Header.Get("Content-Type"))

	// Only a JSON body is parsed. Any other media type reaches the transport
	// checks below with no body, exactly like an unparsed request used to.
	var body any
	if jsonBody {
		parsed, status, code := readBody(w, r)
		if code != "" {
			writeCode(w, status, code)
			return
		}
		body = parsed
	}

	// Authorization is decided before any protocol work. A supplied but wrong
	// bearer token is rejected even for public tools, and a control tool is
	// rejected unless the configured token matched.
	supplied, valid := h.authState(r)
	if supplied && !valid {
		unauthorized(w)
		return
	}
	if usesControlTool(body) && (h.controlToken == "" || !valid) {
		unauthorized(w)
		return
	}
	controlAuthorized := h.controlToken != "" && valid

	accept := strings.ToLower(strings.Join(r.Header.Values("Accept"), ", "))
	if !strings.Contains(accept, "application/json") || !strings.Contains(accept, "text/event-stream") {
		writeTransportError(w, http.StatusNotAcceptable, codeTransport, "Not Acceptable: Client must accept both application/json and text/event-stream")
		return
	}
	if !jsonBody {
		writeTransportError(w, http.StatusUnsupportedMediaType, codeTransport, "Unsupported Media Type: Content-Type must be application/json")
		return
	}

	status, payload := h.serve(r, body, controlAuthorized)
	if payload == nil {
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(payload)
}

// readBody bounds and parses a JSON request body. It returns a fixed error
// code instead of parser text: bodies may carry tokens or arbitrary input.
func readBody(w http.ResponseWriter, r *http.Request) (any, int, string) {
	if encoding := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding"))); encoding != "" && encoding != "identity" {
		return nil, http.StatusUnsupportedMediaType, "unsupported_encoding"
	}
	if _, params, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err == nil {
		if charset := strings.ToLower(params["charset"]); charset != "" && !strings.HasPrefix(charset, "utf-") {
			return nil, http.StatusUnsupportedMediaType, "unsupported_encoding"
		}
	}
	if r.Body == nil {
		return map[string]any{}, 0, ""
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, bodyLimit))
	if err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			return nil, http.StatusRequestEntityTooLarge, "request_too_large"
		}
		return nil, http.StatusBadRequest, "invalid_request"
	}
	if len(data) == 0 {
		// An empty JSON body is an empty object, which is then rejected as a
		// JSON-RPC message by the protocol layer.
		return map[string]any{}, 0, ""
	}
	// Strict mode: only an object or an array is a JSON request body.
	trimmed := bytes.TrimLeft(data, " \t\n\r")
	if len(trimmed) == 0 || (trimmed[0] != '{' && trimmed[0] != '[') {
		return nil, http.StatusBadRequest, "invalid_json"
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, http.StatusBadRequest, "invalid_json"
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, http.StatusBadRequest, "invalid_json"
	}
	return value, 0, ""
}

var bearerPattern = regexp.MustCompile(`(?i)^Bearer\s+(\S+)$`)

// authState reports whether a bearer token was supplied and whether it is the
// configured control token. Cookies and admin sessions are never consulted.
func (h *handler) authState(r *http.Request) (supplied, valid bool) {
	match := bearerPattern.FindStringSubmatch(r.Header.Get("Authorization"))
	if match == nil || match[1] == "" {
		return false, false
	}
	return true, constantTimeTokenMatch(match[1], h.controlToken)
}

// constantTimeTokenMatch compares digests so that neither the content nor the
// length of the configured token influences timing. Empty tokens never match.
func constantTimeTokenMatch(actual, expected string) bool {
	if actual == "" || expected == "" {
		return false
	}
	actualDigest := sha256.Sum256([]byte(actual))
	expectedDigest := sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(actualDigest[:], expectedDigest[:]) == 1
}

// usesControlTool inspects the parsed body, single message or batch, for a
// tools/call that names a control tool.
func usesControlTool(body any) bool {
	messages, isBatch := body.([]any)
	if !isBatch {
		messages = []any{body}
	}
	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if method, _ := message["method"].(string); method != "tools/call" {
			continue
		}
		params, _ := message["params"].(map[string]any)
		name, _ := params["name"].(string)
		if controlTools[name] {
			return true
		}
	}
	return false
}

func isJSONContentType(value string) bool {
	if value == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		// A sloppy parameter section does not hide an unambiguous media type,
		// but joined duplicate headers are ambiguous.
		first, rest, _ := strings.Cut(value, ";")
		if strings.Contains(rest, ",") {
			return false
		}
		mediaType = strings.ToLower(strings.TrimSpace(first))
	}
	return mediaType == "application/json"
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="kano-mcp"`)
	writeCode(w, http.StatusUnauthorized, "mcp_control_auth_required")
}

// writeCode writes the fixed {"error": code} shape used outside JSON-RPC.
func writeCode(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + code + `"}`))
}

// writeTransportError writes a JSON-RPC error that is not tied to a request.
func writeTransportError(w http.ResponseWriter, status, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(transportError(code, message))
}
