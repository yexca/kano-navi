package adminauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPasswordUsesTypeScriptTrimmedUTF16Length(t *testing.T) {
	for _, value := range []string{"12345678901", "中文密码长度不足", "日本語短い", "   12345678901   ", "\uFEFF12345678901\uFEFF"} {
		if _, err := NewConfig("production", value, ""); err != ErrPassword {
			t.Fatal("accepted short password")
		}
	}
	for _, value := range []string{"123456789012", strings.Repeat("中", 12), strings.Repeat("あ", 12), " 123456789012 ", strings.Repeat("😀", 6)} {
		if _, err := NewConfig("production", value, ""); err != nil {
			t.Fatal("rejected compatible length")
		}
	}
}

func TestStrictLoginBodyNeverCreatesSession(t *testing.T) {
	auth, _ := testAuth(t, "production", "synthetic-admin-password")
	for _, body := range []string{`{"password":7}`, `{"password":null}`, `{"password":true}`, `{"password":[]}`, `{"password":{}}`, `{"password":"synthetic-admin-password",}`, `{"password":"synthetic-admin-password"} {}`, `{"password":"synthetic-admin-password"} trailing`, `{"password":"wrong","password":"synthetic-admin-password"}`, `{"password":"synthetic-admin-password","extra":{"x":1,"x":2}}`, `[]`, `null`} {
		rec := httptest.NewRecorder()
		auth.Login(rec, httptest.NewRequest("POST", "/api/admin/login", strings.NewReader(body)))
		if rec.Code != 400 || rec.Body.String() != `{"error":"invalid_json"}` || rec.Header().Get("Set-Cookie") != "" || len(auth.sessions) != 0 {
			t.Fatal("invalid body created a session or leaked input")
		}
	}
	for _, body := range []string{`{"password":"synthetic-admin-password"}` + strings.Repeat(" ", 33<<10), `{"password":"` + strings.Repeat("x", 33<<10) + `"}`} {
		rec := httptest.NewRecorder()
		auth.Login(rec, httptest.NewRequest("POST", "/api/admin/login", strings.NewReader(body)))
		if rec.Code != 413 || rec.Body.String() != `{"error":"request_too_large"}` || len(auth.sessions) != 0 {
			t.Fatal("unbounded body accepted")
		}
	}
}

func TestReloginWithExistingCookieAndSessionPolicy(t *testing.T) {
	auth, now := testAuth(t, "production", "synthetic-admin-password")
	login := func(old string) *http.Cookie {
		r := httptest.NewRequest("POST", "https://example.invalid/api/admin/login", strings.NewReader(`{"password":"synthetic-admin-password"}`))
		if old != "" {
			r.AddCookie(&http.Cookie{Name: CookieName, Value: old})
		}
		rec := httptest.NewRecorder()
		auth.Login(rec, r)
		var payload map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &payload)
		if rec.Code != 200 || payload["authenticated"] != true {
			t.Fatal("login must return new session state")
		}
		c := rec.Result().Cookies()[0]
		if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/api/admin" || c.MaxAge != 28800 || c.Name == "kano_admin_session" {
			t.Fatal("cookie boundary")
		}
		return c
	}
	first := login("forged")
	second := login(first.Value)
	for _, cookie := range []*http.Cookie{first, second} {
		r := httptest.NewRequest("GET", "/api/admin/events", nil)
		r.AddCookie(cookie)
		// A new login keeps prior sessions until logout or expiry.
		rec := httptest.NewRecorder()
		auth.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })).ServeHTTP(rec, r)
		if rec.Code != 204 {
			t.Fatal("new or previous session lost")
		}
	}
	*now = now.Add(DefaultSessionTTL)
	third := login(second.Value)
	r := httptest.NewRequest("POST", "/api/admin/logout", nil)
	r.AddCookie(third)
	auth.Logout(httptest.NewRecorder(), r)
	if auth.authenticated(r) {
		t.Fatal("logout did not revoke")
	}
}

func TestRateLimitResetsAndSameOriginPasses(t *testing.T) {
	auth, now := testAuth(t, "production", "synthetic-admin-password")
	for i := 0; i < 6; i++ {
		rec := httptest.NewRecorder()
		auth.Login(rec, httptest.NewRequest("POST", "/api/admin/login", strings.NewReader(`{"password":"wrong"}`)))
		want := 401
		if i == 5 {
			want = 429
		}
		if rec.Code != want {
			t.Fatal("rate limit")
		}
	}
	*now = now.Add(5 * time.Minute)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "http://example.invalid/api/admin/login", strings.NewReader(`{"password":"synthetic-admin-password"}`))
	r.Header.Set("Origin", "http://example.invalid")
	auth.OriginGuard(http.HandlerFunc(auth.Login)).ServeHTTP(rec, r)
	if rec.Code != 200 {
		t.Fatal("reset or same origin rejected")
	}
}

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
	if !strings.Contains(cookie, CookieName+"=") || !strings.Contains(cookie, "HttpOnly") || !strings.Contains(cookie, "SameSite=Strict") || !strings.Contains(cookie, "Path=/api/admin") {
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
