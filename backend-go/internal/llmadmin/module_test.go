package llmadmin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yexca/kano-navi/backend-go/internal/secretstore"
)

func TestProviderLifecycleNeverReturnsTheCredential(t *testing.T) {
	h := newHarness(t, harnessOptions{})

	created := h.ok(http.MethodPost, "/providers", map[string]any{
		"id": "alpha", "name": " Alpha ", "baseUrl": "https://Provider.Example.Test:443/v1/",
		"apiKey": "  " + testAPIKey + "  ", "model": "alpha-chat",
	}, http.StatusCreated)
	provider := object(t, created.JSON["provider"])
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

	listed := h.ok(http.MethodGet, "/providers", nil, http.StatusOK)
	equal(t, "route", listed.JSON["route"], RouteVision)
	equal(t, "providerOrder", stringsOf(t, listed.JSON["providerOrder"]), []string{"alpha"})
	for _, route := range scheduleRoutes {
		equal(t, route, stringsOf(t, object(t, listed.JSON["providerOrders"])[route]), []string{"alpha"})
		equal(t, route, stringsOf(t, object(t, listed.JSON["routes"])[route]), []string{"alpha"})
	}
	config := h.ok(http.MethodGet, "/config", nil, http.StatusOK)
	legacy := byID(t, config.JSON["models"], "alpha-chat")
	equal(t, "default model origin", legacy["origin"], "legacy")
	equal(t, "default model tags", stringsOf(t, legacy["tags"]), []string{"text", "image"})

	// An edit without apiKey keeps the credential and syncs the default model.
	edited := h.ok(http.MethodPut, "/providers/alpha", map[string]any{"name": "Alpha 2", "capabilities": []string{"text"}, "id": "ignored"}, http.StatusOK)
	provider = object(t, edited.JSON["provider"])
	equal(t, "id", provider["id"], "alpha")
	equal(t, "name", provider["name"], "Alpha 2")
	equal(t, "apiKeyConfigured", provider["apiKeyConfigured"], true)
	equal(t, "visionCapable", provider["visionCapable"], false)
	equal(t, "capabilities", stringsOf(t, provider["capabilities"]), []string{"text"})
	models := h.ok(http.MethodGet, "/models?providerId=alpha", nil, http.StatusOK)
	equal(t, "synced tags", stringsOf(t, byID(t, models.JSON["models"], "alpha-chat")["tags"]), []string{"text"})
	equal(t, "modelTags", stringsOf(t, models.JSON["modelTags"]), llmModelTags)

	h.assertNoSecret(created, listed, config, edited, models)

	cleared := h.ok(http.MethodPut, "/providers/alpha", map[string]any{"apiKey": nil}, http.StatusOK)
	equal(t, "apiKeyConfigured", object(t, cleared.JSON["provider"])["apiKeyConfigured"], false)
	if h.scalar(`SELECT api_key_ciphertext FROM llm_providers WHERE id = 'alpha'`) != "" {
		t.Fatal("clearing the credential must store NULL")
	}

	missing := h.call(http.MethodPut, "/providers/ghost", map[string]any{"name": "x"})
	equal(t, "missing status", missing.Status, http.StatusNotFound)
	equal(t, "missing code", missing.JSON["error"], "llm_provider_not_found")

	deleted := h.ok(http.MethodDelete, "/providers/alpha", nil, http.StatusNoContent)
	equal(t, "delete body", deleted.Raw, "")
	equal(t, "models left", h.scalar(`SELECT CAST(COUNT(*) AS TEXT) FROM llm_models`), "0")
	equal(t, "targets left", h.scalar(`SELECT CAST(COUNT(*) AS TEXT) FROM llm_route_targets`), "0")
	again := h.call(http.MethodDelete, "/providers/alpha", nil)
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
		{"model", base(map[string]any{"model": "bad model!"}), "model contains unsupported characters"},
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
			h.invalid(http.MethodPost, "/providers", test.body, test.message)
		})
	}
	h.t = t
	equal(t, "rows after rejected input", h.scalar(`SELECT CAST(COUNT(*) AS TEXT) FROM llm_providers`), "0")

	malformed := h.call(http.MethodPost, "/providers", `{"id":`)
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
	result := h.invalid(http.MethodPost, "/providers", map[string]any{"id": "alpha", "baseUrl": testBaseURL, "apiKey": testAPIKey},
		"LLM_SECRETS_KEY is required to store a secret")
	h.assertNoSecret(result)
	// Without a credential the provider can still be saved.
	provider := object(t, h.ok(http.MethodPost, "/providers", map[string]any{"id": "alpha", "baseUrl": testBaseURL}, http.StatusCreated).JSON["provider"])
	equal(t, "apiKeyConfigured", provider["apiKeyConfigured"], false)
}

func TestDevelopmentModeAllowsPlainHTTPButNotPrivateHosts(t *testing.T) {
	h := newHarness(t, harnessOptions{mode: "development"})
	provider := h.createProvider("alpha", map[string]any{"baseUrl": "http://provider.example.test:8080/api/"})
	equal(t, "baseUrl", provider["baseUrl"], "http://provider.example.test:8080/api")
	h.invalid(http.MethodPost, "/providers", map[string]any{"id": "beta", "baseUrl": composedURL("http", ipv4(192, 168, 1, 10)+":8080", "")},
		"baseUrl must not target a private or loopback address")
}

func TestProviderOrder(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	h.createProvider("alpha", map[string]any{"model": "alpha-chat"})
	h.createProvider("beta", map[string]any{"model": "beta-chat"})
	h.createProvider("gamma", nil) // no default model: not routed automatically

	initial := h.ok(http.MethodGet, "/providers?route=schedule_message", nil, http.StatusOK)
	equal(t, "initial", stringsOf(t, initial.JSON["providerOrder"]), []string{"alpha", "beta"})
	equal(t, "provider count", len(list(t, initial.JSON["providers"])), 3)

	saved := h.ok(http.MethodPut, "/providers/order", map[string]any{
		"route": RouteMessage, "providerOrder": []any{"beta", " alpha ", "beta", ""},
	}, http.StatusOK)
	equal(t, "route", saved.JSON["route"], RouteMessage)
	equal(t, "saved order", stringsOf(t, saved.JSON["providerOrder"]), []string{"beta", "alpha"})
	orders := object(t, saved.JSON["providerOrders"])
	equal(t, "message", stringsOf(t, orders[RouteMessage]), []string{"beta", "alpha"})
	equal(t, "vision untouched", stringsOf(t, orders[RouteVision]), []string{"alpha", "beta"})
	equal(t, "routes alias", saved.JSON["routes"], saved.JSON["providerOrders"])
	h.assertNoSecret(saved)

	// The legacy "ids" field and the default (vision) route.
	legacy := h.ok(http.MethodPut, "/providers/order", map[string]any{"ids": []string{"gamma"}}, http.StatusOK)
	equal(t, "default route", legacy.JSON["route"], RouteVision)
	equal(t, "vision order", stringsOf(t, legacy.JSON["providerOrder"]), []string{"gamma"})

	// An emptied route falls back to the vision order in the effective view.
	emptied := h.ok(http.MethodPut, "/providers/order", map[string]any{"route": RouteBoard, "providerOrder": []string{}}, http.StatusOK)
	equal(t, "emptied order", stringsOf(t, emptied.JSON["providerOrder"]), []string{})
	equal(t, "board fallback", stringsOf(t, object(t, emptied.JSON["providerOrders"])[RouteBoard]), []string{"gamma"})
	if !strings.Contains(emptied.Raw, `"providerOrder":[]`) {
		t.Fatalf("an empty order must be [] and never null: %s", emptied.Raw)
	}

	h.invalid(http.MethodPut, "/providers/order", map[string]any{"providerOrder": []string{"ghost"}}, "LLM provider does not exist: ghost")
	h.invalid(http.MethodPut, "/providers/order", map[string]any{"providerOrder": "alpha"}, "providerOrder must be an array")
	h.invalid(http.MethodPut, "/providers/order", map[string]any{"route": "bogus", "providerOrder": []string{}}, "route is invalid")
	h.invalid(http.MethodGet, "/providers?route=bogus", nil, "route is invalid")
	equal(t, "order kept after rejection", stringsOf(t, h.ok(http.MethodGet, "/providers", nil, http.StatusOK).JSON["providerOrder"]), []string{"gamma"})
}

func TestModelCatalog(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	h.createProvider("gamma", nil)

	added := h.ok(http.MethodPost, "/providers/gamma/models", map[string]any{
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
	// The first catalog model becomes the provider default.
	provider := object(t, added.JSON["provider"])
	equal(t, "default model", provider["model"], "gpt-4o-mini")
	equal(t, "provider capabilities", stringsOf(t, provider["capabilities"]), []string{"text", "image"})

	// Re-adding keeps operator tags and the original origin.
	readded := h.ok(http.MethodPost, "/providers/gamma/models", map[string]any{"models": []any{map[string]any{"id": "plain-chat"}}}, http.StatusCreated)
	plain = byID(t, readded.JSON["models"], "plain-chat")
	equal(t, "kept tags", stringsOf(t, plain["tags"]), []string{"image", "reasoning"})
	equal(t, "kept origin", plain["origin"], "remote")
	equal(t, "kept enabled", plain["enabled"], false)

	updated := h.ok(http.MethodPut, "/providers/gamma/models", map[string]any{"id": "gpt-4o-mini", "tags": []string{"tools", "text"}, "enabled": false, "name": ""}, http.StatusOK)
	model := object(t, updated.JSON["model"])
	equal(t, "updated tags", stringsOf(t, model["tags"]), []string{"text", "tools"})
	equal(t, "updated enabled", model["enabled"], false)
	equal(t, "updated origin", model["origin"], "remote")
	provider = object(t, updated.JSON["provider"])
	equal(t, "provider follows default model", stringsOf(t, provider["capabilities"]), []string{"text"})
	equal(t, "provider vision", provider["visionCapable"], false)
	equal(t, "models listed", len(list(t, updated.JSON["models"])), 3)

	notFound := h.call(http.MethodPut, "/providers/gamma/models", map[string]any{"id": "absent"})
	equal(t, "model 404", notFound.Status, http.StatusNotFound)
	equal(t, "model 404 code", notFound.JSON["error"], "llm_model_not_found")
	noProvider := h.call(http.MethodPost, "/providers/ghost/models", map[string]any{"models": []any{map[string]any{"id": "x"}}})
	equal(t, "provider 404", noProvider.Status, http.StatusNotFound)
	equal(t, "provider 404 code", noProvider.JSON["error"], "llm_provider_not_found")

	h.invalid(http.MethodPut, "/providers/gamma/models", map[string]any{"id": "gpt-4o-mini", "tags": []string{"bogus"}}, "tags contains an unsupported value")
	h.invalid(http.MethodPut, "/providers/gamma/models", map[string]any{"tags": []string{"text"}}, "model id is required")
	h.invalid(http.MethodPost, "/providers/gamma/models", map[string]any{"models": []any{}}, "models must be a non-empty array")
	h.invalid(http.MethodPost, "/providers/gamma/models", map[string]any{"models": []any{"gpt-4o"}}, "model id is required")
	h.invalid(http.MethodPost, "/providers/gamma/models", map[string]any{"models": []any{map[string]any{"id": "bad id"}}}, "model id contains unsupported characters")

	all := h.ok(http.MethodGet, "/models", nil, http.StatusOK)
	equal(t, "all models", len(list(t, all.JSON["models"])), 3)
	none := h.ok(http.MethodGet, "/models?providerId=ghost", nil, http.StatusOK)
	if !strings.Contains(none.Raw, `"models":[]`) {
		t.Fatalf("an empty catalog must be []: %s", none.Raw)
	}

	// Removing models also drops their route targets and compacts priorities.
	h.ok(http.MethodPut, "/routes/schedule_message", map[string]any{"targets": []any{
		map[string]any{"providerId": "gamma", "modelId": "text-embedding-3-small"},
		map[string]any{"providerId": "gamma", "modelId": ""},
		map[string]any{"providerId": "gamma", "modelId": "plain-chat"},
	}}, http.StatusOK)
	removed := h.ok(http.MethodPost, "/providers/gamma/models/remove", map[string]any{"ids": []string{"text-embedding-3-small", "gpt-4o-mini", "gpt-4o-mini", "absent"}}, http.StatusOK)
	equal(t, "removed", removed.JSON["removed"], float64(2))
	equal(t, "default cleared", object(t, removed.JSON["provider"])["model"], "")
	equal(t, "remaining", len(list(t, removed.JSON["models"])), 1)
	equal(t, "compacted priorities", h.scalar(`SELECT group_concat(model_id || ':' || priority, ',') FROM
		(SELECT model_id, priority FROM llm_route_targets WHERE route = 'schedule_message' ORDER BY priority)`), ":0,plain-chat:1")
	h.invalid(http.MethodPost, "/providers/gamma/models/remove", map[string]any{"ids": []string{}}, "ids must be a non-empty array")
	h.invalid(http.MethodPost, "/providers/gamma/models/remove", map[string]any{"ids": []any{nil}}, "model id is required")
	h.assertNoSecret(added, updated, removed, all)
}

func TestRoutes(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	empty := h.ok(http.MethodGet, "/routes", nil, http.StatusOK)
	for _, group := range []string{"routes", "configured"} {
		for _, route := range scheduleRoutes {
			equal(t, group+"."+route, len(list(t, object(t, empty.JSON[group])[route])), 0)
		}
	}
	// Object keys keep the order the console was built against.
	if !(strings.Index(empty.Raw, RouteMessage) < strings.Index(empty.Raw, RouteBoard) && strings.Index(empty.Raw, RouteBoard) < strings.Index(empty.Raw, RouteVision)) {
		t.Fatalf("route keys are out of order: %s", empty.Raw)
	}

	h.createProvider("alpha", map[string]any{"model": "alpha-chat"})
	h.createProvider("beta", nil)
	h.ok(http.MethodPost, "/providers/beta/models", map[string]any{"models": []any{map[string]any{"id": "beta-vision"}, map[string]any{"id": "beta-fast"}}}, http.StatusCreated)

	saved := h.ok(http.MethodPut, "/routes/schedule_board", map[string]any{"targets": []any{
		map[string]any{"providerId": "beta", "modelId": "beta-fast"},
		map[string]any{"providerId": "alpha"},
		map[string]any{"providerId": "beta", "modelId": "beta-fast"},
	}}, http.StatusOK)
	want := []any{
		map[string]any{"providerId": "beta", "modelId": "beta-fast"},
		map[string]any{"providerId": "alpha", "modelId": ""},
	}
	equal(t, "config routeTargets", object(t, saved.JSON["routeTargets"])[RouteBoard], want)
	equal(t, "config configuredRouteTargets", object(t, saved.JSON["configuredRouteTargets"])[RouteBoard], want)
	equal(t, "config providerOrders", stringsOf(t, object(t, saved.JSON["providerOrders"])[RouteBoard]), []string{"beta", "alpha"})
	if _, present := saved.JSON["updatedAt"].(string); !present {
		t.Fatalf("a saved route reports updatedAt: %s", saved.Raw)
	}
	h.assertNoSecret(saved)

	routes := h.ok(http.MethodGet, "/routes", nil, http.StatusOK)
	vision := []any{map[string]any{"providerId": "alpha", "modelId": ""}}
	equal(t, "board", object(t, routes.JSON["routes"])[RouteBoard], want)
	equal(t, "vision", object(t, routes.JSON["routes"])[RouteVision], vision)
	equal(t, "configured board", object(t, routes.JSON["configured"])[RouteBoard], want)

	// Clearing a route makes the effective view fall back to vision while the
	// configured view stays empty.
	h.ok(http.MethodPut, "/routes/schedule_message", map[string]any{"targets": []any{}}, http.StatusOK)
	routes = h.ok(http.MethodGet, "/routes", nil, http.StatusOK)
	equal(t, "message fallback", object(t, routes.JSON["routes"])[RouteMessage], vision)
	equal(t, "message configured", len(list(t, object(t, routes.JSON["configured"])[RouteMessage])), 0)

	h.invalid(http.MethodPut, "/routes/bogus", map[string]any{"targets": []any{}}, "route is invalid")
	h.invalid(http.MethodPut, "/routes/schedule_board", map[string]any{}, "targets must be an array")
	h.invalid(http.MethodPut, "/routes/schedule_board", map[string]any{"targets": []any{map[string]any{"modelId": "x"}}}, "providerId is required")
	h.invalid(http.MethodPut, "/routes/schedule_board", map[string]any{"targets": []any{map[string]any{"providerId": "ghost"}}}, "LLM provider does not exist: ghost")
	h.invalid(http.MethodPut, "/routes/schedule_board", map[string]any{"targets": []any{map[string]any{"providerId": "beta", "modelId": "absent"}}}, "LLM model does not exist: beta/absent")
	h.invalid(http.MethodPut, "/routes/schedule_board", map[string]any{"targets": []any{map[string]any{"providerId": "beta", "modelId": "bad id"}}}, "modelId contains unsupported characters")
	equal(t, "board kept after rejection", object(t, h.ok(http.MethodGet, "/routes", nil, http.StatusOK).JSON["configured"])[RouteBoard], want)
}

func TestConfig(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	initial := h.ok(http.MethodGet, "/config", nil, http.StatusOK)
	equal(t, "llmModel", initial.JSON["llmModel"], DefaultScheduleModel)
	equal(t, "openAiKeyConfigured", initial.JSON["openAiKeyConfigured"], false)
	for _, key := range []string{"scheduleExtractionEnabled", "scheduleKeywordEnabled", "scheduleVisionEnabled", "scheduleMessageEnabled"} {
		equal(t, key, initial.JSON[key], true)
	}
	equal(t, "keywords", stringsOf(t, initial.JSON["scheduleKeywords"]), defaultScheduleKeywords)
	equal(t, "modelTags", stringsOf(t, initial.JSON["modelTags"]), llmModelTags)
	if value, present := initial.JSON["featuredVideoId"]; !present || value != nil {
		t.Fatalf("featuredVideoId should be null, got %#v", value)
	}
	if _, present := initial.JSON["updatedAt"]; present {
		t.Fatal("a read must not report updatedAt")
	}
	for _, fragment := range []string{`"providers":[]`, `"models":[]`, `"providerOrder":[]`} {
		if !strings.Contains(initial.Raw, fragment) {
			t.Fatalf("expected %s in %s", fragment, initial.Raw)
		}
	}

	saved := h.ok(http.MethodPut, "/config", map[string]any{
		"llmModel": "synthetic-model:1", "scheduleExtractionEnabled": false,
		"scheduleMessageEnabled": "on", "scheduleKeywords": " 予定, live，予定\nboard ",
	}, http.StatusOK)
	equal(t, "llmModel", saved.JSON["llmModel"], "synthetic-model:1")
	equal(t, "keyword stage", saved.JSON["scheduleKeywordEnabled"], false)
	equal(t, "vision stage", saved.JSON["scheduleVisionEnabled"], false)
	equal(t, "message stage", saved.JSON["scheduleMessageEnabled"], true)
	equal(t, "effective", saved.JSON["scheduleExtractionEnabled"], true)
	equal(t, "keywords", stringsOf(t, saved.JSON["scheduleKeywords"]), []string{"予定", "live", "board"})
	equal(t, "stored keywords", h.scalar(`SELECT value FROM app_settings WHERE key = 'schedule_keywords'`), `["予定","live","board"]`)
	updatedAt, _ := saved.JSON["updatedAt"].(string)
	if !strings.HasSuffix(updatedAt, "Z") || len(updatedAt) != len("2026-01-02T03:04:05.000Z") {
		t.Fatalf("updatedAt is not an ISO instant: %q", updatedAt)
	}
	equal(t, "stored stamp", h.scalar(`SELECT updated_at FROM app_settings WHERE key = 'schedule_keywords'`), updatedAt)

	off := h.ok(http.MethodPut, "/config", map[string]any{"scheduleMessageEnabled": 0, "scheduleKeywords": []any{}}, http.StatusOK)
	equal(t, "all stages off", off.JSON["scheduleExtractionEnabled"], false)
	equal(t, "cleared keywords", stringsOf(t, off.JSON["scheduleKeywords"]), []string{})

	// A rejected request leaves no partial change behind.
	h.invalid(http.MethodPut, "/config", map[string]any{"llmModel": "second-model", "scheduleVisionEnabled": "maybe"}, "scheduleVisionEnabled must be a boolean")
	equal(t, "llm_model kept", h.scalar(`SELECT value FROM app_settings WHERE key = 'llm_model'`), "synthetic-model:1")
	h.invalid(http.MethodPut, "/config", map[string]any{"llmModel": "bad model"}, "llmModel contains unsupported characters")
	h.invalid(http.MethodPut, "/config", map[string]any{"llmModel": ""}, "llmModel is required")
	many := make([]string, 31)
	for i := range many {
		many[i] = "keyword-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	h.invalid(http.MethodPut, "/config", map[string]any{"scheduleKeywords": many}, "scheduleKeywords contains too many entries")
	h.invalid(http.MethodPut, "/config", map[string]any{"scheduleKeywords": strings.Repeat("k", 81)}, "scheduleKeywords contains an entry that is too long")
}

func TestConfigFeaturedVideo(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	h.exec(`INSERT INTO videos (id, source, title, url) VALUES ('vid-1', 'youtube', 'Synthetic', 'https://video.example.test/watch/vid-1')`)
	revision := func() string {
		return h.scalar(`SELECT value FROM app_settings WHERE key = 'dashboard_revision'`)
	}
	before := revision()

	selected := h.ok(http.MethodPut, "/config", map[string]any{"featuredVideoId": " vid-1 "}, http.StatusOK)
	equal(t, "featuredVideoId", selected.JSON["featuredVideoId"], "vid-1")
	if _, present := selected.JSON["updatedAt"].(string); !present {
		t.Fatal("selecting a video reports updatedAt")
	}
	afterSelect := revision()
	if afterSelect == before {
		t.Fatal("changing the featured video must advance the dashboard revision")
	}
	h.ok(http.MethodPut, "/config", map[string]any{"featuredVideoId": "vid-1"}, http.StatusOK)
	equal(t, "revision after no-op", revision(), afterSelect)

	h.invalid(http.MethodPut, "/config", map[string]any{"featuredVideoId": "absent"}, "featured video does not exist")
	h.invalid(http.MethodPut, "/config", map[string]any{"featuredVideoId": strings.Repeat("v", 65)}, "featuredVideoId is too long")
	equal(t, "kept after rejection", h.ok(http.MethodGet, "/config", nil, http.StatusOK).JSON["featuredVideoId"], "vid-1")

	cleared := h.ok(http.MethodPut, "/config", map[string]any{"featuredVideoId": nil}, http.StatusOK)
	if value, present := cleared.JSON["featuredVideoId"]; !present || value != nil {
		t.Fatalf("featuredVideoId should be null, got %#v", value)
	}
	if revision() == afterSelect {
		t.Fatal("clearing the featured video must advance the dashboard revision")
	}

	// A stored choice whose video disappeared is reported as none, and the
	// legacy focus row applies only while no choice was ever stored.
	h.ok(http.MethodPut, "/config", map[string]any{"featuredVideoId": "vid-1"}, http.StatusOK)
	h.exec(`DELETE FROM videos WHERE id = 'vid-1'`)
	equal(t, "dangling choice", h.ok(http.MethodGet, "/config", nil, http.StatusOK).JSON["featuredVideoId"], nil)
	h.exec(`DELETE FROM app_settings WHERE key = 'featured_video_id'`)
	h.exec(`INSERT INTO videos (id, source, title, url) VALUES ('vid-2', 'youtube', 'Synthetic', 'https://video.example.test/watch/vid-2')`)
	h.exec(`DELETE FROM focus`)
	h.exec(`INSERT INTO focus (id, video_id, title) VALUES (1, 'vid-2', 'Synthetic')`)
	equal(t, "legacy focus", h.ok(http.MethodGet, "/config", nil, http.StatusOK).JSON["featuredVideoId"], "vid-2")
}

func TestConfigEnvironmentDefaults(t *testing.T) {
	h := newHarness(t, harnessOptions{env: map[string]string{
		"SCHEDULE_EXTRACTION_ENABLED": "0",
		"SCHEDULE_MESSAGE_ENABLED":    "yes",
		"SCHEDULE_KEYWORDS":           `["alpha", "beta", "alpha"]`,
	}})
	config := h.ok(http.MethodGet, "/config", nil, http.StatusOK)
	equal(t, "keyword stage", config.JSON["scheduleKeywordEnabled"], false)
	equal(t, "vision stage", config.JSON["scheduleVisionEnabled"], false)
	equal(t, "message stage", config.JSON["scheduleMessageEnabled"], true)
	equal(t, "keywords", stringsOf(t, config.JSON["scheduleKeywords"]), []string{"alpha", "beta"})
	// A stored setting wins over the environment.
	stored := h.ok(http.MethodPut, "/config", map[string]any{"scheduleKeywordEnabled": true, "scheduleMessageEnabled": false}, http.StatusOK)
	equal(t, "stored keyword stage", stored.JSON["scheduleKeywordEnabled"], true)
	equal(t, "stored message stage", stored.JSON["scheduleMessageEnabled"], false)
}

func TestReadOnlyStoreRejectsMutations(t *testing.T) {
	h := newHarness(t, harnessOptions{readOnly: true})
	h.ok(http.MethodGet, "/config", nil, http.StatusOK)
	h.ok(http.MethodGet, "/providers", nil, http.StatusOK)
	h.ok(http.MethodGet, "/models", nil, http.StatusOK)
	h.ok(http.MethodGet, "/routes", nil, http.StatusOK)
	mutations := []struct{ method, path string }{
		{http.MethodPut, "/config"}, {http.MethodPost, "/providers"}, {http.MethodPut, "/providers/order"},
		{http.MethodPut, "/providers/alpha"}, {http.MethodDelete, "/providers/alpha"},
		{http.MethodPost, "/providers/alpha/test"}, {http.MethodPut, "/providers/alpha/test"},
		{http.MethodPost, "/providers/alpha/models/discover"}, {http.MethodPost, "/providers/alpha/models"},
		{http.MethodPut, "/providers/alpha/models"}, {http.MethodPost, "/providers/alpha/models/remove"},
		{http.MethodPut, "/routes/schedule_vision"},
	}
	for _, mutation := range mutations {
		result := h.call(mutation.method, mutation.path, map[string]any{})
		if result.Status != http.StatusNotImplemented || result.JSON["error"] != "capability_not_migrated" {
			t.Fatalf("%s %s = %d %s", mutation.method, mutation.path, result.Status, result.Raw)
		}
	}
	if len(h.transport.recorded()) != 0 {
		t.Fatal("a read-only store must not trigger provider requests")
	}
}

func TestRouting(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	for _, path := range []string{"", "/", "/events", "/providers/a/b", "/providers/a/models/other", "/providers/a/models/discover/x", "/routes/a/b", "/config/x", "//config"} {
		recorder := httptest.NewRecorder()
		if h.module.ServeAdmin(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/x", nil), path) {
			t.Fatalf("%q should be left to another module", path)
		}
		if recorder.Body.Len() != 0 {
			t.Fatalf("%q wrote a response while declining", path)
		}
	}
	wrong := h.call(http.MethodPatch, "/config", nil)
	equal(t, "405", wrong.Status, http.StatusMethodNotAllowed)
	equal(t, "405 code", wrong.JSON["error"], "method_not_allowed")
	equal(t, "Allow", wrong.Header.Get("Allow"), "GET, PUT")
	equal(t, "405 discover", h.call(http.MethodGet, "/providers/a/models/discover", nil).Status, http.StatusMethodNotAllowed)
	// Trailing slash and HEAD are served like Express did.
	h.ok(http.MethodGet, "/config/", nil, http.StatusOK)
	h.ok(http.MethodHead, "/routes", nil, http.StatusOK)

	// Bodies above the 32 KiB admin limit are refused before parsing.
	large, _ := json.Marshal(map[string]any{"scheduleKeywords": strings.Repeat("k", 40<<10)})
	tooLarge := h.call(http.MethodPut, "/config", string(large))
	equal(t, "413", tooLarge.Status, http.StatusRequestEntityTooLarge)
}
