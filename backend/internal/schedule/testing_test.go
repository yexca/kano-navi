package schedule

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend/internal/secretstore"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

const (
	testKey         = "synthetic-secrets-key"
	testProviderKey = "synthetic-provider-key"
	testTimestamp   = "2026-04-01T00:00:00.000Z"
)

// defaultAttempts is the calls a model gets per input before an operator
// changes the setting.
var defaultAttempts = sqlite.DefaultSyncSettings().LLMModelAttempts

type recordedRequest struct {
	url     string
	headers http.Header
	body    string
}

// fakeProvider is the only transport the tests use; nothing reaches a network.
type fakeProvider struct {
	mu       sync.Mutex
	requests []recordedRequest
	respond  func(request recordedRequest) (int, string)
}

func (f *fakeProvider) RoundTrip(request *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(request.Body)
	recorded := recordedRequest{url: request.URL.String(), headers: request.Header.Clone(), body: string(body)}
	f.mu.Lock()
	f.requests = append(f.requests, recorded)
	respond := f.respond
	f.mu.Unlock()
	status, payload := respond(recorded)
	if status == 0 {
		return nil, errors.New("synthetic transport failure")
	}
	header := http.Header{"Content-Type": []string{"application/json"}}
	if status >= 300 && status < 400 {
		header.Set("Location", "https://redirect.example.test/elsewhere")
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(payload)),
		Request:    request,
	}, nil
}

func (f *fakeProvider) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeProvider) hosts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.requests))
	for i, request := range f.requests {
		out[i] = strings.TrimPrefix(strings.SplitN(request.url, "/", 4)[2], "www.")
	}
	return out
}

type fixture struct {
	t        *testing.T
	store    *sqlite.Store
	db       *sql.DB
	provider *fakeProvider
	run      *Extractor
	env      map[string]string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	store, err := sqlite.OpenInitialized(filepath.Join(t.TempDir(), "database", "kano.sqlite"), true, true)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	f := &fixture{t: t, store: store, db: store.DB(), provider: &fakeProvider{}, env: map[string]string{}}
	// The disposable snapshot needs no durability; this keeps the many small
	// commits of a scan fast on every platform.
	f.exec(`PRAGMA synchronous = OFF`)
	f.provider.respond = func(recordedRequest) (int, string) { return http.StatusInternalServerError, `{}` }
	f.run = New(store, Options{
		SecretsKey: testKey,
		HTTPClient: &http.Client{Transport: f.provider},
		Getenv:     func(key string) string { return f.env[key] },
	})
	f.run.sleep = func(context.Context, time.Duration) error { return nil }
	return f
}

func (f *fixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.db.Exec(query, args...); err != nil {
		f.t.Fatalf("exec %q: %v", query, err)
	}
}

func (f *fixture) text(query string, args ...any) string {
	f.t.Helper()
	var value sql.NullString
	if err := f.db.QueryRow(query, args...).Scan(&value); err != nil {
		f.t.Fatalf("query %q: %v", query, err)
	}
	return value.String
}

func (f *fixture) number(query string, args ...any) int {
	f.t.Helper()
	var value sql.NullInt64
	if err := f.db.QueryRow(query, args...).Scan(&value); err != nil {
		f.t.Fatalf("query %q: %v", query, err)
	}
	return int(value.Int64)
}

// addProvider stores a keyed provider and one catalog model tagged with
// capabilities.
func (f *fixture) addProvider(id, protocol, baseURL, model, capabilities string) {
	f.t.Helper()
	ciphertext, err := secretstore.Encrypt(testProviderKey, testKey)
	if err != nil {
		f.t.Fatalf("encrypt: %v", err)
	}
	vision := 0
	if strings.Contains(capabilities, "image") {
		vision = 1
	}
	f.exec(`INSERT INTO llm_providers (id, name, protocol, base_url, model, enabled, vision_capable,
		capabilities_json, timeout_ms, max_retries, api_key_ciphertext, created_at, updated_at)
		VALUES (?, ?, ?, ?, '', 1, ?, ?, 30000, 2, ?, ?, ?)`,
		id, id, protocol, baseURL, vision, capabilities, ciphertext, testTimestamp, testTimestamp)
	f.exec(`INSERT INTO llm_models (provider_id, model_id, tags_json, enabled, created_at, updated_at)
		VALUES (?, ?, ?, 1, ?, ?)`, id, model, capabilities, testTimestamp, testTimestamp)
}

// queue appends each provider's first catalog model to the priority queue.
func (f *fixture) queue(providerIDs ...string) {
	f.t.Helper()
	for _, id := range providerIDs {
		f.exec(`INSERT INTO llm_route_targets (route, provider_id, model_id, priority, created_at, updated_at)
			VALUES (?, ?, (SELECT model_id FROM llm_models WHERE provider_id = ? ORDER BY rowid LIMIT 1),
			(SELECT COUNT(*) FROM llm_route_targets WHERE route = ?), ?, ?)`, routeQueue, id, id, routeQueue, testTimestamp, testTimestamp)
	}
}

func (f *fixture) addPost(id, label, text, publishedAt, rawJSON string) string {
	f.t.Helper()
	url := "https://x.com/synthetic_handle/status/" + id
	f.exec(`INSERT INTO posts (id, source, account_handle, type, label, text, published_at, url, raw_json)
		VALUES (?, 'x', 'synthetic_handle', 'daily', ?, ?, ?, ?, ?)`, id, label, text, publishedAt, url, nullable(rawJSON))
	return url
}

// addCachedImage registers a ready cache file for a source URL and returns its
// content hash.
func (f *fixture) addCachedImage(sourceURL string, content []byte) string {
	f.t.Helper()
	hash := sha256Hex(string(content))
	relative := "x/sha256/" + hash[:2] + "/" + hash + ".png"
	target := filepath.Join(f.store.DataRoot(), filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		f.t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, content, 0o644); err != nil {
		f.t.Fatalf("write image: %v", err)
	}
	f.exec(`INSERT INTO media_assets (id, source, source_url, cache_path, mime_type, extension, byte_size,
		sha256, status, created_at, updated_at) VALUES (?, 'x', ?, ?, 'image/png', 'png', ?, ?, 'ready', ?, ?)`,
		mediaIDForSourceURL(sourceURL), sourceURL, relative, len(content), hash, testTimestamp, testTimestamp)
	return hash
}

func (f *fixture) scan() Result {
	f.t.Helper()
	result, err := f.run.Run(context.Background())
	if err != nil {
		f.t.Fatalf("run: %v", err)
	}
	return result
}

// chatReply wraps a structured result the way a chat-completions provider
// returns it.
func chatReply(result string) string {
	return stringify(obj("choices", []any{obj("message", obj("content", result))}))
}

const pngHeader = "\x89PNG\r\n\x1a\n\x02"

func scheduleReply(title, date, clock, status string) string {
	var clockValue any
	if clock != "" {
		clockValue = clock
	}
	return stringify(obj(
		"classification", "schedule",
		"events", []any{obj(
			"title", title, "detail", nil, "date", date, "time", clockValue, "endTime", nil,
			"timePrecision", "exact", "status", status, "eventType", "stream", "url", nil,
			"confidence", 0.9, "evidence", "synthetic evidence")},
		"confidence", 0.9, "evidence", "synthetic evidence", "reason", nil, "action", "add",
	))
}
