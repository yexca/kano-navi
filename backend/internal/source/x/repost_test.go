package x

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTimelineListsRepostsApartFromOwnStatuses(t *testing.T) {
	at := time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC)
	own := statusFixture{id: "1900000000000000400", text: "own", at: at}
	reposted := statusFixture{id: "1900000000000000401", text: "agency notice", at: at.Add(-time.Hour), author: "agency_synthetic",
		media: []mediaFixture{{kind: "photo", file: "https://pbs.twimg.com/media/notice.jpg"}}}
	repost := reposted.timeline()
	repost["reposted_by"] = map[string]any{"screen_name": "Kano", "name": "Synthetic"}
	// Another author's status that names no repost of the account is skipped.
	stray := statusFixture{id: "1900000000000000402", text: "stray", at: at, author: "someone"}.timeline()
	strayRepost := statusFixture{id: "1900000000000000403", text: "other repost", at: at, author: "someone"}.timeline()
	strayRepost["reposted_by"] = "other_account"
	page, err := parseTimelineStatuses([]byte(fxPage(t, "next", own.timeline(), repost, stray, strayRepost)), "kano")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Statuses) != 1 || page.Statuses[0].ID != own.id {
		t.Fatalf("statuses=%+v", page.Statuses)
	}
	if len(page.Reposts) != 1 {
		t.Fatalf("reposts=%+v", page.Reposts)
	}
	r := page.Reposts[0]
	if r.ID != reposted.id || r.Author != "agency_synthetic" || r.Status == nil {
		t.Fatalf("repost=%+v", r)
	}
	// The reposted status maps as a quote of it would.
	quoting, err := parseTimelineStatuses([]byte(fxPage(t, "", statusFixture{id: "1900000000000000404", text: "これ", at: at, quote: &reposted}.timeline())), "kano")
	if err != nil || len(quoting.Statuses) != 1 || quoting.Statuses[0].Post == nil {
		t.Fatalf("quoting page=%+v err=%v", quoting, err)
	}
	if want := quoting.Statuses[0].Post.Quote; quoteSummary(t, r.Status) != quoteSummary(t, want) {
		t.Fatalf("status=%s want=%s", quoteSummary(t, r.Status), quoteSummary(t, want))
	}
}

func TestAPIRepostsReadRetweetedReferences(t *testing.T) {
	own := statusFixture{id: "1900000000000000500", text: "own", at: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)}
	answer := apiAnswer(t, "", own)
	ownAuthor := answer["data"].([]any)[0].(map[string]any)["author_id"]
	answer["data"] = append(answer["data"].([]any),
		map[string]any{"id": "1900000000000000501", "author_id": ownAuthor, "created_at": "2026-10-20T00:00:00.000Z", "text": "RT @agency_synthetic: notice",
			"referenced_tweets": []any{map[string]any{"type": "retweeted", "id": "1900000000000000001"}}},
		// A retweet whose status X did not return keeps only the link.
		map[string]any{"id": "1900000000000000502", "author_id": ownAuthor, "created_at": "2026-10-20T00:00:00.000Z", "text": "RT @gone: x",
			"referenced_tweets": []any{map[string]any{"type": "retweeted", "id": "1900000000000000002"}}})
	includes := answer["includes"].(map[string]any)
	includes["users"] = append(includes["users"].([]any), map[string]any{"id": "9001", "username": "agency_synthetic", "name": "Agency"})
	includes["tweets"] = append(list(includes["tweets"]), map[string]any{"id": "1900000000000000001", "author_id": "9001", "created_at": "2026-10-19T00:00:00.000Z", "text": "notice"})
	var payload map[string]any
	if err := json.Unmarshal(decoded(t, answer), &payload); err != nil {
		t.Fatal(err)
	}
	reposts := APIReposts(payload, "kano")
	if len(reposts) != 2 {
		t.Fatalf("reposts=%+v", reposts)
	}
	if r := reposts[0]; r.ID != "1900000000000000001" || r.RetweetID != "1900000000000000501" || r.Author != "agency_synthetic" || r.Status == nil || r.Status.Text != "notice" {
		t.Fatalf("first=%+v status=%+v", r, r.Status)
	}
	if r := reposts[1]; r.ID != "1900000000000000002" || r.RetweetID != "1900000000000000502" || r.Author != "" || r.Status != nil {
		t.Fatalf("second=%+v", r)
	}
	// The posts themselves still leave the reposts out.
	if posts, _, _, err := APIPosts(payload, "kano"); err != nil || len(posts) != 1 {
		t.Fatalf("posts=%+v err=%v", posts, err)
	}
}

// retweetAnswer is a vxtwitter detail answer for an own account's repost:
// the reposting account as author, "RT @…" wording and the reposted status
// under retweet.
func retweetAnswer(id, by, original, author string) string {
	return `{"tweetID":"` + id + `","text":"RT @` + author + `: notice","date_epoch":1792540800,"user_screen_name":"` + by + `","user_name":"Synthetic",` +
		`"mediaURLs":["https://pbs.twimg.com/media/notice.jpg"],"retweet":{"tweetID":"` + original + `","text":"notice","date_epoch":1792537200,` +
		`"user_screen_name":"` + author + `","user_name":"Agency","mediaURLs":["https://pbs.twimg.com/media/notice.jpg"],` +
		`"media_extended":[{"type":"image","url":"https://pbs.twimg.com/media/notice.jpg"}],"tweetURL":"https://twitter.com/` + author + `/status/` + original + `"}}`
}

// A detail answer that is the account's repost is never a post: it is
// recorded as a repost with its own status ID, drops a pending ID at once
// with its own code and fails nothing, and an operator's refresh of it is
// done.
func TestDetailRepostIsRecordedNotStored(t *testing.T) {
	const own, retweet, original, foreign = "1900000000000000600", "1900000000000000601", "1900000000000000001", "1900000000000000602"
	rt := fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.URL.Host == profileHost:
			return response(200, `<a href="/kano/status/`+own+`">own</a>`), nil
		case strings.HasSuffix(r.URL.Path, "/"+own):
			return response(200, `{"tweetID":"`+own+`","text":"own","date_epoch":1792540800,"user_screen_name":"kano"}`), nil
		case strings.HasSuffix(r.URL.Path, "/"+retweet):
			return response(200, retweetAnswer(retweet, "Kano", original, "agency_synthetic")), nil
		default:
			// Another account's repost is that account's status.
			return response(200, retweetAnswer(foreign, "someone", original, "agency_synthetic")), nil
		}
	})
	opts := Options{Chain: []string{DiscoveryProfile}, DiscoveryLimit: 10, MaxDetailRequests: 5, PendingIDs: []string{retweet, foreign},
		PendingRetries: map[string]PendingRetry{retweet: {Failures: 1}}}
	a := NewAdapter(NewClient(&http.Client{Transport: rt}), opts)
	got, err := a.FetchRecent(context.Background(), "kano", opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Posts) != 1 || got.Posts[0].ID != own || len(got.Errors) != 0 || len(got.PendingIDs) != 0 || len(got.PendingRetries) != 0 {
		t.Fatalf("posts=%+v errors=%v pending=%v retries=%v", got.Posts, got.Errors, got.PendingIDs, got.PendingRetries)
	}
	dropped := map[string]string{}
	for _, d := range got.Dropped {
		dropped[d.ID] = d.Code
	}
	if len(dropped) != 2 || dropped[retweet] != DroppedRepost || dropped[foreign] != DroppedForeign {
		t.Fatalf("dropped=%+v", got.Dropped)
	}
	if len(got.Reposts) != 1 {
		t.Fatalf("reposts=%+v", got.Reposts)
	}
	r := got.Reposts[0]
	if r.ID != original || r.RetweetID != retweet || r.Author != "agency_synthetic" || r.Status == nil || r.Status.Text != "notice" ||
		strings.Join(r.Status.MediaURLs, ",") != "https://pbs.twimg.com/media/notice.jpg" || r.Status.URL != "https://x.com/agency_synthetic/status/"+original {
		t.Fatalf("repost=%+v status=%+v", r, r.Status)
	}

	// An operator's refresh of a stored retweet answers the same way: the
	// refresh is done and the run fails nothing, with no post to write.
	refresh := Options{RefreshOnly: true, RefreshIDs: []string{retweet}, MaxDetailRequests: 5}
	got, err = a.FetchRecent(context.Background(), "kano", refresh)
	if err != nil || len(got.Posts) != 0 || len(got.Errors) != 0 || strings.Join(got.Refreshed, ",") != retweet || len(got.Reposts) != 1 || got.Reposts[0].RetweetID != retweet {
		t.Fatalf("refresh=%+v err=%v", got, err)
	}
}

func TestDetailRepostRequiresTheRequestedIdentity(t *testing.T) {
	const requested, other, original = "1900000000000000901", "1900000000000000902", "1900000000000000903"
	for _, key := range []string{"tweetID", "tweetId", "id"} {
		for _, returned := range []string{requested, other, ""} {
			t.Run(key+"/"+returned, func(t *testing.T) {
				var payload map[string]any
				if err := json.Unmarshal([]byte(retweetAnswer(requested, "kano", original, "someone")), &payload); err != nil {
					t.Fatal(err)
				}
				delete(payload, "tweetID")
				if returned != "" {
					payload[key] = returned
				}
				body, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				client := NewClient(&http.Client{Transport: fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
					return response(200, string(body)), nil
				})})
				opts := Options{RefreshOnly: true, RefreshIDs: []string{requested}, MaxDetailRequests: 1}
				got, err := NewAdapter(client, opts).FetchRecent(context.Background(), "kano", opts)
				if returned == requested {
					if err != nil || len(got.Reposts) != 1 || got.Reposts[0].RetweetID != requested {
						t.Fatalf("matching repost: %+v err=%v", got, err)
					}
					return
				}
				if err == nil || safeCode(err) != string(ErrInvalidResponse) || len(got.Reposts) != 0 || len(got.Dropped) != 0 || len(got.Posts) != 0 {
					t.Fatalf("unverified identity must not write or remove a post: %+v err=%v", got, err)
				}
			})
		}
	}
}

func TestAPIRepostsKeepEveryRetweetIdentity(t *testing.T) {
	const original, first, second = "1900000000000000900", "1900000000000000901", "1900000000000000902"
	entry := func(id string) any {
		return map[string]any{"id": id, "referenced_tweets": []any{map[string]any{"type": "retweeted", "id": original}}}
	}
	got := APIReposts(map[string]any{"data": []any{entry(first), entry(second), entry(first)}}, "kano")
	if len(got) != 2 || got[0].RetweetID != first || got[1].RetweetID != second || got[0].ID != original || got[1].ID != original {
		t.Fatalf("distinct retweets of one original: %+v", got)
	}
}

// The account's repost whose reposted status cannot be read (no ID, or the
// requested ID itself) fails as an invalid answer and is never a post.
func TestUnreadableDetailRepostIsNeverAPost(t *testing.T) {
	const requested = "1900000000000000910"
	for name, retweet := range map[string]string{
		"no id":   `{"text":"notice","user_screen_name":"someone"}`,
		"same id": `{"tweetID":"` + requested + `","text":"notice","user_screen_name":"someone"}`,
	} {
		t.Run(name, func(t *testing.T) {
			body := `{"tweetID":"` + requested + `","text":"RT @someone: notice","date_epoch":1792540800,"user_screen_name":"kano","retweet":` + retweet + `}`
			client := NewClient(&http.Client{Transport: fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
				return response(200, body), nil
			})})
			opts := Options{RefreshOnly: true, RefreshIDs: []string{requested}, MaxDetailRequests: 1}
			got, err := NewAdapter(client, opts).FetchRecent(context.Background(), "kano", opts)
			if err == nil || safeCode(err) != string(ErrInvalidResponse) || len(got.Posts) != 0 || len(got.Reposts) != 0 {
				t.Fatalf("unreadable repost: %+v err=%v", got, err)
			}
		})
	}
}
