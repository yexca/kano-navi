package websub

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

const testKey = "synthetic-llm-secrets-key"

func openStore(t *testing.T) *sqlite.Store {
	t.Helper()
	store, err := sqlite.OpenStandalone(filepath.Join(t.TempDir(), "database", "kano.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func newAdmin(t *testing.T, store *sqlite.Store, available bool) (*Admin, *Service) {
	t.Helper()
	service, _ := newDisabledService(t, func(*http.Request) (*http.Response, error) { return hubResponse(http.StatusAccepted), nil }, nil)
	admin := NewAdmin(store, service, testKey, available)
	if err := admin.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	return admin, service
}

func call(t *testing.T, admin *Admin, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, "/api/admin"+path, reader)
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	if !admin.ServeAdmin(w, r, path) {
		t.Fatalf("%s %s was not routed", method, path)
	}
	payload := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &payload)
	return w, payload
}

func storedSettings(t *testing.T, store *sqlite.Store) map[string]string {
	t.Helper()
	rows, err := store.DB().Query(`SELECT key, value FROM app_settings WHERE key LIKE 'youtube_websub_%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			t.Fatal(err)
		}
		out[key] = value
	}
	return out
}

func TestAdminStartsDisabledWithoutSettings(t *testing.T) {
	admin, _ := newAdmin(t, openStore(t), true)
	w, payload := call(t, admin, http.MethodGet, "/youtube-push", "")
	if w.Code != http.StatusOK || payload["enabled"] != false || payload["available"] != true || payload["secretConfigured"] != false ||
		payload["callbackUrl"] != nil || payload["callbackPath"] != CallbackPath {
		t.Fatalf("code=%d payload=%v", w.Code, payload)
	}
	if admin.ServeAdmin(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/admin/workflows", nil), "/workflows") {
		t.Fatal("admin claimed another module's route")
	}
}

func TestAdminEnablesWithGeneratedSealedSecret(t *testing.T) {
	store := openStore(t)
	admin, service := newAdmin(t, store, true)
	w, payload := call(t, admin, http.MethodPut, "/youtube-push", `{"enabled":true,"callbackUrl":"  `+testCallback+`  "}`)
	if w.Code != http.StatusOK || payload["enabled"] != true || payload["callbackUrl"] != testCallback || payload["secretConfigured"] != true {
		t.Fatalf("code=%d payload=%v", w.Code, payload)
	}
	_, _, secret := service.settings()
	if len(secret) != 64 {
		t.Fatalf("generated secret length=%d", len(secret))
	}
	if strings.Contains(w.Body.String(), secret) {
		t.Fatal("response leaked the secret")
	}
	stored := storedSettings(t, store)
	if stored[settingEnabled] != "1" || stored[settingCallback] != testCallback || stored[settingSealedSigning] == "" || strings.Contains(stored[settingSealedSigning], secret) {
		t.Fatalf("stored=%v", stored)
	}

	// A restart restores the same configuration and secret.
	restored, restoredService := newAdmin(t, store, true)
	_, _, restoredSecret := restoredService.settings()
	if restoredSecret != secret || restored.Status()["enabled"] != true {
		t.Fatalf("restored enabled=%v", restored.Status()["enabled"])
	}

	// Disabling keeps the callback and secret for later.
	if w, payload := call(t, admin, http.MethodPut, "/youtube-push", `{"enabled":false}`); w.Code != http.StatusOK || payload["enabled"] != false || payload["callbackUrl"] != testCallback {
		t.Fatalf("disable code=%d payload=%v", w.Code, payload)
	}
	if _, _, kept := service.settings(); kept != secret {
		t.Fatal("disabling replaced the secret")
	}
}

func TestAdminRotatesSecret(t *testing.T) {
	store := openStore(t)
	admin, service := newAdmin(t, store, true)
	call(t, admin, http.MethodPut, "/youtube-push", `{"enabled":true,"callbackUrl":"`+testCallback+`"}`)
	_, _, before := service.settings()
	sealedBefore := storedSettings(t, store)[settingSealedSigning]
	w, payload := call(t, admin, http.MethodPost, "/youtube-push/rotate-secret", "")
	_, _, after := service.settings()
	if w.Code != http.StatusOK || payload["enabled"] != true || after == before || len(after) != 64 {
		t.Fatalf("code=%d payload=%v", w.Code, payload)
	}
	if storedSettings(t, store)[settingSealedSigning] == sealedBefore {
		t.Fatal("rotation did not persist")
	}
	if w, _ := call(t, admin, http.MethodGet, "/youtube-push/rotate-secret", ""); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET rotate code=%d", w.Code)
	}
}

func TestAdminRejectsInvalidSettings(t *testing.T) {
	store := openStore(t)
	admin, _ := newAdmin(t, store, true)
	for name, body := range map[string]string{
		"http callback":     `{"enabled":true,"callbackUrl":"http://board.example.test/api/websub/youtube"}`,
		"missing callback":  `{"enabled":true}`,
		"non-boolean":       `{"enabled":"yes","callbackUrl":"` + testCallback + `"}`,
		"non-string":        `{"callbackUrl":42}`,
		"invalid json":      `{`,
		"fragment callback": `{"callbackUrl":"` + testCallback + `#x"}`,
	} {
		w, payload := call(t, admin, http.MethodPut, "/youtube-push", body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: code=%d payload=%v", name, w.Code, payload)
		}
	}
	if stored := storedSettings(t, store); len(stored) != 0 {
		t.Fatalf("rejected input was stored: %v", stored)
	}
	if w, _ := call(t, admin, http.MethodDelete, "/youtube-push", ""); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE code=%d", w.Code)
	}
}

func TestAdminCannotEnableWhenSourceSkipped(t *testing.T) {
	store := openStore(t)
	admin, _ := newAdmin(t, store, false)
	w, payload := call(t, admin, http.MethodPut, "/youtube-push", `{"enabled":true,"callbackUrl":"`+testCallback+`"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(payload["message"].(string), "SKIP_YOUTUBE") {
		t.Fatalf("code=%d payload=%v", w.Code, payload)
	}
	// Stored enabled settings stay inactive while the source is skipped.
	enabled, _ := newAdmin(t, store, true)
	call(t, enabled, http.MethodPut, "/youtube-push", `{"enabled":true,"callbackUrl":"`+testCallback+`"}`)
	skipped, _ := newAdmin(t, store, false)
	if status := skipped.Status(); status["enabled"] != false || status["available"] != false {
		t.Fatalf("status=%v", status)
	}
}

func TestAdminReplacesUndecryptableSecretOnLoad(t *testing.T) {
	store := openStore(t)
	admin, _ := newAdmin(t, store, true)
	call(t, admin, http.MethodPut, "/youtube-push", `{"enabled":true,"callbackUrl":"`+testCallback+`"}`)
	before := storedSettings(t, store)[settingSealedSigning]
	service, _ := newDisabledService(t, nil, nil)
	rekeyed := NewAdmin(store, service, "synthetic-other-secrets-key", true)
	if err := rekeyed.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if enabled, _, secret := service.settings(); !enabled || len(secret) != 64 {
		t.Fatalf("enabled=%v secret length=%d", enabled, len(secret))
	}
	if storedSettings(t, store)[settingSealedSigning] == before {
		t.Fatal("undecryptable secret was not replaced")
	}
}
