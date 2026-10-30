package history

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	ytsource "github.com/yexca/kano-navi/backend-go/internal/source/youtube"
)

// fakeUploads serves synthetic playlist pages by token and a synthetic feed.
type fakeUploads struct {
	pages   map[string][]ytsource.Video
	next    map[string]string
	fail    map[string]error
	feed    []ytsource.Video
	feedErr error
	tokens  []string
	feeds   int
	// asked records each DescribeVideos call's IDs; describeErr fails them.
	asked       []string
	describeErr string
	// formats records the sets each call was given.
	formats []*ytsource.FormatSets
}

func (f *fakeUploads) UploadsPage(_ context.Context, token string) ([]ytsource.Video, string, error) {
	f.tokens = append(f.tokens, token)
	if err := f.fail[token]; err != nil {
		return nil, "", err
	}
	return f.pages[token], f.next[token], nil
}

func (f *fakeUploads) FeedUploads(context.Context) ([]ytsource.Video, error) {
	f.feeds++
	return f.feed, f.feedErr
}

func (f *fakeUploads) DescribeVideos(_ context.Context, listed []ytsource.Video, stored map[string]ytsource.Video, formats *ytsource.FormatSets) ytsource.Described {
	f.formats = append(f.formats, formats)
	out := ytsource.Described{Videos: []ytsource.Video{}, Localized: map[string]map[string]string{}, Errors: []string{}, APIRequests: 1}
	ids := []string{}
	for _, video := range listed {
		ids = append(ids, video.ID)
		if f.describeErr == "" {
			video.DurationSeconds = 60
			out.Videos = append(out.Videos, video)
		}
	}
	if f.describeErr != "" {
		out.Errors = append(out.Errors, f.describeErr)
	}
	f.asked = append(f.asked, strings.Join(ids, ","))
	return out
}

func upload(id, at string) ytsource.Video {
	return ytsource.Video{ID: id, Title: "title " + id, PublishedAt: at, URL: "https://www.youtube.com/watch?v=" + id}
}

func uploadsFetcher(store Store, uploads Uploads, pages int) *Fetcher {
	fetcher := newFetcher(store, func(*http.Request) (*http.Response, error) {
		return nil, errors.New("uploads fetches never call the search endpoint")
	}, pages)
	fetcher.Uploads = uploads
	return fetcher
}

func uploadsAll() Window { return Window{Method: MethodAPI, Mode: ModeAll} }

func TestUploadsAPIAllPagesThePlaylistToItsEnd(t *testing.T) {
	store := &memoryStore{stored: map[string]bool{"BBBBBBBBBBB": true}}
	uploads := &fakeUploads{
		pages: map[string][]ytsource.Video{
			"":   {upload("AAAAAAAAAAA", "2026-03-09T03:00:00Z"), upload("BBBBBBBBBBB", "2026-03-08T03:00:00Z")},
			"p2": {upload("BBBBBBBBBBB", "2026-03-08T03:00:00Z"), upload("CCCCCCCCCCC", "2026-03-07T03:00:00Z")},
			"p3": {upload("DDDDDDDDDDD", "2020-03-07T03:00:00Z")},
		},
		next: map[string]string{"": "p2", "p2": "p3"},
	}
	fetcher := uploadsFetcher(store, uploads, 2)
	request := Request{Source: SourceYoutube, Account: testChannel, Window: uploadsAll()}
	result, err := fetcher.FetchUploads(context.Background(), request)
	// An entry a shifted playlist repeats on the next page is listed once.
	if err != nil || result.Requested != 2 || result.Listed != 3 || result.New != 2 || result.Stored != 1 || !result.HasMore {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(result.Items) != 2 || result.Items[0] != (Item{ID: "AAAAAAAAAAA", Title: "title AAAAAAAAAAA"}) || len(result.Errors) != 1 {
		t.Fatalf("items=%+v errors=%v", result.Items, result.Errors)
	}
	if first := store.pages[0]; !first.KeepStored || first.Next != "p2" || first.Checkpoint != "" || first.CheckpointKey == "" {
		t.Fatalf("first page=%+v", first)
	}
	result, err = fetcher.FetchUploads(context.Background(), request)
	if err != nil || result.HasMore || result.Requested != 1 || result.New != 1 || len(result.Errors) != 0 {
		t.Fatalf("resume=%+v err=%v", result, err)
	}
	if strings.Join(uploads.tokens, ",") != ",p2,p3" || store.pages[2].Discovered != 4 {
		t.Fatalf("tokens=%v pages=%+v", uploads.tokens, store.pages)
	}
}

func TestUploadsAPICountStopsAtTheNewestAndRefreshesOnRequest(t *testing.T) {
	store := &memoryStore{}
	uploads := &fakeUploads{
		pages: map[string][]ytsource.Video{
			"":   {upload("AAAAAAAAAAA", "2026-03-09T03:00:00Z"), upload("BBBBBBBBBBB", "2026-03-08T03:00:00Z")},
			"p2": {upload("CCCCCCCCCCC", "2026-03-07T03:00:00Z"), upload("DDDDDDDDDDD", "2026-03-06T03:00:00Z")},
		},
		next: map[string]string{"": "p2", "p2": "p3"},
	}
	window := Window{Method: MethodAPI, Mode: ModeCount, Count: 3, Refresh: true}
	result, err := uploadsFetcher(store, uploads, 4).FetchUploads(context.Background(), Request{Source: SourceYoutube, Account: testChannel, Window: window})
	if err != nil || result.Listed != 3 || result.HasMore || result.Requested != 2 || len(result.Errors) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if last := store.pages[1]; last.KeepStored || last.Next != "" || len(last.Videos) != 1 || last.Videos[0].ID != "CCCCCCCCCCC" {
		t.Fatalf("last page=%+v", last)
	}
}

func TestUploadsRSSTakesTheNewestFeedEntriesWithoutACheckpoint(t *testing.T) {
	store := &memoryStore{}
	uploads := &fakeUploads{feed: []ytsource.Video{
		upload("BBBBBBBBBBB", "2026-03-08T03:00:00Z"), upload("AAAAAAAAAAA", "2026-03-09T03:00:00Z"), upload("CCCCCCCCCCC", "2026-03-07T03:00:00Z"),
	}}
	window := Window{Method: MethodRSS, Mode: ModeCount, Count: 2}
	result, err := uploadsFetcher(store, uploads, 4).FetchUploads(context.Background(), Request{Source: SourceYoutube, Account: testChannel, Window: window})
	if err != nil || result.Listed != 2 || result.New != 2 || result.Requested != 1 || result.HasMore || len(uploads.tokens) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if page := store.pages[0]; page.CheckpointKey != "" || !page.KeepStored || page.Videos[0].ID != "AAAAAAAAAAA" || page.Videos[1].ID != "BBBBBBBBBBB" {
		t.Fatalf("page=%+v", page)
	}
	if len(store.states) != 0 {
		t.Fatalf("states=%v", store.states)
	}
}

func TestUploadsFailuresKeepControlledCodesWithoutFallback(t *testing.T) {
	ctx := context.Background()
	quota := &ytsource.Error{Code: "youtube_api_forbidden"}
	// The API method fails with its code; it never reads the feed instead.
	uploads := &fakeUploads{fail: map[string]error{"": quota}, feed: []ytsource.Video{upload("AAAAAAAAAAA", "2026-03-09T03:00:00Z")}}
	store := &memoryStore{}
	_, err := uploadsFetcher(store, uploads, 4).FetchUploads(ctx, Request{Source: SourceYoutube, Account: testChannel, Window: uploadsAll()})
	var controlled *Error
	if !errors.As(err, &controlled) || controlled.Code != "youtube_api_forbidden" || uploads.feeds != 0 || len(store.pages) != 0 {
		t.Fatalf("err=%v feeds=%d", err, uploads.feeds)
	}
	// The RSS method fails with the feed's code.
	uploads = &fakeUploads{feedErr: &ytsource.Error{Code: "youtube_feed_upstream_5xx"}}
	rss := Window{Method: MethodRSS, Mode: ModeCount, Count: 5}
	_, err = uploadsFetcher(store, uploads, 4).FetchUploads(ctx, Request{Source: SourceYoutube, Account: testChannel, Window: rss})
	if !errors.As(err, &controlled) || controlled.Code != "youtube_feed_upstream_5xx" || len(uploads.tokens) != 0 {
		t.Fatalf("rss err=%v", err)
	}
	// A later page failure keeps the committed page and its token.
	uploads = &fakeUploads{
		pages: map[string][]ytsource.Video{"": {upload("AAAAAAAAAAA", "2026-03-09T03:00:00Z")}},
		next:  map[string]string{"": "p2"},
		fail:  map[string]error{"p2": quota, "stale": quota},
	}
	store = &memoryStore{}
	result, err := uploadsFetcher(store, uploads, 4).FetchUploads(ctx, Request{Source: SourceYoutube, Account: testChannel, Window: uploadsAll()})
	if err != nil || !result.HasMore || len(result.Errors) != 1 || result.Errors[0] != "youtube_api_forbidden" || store.pages[0].Next != "p2" {
		t.Fatalf("later=%+v err=%v", result, err)
	}
	// A stale token restarts at the newest page.
	key := uploadsAll().checkpointKey(testChannel)
	store = &memoryStore{states: map[string]State{"youtube-window/" + key: {Found: true, CursorID: "stale", Metadata: map[string]any{"discovered": float64(9)}}}}
	uploads.tokens = nil
	result, err = uploadsFetcher(store, uploads, 4).FetchUploads(ctx, Request{Source: SourceYoutube, Account: testChannel, Window: uploadsAll()})
	if err != nil || strings.Join(uploads.tokens, ",") != "stale,,p2" || store.pages[0].Discovered != 1 {
		t.Fatalf("restart=%+v tokens=%v err=%v", result, uploads.tokens, err)
	}
	// Retired selections never reach the listing.
	uploads = &fakeUploads{}
	retired := Window{Mode: ModeBefore, Days: 7}
	if _, err = uploadsFetcher(&memoryStore{}, uploads, 4).FetchUploads(ctx, Request{Source: SourceYoutube, Account: testChannel, Window: retired}); !errors.Is(err, ErrRetired) || len(uploads.tokens) != 0 {
		t.Fatalf("retired err=%v", err)
	}
}

func TestUploadsAPIDescribesNewVideosAndStoredOnesOnRefresh(t *testing.T) {
	pages := map[string][]ytsource.Video{
		"":   {upload("AAAAAAAAAAA", "2026-03-09T03:00:00Z"), upload("BBBBBBBBBBB", "2026-03-08T03:00:00Z")},
		"p2": {upload("CCCCCCCCCCC", "2026-03-07T03:00:00Z")},
	}
	next := map[string]string{"": "p2"}
	store := &memoryStore{stored: map[string]bool{"BBBBBBBBBBB": true}}
	uploads := &fakeUploads{pages: pages, next: next}
	result, err := uploadsFetcher(store, uploads, 4).FetchUploads(context.Background(), Request{Source: SourceYoutube, Account: testChannel, Window: uploadsAll()})
	// Each page's new videos get one details call; the stored one is left.
	if err != nil || strings.Join(uploads.asked, ";") != "AAAAAAAAAAA;CCCCCCCCCCC" || result.Inspected != 2 || result.APIRequests != 4 || len(result.Errors) != 0 {
		t.Fatalf("result=%+v asked=%v err=%v", result, uploads.asked, err)
	}
	if first := store.pages[0]; len(first.Described) != 1 || first.Described[0].ID != "AAAAAAAAAAA" || first.Described[0].DurationSeconds != 60 {
		t.Fatalf("described=%+v", first.Described)
	}
	// A refresh describes the stored videos too.
	store = &memoryStore{stored: map[string]bool{"BBBBBBBBBBB": true}}
	uploads = &fakeUploads{pages: pages, next: next}
	refresh := Window{Method: MethodAPI, Mode: ModeAll, Refresh: true}
	if _, err = uploadsFetcher(store, uploads, 4).FetchUploads(context.Background(), Request{Source: SourceYoutube, Account: testChannel, Window: refresh}); err != nil || strings.Join(uploads.asked, ";") != "AAAAAAAAAAA,BBBBBBBBBBB;CCCCCCCCCCC" {
		t.Fatalf("refresh asked=%v err=%v", uploads.asked, err)
	}
	// A failed details call keeps the listed videos and the checkpoint and
	// reports its code; the fetch goes on.
	store = &memoryStore{}
	uploads = &fakeUploads{pages: pages, next: next, describeErr: "youtube_api_forbidden"}
	result, err = uploadsFetcher(store, uploads, 4).FetchUploads(context.Background(), Request{Source: SourceYoutube, Account: testChannel, Window: uploadsAll()})
	if err != nil || result.New != 3 || result.Inspected != 0 || result.HasMore || strings.Join(result.Errors, ",") != "youtube_api_forbidden,youtube_api_forbidden" {
		t.Fatalf("failed details=%+v err=%v", result, err)
	}
	if len(store.pages) != 2 || store.pages[0].Next != "p2" || len(store.pages[0].Videos) != 2 || len(store.pages[0].Described) != 0 {
		t.Fatalf("pages=%+v", store.pages)
	}
	// The RSS method lists only.
	uploads = &fakeUploads{feed: []ytsource.Video{upload("AAAAAAAAAAA", "2026-03-09T03:00:00Z")}}
	result, err = uploadsFetcher(&memoryStore{}, uploads, 4).FetchUploads(context.Background(), Request{Source: SourceYoutube, Account: testChannel, Window: Window{Method: MethodRSS, Mode: ModeCount, Count: 5}})
	if err != nil || len(uploads.asked) != 0 || result.APIRequests != 0 || result.Inspected != 0 {
		t.Fatalf("rss=%+v asked=%v err=%v", result, uploads.asked, err)
	}
}

func TestUploadsAllSharesOneSetOfFormatListsAcrossItsPages(t *testing.T) {
	pages := map[string][]ytsource.Video{
		"":   {upload("AAAAAAAAAAA", "2026-03-09T03:00:00Z")},
		"p2": {upload("CCCCCCCCCCC", "2026-03-07T03:00:00Z")},
	}
	uploads := &fakeUploads{pages: pages, next: map[string]string{"": "p2"}}
	if _, err := uploadsFetcher(&memoryStore{}, uploads, 4).FetchUploads(context.Background(), Request{Source: SourceYoutube, Account: testChannel, Window: uploadsAll()}); err != nil {
		t.Fatal(err)
	}
	if len(uploads.formats) != 2 || uploads.formats[0] == nil || uploads.formats[0] != uploads.formats[1] || uploads.formats[0].Pages != formatListPages {
		t.Fatalf("formats=%v", uploads.formats)
	}
	// A count fetch reads only the newest page of each list, as the recent
	// step does.
	uploads = &fakeUploads{pages: pages, next: map[string]string{"": "p2"}}
	if _, err := uploadsFetcher(&memoryStore{}, uploads, 4).FetchUploads(context.Background(), Request{Source: SourceYoutube, Account: testChannel, Window: Window{Method: MethodAPI, Mode: ModeCount, Count: 2}}); err != nil {
		t.Fatal(err)
	}
	if len(uploads.formats) != 2 || uploads.formats[0] != nil || uploads.formats[1] != nil {
		t.Fatalf("count formats=%v", uploads.formats)
	}
}
