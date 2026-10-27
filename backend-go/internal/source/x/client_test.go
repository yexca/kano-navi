package x

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type fakeRoundTripper func(*http.Request) (*http.Response, error)

func (f fakeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestFetchRecentFakeTransportNormalizesAndFiltersAccount(t *testing.T) {
	rt := fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == profileHost {
			return response(200, `<a href="/kano/status/111111111111">a</a><a href="/kano/status/222222222222">b</a>`), nil
		}
		if strings.HasSuffix(r.URL.Path, "/111111111111") {
			return response(200, `{"tweetID":"111111111111","text":"schedule this","date":"2026-10-11T00:00:00Z","mediaURLs":["https://pbs.twimg.com/a.jpg"],"author":{"screenName":"kano"}}`), nil
		}
		return response(200, `{"tweetID":"222222222222","text":"wrong account","date":"2026-10-11T00:01:00Z","author":{"screenName":"other"}}`), nil
	})
	a := NewAdapter(NewClient(&http.Client{Transport: rt}), Options{DiscoveryLimit: 10, MaxDetailRequests: 2, ScheduleKeywords: []string{"schedule"}})
	got, err := a.FetchRecent(context.Background(), "kano", a.Options)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Posts) != 1 || got.Posts[0].ID != "111111111111" {
		t.Fatalf("posts=%#v", got.Posts)
	}
	// Another account's status is dropped at once, without a failure.
	if len(got.ScheduleCandidates) != 1 || len(got.PendingIDs) != 0 || len(got.PendingRetries) != 0 || len(got.Errors) != 0 {
		t.Fatalf("result=%#v", got)
	}
	if len(got.Dropped) != 1 || got.Dropped[0] != (DroppedStatus{ID: "222222222222", Code: DroppedForeign}) || got.RequestedCount != 2 {
		t.Fatalf("dropped=%+v requested=%d", got.Dropped, got.RequestedCount)
	}
}

func TestDiscoverIDsKeepsOnlyTheAccountsStatusLinks(t *testing.T) {
	body := `<a href="/Kano/status/111111111111">own</a>` +
		`<a href="https://x.com/other/status/222222222222">quoted</a>` +
		`<a href="/kano_alt/status/333333333333">similar handle</a>` +
		`<a href="/i/web/status/444444444444">web</a>` +
		`<a href="/login?redirect_after_login=%2Fkano%2Fstatus%2F555555555555">encoded</a>` +
		`<a href="/other%2Fstatus%2F666666666666">encoded quote</a>` +
		`<a href="/kano/status/111111111111/photo/1">photo</a>`
	if got := strings.Join(DiscoverIDs([]byte(body), "kano", 10), ","); got != "111111111111,555555555555" {
		t.Fatalf("ids=%s", got)
	}
	if got := DiscoverIDs([]byte(body), "kano", 1); len(got) != 1 || got[0] != "111111111111" {
		t.Fatalf("limited=%v", got)
	}
}

func TestForeignAnswersAloneDoNotFailTheAccount(t *testing.T) {
	rt := fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == profileHost {
			return response(200, `<a href="/kano/status/111111111111">a</a>`), nil
		}
		return response(200, `{"tweetID":"111111111111","text":"quoted","date":"2026-10-11T00:00:00Z","author":{"screenName":"other"}}`), nil
	})
	a := NewAdapter(NewClient(&http.Client{Transport: rt}), Options{DiscoveryLimit: 10, MaxDetailRequests: 2})
	got, err := a.FetchRecent(context.Background(), "kano", a.Options)
	if err != nil || len(got.Posts) != 0 || len(got.Errors) != 0 || len(got.Dropped) != 1 || got.Dropped[0].Code != DroppedForeign {
		t.Fatalf("result=%#v err=%v", got, err)
	}
}
func TestClientErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   ErrorCode
	}{{429, ErrRateLimited}, {500, ErrServer}, {302, ErrRedirect}} {
		a := NewAdapter(NewClient(&http.Client{Transport: fakeRoundTripper(func(*http.Request) (*http.Response, error) { return response(tc.status, "x"), nil })}), Options{})
		_, err := a.Client.Profile(context.Background(), "kano")
		var e *Error
		if !errors.As(err, &e) || e.Code != tc.code {
			t.Fatalf("status %d err=%v", tc.status, err)
		}
	}
}
func TestDiscoveryBudgetLeavesPending(t *testing.T) {
	rt := fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == profileHost {
			return response(200, `/kano/status/111111111111 /kano/status/222222222222`), nil
		}
		return response(200, `{"tweetID":"111111111111","text":"x","date":"2026-10-11T00:00:00Z"}`), nil
	})
	a := NewAdapter(NewClient(&http.Client{Transport: rt}), Options{DiscoveryLimit: 10, MaxDetailRequests: 1})
	got, err := a.FetchRecent(context.Background(), "kano", a.Options)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.PendingIDs) != 1 {
		t.Fatalf("pending=%v", got.PendingIDs)
	}
}

func TestInvalidJSONAndOversizedResponsesAreTyped(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		code ErrorCode
	}{
		{"invalid-json", `not-json`, ErrInvalidJSON},
		{"oversized", strings.Repeat("x", int(maxResponseBytes)+1), ErrOversized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt := fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host == profileHost {
					return response(200, `/kano/status/111111111111`), nil
				}
				return response(200, tc.body), nil
			})
			a := NewAdapter(NewClient(&http.Client{Transport: rt}), Options{DiscoveryLimit: 1, MaxDetailRequests: 1})
			_, err := a.FetchRecent(context.Background(), "kano", a.Options)
			var typed *Error
			if !errors.As(err, &typed) || typed.Code != tc.code {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestTimeoutIsTyped(t *testing.T) {
	rt := fakeRoundTripper(func(*http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded })
	a := NewAdapter(NewClient(&http.Client{Transport: rt, Timeout: time.Millisecond}), Options{})
	_, err := a.Client.Profile(context.Background(), "kano")
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != ErrTimeout {
		t.Fatalf("err=%v", err)
	}
}
