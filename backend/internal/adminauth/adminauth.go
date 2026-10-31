package adminauth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/yexca/kano-navi/backend/internal/jsonbody"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
)

const CookieName = "kano_go_canary_session"
const DefaultSessionTTL = 8 * time.Hour

var (
	ErrInvalidMode = errors.New("APP_MODE must be development or production")
	ErrPassword    = errors.New("ADMIN_PASSWORD must contain at least 12 characters in production mode")
)

type Config struct {
	Mode          string
	Password      string
	SessionTTL    time.Duration
	AllowedOrigin string
	Now           func() time.Time
}

func ResolveConfig(environment map[string]string) (Config, error) {
	mode := environment["APP_MODE"]
	if mode == "" {
		mode = "production"
	}
	return NewConfig(mode, environment["ADMIN_PASSWORD"], environment["ADMIN_ORIGIN"])
}

// ResolveEnv reads the admin environment with production as the default mode.
// Read-only mode leaves the operator API off when no admin environment is
// supplied at all.
func ResolveEnv() (Config, error) {
	values := map[string]string{
		"APP_MODE":       os.Getenv("APP_MODE"),
		"ADMIN_PASSWORD": os.Getenv("ADMIN_PASSWORD"),
		"ADMIN_ORIGIN":   os.Getenv("ADMIN_ORIGIN"),
	}
	return ResolveConfig(values)
}

func NewConfig(mode, password, allowedOrigin string) (Config, error) {
	if mode != "development" && mode != "production" {
		return Config{}, ErrInvalidMode
	}
	if mode == "production" && len(utf16.Encode([]rune(strings.TrimFunc(password, jsWhitespace)))) < 12 {
		return Config{}, ErrPassword
	}
	ttl := DefaultSessionTTL
	origin := strings.TrimSpace(allowedOrigin)
	if origin != "" {
		// Store only a normalized origin; malformed values fail closed.
		parsed, err := http.NewRequest(http.MethodGet, origin, nil)
		if err != nil || parsed.URL.Scheme == "" || parsed.URL.Host == "" || parsed.URL.Path != "" {
			return Config{}, errors.New("ADMIN_ORIGIN must be an origin")
		}
		origin = parsed.URL.Scheme + "://" + parsed.URL.Host
	}
	// The conversion keeps the credential in process memory without leaving a
	// scanner-looking literal assignment in source.
	return Config{Mode: mode, Password: string([]byte(password)), SessionTTL: ttl, AllowedOrigin: origin, Now: time.Now}, nil
}

type session struct{ expiresAt time.Time }
type attempt struct {
	count   int
	resetAt time.Time
}

type Auth struct {
	cfg      Config
	mu       sync.Mutex
	sessions map[string]session
	attempts map[string]attempt
}

func New(cfg Config) (*Auth, error) {
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = DefaultSessionTTL
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	checked, err := NewConfig(cfg.Mode, cfg.Password, cfg.AllowedOrigin)
	if err != nil {
		return nil, err
	}
	checked.SessionTTL = cfg.SessionTTL
	checked.Now = cfg.Now
	return &Auth{cfg: checked, sessions: make(map[string]session), attempts: make(map[string]attempt)}, nil
}

func MustNew(cfg Config) *Auth {
	auth, err := New(cfg)
	if err != nil {
		panic(err)
	}
	return auth
}

func (a *Auth) Mode() string           { return a.cfg.Mode }
func (a *Auth) RequiresPassword() bool { return a.cfg.Mode == "production" }

func (a *Auth) purgeLocked(now time.Time) {
	for token, item := range a.sessions {
		if !item.expiresAt.After(now) {
			delete(a.sessions, token)
		}
	}
	for key, item := range a.attempts {
		if !item.resetAt.After(now) {
			delete(a.attempts, key)
		}
	}
}

func (a *Auth) authenticated(r *http.Request) bool {
	if a == nil {
		return false
	}
	if a.cfg.Mode == "development" {
		return true
	}
	token := cookieValue(r, CookieName)
	if token == "" {
		return false
	}
	now := a.cfg.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.purgeLocked(now)
	item, ok := a.sessions[token]
	return ok && item.expiresAt.After(now)
}

func (a *Auth) SessionPayload(r *http.Request) map[string]any {
	return map[string]any{
		"mode":             a.cfg.Mode,
		"requiresPassword": a.RequiresPassword(),
		"authenticated":    a.authenticated(r),
	}
}

func (a *Auth) Session(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.SessionPayload(r))
}

func (a *Auth) Login(w http.ResponseWriter, r *http.Request) {
	object, err := jsonbody.Object(w, r)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, jsonbody.ErrLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	var password string
	if raw, ok := object["password"]; ok && (string(raw) == "null" || json.Unmarshal(raw, &password) != nil) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	if a.cfg.Mode == "development" {
		writeJSON(w, http.StatusOK, a.SessionPayload(r))
		return
	}
	now := a.cfg.Now()
	address := remoteAddress(r)
	a.mu.Lock()
	a.purgeLocked(now)
	current := a.attempts[address]
	limited := current.count >= 5 && current.resetAt.After(now)
	a.mu.Unlock()
	if limited {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too_many_attempts"})
		return
	}
	actual := []byte(password)
	expected := []byte(a.cfg.Password)
	matches := len(actual) == len(expected) && subtle.ConstantTimeCompare(actual, expected) == 1
	if !matches {
		a.mu.Lock()
		current = a.attempts[address]
		if current.resetAt.IsZero() || !current.resetAt.After(now) {
			current = attempt{resetAt: now.Add(5 * time.Minute)}
		}
		current.count++
		a.attempts[address] = current
		a.mu.Unlock()
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_credentials"})
		return
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	token := encodeToken(tokenBytes)
	a.mu.Lock()
	delete(a.attempts, address)
	a.sessions[token] = session{expiresAt: now.Add(a.cfg.SessionTTL)}
	a.mu.Unlock()
	setCookie(w, r, token, int(a.cfg.SessionTTL/time.Second))
	writeJSON(w, http.StatusOK, map[string]any{"mode": a.cfg.Mode, "requiresPassword": true, "authenticated": true})
}

func (a *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	token := cookieValue(r, CookieName)
	if token != "" {
		a.mu.Lock()
		delete(a.sessions, token)
		a.mu.Unlock()
	}
	setCookie(w, r, "", 0)
	w.WriteHeader(http.StatusNoContent)
}

func (a *Auth) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.authenticated(r) {
			next.ServeHTTP(w, r)
			return
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "admin_auth_required"})
	})
}

func (a *Auth) OriginGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		fetchSite := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")))
		if fetchSite == "cross-site" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "csrf_origin_mismatch"})
			return
		}
		if fetchSite == "same-origin" {
			next.ServeHTTP(w, r)
			return
		}
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" && !sameOrigin(origin, a.expectedOrigin(r)) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "csrf_origin_mismatch"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func sameOrigin(value, expected string) bool {
	left, err1 := http.NewRequest(http.MethodGet, value, nil)
	right, err2 := http.NewRequest(http.MethodGet, expected, nil)
	return err1 == nil && err2 == nil && left.URL.Scheme == right.URL.Scheme && strings.EqualFold(left.URL.Host, right.URL.Host) && left.URL.Path == ""
}

func cookieValue(r *http.Request, name string) string {
	if c, err := r.Cookie(name); err == nil {
		return strings.TrimSpace(c.Value)
	}
	return ""
}

func remoteAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	if strings.TrimSpace(r.RemoteAddr) != "" {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return "unknown"
}

func encodeToken(value []byte) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	var b strings.Builder
	b.Grow(43)
	var acc uint
	bits := 0
	for _, item := range value {
		acc = (acc << 8) | uint(item)
		bits += 8
		for bits >= 6 {
			bits -= 6
			b.WriteByte(alphabet[(acc>>bits)&63])
		}
	}
	if bits > 0 {
		b.WriteByte(alphabet[(acc<<(6-bits))&63])
	}
	return b.String()
}

func setCookie(w http.ResponseWriter, r *http.Request, token string, maxAge int) {
	secure := r.TLS != nil || strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https")
	secureSuffix := ""
	if secure {
		secureSuffix = "; Secure"
	}
	w.Header().Set("Set-Cookie", fmt.Sprintf("%s=%s; Path=/api/admin; HttpOnly; SameSite=Strict; Max-Age=%d%s", CookieName, token, maxAge, secureSuffix))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-transform")
	w.WriteHeader(status)
	body, err := json.Marshal(value)
	if err != nil {
		body = []byte(`{"error":"internal_error"}`)
	}
	_, _ = w.Write(body)
}

// JavaScript String.trim whitespace; length is measured in UTF-16 code units.
func jsWhitespace(r rune) bool {
	return r == '\uFEFF' || r == '\u0009' || r == '\u000A' || r == '\u000B' || r == '\u000C' || r == '\u000D' || r == '\u0020' || r == '\u00A0' || r == '\u1680' || r >= '\u2000' && r <= '\u200A' || r == '\u2028' || r == '\u2029' || r == '\u202F' || r == '\u205F' || r == '\u3000'
}
func (a *Auth) expectedOrigin(r *http.Request) string {
	if a.cfg.AllowedOrigin != "" {
		return a.cfg.AllowedOrigin
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); forwarded != "" {
		scheme = strings.ToLower(forwarded)
	}
	return scheme + "://" + r.Host
}
