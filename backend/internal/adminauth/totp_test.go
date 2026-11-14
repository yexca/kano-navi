package adminauth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// rfcKey is the SHA-1 key of RFC 6238 appendix B.
var rfcKey = []byte("12345678901234567890")

func TestTOTPMatchesRFC6238Vectors(t *testing.T) {
	// The appendix lists eight digits; six-digit codes are their last six.
	for unix, want := range map[int64]string{59: "287082", 1111111109: "081804", 1111111111: "050471", 1234567890: "005924", 2000000000: "279037", 20000000000: "353130"} {
		if got := totpCode(rfcKey, totpStep(time.Unix(unix, 0))); got != want {
			t.Fatalf("code at %d = %s, want %s", unix, got, want)
		}
	}
}

func TestTOTPSecretParsingAndGeneration(t *testing.T) {
	for _, value := range []string{"", "  ", "\t\n "} {
		if secret, err := ParseTOTPSecret(value); secret != nil || err != nil {
			t.Fatalf("blank secret %q must leave codes off", value)
		}
	}
	encoded := totpEncoding.EncodeToString(rfcKey)
	for _, formatted := range []string{strings.ToLower(encoded[:4] + " " + encoded[4:] + "===="), " " + encoded[:8] + "-" + encoded[8:16] + "-" + encoded[16:] + " "} {
		if secret, err := ParseTOTPSecret(formatted); err != nil || !bytes.Equal(secret, rfcKey) {
			t.Fatalf("authenticator formatting %q rejected", formatted)
		}
	}
	// A nonblank value is never read as absent, even when only separators
	// and padding remain.
	for _, value := range []string{"====", "----", " -=- ", "= = =", totpEncoding.EncodeToString(rfcKey[:15]), "not-base32!", "01234567890123456789012345"} {
		if _, err := ParseTOTPSecret(value); err != ErrTOTPSecret {
			t.Fatalf("malformed secret %q accepted", value)
		}
	}
	generated, err := GenerateTOTPSecret(bytes.NewReader(bytes.Repeat([]byte{7}, 20)))
	if err != nil {
		t.Fatal(err)
	}
	if secret, err := ParseTOTPSecret(generated); err != nil || len(secret) != 20 {
		t.Fatal("generated secret does not parse back to 160 bits")
	}
	if uri := TOTPURI(generated); uri != "otpauth://totp/Kano%20Navi:admin?algorithm=SHA1&digits=6&issuer=Kano%20Navi&period=30&secret="+generated {
		t.Fatalf("otpauth address = %s", uri)
	}
	if _, err := GenerateTOTPSecret(bytes.NewReader(nil)); err == nil {
		t.Fatal("short entropy accepted")
	}
}

func TestTOTPSecretConfiguration(t *testing.T) {
	for _, mode := range []string{"production", "development"} {
		for _, value := range []string{"short", "====", "----", " -=- "} {
			if _, err := ResolveConfig(map[string]string{"APP_MODE": mode, "ADMIN_PASSWORD": "synthetic-admin-password", "ADMIN_TOTP_SECRET": value}); err != ErrTOTPSecret {
				t.Fatalf("%s accepted the malformed secret %q: %v", mode, value, err)
			}
		}
	}
	cfg, err := ResolveConfig(map[string]string{"APP_MODE": "development", "ADMIN_TOTP_SECRET": totpEncoding.EncodeToString(rfcKey)})
	if err != nil {
		t.Fatal(err)
	}
	if auth := MustNew(cfg); auth.RequiresCode() {
		t.Fatal("development must not ask for a code")
	}
	cfg, err = ResolveConfig(map[string]string{"ADMIN_PASSWORD": "synthetic-admin-password"})
	if err != nil || MustNew(cfg).RequiresCode() {
		t.Fatal("codes must stay off without a secret")
	}
	short := rfcKey[:15]
	cfg.TOTPSecret = short
	if _, err := New(cfg); err != ErrTOTPSecret {
		t.Fatal("short secret accepted")
	}
}

func codeAuth(t *testing.T) (*Auth, *time.Time) {
	t.Helper()
	auth, now := testAuth(t, "production", "synthetic-admin-password")
	auth.cfg.TOTPSecret = rfcKey
	return auth, now
}

func codeLogin(auth *Auth, address, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/admin/session", strings.NewReader(body))
	r.RemoteAddr = address + ":4000"
	rec := httptest.NewRecorder()
	auth.Login(rec, r)
	return rec
}

func withCode(code string) string {
	return fmt.Sprintf(`{"password":"synthetic-admin-password","code":%q}`, code)
}

func TestLoginWithCodeNamesNeitherFactorAndAcceptsEachCodeOnce(t *testing.T) {
	auth, now := codeAuth(t)
	rec := httptest.NewRecorder()
	auth.Session(rec, httptest.NewRequest(http.MethodGet, "/api/admin/session", nil))
	var payload map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	if payload["requiresCode"] != true {
		t.Fatalf("session payload = %s", rec.Body)
	}
	current := totpCode(rfcKey, totpStep(*now))
	wrong := "000000"
	if wrong == current {
		wrong = "111111"
	}
	for _, body := range []string{`{"password":"synthetic-admin-password"}`, withCode(wrong), withCode("12345"), `{"password":"wrong","code":"` + current + `"}`} {
		if rec := codeLogin(auth, "192.0.2.1", body); rec.Code != 401 || rec.Body.String() != `{"error":"invalid_credentials"}` || len(auth.sessions) != 0 {
			t.Fatalf("login %s = %d %s", body, rec.Code, rec.Body)
		}
	}
	if rec := codeLogin(auth, "192.0.2.2", `{"password":"synthetic-admin-password","code":7}`); rec.Code != 400 {
		t.Fatal("a non-string code must be invalid input")
	}
	// The previous step's code still signs in, for clock skew.
	if rec := codeLogin(auth, "192.0.2.2", withCode(totpCode(rfcKey, totpStep(*now)-1))); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"requiresCode":true`) {
		t.Fatalf("skewed code = %d %s", rec.Code, rec.Body)
	}
	// The current code works once, with the space some apps show.
	if rec := codeLogin(auth, "192.0.2.3", withCode(current[:3]+" "+current[3:])); rec.Code != 200 {
		t.Fatalf("current code = %d", rec.Code)
	}
	for _, code := range []string{current, totpCode(rfcKey, totpStep(*now)-1)} {
		if rec := codeLogin(auth, "192.0.2.4", withCode(code)); rec.Code != 401 {
			t.Fatal("a spent or older code signed in again")
		}
	}
	*now = now.Add(totpPeriod * time.Second)
	if rec := codeLogin(auth, "192.0.2.4", withCode(totpCode(rfcKey, totpStep(*now)))); rec.Code != 200 {
		t.Fatal("the next step's code was refused")
	}
	*now = now.Add(2 * totpPeriod * time.Second)
	if rec := codeLogin(auth, "192.0.2.5", withCode(totpCode(rfcKey, totpStep(*now)-2))); rec.Code != 401 {
		t.Fatal("a code outside the skew window signed in")
	}
}

func TestWrongCodesAreLimitedAcrossAddresses(t *testing.T) {
	auth, now := codeAuth(t)
	wrong := "000000"
	if wrong == totpCode(rfcKey, totpStep(*now)) || wrong == totpCode(rfcKey, totpStep(*now)-1) || wrong == totpCode(rfcKey, totpStep(*now)+1) {
		wrong = "111111"
	}
	// A wrong password never counts toward the shared limit.
	for i := 0; i < codeFailureLimit; i++ {
		codeLogin(auth, fmt.Sprintf("198.51.100.%d", i), `{"password":"wrong","code":"000000"}`)
	}
	for i := 0; i < codeFailureLimit; i++ {
		if rec := codeLogin(auth, fmt.Sprintf("203.0.113.%d", i), withCode(wrong)); rec.Code != 401 {
			t.Fatalf("attempt %d = %d", i, rec.Code)
		}
	}
	// Past the limit the right password and a wrong one answer alike, so the
	// lockout confirms no password.
	right := withCode(totpCode(rfcKey, totpStep(*now)))
	for address, body := range map[string]string{"192.0.2.9": right, "192.0.2.10": `{"password":"wrong","code":"000000"}`} {
		if rec := codeLogin(auth, address, body); rec.Code != 401 || rec.Body.String() != `{"error":"invalid_credentials"}` {
			t.Fatalf("fresh address after the shared limit = %d %s", rec.Code, rec.Body)
		}
	}
	if auth.codeFailures.count != codeFailureLimit {
		t.Fatalf("a locked login spent the shared allowance: %d", auth.codeFailures.count)
	}
	*now = now.Add(5 * time.Minute)
	if rec := codeLogin(auth, "192.0.2.9", withCode(totpCode(rfcKey, totpStep(*now)))); rec.Code != 200 {
		t.Fatalf("login after the reset = %d", rec.Code)
	}
}

// burst runs every login at once, released together, and counts the
// answers by status.
func burst(auth *Auth, bodies map[string]string) map[int]int {
	var (
		start sync.WaitGroup
		done  sync.WaitGroup
		mu    sync.Mutex
	)
	release := make(chan struct{})
	counts := map[int]int{}
	for address, body := range bodies {
		start.Add(1)
		done.Add(1)
		go func() {
			defer done.Done()
			start.Done()
			<-release
			rec := codeLogin(auth, address, body)
			mu.Lock()
			counts[rec.Code]++
			mu.Unlock()
		}()
	}
	start.Wait()
	close(release)
	done.Wait()
	return counts
}

func TestConcurrentWrongCodesStopAtTheSharedLimit(t *testing.T) {
	auth, now := codeAuth(t)
	right := totpCode(rfcKey, totpStep(*now))
	wrong := "000000"
	for step := totpStep(*now) - totpSkew; step <= totpStep(*now)+totpSkew; step++ {
		if totpCode(rfcKey, step) == wrong {
			wrong = "111111"
		}
	}
	// Wrong passwords from many addresses at once never spend the shared
	// allowance.
	bodies := map[string]string{}
	for i := 0; i < 40; i++ {
		bodies[fmt.Sprintf("198.51.100.%d", i)] = `{"password":"wrong","code":"` + right + `"}`
	}
	if counts := burst(auth, bodies); counts[401] != 40 || auth.codeFailures.count != 0 {
		t.Fatalf("wrong passwords = %v, shared failures %d", counts, auth.codeFailures.count)
	}
	// Right-password logins at once, wrong codes and the one current code
	// mixed: exactly the limit's worth of failures is counted, at most one
	// signs in, and the rest fail.
	bodies = map[string]string{}
	for i := 0; i < 40; i++ {
		bodies[fmt.Sprintf("203.0.113.%d", i)] = withCode(wrong)
		bodies[fmt.Sprintf("192.0.2.%d", i)] = withCode(right)
	}
	counts := burst(auth, bodies)
	if counts[200] > 1 || counts[200]+counts[401] != 80 || len(auth.sessions) != counts[200] || auth.codeFailures.count != codeFailureLimit {
		t.Fatalf("mixed burst = %v, sessions %d, shared failures %d", counts, len(auth.sessions), auth.codeFailures.count)
	}
	// Once the limit is reached, logins with a valid, unspent code in flight
	// together all fail and none signs in.
	sessions := len(auth.sessions)
	bodies = map[string]string{}
	for i := 0; i < 20; i++ {
		bodies[fmt.Sprintf("192.0.2.%d", 100+i)] = withCode(totpCode(rfcKey, totpStep(*now)+1))
	}
	if counts := burst(auth, bodies); counts[401] != 20 || len(auth.sessions) != sessions {
		t.Fatalf("after the limit = %v, sessions %d", counts, len(auth.sessions))
	}
}

func TestConcurrentLoginsFromOneAddressStopAtItsLimit(t *testing.T) {
	auth, _ := codeAuth(t)
	var (
		done  sync.WaitGroup
		mu    sync.Mutex
		codes = map[int]int{}
	)
	release := make(chan struct{})
	for i := 0; i < 20; i++ {
		done.Add(1)
		go func() {
			defer done.Done()
			<-release
			rec := codeLogin(auth, "192.0.2.50", `{"password":"wrong","code":"000000"}`)
			mu.Lock()
			codes[rec.Code]++
			mu.Unlock()
		}()
	}
	close(release)
	done.Wait()
	if codes[401] != 5 || codes[429] != 15 {
		t.Fatalf("one address = %v", codes)
	}
}

func TestConcurrentLoginsSpendOneCodeOnce(t *testing.T) {
	auth, now := codeAuth(t)
	bodies := map[string]string{}
	for i := 0; i < 8; i++ {
		bodies[fmt.Sprintf("192.0.2.%d", i)] = withCode(totpCode(rfcKey, totpStep(*now)))
	}
	if counts := burst(auth, bodies); counts[200] != 1 || counts[401] != 7 || len(auth.sessions) != 1 {
		t.Fatalf("one code at once = %v, sessions %d", counts, len(auth.sessions))
	}
}
