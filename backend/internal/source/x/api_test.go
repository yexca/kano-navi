package x

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fixtureLink finds the reserved example links fixture texts carry.
var fixtureLink = regexp.MustCompile(`https://example\.(?:com|org)\S*`)

// apiEntry is the status as an X API v2 answer carries it: entities.urls
// shorten its links and its own media, the text is HTML-escaped and
// cut at 280 characters with the whole text in note_tweet, and its media,
// author and quoted status come as expansions.
func (f statusFixture) apiEntry(includes map[string][]any) map[string]any {
	urls := []any{}
	short := func(target string, mediaKey string) string {
		// X shortens links on its own host; a reserved one stands in.
		link := fmt.Sprintf("https://short.example.test/s%d%s", len(urls), f.id[len(f.id)-3:])
		entry := map[string]any{"url": link, "expanded_url": target, "display_url": strings.TrimPrefix(target, "https://")}
		if mediaKey != "" {
			entry["media_key"] = mediaKey
		}
		urls = append(urls, entry)
		return link
	}
	full := html.EscapeString(fixtureLink.ReplaceAllStringFunc(f.text, func(link string) string { return short(link, "") }))
	keys := []any{}
	for i, m := range f.media {
		key := fmt.Sprintf("3_%s_%d", f.id, i)
		keys = append(keys, key)
		item := map[string]any{"media_key": key, "type": m.kind}
		switch m.kind {
		case "photo":
			item["url"] = m.file
		case "gif":
			item["type"], item["preview_image_url"] = "animated_gif", m.thumb
		default:
			item["preview_image_url"] = m.thumb
		}
		includes["media"] = append(includes["media"], item)
		kind := "photo"
		if m.kind != "photo" {
			kind = "video"
		}
		full += " " + short("https://x.com/"+f.handle()+"/status/"+f.id+"/"+kind+"/"+strconv.Itoa(i+1), key)
	}
	userID := "7" + strconv.Itoa(len(f.handle()))
	includes["users"] = append(includes["users"], map[string]any{"id": userID, "username": f.handle(), "name": "Synthetic"})
	entry := map[string]any{
		"id": f.id, "author_id": userID, "created_at": f.at.UTC().Format("2006-01-02T15:04:05.000Z"),
		"text":           full,
		"public_metrics": map[string]any{"like_count": f.likes, "retweet_count": f.reposts, "reply_count": f.replies, "quote_count": 0},
		"entities":       map[string]any{"urls": urls},
	}
	if runes := []rune(full); len(runes) > 280 {
		entry["text"] = string(runes[:270]) + "…"
		entry["note_tweet"] = map[string]any{"text": full, "entities": map[string]any{"urls": urls}}
	}
	if len(keys) > 0 {
		entry["attachments"] = map[string]any{"media_keys": keys}
	}
	if f.quote != nil {
		entry["referenced_tweets"] = []any{map[string]any{"type": "quoted", "id": f.quote.id}}
		includes["tweets"] = append(includes["tweets"], f.quote.apiEntry(includes))
	}
	return entry
}

// apiAnswer is one X API v2 posts answer listing the statuses.
func apiAnswer(t *testing.T, next string, statuses ...statusFixture) map[string]any {
	t.Helper()
	includes := map[string][]any{}
	data := []any{}
	for _, f := range statuses {
		data = append(data, f.apiEntry(includes))
	}
	meta := map[string]any{"result_count": len(data)}
	if next != "" {
		meta["next_token"] = next
	}
	var payload map[string]any
	if err := json.Unmarshal(decoded(t, map[string]any{"data": data, "includes": includes, "meta": meta}), &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

// TestAPIStoresTheSameValuesAsTimelineAndDetail reads one status through each
// path. A post the chain's API method stores must keep its extraction
// fingerprint when the timeline lists it or a detail request refreshes it
// later: text (a long post's whole text, links expanded, its own media links
// removed, entities decoded), time, URL, counters, own images in post order,
// raw_json.mediaURLs and the quote.
func TestAPIStoresTheSameValuesAsTimelineAndDetail(t *testing.T) {
	at := time.Date(2026, 10, 20, 3, 4, 5, 0, time.UTC)
	photo := func(name string) mediaFixture {
		return mediaFixture{kind: "photo", file: "https://pbs.twimg.com/media/" + name + ".jpg"}
	}
	quoted := &statusFixture{id: "1900000000000000001", text: "quoted & linked https://example.com/q", at: at.Add(-time.Hour), author: "other", media: []mediaFixture{photo("quoted")}}
	for name, f := range map[string]statusFixture{
		"text only":       {text: "plain text"},
		"entities":        {text: "Tom & Jerry <live> at https://example.com/a?b=1 and https://example.org/"},
		"long text":       {text: strings.Repeat("長い本文の行です。\n", 60) + "https://example.com/long"},
		"photos counters": {text: "今週の配信予定", likes: 5, reposts: 2, replies: 1, media: []mediaFixture{photo("first"), {kind: "photo", file: "https://pbs.twimg.com/media/second.png"}}},
		"quoted photo":    {text: "look", quote: quoted},
		"own and quoted":  {text: "both", media: []mediaFixture{photo("own")}, quote: quoted},
		"video":           {text: "clip", media: []mediaFixture{{kind: "video", file: "https://video.twimg.com/ext_tw_video/1/pu/vid/720x720/v.mp4?tag=12", thumb: "https://pbs.twimg.com/ext_tw_video_thumb/1/pu/img/v.jpg"}}},
		"gif and photo":   {text: "loop", media: []mediaFixture{{kind: "gif", file: "https://video.twimg.com/tweet_video/g.mp4", thumb: "https://pbs.twimg.com/tweet_video_thumb/g.jpg"}, photo("after")}},
	} {
		t.Run(name, func(t *testing.T) {
			f.id, f.at = "1900000000000000100", at
			var payload map[string]any
			if err := json.Unmarshal(decoded(t, f.detail()), &payload); err != nil {
				t.Fatal(err)
			}
			fromDetail := Normalize(payload, f.id, "kano")
			page, err := parseTimelineStatuses([]byte(fxPage(t, "", f.timeline())), "kano")
			if err != nil || len(page.Statuses) != 1 || page.Statuses[0].Post == nil || fromDetail == nil {
				t.Fatalf("detail=%+v page=%+v err=%v", fromDetail, page, err)
			}
			posts, read, next, err := APIPosts(apiAnswer(t, "", f), "kano")
			if err != nil || len(posts) != 1 || next != "" {
				t.Fatalf("api posts=%+v err=%v", posts, err)
			}
			if want := 1 + map[bool]int{true: 1}[f.quote != nil]; read != want {
				t.Errorf("read=%d, want %d", read, want)
			}
			fromAPI := posts[0]
			for path, other := range map[string]*Post{"detail": fromDetail, "timeline": page.Statuses[0].Post} {
				for field, pair := range map[string][2]any{
					"text":      {other.Text, fromAPI.Text},
					"published": {other.PublishedAt, fromAPI.PublishedAt},
					"url":       {other.URL, fromAPI.URL},
					"type":      {other.Type + other.Label, fromAPI.Type + fromAPI.Label},
					"counters":  {[3]int{other.Likes, other.Reposts, other.Replies}, [3]int{fromAPI.Likes, fromAPI.Reposts, fromAPI.Replies}},
					"media":     {strings.Join(other.MediaURLs, " "), strings.Join(fromAPI.MediaURLs, " ")},
					"declared":  {string(decoded(t, other.Raw["mediaURLs"])), string(decoded(t, fromAPI.Raw["mediaURLs"]))},
					"quote":     {quoteSummary(t, other.Quote), quoteSummary(t, fromAPI.Quote)},
					"author":    {other.AuthorScreenName, fromAPI.AuthorScreenName},
				} {
					if pair[0] != pair[1] {
						t.Errorf("%s: %s %q, api %q", field, path, pair[0], pair[1])
					}
				}
			}
			// The startup backfill rebuilds a quote from raw_json alone.
			if f.quote != nil && quoteSummary(t, QuoteOf(fromAPI.Raw)) != quoteSummary(t, fromAPI.Quote) {
				t.Errorf("raw quote=%s", quoteSummary(t, QuoteOf(fromAPI.Raw)))
			}
		})
	}
}

func TestAPIPostsSkipRepostsAndRefuseOtherAuthors(t *testing.T) {
	own := statusFixture{id: "1900000000000000300", text: "own", at: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)}
	answer := apiAnswer(t, "next", own)
	repost := map[string]any{"id": "1900000000000000301", "author_id": "74", "created_at": "2026-10-20T00:00:00.000Z", "text": "RT @other: hi",
		"referenced_tweets": []any{map[string]any{"type": "retweeted", "id": "1900000000000000001"}}}
	answer["data"] = append(answer["data"].([]any), repost)
	// A quoted status X cannot return is named but not carried.
	gone := map[string]any{"id": "1900000000000000302", "author_id": "74", "created_at": "2026-10-20T00:00:00.000Z", "text": "see",
		"referenced_tweets": []any{map[string]any{"type": "quoted", "id": "1900000000000000002"}}}
	answer["data"] = append(answer["data"].([]any), gone)
	posts, read, next, err := APIPosts(answer, "kano")
	if err != nil || len(posts) != 2 || posts[0].ID != own.id || next != "next" || read != 3 {
		t.Fatalf("posts=%+v read=%d next=%q err=%v", posts, read, next, err)
	}
	if q := posts[1].Quote; q == nil || !q.Unavailable || q.ID != "1900000000000000002" {
		t.Fatalf("quote=%+v", q)
	}
	foreign := apiAnswer(t, "", statusFixture{id: "1900000000000000303", text: "x", at: own.at, author: "someone"})
	if _, _, _, err = APIPosts(foreign, "kano"); err == nil {
		t.Fatal("another author's post was accepted")
	}
}

// fakeAPI answers AccountPosts from newest-first statuses, honoring since,
// until, start time, page size and continuation.
type fakeAPI struct {
	t        *testing.T
	statuses []statusFixture
	queries  []APIQuery
	fail     map[int]error
}

func (f *fakeAPI) AccountPosts(_ context.Context, q APIQuery) (APIPage, error) {
	f.queries = append(f.queries, q)
	if err := f.fail[len(f.queries)]; err != nil {
		return APIPage{}, err
	}
	matched := []statusFixture{}
	for _, s := range f.statuses {
		if (q.SinceID == "" || idAfter(s.id, q.SinceID)) && (q.UntilID == "" || idAfter(q.UntilID, s.id)) {
			if start, err := time.Parse(time.RFC3339, q.StartTime); err == nil && s.at.Before(start) {
				continue
			}
			matched = append(matched, s)
		}
	}
	offset, _ := strconv.Atoi(q.Token)
	end := min(offset+q.Max, len(matched))
	next := ""
	if end < len(matched) {
		next = strconv.Itoa(end)
	}
	posts, read, _, err := APIPosts(apiAnswer(f.t, next, matched[offset:end]...), q.Account)
	return APIPage{Posts: posts, Next: next, Read: read}, err
}

// apiStatuses are n own statuses an hour apart, newest first.
func apiStatuses(n int) []statusFixture {
	out := []statusFixture{}
	for i := 1; i <= n; i++ {
		at := timelineNow.Add(-time.Duration(i) * time.Hour)
		out = append(out, statusFixture{id: statusAt(at), text: "post " + strconv.Itoa(i), at: at})
	}
	return out
}

func TestChainFallsThroughToTheAPIAndRecordsEveryMethod(t *testing.T) {
	statuses := apiStatuses(3)
	stored := statusAt(timelineNow.Add(-10 * time.Hour))
	s := &timelineServer{pages: map[string]*http.Response{"": response(503, "down")}}
	a := s.adapter(Options{BootstrapDays: 7, DiscoveryLimit: 50})
	api := &fakeAPI{t: t, statuses: statuses}
	a.API = api
	opts := a.Options
	opts.Chain = []string{DiscoveryTimeline, DiscoveryAPI, DiscoveryProfile}
	opts.CursorID, opts.KnownIDs = stored, map[string]bool{stored: true}
	opts.Gap = &Gap{Cursor: "fx-continuation", Floor: statusAt(timelineNow.Add(-20 * time.Hour))}
	opts.GapCeilings = map[string]string{opts.Gap.Floor: stored}
	opts.APIGate, opts.TimelineFailures, opts.APIBudget = 3, 2, 100
	got, err := a.FetchRecent(context.Background(), "kano", opts)
	if err != nil {
		t.Fatal(err)
	}
	// The third consecutive timeline failure opens the gate in this run.
	if got.Discovery != DiscoveryAPI || !got.TimelineFailed || s.profiles != 0 || got.APIRead != 3 || len(got.Posts) != 3 || len(got.FromListing) != 3 || got.RequestedCount != 0 {
		t.Fatalf("result=%+v profiles=%d", got, s.profiles)
	}
	if fmt.Sprint(got.Tried) != "[{timeline x_upstream_5xx} {api }]" || api.queries[0].SinceID != stored {
		t.Fatalf("tried=%v queries=%+v", got.Tried, api.queries)
	}
	// The head reached the cursor, so the gap is read exactly and closed.
	if got.Gap != nil || len(api.queries) != 2 || api.queries[1].SinceID != opts.Gap.Floor || api.queries[1].UntilID != stored {
		t.Fatalf("gap=%+v queries=%+v", got.Gap, api.queries)
	}
	if got.NextCursor != statuses[0].id {
		t.Fatalf("cursor=%s", got.NextCursor)
	}
}

func TestChainSkipsTheGatedOrCappedAPI(t *testing.T) {
	for name, tc := range map[string]struct {
		failures, gate, budget int
		api                    APIReader
		code                   string
	}{
		"gated":       {failures: 0, gate: 3, budget: 100, api: &fakeAPI{t: t}, code: string(ErrAPIGated)},
		"cap spent":   {failures: 5, gate: 3, budget: 4, api: &fakeAPI{t: t}, code: string(ErrAPIDailyCap)},
		"no reader":   {failures: 5, gate: 0, budget: 100, code: string(ErrAPIUnavailable)},
		"first fails": {failures: 5, gate: 3, budget: 100, api: &fakeAPI{t: t, fail: map[int]error{1: &Error{Code: "source_api_http_429"}}}, code: "source_api_http_429"},
	} {
		t.Run(name, func(t *testing.T) {
			s := &timelineServer{pages: map[string]*http.Response{"": response(503, "down")}}
			a := s.adapter(Options{BootstrapDays: 7, DiscoveryLimit: 50})
			a.API = tc.api
			opts := a.Options
			opts.Chain = []string{DiscoveryTimeline, DiscoveryAPI, DiscoveryProfile}
			opts.APIGate, opts.TimelineFailures, opts.APIBudget = tc.gate, tc.failures, tc.budget
			got, err := a.FetchRecent(context.Background(), "kano", opts)
			if err != nil || got.Discovery != DiscoveryProfile || s.profiles != 1 {
				t.Fatalf("result=%+v err=%v", got, err)
			}
			if fmt.Sprint(got.Tried) != "[{timeline x_upstream_5xx} {api "+tc.code+"} {profile }]" {
				t.Fatalf("tried=%v", got.Tried)
			}
		})
	}
}

func TestChainWithoutAServingMethodFailsAndKeepsTheGap(t *testing.T) {
	s := &timelineServer{pages: map[string]*http.Response{"": response(200, timelineBody("", statusAt(timelineNow)+"@other"))}}
	a := s.adapter(Options{BootstrapDays: 7, DiscoveryLimit: 50})
	opts := a.Options
	opts.Chain = []string{DiscoveryTimeline}
	opts.Gap = &Gap{Cursor: "fx", Floor: "1900000000000000000"}
	got, err := a.FetchRecent(context.Background(), "kano", opts)
	var coded *Error
	if !errors.As(err, &coded) || coded.Code != ErrEmptyListing || got.Discovery != "" || got.Gap != opts.Gap || !got.TimelineFailed {
		t.Fatalf("result=%+v err=%v", got, err)
	}
}

func TestOtherMethodsDropFxTwitterContinuations(t *testing.T) {
	s := &timelineServer{pages: map[string]*http.Response{"": response(503, "down")}}
	a := s.adapter(Options{BootstrapDays: 7, DiscoveryLimit: 50})
	opts := a.Options
	newest := statusAt(timelineNow.Add(-time.Hour))
	opts.CursorID, opts.KnownIDs = newest, map[string]bool{newest: true}
	opts.Gap = &Gap{Cursor: "fx-1", Floor: "1900000000000000010", Older: &Gap{Cursor: "fx-2", Floor: "1900000000000000001"}}
	got, err := a.FetchRecent(context.Background(), "kano", opts)
	if err != nil || got.Discovery != DiscoveryProfile {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	if got.Gap == nil || got.Gap.Cursor != "" || got.Gap.Floor != "1900000000000000010" || got.Gap.Older == nil || got.Gap.Older.Cursor != "" || got.Gap.Older.Floor != "1900000000000000001" {
		t.Fatalf("gap=%+v", got.Gap)
	}
}

func TestAPICapOpensAGapThatLaterReadsCloseExactly(t *testing.T) {
	statuses := apiStatuses(7)
	floor := statusAt(timelineNow.Add(-50 * time.Hour))
	api := &fakeAPI{t: t, statuses: statuses}
	a := (&timelineServer{}).adapter(Options{BootstrapDays: 7, DiscoveryLimit: 3})
	a.API = api
	opts := a.Options
	opts.Chain = []string{DiscoveryAPI}
	opts.CursorID, opts.KnownIDs, opts.APIBudget = floor, map[string]bool{floor: true}, 100
	read := []string{}
	for run := 0; run < 3; run++ {
		got, err := a.FetchRecent(context.Background(), "kano", opts)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range got.Posts {
			read = append(read, p.ID)
			opts.KnownIDs[p.ID] = true
		}
		// The store's ceiling is the oldest stored status above each floor.
		opts.Gap, opts.GapCeilings = got.Gap, map[string]string{}
		for gap := got.Gap; gap != nil; gap = gap.Older {
			ceiling := ""
			for id := range opts.KnownIDs {
				if idAfter(id, gap.Floor) && (ceiling == "" || idAfter(ceiling, id)) {
					ceiling = id
				}
			}
			opts.GapCeilings[gap.Floor] = ceiling
		}
		if got.NextCursor != "" {
			opts.CursorID = got.NextCursor
		}
	}
	want := []string{}
	for _, s := range statuses {
		want = append(want, s.id)
	}
	if strings.Join(read, ",") != strings.Join(want, ",") || opts.Gap != nil {
		t.Fatalf("read=%v want=%v gap=%+v queries=%+v", read, want, opts.Gap, api.queries)
	}
	// The daily budget ends a read short with a controlled note.
	opts.KnownIDs, opts.CursorID, opts.APIBudget, opts.DiscoveryLimit = map[string]bool{floor: true}, floor, 6, 50
	got, err := a.FetchRecent(context.Background(), "kano", opts)
	if err != nil || len(got.Posts) != 6 || got.APIRead != 6 || !strings.Contains(strings.Join(got.Notes, ","), string(ErrAPIDailyCap)) || got.Gap == nil {
		t.Fatalf("capped=%+v err=%v", got, err)
	}
}
