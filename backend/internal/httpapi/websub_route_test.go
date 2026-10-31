package httpapi

import (
	"net/http"
	"testing"
)

func TestYoutubeWebSubRouteIsMountedOnlyWhenConfigured(t *testing.T) {
	store, _ := openFixture(t)
	without := NewStandaloneHandler(store, nil, Standalone{})
	if got := request(t, without, http.MethodGet, "/api/websub/youtube"); got.Code != http.StatusNotFound || decodeObject(t, got)["error"] != "api_not_found" {
		t.Fatalf("unconfigured route: status=%d body=%q", got.Code, got.Body.String())
	}

	reached := 0
	with := NewStandaloneHandler(store, nil, Standalone{WebSub: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached++
		w.WriteHeader(http.StatusAccepted)
	})})
	if got := request(t, with, http.MethodPost, "/api/websub/youtube"); got.Code != http.StatusAccepted || reached != 1 {
		t.Fatalf("configured route: status=%d reached=%d", got.Code, reached)
	}
	if got := request(t, with, http.MethodGet, "/api/websub/youtube/extra"); got.Code != http.StatusNotFound || reached != 1 {
		t.Fatalf("nested path: status=%d reached=%d", got.Code, reached)
	}
}
