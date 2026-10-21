package llmadmin

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/yexca/kano-navi/backend-go/internal/secretstore"
)

func TestProviderLifecycleNeverReturnsTheCredential(t *testing.T) {
	h := newHarness(t, harnessOptions{})

	created := h.ok(http.MethodPost, "/llm/providers", map[string]any{
		"id": "alpha", "name": " Alpha ", "baseUrl": "https://Provider.Example.Test:443/v1/",
		"apiKey": "  " + testAPIKey + "  ", "model": "alpha-chat",
	}, http.StatusCreated)
	provider := object(t, created.JSON["provider"])
	// Providers have no default model; an older client's field is ignored.
	if _, present := provider["model"]; present {
		t.Fatal("a provider must not report a default model")
	}
	equal(t, "name", provider["name"], "Alpha")
	equal(t, "baseUrl", provider["baseUrl"], testBaseURL)
	equal(t, "protocol", provider["protocol"], "openai-responses")
	equal(t, "apiKeyConfigured", provider["apiKeyConfigured"], true)
	equal(t, "enabled", provider["enabled"], true)
	equal(t, "visionCapable", provider["visionCapable"], true)
	equal(t, "capabilities", stringsOf(t, provider["capabilities"]), []string{"text", "image"})
	equal(t, "timeoutMs", provider["timeoutMs"], float64(30000))
	equal(t, "maxRetries", provider["maxRetries"], float64(MaxRetries))
	for _, key := range []string{"lastStatus", "lastError", "lastCheckedAt"} {
		if value, present := provider[key]; !present || value != nil {
			t.Fatalf("%s should be an explicit null, got %#v (present=%v)", key, value, present)
		}
	}
	if _, present := provider["priority"]; present {
		t.Fatal("an unrouted listing must not carry a priority")
	}

	ciphertext := h.scalar(`SELECT api_key_ciphertext FROM llm_providers WHERE id = 'alpha'`)
	if !strings.HasPrefix(ciphertext, "v1:") {
		t.Fatalf("credential is not stored in the shared v1 format")
	}
	if plaintext, err := secretstore.Decrypt(ciphertext, testSecretsKey); err != nil || plaintext != testAPIKey {
		t.Fatalf("stored credential does not round-trip (err=%v)", err)
	}

	listed := h.ok(http.MethodGet, "/llm", nil, http.StatusOK)
	equal(t, "listed provider", byID(t, listed.JSON["providers"], "alpha")["apiKeyConfigured"], true)
	equal(t, "no implicit catalog model", len(list(t, listed.JSON["models"])), 0)
	equal(t, "no implicit queue target", len(list(t, listed.JSON["queue"])), 0)
	equal(t, "retired model column", h.scalar(`SELECT model FROM llm_providers WHERE id = 'alpha'`), "")

	// An edit without apiKey keeps the credential.
	edited := h.ok(http.MethodPut, "/llm/providers/alpha", map[string]any{"name": "Alpha 2", "capabilities": []string{"text"}, "id": "ignored"}, http.StatusOK)
	provider = object(t, edited.JSON["provider"])
	equal(t, "id", provider["id"], "alpha")
	equal(t, "name", provider["name"], "Alpha 2")
	equal(t, "apiKeyConfigured", provider["apiKeyConfigured"], true)
	equal(t, "visionCapable", provider["visionCapable"], false)
	equal(t, "capabilities", stringsOf(t, provider["capabilities"]), []string{"text"})
	models := h.ok(http.MethodGet, "/llm", nil, http.StatusOK)
	equal(t, "modelTags", stringsOf(t, models.JSON["modelTags"]), llmModelTags)

	h.assertNoSecret(created, listed, edited, models)

	cleared := h.ok(http.MethodPut, "/llm/providers/alpha", map[string]any{"apiKey": nil}, http.StatusOK)
	equal(t, "apiKeyConfigured", object(t, cleared.JSON["provider"])["apiKeyConfigured"], false)
	if h.scalar(`SELECT api_key_ciphertext FROM llm_providers WHERE id = 'alpha'`) != "" {
		t.Fatal("clearing the credential must store NULL")
	}

	missing := h.call(http.MethodPut, "/llm/providers/ghost", map[string]any{"name": "x"})
	equal(t, "missing status", missing.Status, http.StatusNotFound)
	equal(t, "missing code", missing.JSON["error"], "llm_provider_not_found")

	deleted := h.ok(http.MethodDelete, "/llm/providers/alpha", nil, http.StatusNoContent)
	equal(t, "delete body", deleted.Raw, "")
	equal(t, "models left", h.scalar(`SELECT CAST(COUNT(*) AS TEXT) FROM llm_models`), "0")
	equal(t, "targets left", h.scalar(`SELECT CAST(COUNT(*) AS TEXT) FROM llm_route_targets`), "0")
	again := h.call(http.MethodDelete, "/llm/providers/alpha", nil)
	equal(t, "second delete", again.Status, http.StatusNotFound)
	equal(t, "second delete code", again.JSON["error"], "llm_provider_not_found")
}

func TestProviderValidation(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	base := func(extra map[string]any) map[string]any {
		body := map[string]any{"id": "alpha", "baseUrl": testBaseURL}
		for key, value := range extra {
			body[key] = value
		}
		return body
	}
	private := "baseUrl must not target a private or loopback address"
	cases := []struct {
		name    string
		body    map[string]any
		message string
	}{
		{"missing id", map[string]any{"baseUrl": testBaseURL}, "id is required"},
		{"bad id", base(map[string]any{"id": "Bad Id"}), "id contains unsupported characters"},
		{"missing url", map[string]any{"id": "alpha"}, "baseUrl is required"},
		{"not a url", base(map[string]any{"baseUrl": "not a url"}), "baseUrl is invalid"},
		{"scheme", base(map[string]any{"baseUrl": "ftp://provider.example.test"}), "baseUrl must use HTTP or HTTPS"},
		{"credentials", base(map[string]any{"baseUrl": secureURL(strings.Join([]string{"user", "pass"}, ":")+"@provider.example.test", "")}), "baseUrl must not contain credentials, query, or fragment"},
		{"query", base(map[string]any{"baseUrl": testBaseURL + "?x=1"}), "baseUrl must not contain credentials, query, or fragment"},
		{"fragment", base(map[string]any{"baseUrl": testBaseURL + "#x"}), "baseUrl must not contain credentials, query, or fragment"},
		{"loopback v4", base(map[string]any{"baseUrl": "https://127.0.0.1/v1"}), private},
		{"loopback integer", base(map[string]any{"baseUrl": "https://2130706433/v1"}), private},
		{"loopback hex", base(map[string]any{"baseUrl": "https://0x7f.1/v1"}), private},
		{"private v4", base(map[string]any{"baseUrl": secureURL(ipv4(10, 1, 2, 3), "")}), private},
		{"link local", base(map[string]any{"baseUrl": secureURL(ipv4(169, 254, 169, 254), "/latest")}), private},
		{"loopback v6", base(map[string]any{"baseUrl": "https://[::1]/v1"}), private},
		{"unique local v6", base(map[string]any{"baseUrl": secureURL("[fd00::1]", "/v1")}), private},
		{"mapped v4", base(map[string]any{"baseUrl": secureURL("[::ffff:"+ipv4(10, 0, 0, 1)+"]", "/v1")}), private},
		{"localhost", base(map[string]any{"baseUrl": "https://LOCALHOST./v1"}), private},
		{"internal name", base(map[string]any{"baseUrl": secureURL("svc.internal", "/v1")}), private},
		{"local name", base(map[string]any{"baseUrl": secureURL("printer.local", "")}), private},
		{"http in production", base(map[string]any{"baseUrl": "http://provider.example.test/v1"}), "baseUrl must use HTTPS in production"},
		{"protocol", base(map[string]any{"protocol": "bogus"}), "protocol is invalid"},
		{"enabled", base(map[string]any{"enabled": "maybe"}), "enabled must be a boolean"},
		{"vision flag", base(map[string]any{"visionCapable": nil}), "visionCapable must be a boolean"},
		{"no capability", base(map[string]any{"capabilities": []string{"audio"}}), "capabilities must include at least one of text or image"},
		{"extra capability", base(map[string]any{"capabilities": []string{"text", "audio"}}), "capabilities contains an unsupported value"},
		{"timeout", base(map[string]any{"timeoutMs": 5}), "timeoutMs must be an integer between 1000 and 120000"},
		{"retries", base(map[string]any{"maxRetries": 9}), "maxRetries must be an integer between 0 and 2"},
		{"long key", base(map[string]any{"apiKey": strings.Repeat("k", 501)}), "apiKey is too long"},
		{"long name", base(map[string]any{"name": strings.Repeat("n", 121)}), "name is too long"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			h.t = t
			h.invalid(http.MethodPost, "/llm/providers", test.body, test.message)
		})
	}
	h.t = t
	equal(t, "rows after rejected input", h.scalar(`SELECT CAST(COUNT(*) AS TEXT) FROM llm_providers`), "0")

	malformed := h.call(http.MethodPost, "/llm/providers", `{"id":`)
	equal(t, "malformed status", malformed.Status, http.StatusBadRequest)
	equal(t, "malformed code", malformed.JSON["error"], "invalid_json")

	// Legacy forms are still accepted.
	legacy := h.createProvider("legacy", map[string]any{"visionCapable": "false", "enabled": "off", "timeoutMs": "45000", "capabilitiesIgnored": true})
	equal(t, "legacy capabilities", stringsOf(t, legacy["capabilities"]), []string{"text"})
	equal(t, "legacy enabled", legacy["enabled"], false)
	equal(t, "legacy timeout", legacy["timeoutMs"], float64(45000))
	textual := h.createProvider("textual", map[string]any{"capabilities": "vision, text"})
	equal(t, "string capabilities", stringsOf(t, textual["capabilities"]), []string{"image", "text"})
}

func TestProviderSecretRequiresTheSecretsKey(t *testing.T) {
	empty := ""
	h := newHarness(t, harnessOptions{secretsKey: &empty})
	result := h.invalid(http.MethodPost, "/llm/providers", map[string]any{"id": "alpha", "baseUrl": testBaseURL, "apiKey": testAPIKey},
		"LLM_SECRETS_KEY is required to store a secret")
	h.assertNoSecret(result)
	// Without a credential the provider can still be saved.
	provider := object(t, h.ok(http.MethodPost, "/llm/providers", map[string]any{"id": "alpha", "baseUrl": testBaseURL}, http.StatusCreated).JSON["provider"])
	equal(t, "apiKeyConfigured", provider["apiKeyConfigured"], false)
}

func TestDevelopmentModeAllowsPlainHTTPButNotPrivateHosts(t *testing.T) {
	h := newHarness(t, harnessOptions{mode: "development"})
	provider := h.createProvider("alpha", map[string]any{"baseUrl": "http://provider.example.test:8080/api/"})
	equal(t, "baseUrl", provider["baseUrl"], "http://provider.example.test:8080/api")
	h.invalid(http.MethodPost, "/llm/providers", map[string]any{"id": "beta", "baseUrl": composedURL("http", ipv4(192, 168, 1, 10)+":8080", "")},
		"baseUrl must not target a private or loopback address")
}

func TestNewProvidersAndModelsAreNotQueuedImplicitly(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	h.createProvider("alpha", map[string]any{"model": "alpha-chat"})
	h.createProvider("beta", nil)
	h.addModels("beta", "beta-chat")

	catalog := h.ok(http.MethodGet, "/llm", nil, http.StatusOK)
	equal(t, "provider count", len(list(t, catalog.JSON["providers"])), 2)
	equal(t, "queue", len(list(t, catalog.JSON["queue"])), 0)
	h.assertNoSecret(catalog)
}

func TestModelCatalog(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	h.createProvider("gamma", nil)

	added := h.ok(http.MethodPost, "/llm/providers/gamma/models", map[string]any{
		"origin": "remote",
		"models": []any{
			map[string]any{"id": "gpt-4o-mini"},
			map[string]any{"id": "text-embedding-3-small", "name": " Embed ", "ownedBy": "synthetic"},
			map[string]any{"id": "plain-chat", "tags": "vision,reasoning", "enabled": "no"},
		},
	}, http.StatusCreated)
	first := byID(t, added.JSON["models"], "gpt-4o-mini")
	equal(t, "inferred tags", stringsOf(t, first["tags"]), []string{"text", "image", "tools"})
	equal(t, "capabilities", stringsOf(t, first["capabilities"]), []string{"text", "image"})
	equal(t, "origin", first["origin"], "remote")
	equal(t, "enabled", first["enabled"], true)
	equal(t, "providerId", first["providerId"], "gamma")
	if value, present := first["name"]; !present || value != nil {
		t.Fatalf("an unnamed model needs name:null, got %#v", value)
	}
	embedding := byID(t, added.JSON["models"], "text-embedding-3-small")
	equal(t, "embedding tags", stringsOf(t, embedding["tags"]), []string{"embedding"})
	equal(t, "embedding capabilities", stringsOf(t, embedding["capabilities"]), []string{})
	equal(t, "embedding name", embedding["name"], "Embed")
	equal(t, "embedding owner", embedding["ownedBy"], "synthetic")
	plain := byID(t, added.JSON["models"], "plain-chat")
	equal(t, "explicit tags", stringsOf(t, plain["tags"]), []string{"image", "reasoning"})
	equal(t, "explicit enabled", plain["enabled"], false)
	// Adding models does not change the provider row.
	provider := object(t, added.JSON["provider"])
	equal(t, "provider capabilities", stringsOf(t, provider["capabilities"]), []string{"text", "image"})

	// Re-adding keeps operator tags and the original origin.
	readded := h.ok(http.MethodPost, "/llm/providers/gamma/models", map[string]any{"models": []any{map[string]any{"id": "plain-chat"}}}, http.StatusCreated)
	plain = byID(t, readded.JSON["models"], "plain-chat")
	equal(t, "kept tags", stringsOf(t, plain["tags"]), []string{"image", "reasoning"})
	equal(t, "kept origin", plain["origin"], "remote")
	equal(t, "kept enabled", plain["enabled"], false)

	updated := h.ok(http.MethodPut, "/llm/providers/gamma/models", map[string]any{"id": "gpt-4o-mini", "tags": []string{"tools", "text"}, "enabled": false, "name": ""}, http.StatusOK)
	model := object(t, updated.JSON["model"])
	equal(t, "updated tags", stringsOf(t, model["tags"]), []string{"text", "tools"})
	equal(t, "updated enabled", model["enabled"], false)
	equal(t, "updated origin", model["origin"], "remote")
	provider = object(t, updated.JSON["provider"])
	equal(t, "provider capabilities kept", stringsOf(t, provider["capabilities"]), []string{"text", "image"})
	equal(t, "models listed", len(list(t, updated.JSON["models"])), 3)

	notFound := h.call(http.MethodPut, "/llm/providers/gamma/models", map[string]any{"id": "absent"})
	equal(t, "model 404", notFound.Status, http.StatusNotFound)
	equal(t, "model 404 code", notFound.JSON["error"], "llm_model_not_found")
	noProvider := h.call(http.MethodPost, "/llm/providers/ghost/models", map[string]any{"models": []any{map[string]any{"id": "x"}}})
	equal(t, "provider 404", noProvider.Status, http.StatusNotFound)
	equal(t, "provider 404 code", noProvider.JSON["error"], "llm_provider_not_found")

	h.invalid(http.MethodPut, "/llm/providers/gamma/models", map[string]any{"id": "gpt-4o-mini", "tags": []string{"bogus"}}, "tags contains an unsupported value")
	h.invalid(http.MethodPut, "/llm/providers/gamma/models", map[string]any{"tags": []string{"text"}}, "model id is required")
	h.invalid(http.MethodPost, "/llm/providers/gamma/models", map[string]any{"models": []any{}}, "models must be a non-empty array")
	h.invalid(http.MethodPost, "/llm/providers/gamma/models", map[string]any{"models": []any{"gpt-4o"}}, "model id is required")
	h.invalid(http.MethodPost, "/llm/providers/gamma/models", map[string]any{"models": []any{map[string]any{"id": "bad id"}}}, "model id contains unsupported characters")

	all := h.ok(http.MethodGet, "/llm", nil, http.StatusOK)
	equal(t, "all models", len(list(t, all.JSON["models"])), 3)

	// Removing models also drops their queue targets and compacts priorities.
	h.ok(http.MethodPut, "/llm/queue", map[string]any{"targets": []any{
		map[string]any{"providerId": "gamma", "modelId": "text-embedding-3-small"},
		map[string]any{"providerId": "gamma", "modelId": "gpt-4o-mini"},
		map[string]any{"providerId": "gamma", "modelId": "plain-chat"},
	}}, http.StatusOK)
	removed := h.ok(http.MethodPost, "/llm/providers/gamma/models/remove", map[string]any{"ids": []string{"text-embedding-3-small", "gpt-4o-mini", "gpt-4o-mini", "absent"}}, http.StatusOK)
	equal(t, "removed", removed.JSON["removed"], float64(2))
	equal(t, "remaining", len(list(t, removed.JSON["models"])), 1)
	equal(t, "compacted priorities", h.scalar(`SELECT group_concat(model_id || ':' || priority, ',') FROM
		(SELECT model_id, priority FROM llm_route_targets WHERE route = 'schedule' ORDER BY priority)`), "plain-chat:0")
	h.invalid(http.MethodPost, "/llm/providers/gamma/models/remove", map[string]any{"ids": []string{}}, "ids must be a non-empty array")
	h.invalid(http.MethodPost, "/llm/providers/gamma/models/remove", map[string]any{"ids": []any{nil}}, "model id is required")
	h.assertNoSecret(added, updated, removed, all)
}

func TestQueue(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	empty := h.ok(http.MethodGet, "/llm", nil, http.StatusOK)
	for _, fragment := range []string{`"providers":[]`, `"models":[]`, `"queue":[]`} {
		if !strings.Contains(empty.Raw, fragment) {
			t.Fatalf("expected %s in %s", fragment, empty.Raw)
		}
	}
	equal(t, "modelTags", stringsOf(t, empty.JSON["modelTags"]), llmModelTags)

	h.createProvider("alpha", nil)
	h.addModels("alpha", "alpha-chat")
	h.createProvider("beta", nil)
	h.addModels("beta", "beta-vision", "beta-fast")

	saved := h.ok(http.MethodPut, "/llm/queue", map[string]any{"targets": []any{
		map[string]any{"providerId": "beta", "modelId": "beta-fast"},
		map[string]any{"providerId": "alpha", "modelId": "alpha-chat"},
		map[string]any{"providerId": "beta", "modelId": "beta-fast"},
	}}, http.StatusOK)
	want := []any{
		map[string]any{"providerId": "beta", "modelId": "beta-fast"},
		map[string]any{"providerId": "alpha", "modelId": "alpha-chat"},
	}
	equal(t, "saved queue", saved.JSON["queue"], want)
	h.assertNoSecret(saved)
	equal(t, "catalog queue", h.ok(http.MethodGet, "/llm", nil, http.StatusOK).JSON["queue"], want)

	h.invalid(http.MethodPut, "/llm/queue", map[string]any{}, "targets must be an array")
	h.invalid(http.MethodPut, "/llm/queue", map[string]any{"targets": []any{map[string]any{"modelId": "x"}}}, "providerId is required")
	h.invalid(http.MethodPut, "/llm/queue", map[string]any{"targets": []any{map[string]any{"providerId": "alpha"}}}, "modelId is required")
	h.invalid(http.MethodPut, "/llm/queue", map[string]any{"targets": []any{map[string]any{"providerId": "alpha", "modelId": ""}}}, "modelId is required")
	h.invalid(http.MethodPut, "/llm/queue", map[string]any{"targets": []any{map[string]any{"providerId": "ghost", "modelId": "x"}}}, "LLM provider does not exist: ghost")
	h.invalid(http.MethodPut, "/llm/queue", map[string]any{"targets": []any{map[string]any{"providerId": "beta", "modelId": "absent"}}}, "LLM model does not exist: beta/absent")
	h.invalid(http.MethodPut, "/llm/queue", map[string]any{"targets": []any{map[string]any{"providerId": "beta", "modelId": "bad id"}}}, "modelId contains unsupported characters")
	equal(t, "queue kept after rejection", h.ok(http.MethodGet, "/llm", nil, http.StatusOK).JSON["queue"], want)

	cleared := h.ok(http.MethodPut, "/llm/queue", map[string]any{"targets": []any{}}, http.StatusOK)
	equal(t, "cleared queue", len(list(t, cleared.JSON["queue"])), 0)
}

func TestExtraction(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	initial := h.ok(http.MethodGet, "/extraction", nil, http.StatusOK)
	for _, key := range []string{"enabled", "keywordEnabled", "visionEnabled", "messageEnabled"} {
		equal(t, key, initial.JSON[key], true)
	}
	equal(t, "keywords", stringsOf(t, initial.JSON["keywords"]), defaultScheduleKeywords)
	equal(t, "all posts off by default", initial.JSON["messageAllPosts"], false)

	saved := h.ok(http.MethodPut, "/extraction", map[string]any{
		"keywordEnabled": false, "visionEnabled": "off",
		"messageEnabled": "on", "messageAllPosts": true, "keywords": " 予定, live，予定\nboard ",
	}, http.StatusOK)
	equal(t, "all posts", saved.JSON["messageAllPosts"], true)
	equal(t, "stored all posts", h.scalar(`SELECT value FROM app_settings WHERE key = 'schedule_message_all_posts'`), "1")
	equal(t, "keyword stage", saved.JSON["keywordEnabled"], false)
	equal(t, "vision stage", saved.JSON["visionEnabled"], false)
	equal(t, "message stage", saved.JSON["messageEnabled"], true)
	equal(t, "effective", saved.JSON["enabled"], true)
	equal(t, "keywords", stringsOf(t, saved.JSON["keywords"]), []string{"予定", "live", "board"})
	equal(t, "stored keywords", h.scalar(`SELECT value FROM app_settings WHERE key = 'schedule_keywords'`), `["予定","live","board"]`)

	off := h.ok(http.MethodPut, "/extraction", map[string]any{"messageEnabled": 0, "keywords": []any{}}, http.StatusOK)
	equal(t, "all stages off", off.JSON["enabled"], false)
	equal(t, "cleared keywords", stringsOf(t, off.JSON["keywords"]), []string{})

	// A rejected request leaves no partial change behind.
	h.invalid(http.MethodPut, "/extraction", map[string]any{"keywordEnabled": true, "visionEnabled": "maybe"}, "visionEnabled must be a boolean")
	equal(t, "keyword stage kept", h.scalar(`SELECT value FROM app_settings WHERE key = 'schedule_keyword_enabled'`), "0")
	many := make([]string, 31)
	for i := range many {
		many[i] = "keyword-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	h.invalid(http.MethodPut, "/extraction", map[string]any{"keywords": many}, "keywords contains too many entries")
	h.invalid(http.MethodPut, "/extraction", map[string]any{"keywords": strings.Repeat("k", 81)}, "keywords contains an entry that is too long")
}

func TestFeaturedVideo(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	h.exec(`INSERT INTO videos (id, source, title, url) VALUES ('vid-1', 'youtube', 'Synthetic', 'https://video.example.test/watch/vid-1')`)
	revision := func() string {
		return h.scalar(`SELECT value FROM app_settings WHERE key = 'dashboard_revision'`)
	}
	before := revision()

	selected := h.ok(http.MethodPut, "/videos/featured", map[string]any{"videoId": " vid-1 "}, http.StatusOK)
	equal(t, "featuredVideoId", selected.JSON["featuredVideoId"], "vid-1")
	afterSelect := revision()
	if afterSelect == before {
		t.Fatal("changing the featured video must advance the dashboard revision")
	}
	h.ok(http.MethodPut, "/videos/featured", map[string]any{"videoId": "vid-1"}, http.StatusOK)
	equal(t, "revision after no-op", revision(), afterSelect)

	h.invalid(http.MethodPut, "/videos/featured", map[string]any{"videoId": "absent"}, "featured video does not exist")
	h.invalid(http.MethodPut, "/videos/featured", map[string]any{"videoId": strings.Repeat("v", 65)}, "videoId is too long")
	h.invalid(http.MethodPut, "/videos/featured", map[string]any{}, "videoId is required")

	cleared := h.ok(http.MethodPut, "/videos/featured", map[string]any{"videoId": nil}, http.StatusOK)
	if value, present := cleared.JSON["featuredVideoId"]; !present || value != nil {
		t.Fatalf("featuredVideoId should be null, got %#v", value)
	}
	if revision() == afterSelect {
		t.Fatal("clearing the featured video must advance the dashboard revision")
	}
}

func TestExtractionEnvironmentDefaults(t *testing.T) {
	h := newHarness(t, harnessOptions{env: map[string]string{
		"SCHEDULE_EXTRACTION_ENABLED": "0",
		"SCHEDULE_MESSAGE_ENABLED":    "yes",
		"SCHEDULE_KEYWORDS":           `["alpha", "beta", "alpha"]`,
		"SCHEDULE_MESSAGE_ALL_POSTS":  "1",
	}})
	config := h.ok(http.MethodGet, "/extraction", nil, http.StatusOK)
	// The legacy switch does not turn the all-posts option off.
	equal(t, "all posts", config.JSON["messageAllPosts"], true)
	equal(t, "keyword stage", config.JSON["keywordEnabled"], false)
	equal(t, "vision stage", config.JSON["visionEnabled"], false)
	equal(t, "message stage", config.JSON["messageEnabled"], true)
	equal(t, "keywords", stringsOf(t, config.JSON["keywords"]), []string{"alpha", "beta"})
	// A stored setting wins over the environment.
	stored := h.ok(http.MethodPut, "/extraction", map[string]any{"keywordEnabled": true, "messageEnabled": false}, http.StatusOK)
	equal(t, "stored keyword stage", stored.JSON["keywordEnabled"], true)
	equal(t, "stored message stage", stored.JSON["messageEnabled"], false)
}

func TestReadOnlyStoreRejectsMutations(t *testing.T) {
	h := newHarness(t, harnessOptions{readOnly: true})
	h.ok(http.MethodGet, "/extraction", nil, http.StatusOK)
	h.ok(http.MethodGet, "/llm", nil, http.StatusOK)
	mutations := []struct{ method, path string }{
		{http.MethodPut, "/extraction"}, {http.MethodPut, "/videos/featured"}, {http.MethodPost, "/llm/providers"},
		{http.MethodPut, "/llm/providers/alpha"}, {http.MethodDelete, "/llm/providers/alpha"},
		{http.MethodPost, "/llm/providers/alpha/test"}, {http.MethodPost, "/llm/providers/alpha/discover"},
		{http.MethodPost, "/llm/providers/alpha/models"}, {http.MethodPut, "/llm/providers/alpha/models"},
		{http.MethodPost, "/llm/providers/alpha/models/remove"}, {http.MethodPut, "/llm/queue"},
	}
	for _, mutation := range mutations {
		result := h.call(mutation.method, mutation.path, map[string]any{})
		if result.Status != http.StatusServiceUnavailable || result.JSON["error"] != "read_only" {
			t.Fatalf("%s %s = %d %s", mutation.method, mutation.path, result.Status, result.Raw)
		}
	}
	if len(h.transport.recorded()) != 0 {
		t.Fatal("a read-only store must not trigger provider requests")
	}
}

func TestRouting(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	for _, path := range []string{"/", "/config", "/providers", "/llm/providers/a/b", "/llm/providers/a/models/other", "/llm/routes/schedule_board", "/llm/queue/x", "/extraction/x"} {
		missing := h.call(http.MethodGet, path, nil)
		equal(t, path+" status", missing.Status, http.StatusNotFound)
		equal(t, path+" code", missing.JSON["error"], "not_found")
	}
	wrong := h.call(http.MethodPatch, "/extraction", nil)
	equal(t, "405", wrong.Status, http.StatusMethodNotAllowed)
	equal(t, "405 code", wrong.JSON["error"], "method_not_allowed")
	equal(t, "Allow", wrong.Header.Get("Allow"), "GET, HEAD, PUT")
	equal(t, "405 discover", h.call(http.MethodGet, "/llm/providers/a/discover", nil).Status, http.StatusMethodNotAllowed)
	h.ok(http.MethodHead, "/llm", nil, http.StatusOK)

	// Bodies above the 32 KiB admin limit are refused before parsing.
	large, _ := json.Marshal(map[string]any{"keywords": strings.Repeat("k", 40<<10)})
	tooLarge := h.call(http.MethodPut, "/extraction", string(large))
	equal(t, "413", tooLarge.Status, http.StatusRequestEntityTooLarge)
}
