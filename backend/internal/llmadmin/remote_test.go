package llmadmin

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const upstreamDetail = "synthetic-upstream-detail"

func TestDiscoverModels(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	h.createProvider("alpha", nil)
	h.ok(http.MethodPost, "/llm/providers/alpha/models", map[string]any{"models": []any{map[string]any{"id": "zeta-vision", "tags": []string{"text"}}}}, http.StatusCreated)
	h.transport.respond = func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"data":[
			{"id":"zeta-vision","owned_by":" synthetic "},
			{"id":"Beta-chat","display_name":" Beta "},
			{"id":"alpha-embed-1","name":"Alpha Embed"},
			{"id":"bad id"}, {"id":"Beta-chat"}, {"name":"models/gamma-1"}, "delta-1", 7, null
		]}`), nil
	}

	result := h.ok(http.MethodPost, "/llm/providers/alpha/discover", nil, http.StatusOK)
	models := list(t, result.JSON["models"])
	ids := []string{}
	for _, model := range models {
		ids = append(ids, object(t, model)["id"].(string))
	}
	equal(t, "sorted ids", ids, []string{"alpha-embed-1", "Beta-chat", "delta-1", "gamma-1", "zeta-vision"})
	added := byID(t, result.JSON["models"], "zeta-vision")
	equal(t, "added", added["added"], true)
	equal(t, "added tags", stringsOf(t, added["tags"]), []string{"text"})
	equal(t, "ownedBy", added["ownedBy"], "synthetic")
	equal(t, "suggested", stringsOf(t, added["suggestedTags"]), []string{"text", "image"})
	fresh := byID(t, result.JSON["models"], "Beta-chat")
	equal(t, "fresh added", fresh["added"], false)
	equal(t, "fresh name", fresh["name"], "Beta")
	if value, present := fresh["tags"]; !present || value != nil {
		t.Fatalf("a model outside the catalog needs tags:null, got %#v", value)
	}
	if value, present := fresh["ownedBy"]; !present || value != nil {
		t.Fatalf("ownedBy should be null, got %#v", value)
	}
	equal(t, "embedding suggestion", stringsOf(t, byID(t, result.JSON["models"], "alpha-embed-1")["suggestedTags"]), []string{"embedding"})
	equal(t, "name needs id", byID(t, result.JSON["models"], "gamma-1")["name"], nil)
	provider := object(t, result.JSON["provider"])
	equal(t, "lastStatus", provider["lastStatus"], "success")
	equal(t, "lastError", provider["lastError"], nil)
	if _, ok := provider["lastCheckedAt"].(string); !ok {
		t.Fatal("a checked provider reports lastCheckedAt")
	}
	h.assertNoSecret(result)

	requests := h.transport.recorded()
	equal(t, "request count", len(requests), 1)
	equal(t, "method", requests[0].Method, http.MethodGet)
	equal(t, "url", requests[0].URL, testBaseURL+"/models")
	equal(t, "authorization", requests[0].Header.Get("Authorization"), "Bearer "+testAPIKey)
	equal(t, "accept", requests[0].Header.Get("Accept"), "application/json")
	equal(t, "no anthropic header", requests[0].Header.Get("X-Api-Key"), "")
}

func TestDiscoverModelsFollowsAnthropicCursors(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	h.createProvider("alpha", map[string]any{"protocol": "anthropic-messages"})
	h.transport.respond = func(request *http.Request) (*http.Response, error) {
		if request.URL.Query().Get("after_id") == "" {
			return jsonResponse(http.StatusOK, `{"data":[{"id":"model-b"}],"has_more":true,"last_id":"model-b"}`), nil
		}
		return jsonResponse(http.StatusOK, `{"data":[{"id":"model-a"},{"id":"model-b"}],"has_more":false}`), nil
	}
	result := h.ok(http.MethodPost, "/llm/providers/alpha/discover", nil, http.StatusOK)
	equal(t, "models", len(list(t, result.JSON["models"])), 2)
	requests := h.transport.recorded()
	equal(t, "pages", len(requests), 2)
	equal(t, "first page", requests[0].URL, testBaseURL+"/models?limit=100")
	equal(t, "second page", requests[1].URL, testBaseURL+"/models?after_id=model-b&limit=100")
	equal(t, "x-api-key", requests[0].Header.Get("X-Api-Key"), testAPIKey)
	equal(t, "version", requests[0].Header.Get("Anthropic-Version"), "2023-06-01")
	equal(t, "no bearer", requests[0].Header.Get("Authorization"), "")

	// A repeated cursor is a broken upstream, not an endless loop.
	h.transport.respond = func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"data":[],"has_more":true,"last_id":"model-b"}`), nil
	}
	h.invalid(http.MethodPost, "/llm/providers/alpha/discover", nil, messageListFailed)
	equal(t, "bounded pages", len(h.transport.recorded()), 4)
}

func TestProviderRequestFailuresAreSanitized(t *testing.T) {
	failures := map[string]func(*http.Request) (*http.Response, error){
		"error status": func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusUnauthorized, `{"error":"`+upstreamDetail+`"}`), nil
		},
		"redirect": func(request *http.Request) (*http.Response, error) {
			response := jsonResponse(http.StatusFound, `{}`)
			response.Header.Set("Location", "https://elsewhere.example.test/"+upstreamDetail)
			response.Request = request
			return response, nil
		},
		"not json": func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, upstreamDetail), nil
		},
		"declared too large": func(*http.Request) (*http.Response, error) {
			response := jsonResponse(http.StatusOK, `{}`)
			response.ContentLength = maxModelListBytes + 1
			return response, nil
		},
		"streamed too large": func(*http.Request) (*http.Response, error) {
			response := jsonResponse(http.StatusOK, `{"data":["`+strings.Repeat("a", maxModelListBytes)+`"]}`)
			response.ContentLength = -1
			return response, nil
		},
		"transport error": func(*http.Request) (*http.Response, error) {
			return nil, &transportFailure{}
		},
	}
	for name, respond := range failures {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, harnessOptions{})
			h.createProvider("alpha", nil)
			h.createProvider("beta", nil)
			h.addModels("beta", "beta-chat")
			h.transport.respond = respond

			listing := h.invalid(http.MethodPost, "/llm/providers/alpha/discover", nil, messageListFailed)
			viaTest := h.invalid(http.MethodPost, "/llm/providers/alpha/test", nil, messageListFailed)
			inference := h.invalid(http.MethodPost, "/llm/providers/beta/test", map[string]any{"model": "beta-chat"}, messageRequestFailed)
			providers := h.ok(http.MethodGet, "/llm", nil, http.StatusOK)
			alpha := byID(t, providers.JSON["providers"], "alpha")
			equal(t, "alpha status", alpha["lastStatus"], "failed")
			equal(t, "alpha error", alpha["lastError"], messageListFailed)
			beta := byID(t, providers.JSON["providers"], "beta")
			equal(t, "beta status", beta["lastStatus"], "failed")
			equal(t, "beta error", beta["lastError"], messageRequestFailed)
			for _, result := range []reply{listing, viaTest, inference, providers} {
				if strings.Contains(result.Raw, upstreamDetail) || strings.Contains(result.Raw, "elsewhere") {
					t.Fatalf("an upstream detail leaked: %s", result.Raw)
				}
			}
			h.assertNoSecret(listing, viaTest, inference, providers)
			// One request each: a refused redirect is never followed.
			equal(t, "requests", len(h.transport.recorded()), 3)
		})
	}
}

type transportFailure struct{}

func (*transportFailure) Error() string {
	return "dial failed for " + upstreamDetail + " using " + testAPIKey
}

func TestProviderTestWithoutModelListsModels(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	h.createProvider("alpha", nil)
	h.transport.respond = func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"models":[{"name":"models/one"},{"name":"models/two"}]}`), nil
	}
	for _, method := range []string{http.MethodPost} {
		result := h.ok(method, "/llm/providers/alpha/test", nil, http.StatusOK)
		equal(t, "ok", result.JSON["ok"], true)
		equal(t, "method", result.JSON["method"], "models")
		equal(t, "count", result.JSON["remoteModelCount"], float64(2))
		if _, present := result.JSON["model"]; present {
			t.Fatal("the models check does not report a model")
		}
		equal(t, "status", object(t, result.JSON["provider"])["lastStatus"], "success")
		h.assertNoSecret(result)
	}
}

func TestProviderTestInference(t *testing.T) {
	cases := []struct {
		protocol string
		url      string
		body     map[string]any
	}{
		{"openai-responses", testBaseURL + "/responses", map[string]any{"model": "beta-chat", "store": false, "input": "ping", "max_output_tokens": float64(1)}},
		{"openai-chat-completions", testBaseURL + "/chat/completions", map[string]any{"model": "beta-chat", "max_completion_tokens": float64(1),
			"messages": []any{map[string]any{"role": "user", "content": "ping"}}}},
		{"anthropic-messages", testBaseURL + "/messages", map[string]any{"model": "beta-chat", "max_tokens": float64(1),
			"messages": []any{map[string]any{"role": "user", "content": "ping"}}}},
	}
	for _, test := range cases {
		t.Run(test.protocol, func(t *testing.T) {
			h := newHarness(t, harnessOptions{})
			h.createProvider("beta", map[string]any{"protocol": test.protocol})
			h.addModels("beta", "beta-chat")
			h.transport.respond = func(*http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusOK, `{"id":"synthetic","output":"`+upstreamDetail+`"}`), nil
			}
			result := h.ok(http.MethodPost, "/llm/providers/beta/test", map[string]any{"model": "beta-chat"}, http.StatusOK)
			equal(t, "ok", result.JSON["ok"], true)
			equal(t, "method", result.JSON["method"], "inference")
			equal(t, "model", result.JSON["model"], "beta-chat")
			if _, present := result.JSON["remoteModelCount"]; present {
				t.Fatal("the inference check does not report a model count")
			}
			equal(t, "status", object(t, result.JSON["provider"])["lastStatus"], "success")
			if strings.Contains(result.Raw, upstreamDetail) {
				t.Fatal("the provider's answer must not be relayed")
			}
			h.assertNoSecret(result)

			requests := h.transport.recorded()
			equal(t, "request count", len(requests), 1)
			equal(t, "method", requests[0].Method, http.MethodPost)
			equal(t, "url", requests[0].URL, test.url)
			equal(t, "content type", requests[0].Header.Get("Content-Type"), "application/json")
			var sent map[string]any
			if err := json.Unmarshal([]byte(requests[0].Body), &sent); err != nil {
				t.Fatalf("request body: %v", err)
			}
			equal(t, "request body", sent, test.body)
			if test.protocol == "anthropic-messages" {
				equal(t, "x-api-key", requests[0].Header.Get("X-Api-Key"), testAPIKey)
			} else {
				equal(t, "authorization", requests[0].Header.Get("Authorization"), "Bearer "+testAPIKey)
			}
		})
	}
}

func TestProviderTestModelSelectionAndPreconditions(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	h.createProvider("beta", nil)
	h.addModels("beta", "beta-chat", "beta-fast")

	chosen := h.ok(http.MethodPost, "/llm/providers/beta/test", map[string]any{"model": "beta-fast"}, http.StatusOK)
	equal(t, "chosen model", chosen.JSON["model"], "beta-fast")
	if !strings.Contains(h.transport.recorded()[0].Body, `"model":"beta-fast"`) {
		t.Fatal("the requested catalog model is the one that is probed")
	}
	h.invalid(http.MethodPost, "/llm/providers/beta/test", map[string]any{"model": "absent"}, "model is not in the provider catalog")
	h.invalid(http.MethodPost, "/llm/providers/beta/test", map[string]any{"model": "bad id"}, "model contains unsupported characters")

	missing := h.call(http.MethodPost, "/llm/providers/ghost/test", nil)
	equal(t, "404", missing.Status, http.StatusNotFound)
	equal(t, "404 code", missing.JSON["error"], "llm_provider_not_found")
	missing = h.call(http.MethodPost, "/llm/providers/ghost/discover", nil)
	equal(t, "discover 404", missing.Status, http.StatusNotFound)

	// Without a usable credential no request leaves the process.
	before := len(h.transport.recorded())
	h.ok(http.MethodPut, "/llm/providers/beta", map[string]any{"apiKey": ""}, http.StatusOK)
	h.invalid(http.MethodPost, "/llm/providers/beta/test", nil, "provider API key is not configured")
	h.invalid(http.MethodPost, "/llm/providers/beta/discover", nil, "provider API key is not configured")
	h.exec(`UPDATE llm_providers SET api_key_ciphertext = ? WHERE id = ?`, "v1:not:a:credential", "beta")
	h.invalid(http.MethodPost, "/llm/providers/beta/test", nil, "provider API key is not configured")
	equal(t, "no outbound request", len(h.transport.recorded()), before)
}

func TestProviderTestTimeout(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	h.createProvider("alpha", map[string]any{"timeoutMs": 1000})
	h.createProvider("beta", map[string]any{"timeoutMs": 1000})
	h.addModels("beta", "beta-chat")
	h.transport.respond = func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	}
	h.invalid(http.MethodPost, "/llm/providers/beta/test", map[string]any{"model": "beta-chat"}, messageRequestTimeout)
	h.invalid(http.MethodPost, "/llm/providers/alpha/discover", nil, messageListTimeout)
	providers := h.ok(http.MethodGet, "/llm", nil, http.StatusOK)
	equal(t, "beta error", byID(t, providers.JSON["providers"], "beta")["lastError"], messageRequestTimeout)
	equal(t, "alpha error", byID(t, providers.JSON["providers"], "alpha")["lastError"], messageListTimeout)
}
