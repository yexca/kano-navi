package mcpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
	codeTransport      = -32000
)

const (
	serverName    = "kano-status-board"
	serverVersion = "0.1.2"
	instructions  = "Public tools are read-only. Control tools can refresh the dashboard or start automatic jobs; human confirmation and editing remain available only in /admin."

	latestProtocolVersion = "2025-11-25"
)

// supportedProtocolVersions are the revisions accepted in initialize and in
// the MCP-Protocol-Version header, newest first.
var supportedProtocolVersions = []string{latestProtocolVersion, "2025-06-18", "2025-03-26", "2024-11-05", "2024-10-07"}

func supportedProtocolVersion(version string) bool {
	for _, supported := range supportedProtocolVersions {
		if version == supported {
			return true
		}
	}
	return false
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// message is one validated JSON-RPC message. Responses sent by a client are
// accepted and ignored, as are all notifications.
type message struct {
	isRequest bool
	id        json.RawMessage
	idKey     string
	method    string
	params    map[string]any
}

// encodeJSON marshals without HTML escaping so that the text content of a
// tool result reads like the JSON the original service produced.
func encodeJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buffer.Bytes(), "\n"), nil
}

func transportError(code int, message string) []byte {
	body, err := encodeJSON(struct {
		JSONRPC string   `json:"jsonrpc"`
		Error   rpcError `json:"error"`
		ID      any      `json:"id"`
	}{JSONRPC: "2.0", Error: rpcError{Code: code, Message: message}})
	if err != nil {
		return []byte(`{"jsonrpc":"2.0","error":{"code":-32603,"message":"MCP request failed"},"id":null}`)
	}
	return body
}

// serve runs the protocol layer for one parsed POST body and returns the HTTP
// status with the response body. A nil body means "no content".
func (h *handler) serve(r *http.Request, body any, controlAuthorized bool) (status int, payload []byte) {
	defer func() {
		if recover() != nil {
			// Never surface the panic value: it may quote stored data.
			status = http.StatusInternalServerError
			payload = transportError(codeInternalError, "MCP request failed")
		}
	}()

	rawMessages, isBatch := body.([]any)
	if !isBatch {
		rawMessages = []any{body}
	} else if len(rawMessages) > maxBatchSize {
		return http.StatusBadRequest, transportError(codeInvalidRequest, "Invalid Request: Batch must not exceed "+strconv.Itoa(maxBatchSize)+" messages")
	}
	messages := make([]message, 0, len(rawMessages))
	for _, raw := range rawMessages {
		parsed, ok := parseMessage(raw)
		if !ok {
			return http.StatusBadRequest, transportError(codeParseError, "Parse error: Invalid JSON-RPC message")
		}
		messages = append(messages, parsed)
	}

	initialization := false
	for _, m := range messages {
		if m.isRequest && m.method == "initialize" && validInitializeParams(m.params) {
			initialization = true
		}
	}
	if initialization {
		if len(messages) > 1 {
			return http.StatusBadRequest, transportError(codeInvalidRequest, "Invalid Request: Only one initialization request is allowed")
		}
	} else if versions := r.Header.Values("Mcp-Protocol-Version"); len(versions) > 0 {
		// Version negotiation happens in initialize; later requests may only
		// name a revision this server implements.
		if version := strings.Join(versions, ", "); !supportedProtocolVersion(version) {
			return http.StatusBadRequest, transportError(codeTransport, "Bad Request: Unsupported protocol version: "+version+" (supported versions: "+strings.Join(supportedProtocolVersions, ", ")+")")
		}
	}

	// Responses are keyed by request id in first-seen order, so a repeated id
	// yields one response, like the original transport's request map.
	order := []string{}
	responses := map[string]rpcResponse{}
	for _, m := range messages {
		if !m.isRequest {
			continue
		}
		if _, seen := responses[m.idKey]; !seen {
			order = append(order, m.idKey)
		}
		responses[m.idKey] = h.dispatch(r.Context(), m, controlAuthorized)
	}
	if len(order) == 0 {
		return http.StatusAccepted, nil
	}
	var value any
	if len(order) == 1 {
		value = responses[order[0]]
	} else {
		list := make([]rpcResponse, 0, len(order))
		for _, key := range order {
			list = append(list, responses[key])
		}
		value = list
	}
	encoded, err := encodeJSON(value)
	if err != nil {
		return http.StatusInternalServerError, transportError(codeInternalError, "MCP request failed")
	}
	return http.StatusOK, encoded
}

func (h *handler) dispatch(ctx context.Context, m message, controlAuthorized bool) rpcResponse {
	switch m.method {
	case "initialize":
		if !validInitializeParams(m.params) {
			return errorResponse(m.id, codeInvalidParams, "Invalid params")
		}
		version, _ := m.params["protocolVersion"].(string)
		if !supportedProtocolVersion(version) {
			version = latestProtocolVersion
		}
		return resultResponse(m.id, initializeResult{
			ProtocolVersion: version,
			Capabilities:    serverCapabilities{Tools: toolCapabilities{ListChanged: true}},
			ServerInfo:      serverInfo{Name: serverName, Version: serverVersion},
			Instructions:    instructions,
		})
	case "ping":
		return resultResponse(m.id, struct{}{})
	case "tools/list":
		if cursor, present := m.params["cursor"]; present {
			if _, ok := cursor.(string); !ok {
				return errorResponse(m.id, codeInvalidParams, "Invalid params")
			}
		}
		return resultResponse(m.id, toolList{Tools: listedTools(controlAuthorized)})
	case "tools/call":
		name, ok := m.params["name"].(string)
		if !ok {
			return errorResponse(m.id, codeInvalidParams, "Invalid params")
		}
		arguments := map[string]any{}
		if raw, present := m.params["arguments"]; present {
			if arguments, ok = raw.(map[string]any); !ok {
				return errorResponse(m.id, codeInvalidParams, "Invalid params")
			}
		}
		return resultResponse(m.id, h.callTool(ctx, name, arguments, controlAuthorized))
	default:
		return errorResponse(m.id, codeMethodNotFound, "Method not found")
	}
}

type initializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    serverCapabilities `json:"capabilities"`
	ServerInfo      serverInfo         `json:"serverInfo"`
	Instructions    string             `json:"instructions"`
}

type serverCapabilities struct {
	Tools toolCapabilities `json:"tools"`
}

type toolCapabilities struct {
	ListChanged bool `json:"listChanged"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func resultResponse(id json.RawMessage, result any) rpcResponse {
	encoded, err := encodeJSON(result)
	if err != nil {
		return errorResponse(id, codeInternalError, "MCP request failed")
	}
	return rpcResponse{JSONRPC: "2.0", ID: id, Result: encoded}
}

func errorResponse(id json.RawMessage, code int, text string) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: text}}
}

// validInitializeParams checks the members every client must send. A request
// that fails this is an ordinary request with invalid parameters, not an
// initialization, which matters for the single-initialize batch rule.
func validInitializeParams(params map[string]any) bool {
	if _, ok := params["protocolVersion"].(string); !ok {
		return false
	}
	if _, ok := params["capabilities"].(map[string]any); !ok {
		return false
	}
	client, ok := params["clientInfo"].(map[string]any)
	if !ok {
		return false
	}
	if _, ok := client["name"].(string); !ok {
		return false
	}
	_, ok = client["version"].(string)
	return ok
}

// parseMessage validates the JSON-RPC 2.0 envelope. Envelopes are strict:
// unknown members, a non-object params value or a null id are rejected.
func parseMessage(raw any) (message, bool) {
	object, ok := raw.(map[string]any)
	if !ok {
		return message{}, false
	}
	if version, _ := object["jsonrpc"].(string); version != "2.0" {
		return message{}, false
	}
	if rawMethod, present := object["method"]; present {
		method, ok := rawMethod.(string)
		if !ok || !onlyMembers(object, "jsonrpc", "id", "method", "params") {
			return message{}, false
		}
		parsed := message{method: method}
		if rawParams, present := object["params"]; present {
			params, ok := rawParams.(map[string]any)
			if !ok {
				return message{}, false
			}
			if meta, present := params["_meta"]; present {
				if _, ok := meta.(map[string]any); !ok {
					return message{}, false
				}
			}
			parsed.params = params
		}
		if rawID, present := object["id"]; present {
			id, key, ok := requestID(rawID)
			if !ok {
				return message{}, false
			}
			parsed.isRequest, parsed.id, parsed.idKey = true, id, key
		}
		return parsed, true
	}
	if rawResult, present := object["result"]; present {
		if !onlyMembers(object, "jsonrpc", "id", "result") {
			return message{}, false
		}
		if _, ok := rawResult.(map[string]any); !ok {
			return message{}, false
		}
		_, _, ok := requestID(object["id"])
		return message{}, ok
	}
	if rawError, present := object["error"]; present {
		if !onlyMembers(object, "jsonrpc", "id", "error") {
			return message{}, false
		}
		detail, ok := rawError.(map[string]any)
		if !ok {
			return message{}, false
		}
		if code, ok := detail["code"].(json.Number); !ok || !integral(code) {
			return message{}, false
		}
		if _, ok := detail["message"].(string); !ok {
			return message{}, false
		}
		if rawID, present := object["id"]; present {
			if _, _, ok := requestID(rawID); !ok {
				return message{}, false
			}
		}
		return message{}, true
	}
	return message{}, false
}

func onlyMembers(object map[string]any, allowed ...string) bool {
	for key := range object {
		known := false
		for _, name := range allowed {
			if key == name {
				known = true
				break
			}
		}
		if !known {
			return false
		}
	}
	return true
}

func integral(number json.Number) bool {
	value, err := strconv.ParseFloat(number.String(), 64)
	return err == nil && !math.IsInf(value, 0) && value == math.Trunc(value)
}

var plainIntegerPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

// requestID accepts a string or an integer id and returns its JSON form plus
// a key that distinguishes the string "1" from the number 1.
func requestID(raw any) (json.RawMessage, string, bool) {
	switch id := raw.(type) {
	case string:
		encoded, err := encodeJSON(id)
		if err != nil {
			return nil, "", false
		}
		return encoded, "s:" + id, true
	case json.Number:
		if !integral(id) {
			return nil, "", false
		}
		text := id.String()
		if !plainIntegerPattern.MatchString(text) || text == "-0" {
			// Normalize spellings such as 1.0 or 1e3 to the integer they name.
			value, _ := strconv.ParseFloat(text, 64)
			if math.Abs(value) < 1e21 {
				text = strconv.FormatFloat(value, 'f', -1, 64)
			} else {
				text = strconv.FormatFloat(value, 'e', -1, 64)
			}
			if text == "-0" {
				text = "0"
			}
		}
		return json.RawMessage(text), "n:" + text, true
	default:
		return nil, "", false
	}
}
