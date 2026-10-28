package llmadmin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/httpx"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// Synthetic values only: no real provider, host or credential.
const (
	testSecretsKey = "synthetic-secrets-key"
	testAPIKey     = "synthetic-provider-credential"
	testBaseURL    = "https://provider.example.test/v1"
)

// Rejected and literal-address inputs are composed from parts, so the sources
// carry no address-like literals.
func ipv4(a, b, c, d byte) string { return netip.AddrFrom4([4]byte{a, b, c, d}).String() }

func composedURL(scheme, host, path string) string { return scheme + "://" + host + path }

func secureURL(host, path string) string { return composedURL("https", host, path) }

type recordedRequest struct {
	Method string
	URL    string
	Header http.Header
	Body   string
}

// fakeTransport replaces the network. Loopback test servers cannot be used
// because loopback base URLs are rejected by validation.
type fakeTransport struct {
	mu       sync.Mutex
	requests []recordedRequest
	respond  func(*http.Request) (*http.Response, error)
}

func (f *fakeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	body := ""
	if request.Body != nil {
		data, _ := io.ReadAll(request.Body)
		body = string(data)
	}
	f.mu.Lock()
	f.requests = append(f.requests, recordedRequest{request.Method, request.URL.String(), request.Header.Clone(), body})
	respond := f.respond
	f.mu.Unlock()
	if respond == nil {
		return jsonResponse(http.StatusOK, `{}`), nil
	}
	return respond(request)
}

func (f *fakeTransport) recorded() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedRequest(nil), f.requests...)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode:    status,
		Header:        http.Header{"Content-Type": {"application/json"}},
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
	}
}

type harness struct {
	t         *testing.T
	store     *sqlite.Store
	module    *Module
	transport *fakeTransport
}

type harnessOptions struct {
	mode       string
	readOnly   bool
	secretsKey *string
	env        map[string]string
}

func newHarness(t *testing.T, options harnessOptions) *harness {
	t.Helper()
	store, err := sqlite.OpenInitialized(filepath.Join(t.TempDir(), "database", "kano.sqlite"), !options.readOnly, true)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	transport := &fakeTransport{}
	key := testSecretsKey
	if options.secretsKey != nil {
		key = *options.secretsKey
	}
	// A strictly increasing clock keeps updated_at ordering deterministic.
	var clockMu sync.Mutex
	clock := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	module := New(store, Options{
		Mode:       options.mode,
		SecretsKey: key,
		HTTPClient: &http.Client{Transport: transport},
		LookupEnv: func(name string) (string, bool) {
			value, ok := options.env[name]
			return value, ok
		},
		Now: func() time.Time {
			clockMu.Lock()
			defer clockMu.Unlock()
			clock = clock.Add(time.Second)
			return clock
		},
	})
	return &harness{t: t, store: store, module: module, transport: transport}
}

type reply struct {
	Status int
	Raw    string
	JSON   map[string]any
	Header http.Header
}

func (h *harness) call(method, target string, body any) reply {
	h.t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, ok := body.(string)
		if !ok {
			data, err := json.Marshal(body)
			if err != nil {
				h.t.Fatalf("encode body: %v", err)
			}
			encoded = string(data)
		}
		reader = bytes.NewReader([]byte(encoded))
	}
	request := httptest.NewRequest(method, "/api/admin"+target, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	httpx.NewAdminMux(h.module.AdminRoutes()).ServeHTTP(recorder, request)
	result := reply{Status: recorder.Code, Raw: recorder.Body.String(), Header: recorder.Header()}
	if strings.TrimSpace(result.Raw) != "" {
		if err := json.Unmarshal(recorder.Body.Bytes(), &result.JSON); err != nil {
			h.t.Fatalf("%s %s returned non-object JSON %q", method, target, result.Raw)
		}
	}
	return result
}

func (h *harness) ok(method, target string, body any, status int) reply {
	h.t.Helper()
	result := h.call(method, target, body)
	if result.Status != status {
		h.t.Fatalf("%s %s = %d %s, want %d", method, target, result.Status, result.Raw, status)
	}
	return result
}

func (h *harness) invalid(method, target string, body any, message string) reply {
	h.t.Helper()
	result := h.call(method, target, body)
	if result.Status != http.StatusBadRequest || result.JSON["error"] != "invalid_input" || result.JSON["message"] != message {
		h.t.Fatalf("%s %s = %d %s, want invalid_input %q", method, target, result.Status, result.Raw, message)
	}
	return result
}

func (h *harness) createProvider(id string, extra map[string]any) map[string]any {
	h.t.Helper()
	body := map[string]any{"id": id, "baseUrl": testBaseURL, "apiKey": testAPIKey}
	for key, value := range extra {
		body[key] = value
	}
	return object(h.t, h.ok(http.MethodPost, "/llm/providers", body, http.StatusCreated).JSON["provider"])
}

// addModels adds catalog models by ID with inferred tags.
func (h *harness) addModels(providerID string, ids ...string) {
	h.t.Helper()
	models := []any{}
	for _, id := range ids {
		models = append(models, map[string]any{"id": id})
	}
	h.ok(http.MethodPost, "/llm/providers/"+providerID+"/models", map[string]any{"models": models}, http.StatusCreated)
}

func (h *harness) scalar(query string, args ...any) string {
	h.t.Helper()
	var value *string
	if err := h.store.DB().QueryRowContext(context.Background(), query, args...).Scan(&value); err != nil {
		h.t.Fatalf("query %q: %v", query, err)
	}
	if value == nil {
		return ""
	}
	return *value
}

func (h *harness) exec(query string, args ...any) {
	h.t.Helper()
	if _, err := h.store.DB().ExecContext(context.Background(), query, args...); err != nil {
		h.t.Fatalf("exec %q: %v", query, err)
	}
}

// assertNoSecret fails when a response exposes the credential in any form.
func (h *harness) assertNoSecret(results ...reply) {
	h.t.Helper()
	ciphertext := h.scalar(`SELECT COALESCE(MAX(api_key_ciphertext), '') FROM llm_providers`)
	for _, result := range results {
		for _, forbidden := range []string{testAPIKey, testSecretsKey, "apiKeyCiphertext", "api_key_ciphertext", `"apiKey"`} {
			if strings.Contains(result.Raw, forbidden) {
				h.t.Fatalf("response exposes %q: %s", forbidden, result.Raw)
			}
		}
		if ciphertext != "" && strings.Contains(result.Raw, ciphertext) {
			h.t.Fatalf("response exposes the stored ciphertext: %s", result.Raw)
		}
	}
}

func object(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected an object, got %#v", value)
	}
	return result
}

func list(t *testing.T, value any) []any {
	t.Helper()
	result, ok := value.([]any)
	if !ok {
		t.Fatalf("expected an array, got %#v", value)
	}
	return result
}

func stringsOf(t *testing.T, value any) []string {
	t.Helper()
	out := []string{}
	for _, item := range list(t, value) {
		out = append(out, item.(string))
	}
	return out
}

func equal(t *testing.T, label string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %#v, want %#v", label, got, want)
	}
}

func byID(t *testing.T, items any, id string) map[string]any {
	t.Helper()
	for _, item := range list(t, items) {
		if entry := object(t, item); entry["id"] == id {
			return entry
		}
	}
	t.Fatalf("no entry %q in %#v", id, items)
	return nil
}
