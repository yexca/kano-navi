package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVisitorRegionUsesCountryHeadersWithoutExposingThem(t *testing.T) {
	handler := &Handler{}

	for _, test := range []struct {
		name   string
		header string
		value  string
		wantCN string
	}{
		{name: "cloudflare", header: "CF-IPCountry", value: "CN", wantCN: "true"},
		{name: "case insensitive", header: "X-Country-Code", value: "cn", wantCN: "true"},
		{name: "unknown", header: "X-Geo-Country", value: "US", wantCN: "false"},
		{name: "missing", wantCN: "false"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/visitor-region", nil)
			if test.header != "" {
				request.Header.Set(test.header, test.value)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			var response map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || recorder.Code != http.StatusOK || response["enabled"] != false || response["mainlandChina"] != false || response["mirrorUrl"] != "" {
				t.Fatalf("response status=%d body=%q", recorder.Code, recorder.Body.String())
			}
			payload := visitorRegionPayload(request, visitorRegionConfig{enabled: true, providerName: "Provider", providerURL: "https://provider.example.test", mirrorURL: "https://mirror.example.test"})
			if payload["mainlandChina"] != (test.wantCN == "true") {
				t.Fatalf("enabled payload=%v", payload)
			}
			if recorder.Header().Get("Cache-Control") != "no-store, no-transform" {
				t.Fatalf("cache-control=%q", recorder.Header().Get("Cache-Control"))
			}
			if recorder.Header().Get("Vary") == "" || recorder.Header().Get("Vary") == "Accept-Encoding" {
				t.Fatalf("vary=%q", recorder.Header().Get("Vary"))
			}
			if recorder.Header().Get("CF-IPCountry") != "" {
				t.Fatal("response reflected a request header")
			}
		})
	}
}

func TestVisitorRegionPayloadHonorsTheSetting(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/visitor-region", nil)
	request.Header.Set("CF-IPCountry", "CN")
	config := visitorRegionConfig{enabled: true, providerName: "Provider", providerURL: "https://provider.example.test", mirrorURL: "https://mirror.example.test"}
	if payload := visitorRegionPayload(request, config); payload["mainlandChina"] != true || payload["enabled"] != true || payload["mirrorUrl"] != config.mirrorURL {
		t.Fatalf("enabled CN payload=%v", payload)
	}
	config.enabled = false
	if payload := visitorRegionPayload(request, config); payload["mainlandChina"] != false || payload["enabled"] != false {
		t.Fatalf("disabled CN payload=%v", payload)
	}
}

func TestVisitorRegionMethod(t *testing.T) {
	handler := &Handler{}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/visitor-region", nil))
	if recorder.Code != http.StatusMethodNotAllowed || recorder.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("status=%d allow=%q", recorder.Code, recorder.Header().Get("Allow"))
	}
}
