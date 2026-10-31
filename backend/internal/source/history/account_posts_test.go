package history

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	xsource "github.com/yexca/kano-navi/backend/internal/source/x"
)

func TestAccountPostsReadsTheUserTimelineWithItsBounds(t *testing.T) {
	requests := []string{}
	transport := fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.x.com" || r.Header.Get("Authorization") != "Bearer "+testCredential || strings.Contains(r.URL.String(), testCredential) {
			t.Errorf("unexpected request %s", r.URL)
		}
		requests = append(requests, r.URL.Path)
		switch r.URL.Path {
		case "/2/users/by/username/" + testAccount:
			return response(200, `{"data":{"id":"42","username":"Synthetic_Handle"}}`), nil
		case "/2/users/42/tweets":
			query := r.URL.Query()
			if query.Get("since_id") != "1900000000000000001" || query.Get("until_id") != "1900000000000000009" || query.Get("max_results") != "5" ||
				query.Get("exclude") != "retweets,replies" || query.Get("pagination_token") != "" || !strings.Contains(query.Get("tweet.fields"), "note_tweet") {
				t.Errorf("unexpected query %v", query)
			}
			return response(200, `{"data":[{"id":"1900000000000000005","text":"hello","created_at":"2026-03-05T03:00:00.000Z","author_id":"42"}],
				"includes":{"users":[{"id":"42","username":"Synthetic_Handle","name":"Synthetic"}]},"meta":{"result_count":1,"next_token":"n2"}}`), nil
		}
		return response(404, ""), nil
	})
	reader := &XAPI{Fetcher: newFetcher(&memoryStore{}, transport, 4), Credential: testCredential}
	query := xsource.APIQuery{Account: testAccount, SinceID: "1900000000000000001", UntilID: "1900000000000000009", Max: 3}
	for range 2 {
		page, err := reader.AccountPosts(context.Background(), query)
		if err != nil || len(page.Posts) != 1 || page.Posts[0].Text != "hello" || page.Next != "n2" || page.Read != 1 {
			t.Fatalf("page=%+v err=%v", page, err)
		}
	}
	// The user ID is looked up once per process.
	if strings.Join(requests, ",") != "/2/users/by/username/"+testAccount+",/2/users/42/tweets,/2/users/42/tweets" {
		t.Fatalf("requests=%v", requests)
	}
}

func TestAccountPostsReportsControlledCodes(t *testing.T) {
	for name, tc := range map[string]struct {
		lookup, posts *http.Response
		code          string
	}{
		"rate limited":   {lookup: response(429, testCredential), code: "source_api_http_429"},
		"wrong account":  {lookup: response(200, `{"data":{"id":"42","username":"someone"}}`), code: "source_api_invalid_response"},
		"quota":          {lookup: response(200, `{"data":{"id":"42","username":"`+testAccount+`"}}`), posts: response(402, ""), code: "source_api_http_402"},
		"no data errors": {lookup: response(200, `{"data":{"id":"42","username":"`+testAccount+`"}}`), posts: response(200, `{"errors":[{"title":"x"}],"meta":{}}`), code: "source_api_invalid_response"},
	} {
		t.Run(name, func(t *testing.T) {
			transport := fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
				if strings.Contains(r.URL.Path, "/by/username/") {
					return tc.lookup, nil
				}
				return tc.posts, nil
			})
			reader := &XAPI{Fetcher: newFetcher(&memoryStore{}, transport, 4), Credential: testCredential}
			_, err := reader.AccountPosts(context.Background(), xsource.APIQuery{Account: testAccount, Max: 10})
			var coded *xsource.Error
			if !errors.As(err, &coded) || string(coded.Code) != tc.code || strings.Contains(err.Error(), testCredential) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
