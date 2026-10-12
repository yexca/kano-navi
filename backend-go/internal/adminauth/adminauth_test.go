package adminauth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testAuth(t *testing.T, mode, password string) (*Auth, *time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	cfg, err := NewConfig(mode, password, "")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Now = func() time.Time { return now }
	auth, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return auth, &now
}

func TestProductionConfigAndDevelopmentOptIn(t *testing.T) {
	if _, err := ResolveConfig(map[string]string{}); err != ErrPassword {
		t.Fatalf("missing production password error = %v", err)
	}
	if _, err := NewConfig("production", "          ", ""); err != ErrPassword {
		t.Fatalf("whitespace password error = %v", err)
	}
	if cfg, err := NewConfig("development", "", ""); err != nil || cfg.Mode != "development" {
		t.Fatalf("development config: %+v %v", cfg, err)
	}
}

func TestLoginLogoutExpiryAndStableFailures(t *testing.T) {
	auth, now := testAuth(t, "production", "synthetic-admin-password")
	unauth := httptest.NewRequest(http.MethodGet, "/api/admin/session", nil)
	recorder := httptest.NewRecorder()
	auth.Session(recorder, unauth)
	if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), "password") {
		t.Fatalf("session response: %d %s", recorder.Code, recorder.Body)
	}

	bad := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{"password":"wrong"}`))
	bad.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	auth.Login(recorder, bad)
	if recorder.Code != http.StatusUnauthorized || recorder.Body.String() != `{"error":"invalid_credentials"}` {
		t.Fatalf("bad login: %d %q", recorder.Code, recorder.Body.String())
	}

	good := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{"password":"synthetic-admin-password"}`))
	good.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	auth.Login(recorder, good)
	if recorder.Code != http.StatusOK {
		t.Fatalf("good login: %d %s", recorder.Code, recorder.Body)
	}
	cookie := recorder.Header().Get("Set-Cookie")
	if !strings.Contains(cookie, "kano_admin_session=") || !strings.Contains(cookie, "HttpOnly") || !strings.Contains(cookie, "SameSite=Strict") || !strings.Contains(cookie, "Path=/api/admin") {
		t.Fatalf("cookie contract: %q", cookie)
	}

	session := httptest.NewRequest(http.MethodGet, "/api/admin/session", nil)
	session.Header.Set("Cookie", strings.Split(cookie, ";")[0])
	if !auth.authenticated(session) {
		t.Fatal("session should authenticate")
	}
	*now = now.Add(DefaultSessionTTL + time.Second)
	if auth.authenticated(session) {
		t.Fatal("expired session should fail")
	}
	logout := httptest.NewRequest(http.MethodPost, "/api/admin/logout", nil)
	logout.Header.Set("Cookie", strings.Split(cookie, ";")[0])
	recorder = httptest.NewRecorder()
	auth.Logout(recorder, logout)
	if recorder.Code != http.StatusNoContent || !strings.Contains(recorder.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Fatalf("logout: %d %q", recorder.Code, recorder.Header().Get("Set-Cookie"))
	}
}

func TestOriginGuardRejectsCrossSiteAndUntrustedOrigin(t *testing.T) {
	auth, _ := testAuth(t, "development", "")
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	request := httptest.NewRequest(http.MethodPost, "/api/admin/logout", nil)
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	recorder := httptest.NewRecorder()
	auth.OriginGuard(next).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("cross-site status = %d", recorder.Code)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/admin/logout", nil)
	request.Header.Set("Origin", "https://evil.example.invalid")
	recorder = httptest.NewRecorder()
	auth.OriginGuard(next).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("untrusted origin status = %d", recorder.Code)
	}
}
