package websub

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const (
	testChannel   = "UCAAAAAAAAAAAAAAAAAAAAAA"
	otherChannel  = "UCBBBBBBBBBBBBBBBBBBBBBB"
	testSecret    = "synthetic-websub-secret"
	testCallback  = "https://board.example.test/api/websub/youtube"
	movedCallback = "https://moved.example.test/api/websub/youtube"
)

type fakeTransport func(*http.Request) (*http.Response, error)

func (f fakeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func hubResponse(status int) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("hub body"))}
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time          { return c.now }
func (c *clock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func signWith(factory func() hash.Hash, prefix, body string) string {
	m := hmac.New(factory, []byte(testSecret))
	m.Write([]byte(body))
	return prefix + "=" + hex.EncodeToString(m.Sum(nil))
}

// newDisabledService returns a service that has not been configured yet.
func newDisabledService(t *testing.T, hub fakeTransport, trigger TriggerFunc) (*Service, *clock) {
	t.Helper()
	c := &clock{now: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)}
	s := New(testChannel, &http.Client{Transport: hub}, trigger)
	s.now = c.Now
	s.log = io.Discard
	return s, c
}

func newTestService(t *testing.T, hub fakeTransport, trigger TriggerFunc) (*Service, *clock) {
	t.Helper()
	s, c := newDisabledService(t, hub, trigger)
	if err := s.Configure(true, testCallback, "synthetic-websub-secret"); err != nil {
		t.Fatal(err)
	}
	return s, c
}

func recordingHub(t *testing.T, requests *[]url.Values) fakeTransport {
	return func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != HubURL || r.Method != http.MethodPost {
			t.Fatalf("unexpected hub request %s %s", r.Method, r.URL)
		}
		body, _ := io.ReadAll(r.Body)
		form, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatal(err)
		}
		*requests = append(*requests, form)
		return hubResponse(http.StatusAccepted), nil
	}
}

func feed(channel, video string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><feed xmlns:yt="http://www.youtube.com/xml/schemas/2015">` +
		`<entry><id>yt:video:` + video + `</id><yt:videoId>` + video + `</yt:videoId><yt:channelId>` + channel + `</yt:channelId><title>synthetic</title></entry></feed>`
}

func post(s *Service, body, signature string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, CallbackPath, strings.NewReader(body))
	if signature != "" {
		r.Header.Set("X-Hub-Signature", signature)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func verification(s *Service, values url.Values) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, CallbackPath+"?"+values.Encode(), nil))
	return w
}

func hubVerification(mode, challenge, lease string) url.Values {
	return url.Values{"hub.mode": {mode}, "hub.topic": {Topic(testChannel)}, "hub.challenge": {challenge}, "hub.lease_seconds": {lease}}
}

func TestValidateRequiresHTTPSCallbackAndBoundedSecret(t *testing.T) {
	valid := Options{CallbackURL: testCallback, Secret: "synthetic-websub-secret", ChannelID: testChannel}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Options){
		"http callback": func(o *Options) { o.CallbackURL = "http://board.example.test/api/websub/youtube" },
		"relative":      func(o *Options) { o.CallbackURL = "/api/websub/youtube" },
		"credentials": func(o *Options) {
			o.CallbackURL = (&url.URL{Scheme: "https", User: url.User("synthetic"), Host: "board.example.test", Path: CallbackPath}).String()
		},
		"fragment":        func(o *Options) { o.CallbackURL = testCallback + "#x" },
		"too long":        func(o *Options) { o.CallbackURL = testCallback + "?" + strings.Repeat("a", maxCallbackLength) },
		"short secret":    func(o *Options) { o.Secret = "synthetic" },
		"long secret":     func(o *Options) { o.Secret = strings.Repeat("s", 200) },
		"padded secret":   func(o *Options) { o.Secret = " " + testSecret },
		"invalid channel": func(o *Options) { o.ChannelID = "channel" },
	} {
		o := valid
		mutate(&o)
		if err := o.Validate(); !errors.Is(err, ErrConfig) {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
	s, _ := newDisabledService(t, nil, nil)
	if err := s.Configure(true, "http://board.example.test/", testSecret); !errors.Is(err, ErrConfig) {
		t.Fatalf("Configure accepted an invalid callback: %v", err)
	}
	if s.Status()["enabled"] != false {
		t.Fatal("rejected configuration was applied")
	}
}

func TestDisabledServiceStaysSilent(t *testing.T) {
	s, _ := newDisabledService(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("disabled service contacted the hub")
		return nil, nil
	}, func(context.Context) (bool, error) {
		t.Fatal("disabled service triggered a sync")
		return false, nil
	})
	s.Step(context.Background())
	body := feed(testChannel, "AAAAAAAAAAA")
	for _, w := range []*httptest.ResponseRecorder{
		post(s, body, signWith(sha1.New, "sha1", body)),
		verification(s, hubVerification("subscribe", "abc", "86400")),
	} {
		if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "api_not_found") {
			t.Fatalf("disabled route: code=%d body=%q", w.Code, w.Body.String())
		}
	}
	if s.Status()["pending"] != false {
		t.Fatal("disabled service accepted a notification")
	}
}

func TestSubscribeSendsLeaseAndSecretToHubOnly(t *testing.T) {
	var requests []url.Values
	s, _ := newTestService(t, recordingHub(t, &requests), nil)
	s.Step(context.Background())
	if len(requests) != 1 {
		t.Fatalf("requests=%d", len(requests))
	}
	form := requests[0]
	if form.Get("hub.mode") != "subscribe" || form.Get("hub.topic") != Topic(testChannel) || form.Get("hub.callback") != testCallback ||
		form.Get("hub.verify") != "async" || form.Get("hub.secret") != testSecret || form.Get("hub.lease_seconds") != "432000" {
		t.Fatalf("form=%v", form)
	}
	// Not due again until the lease renewal point.
	s.Step(context.Background())
	if len(requests) != 1 {
		t.Fatalf("renewed early: %d", len(requests))
	}
	status := s.Status()
	if status["secretConfigured"] != true || status["callbackUrl"] != testCallback {
		t.Fatalf("status=%v", status)
	}
	for _, value := range status {
		if text, ok := value.(string); ok && strings.Contains(text, testSecret) {
			t.Fatalf("status leaked the secret: %v", status)
		}
	}
}

func TestSubscribeFailuresUseFixedCodesAndRetry(t *testing.T) {
	for status, code := range map[int]string{302: "websub_hub_redirect_rejected", 429: "websub_hub_rate_limited", 500: "websub_hub_http_error"} {
		calls := 0
		s, c := newTestService(t, func(*http.Request) (*http.Response, error) {
			calls++
			return hubResponse(status), nil
		}, nil)
		s.Step(context.Background())
		if got := s.Status()["lastError"]; got != code {
			t.Fatalf("status %d: lastError=%v", status, got)
		}
		c.Advance(subscribeRetry - time.Second)
		s.Step(context.Background())
		c.Advance(time.Second)
		s.Step(context.Background())
		if calls != 2 {
			t.Fatalf("status %d: calls=%d", status, calls)
		}
	}
}

func TestVerificationEchoesChallengeOnlyForTopic(t *testing.T) {
	var requests []url.Values
	s, c := newTestService(t, recordingHub(t, &requests), nil)
	s.Step(context.Background())

	foreign := hubVerification("subscribe", "abc", "86400")
	foreign.Set("hub.topic", Topic(otherChannel))
	if w := verification(s, foreign); w.Code != http.StatusNotFound {
		t.Fatalf("foreign topic code=%d", w.Code)
	}
	if w := verification(s, hubVerification("unsubscribe", "abc", "")); w.Code != http.StatusNotFound {
		t.Fatalf("unrequested unsubscribe code=%d", w.Code)
	}
	if w := verification(s, hubVerification("subscribe", "", "86400")); w.Code != http.StatusBadRequest {
		t.Fatalf("empty challenge code=%d", w.Code)
	}

	w := verification(s, hubVerification("subscribe", "challenge-123", "86400"))
	if w.Code != http.StatusOK || w.Body.String() != "challenge-123" {
		t.Fatalf("code=%d body=%q", w.Code, w.Body.String())
	}
	status := s.Status()
	if status["verified"] != true || status["leaseExpiresAt"] != "2026-10-16T00:00:00Z" || status["nextRenewalAt"] != "2026-10-15T12:00:00Z" {
		t.Fatalf("status=%v", status)
	}

	// A later unsolicited verification is answered but cannot shorten renewal.
	c.Advance(time.Minute)
	if w := verification(s, hubVerification("subscribe", "again", "60")); w.Code != http.StatusOK || w.Body.String() != "again" {
		t.Fatalf("code=%d body=%q", w.Code, w.Body.String())
	}
	if got := s.Status()["nextRenewalAt"]; got != "2026-10-15T12:00:00Z" {
		t.Fatalf("unsolicited verification changed renewal: %v", got)
	}
}

func TestVerificationBeforeHubResponseIsAccepted(t *testing.T) {
	var s *Service
	s, _ = newTestService(t, func(*http.Request) (*http.Response, error) {
		// The hub verifies while the subscribe request is still open.
		if w := verification(s, hubVerification("subscribe", "early", "86400")); w.Body.String() != "early" {
			t.Fatalf("early verification body=%q", w.Body.String())
		}
		return hubResponse(http.StatusAccepted), nil
	}, nil)
	s.Step(context.Background())
	status := s.Status()
	if status["verified"] != true || status["nextRenewalAt"] != "2026-10-15T12:00:00Z" {
		t.Fatalf("status=%v", status)
	}
}

func TestShortGrantedLeaseRenewsNoMoreThanHourly(t *testing.T) {
	var requests []url.Values
	s, _ := newTestService(t, recordingHub(t, &requests), nil)
	s.Step(context.Background())
	verification(s, hubVerification("subscribe", "x", "60"))
	if got := s.Status()["nextRenewalAt"]; got != "2026-10-15T01:00:00Z" {
		t.Fatalf("nextRenewalAt=%v", got)
	}
}

func TestMissingVerificationIsReportedAndResubscribed(t *testing.T) {
	var requests []url.Values
	s, c := newTestService(t, recordingHub(t, &requests), nil)
	s.Step(context.Background())
	c.Advance(verifyWindow + time.Second)
	s.Step(context.Background())
	if len(requests) != 2 {
		t.Fatalf("requests=%d", len(requests))
	}
	if got := s.Status()["lastError"]; got != "websub_verification_missing" {
		t.Fatalf("lastError=%v", got)
	}
}

func TestDeniedVerificationSchedulesRetry(t *testing.T) {
	var requests []url.Values
	s, _ := newTestService(t, recordingHub(t, &requests), nil)
	s.Step(context.Background())
	denied := url.Values{"hub.mode": {"denied"}, "hub.topic": {Topic(testChannel)}, "hub.reason": {"synthetic"}}
	if w := verification(s, denied); w.Code != http.StatusOK {
		t.Fatalf("code=%d", w.Code)
	}
	status := s.Status()
	if status["lastError"] != "websub_hub_denied" || status["nextRenewalAt"] != "2026-10-15T00:15:00Z" {
		t.Fatalf("status=%v", status)
	}
}

func TestDisablingWithdrawsTheSubscription(t *testing.T) {
	var requests []url.Values
	s, c := newTestService(t, recordingHub(t, &requests), nil)
	s.Step(context.Background())
	verification(s, hubVerification("subscribe", "x", "86400"))
	if err := s.Configure(false, testCallback, testSecret); err != nil {
		t.Fatal(err)
	}
	c.Advance(time.Second)
	s.Step(context.Background())
	if len(requests) != 2 {
		t.Fatalf("requests=%d", len(requests))
	}
	form := requests[1]
	if form.Get("hub.mode") != "unsubscribe" || form.Get("hub.callback") != testCallback || form.Has("hub.secret") {
		t.Fatalf("unsubscribe form=%v", form)
	}
	// The route stays open only to confirm this withdrawal, once.
	body := feed(testChannel, "AAAAAAAAAAA")
	if w := post(s, body, signWith(sha1.New, "sha1", body)); w.Code != http.StatusAccepted || s.Status()["pending"] != false {
		t.Fatalf("notification while withdrawing: code=%d status=%v", w.Code, s.Status())
	}
	if w := verification(s, hubVerification("subscribe", "x", "86400")); w.Code != http.StatusNotFound {
		t.Fatalf("subscribe while disabled code=%d", w.Code)
	}
	if w := verification(s, hubVerification("unsubscribe", "bye", "")); w.Code != http.StatusOK || w.Body.String() != "bye" {
		t.Fatalf("unsubscribe code=%d body=%q", w.Code, w.Body.String())
	}
	if w := verification(s, hubVerification("unsubscribe", "again", "")); w.Code != http.StatusNotFound {
		t.Fatalf("second unsubscribe code=%d", w.Code)
	}
	status := s.Status()
	if status["enabled"] != false || status["verified"] != false || status["nextRenewalAt"] != nil {
		t.Fatalf("status=%v", status)
	}
	c.Advance(LeaseSeconds * time.Second)
	s.Step(context.Background())
	if len(requests) != 2 {
		t.Fatalf("disabled service renewed: %d", len(requests))
	}
}

func TestMovingCallbackWithdrawsOldAndSubscribesNew(t *testing.T) {
	var requests []url.Values
	s, c := newTestService(t, recordingHub(t, &requests), nil)
	s.Step(context.Background())
	if err := s.Configure(true, movedCallback, testSecret); err != nil {
		t.Fatal(err)
	}
	c.Advance(time.Second)
	s.Step(context.Background())
	if len(requests) != 3 {
		t.Fatalf("requests=%d", len(requests))
	}
	if requests[1].Get("hub.mode") != "unsubscribe" || requests[1].Get("hub.callback") != testCallback {
		t.Fatalf("withdrawal=%v", requests[1])
	}
	if requests[2].Get("hub.mode") != "subscribe" || requests[2].Get("hub.callback") != movedCallback {
		t.Fatalf("subscription=%v", requests[2])
	}
}

func TestRotatedSecretResubscribesAndRejectsOldSignatures(t *testing.T) {
	var requests []url.Values
	s, c := newTestService(t, recordingHub(t, &requests), nil)
	s.Step(context.Background())
	const rotated = "synthetic-rotated-secret"
	if err := s.Configure(true, testCallback, rotated); err != nil {
		t.Fatal(err)
	}
	c.Advance(time.Second)
	s.Step(context.Background())
	if len(requests) != 2 || requests[1].Get("hub.mode") != "subscribe" || requests[1].Get("hub.secret") != rotated {
		t.Fatalf("requests=%v", requests)
	}
	body := feed(testChannel, "AAAAAAAAAAA")
	post(s, body, signWith(sha1.New, "sha1", body))
	if s.Status()["pending"] != false {
		t.Fatal("old secret still accepted")
	}
}

func TestUnsubscribeRetriesAreBounded(t *testing.T) {
	calls := 0
	s, c := newTestService(t, func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		if form.Get("hub.mode") == "unsubscribe" {
			calls++
			return hubResponse(http.StatusInternalServerError), nil
		}
		return hubResponse(http.StatusAccepted), nil
	}, nil)
	s.Step(context.Background())
	if err := s.Configure(false, "", ""); err != nil {
		t.Fatal(err)
	}
	for range 6 {
		s.Step(context.Background())
		c.Advance(subscribeRetry)
	}
	if calls != unsubscribeAttempts {
		t.Fatalf("unsubscribe attempts=%d", calls)
	}
}

func TestNotificationRequiresSignatureAndChannelVideo(t *testing.T) {
	s, _ := newTestService(t, nil, nil)
	body := feed(testChannel, "AAAAAAAAAAA")
	deletedEntry := `<feed><deleted-entry ref="yt:video:AAAAAAAAAAA"/></feed>`
	for name, tc := range map[string]struct{ body, signature string }{
		"missing signature": {body, ""},
		"wrong signature":   {body, signWith(sha1.New, "sha1", body+" ")},
		"unknown algorithm": {body, signWith(sha1.New, "md5", body)},
		"other channel":     {feed(otherChannel, "AAAAAAAAAAA"), signWith(sha1.New, "sha1", feed(otherChannel, "AAAAAAAAAAA"))},
		"invalid video":     {feed(testChannel, "bad"), signWith(sha1.New, "sha1", feed(testChannel, "bad"))},
		"deleted entry":     {deletedEntry, signWith(sha1.New, "sha1", deletedEntry)},
		"malformed xml":     {"<feed", signWith(sha1.New, "sha1", "<feed")},
	} {
		if w := post(s, tc.body, tc.signature); w.Code != http.StatusAccepted {
			t.Fatalf("%s: code=%d", name, w.Code)
		}
		if s.Status()["pending"] != false {
			t.Fatalf("%s set pending", name)
		}
	}
	if w := post(s, body, signWith(sha256.New, "sha256", body)); w.Code != http.StatusAccepted || s.Status()["pending"] != true {
		t.Fatalf("signed sha256 notification ignored: code=%d", w.Code)
	}
}

func TestNotificationBodyIsBounded(t *testing.T) {
	s, _ := newTestService(t, nil, nil)
	body := strings.Repeat("x", MaxNotificationBytes+1)
	if w := post(s, body, signWith(sha1.New, "sha1", body)); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("code=%d", w.Code)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodPut, CallbackPath, nil))
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET, POST" {
		t.Fatalf("code=%d allow=%q", w.Code, w.Header().Get("Allow"))
	}
}

func TestNotificationsAreDebouncedAndRetriedWhileBusy(t *testing.T) {
	var requests []url.Values
	busy := true
	triggers := 0
	s, c := newTestService(t, recordingHub(t, &requests), func(context.Context) (bool, error) {
		triggers++
		return !busy, nil
	})
	s.Step(context.Background()) // initial subscription
	body := feed(testChannel, "AAAAAAAAAAA")
	post(s, body, signWith(sha1.New, "sha1", body))

	c.Advance(debounce - time.Second)
	s.Step(context.Background())
	if triggers != 0 {
		t.Fatal("triggered before the debounce elapsed")
	}
	// A second notification inside the window extends it.
	post(s, body, signWith(sha1.New, "sha1", body))
	c.Advance(debounce - time.Second)
	s.Step(context.Background())
	if triggers != 0 {
		t.Fatal("burst was not coalesced")
	}
	c.Advance(time.Second)
	s.Step(context.Background())
	if triggers != 1 || s.Status()["pending"] != true {
		t.Fatalf("busy queue: triggers=%d status=%v", triggers, s.Status())
	}
	c.Advance(triggerRetry - time.Second)
	s.Step(context.Background())
	if triggers != 1 {
		t.Fatal("retried before the retry delay")
	}
	busy = false
	c.Advance(time.Second)
	s.Step(context.Background())
	if triggers != 2 || s.Status()["pending"] != false || s.Status()["lastTriggeredAt"] == nil {
		t.Fatalf("accepted run: triggers=%d status=%v", triggers, s.Status())
	}

	// The next notification waits for the minimum gap between runs.
	post(s, body, signWith(sha1.New, "sha1", body))
	c.Advance(debounce)
	s.Step(context.Background())
	if triggers != 2 {
		t.Fatal("ignored the minimum trigger gap")
	}
	c.Advance(minTriggerGap - debounce)
	s.Step(context.Background())
	if triggers != 3 {
		t.Fatalf("triggers=%d", triggers)
	}
}

func TestTriggerErrorKeepsNotificationPending(t *testing.T) {
	s, c := newTestService(t, func(*http.Request) (*http.Response, error) { return hubResponse(http.StatusAccepted), nil }, func(context.Context) (bool, error) {
		return false, errors.New("store unavailable")
	})
	body := feed(testChannel, "AAAAAAAAAAA")
	post(s, body, signWith(sha1.New, "sha1", body))
	c.Advance(debounce)
	s.Step(context.Background())
	if s.Status()["pending"] != true {
		t.Fatal("failed trigger dropped the notification")
	}
}
