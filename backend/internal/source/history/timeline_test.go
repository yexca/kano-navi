package history

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	xsource "github.com/yexca/kano-navi/backend/internal/source/x"
)

// fakeTimeline serves synthetic pages by cursor and records each request.
type fakeTimeline struct {
	pages   map[string]xsource.TimelineStatuses
	fail    map[string]error
	cursors []string
	counts  []int
}

func (f *fakeTimeline) TimelineStatuses(_ context.Context, account, cursor string, count int) (xsource.TimelineStatuses, error) {
	f.cursors = append(f.cursors, cursor)
	f.counts = append(f.counts, count)
	if account != testAccount {
		return xsource.TimelineStatuses{}, &xsource.Error{Code: xsource.ErrInvalidResponse}
	}
	if err := f.fail[cursor]; err != nil {
		return xsource.TimelineStatuses{}, err
	}
	if page, ok := f.pages[cursor]; ok {
		return page, nil
	}
	return xsource.TimelineStatuses{}, &xsource.Error{Code: xsource.ErrHTTP, Status: 404}
}

// timelineStatus is one synthetic own status; an empty text marks one the
// adapter could not normalize.
func timelineStatus(id, at, text string, media ...string) xsource.TimelineStatus {
	status := xsource.TimelineStatus{ID: id, PublishedAt: at}
	if text != "" {
		status.Post = &xsource.Post{ID: id, AccountHandle: testAccount, Text: text, PublishedAt: at, URL: "https://x.com/" + testAccount + "/status/" + id, MediaURLs: media}
	}
	return status
}

func timelinePage(next string, statuses ...xsource.TimelineStatus) xsource.TimelineStatuses {
	return xsource.TimelineStatuses{Statuses: statuses, Next: next}
}

func timelineFetcher(store Store, timeline Timeline, pages int) *Fetcher {
	fetcher := newFetcher(store, func(*http.Request) (*http.Response, error) {
		return nil, errors.New("timeline fetches never call the source APIs")
	}, pages)
	fetcher.Timeline = timeline
	return fetcher
}

func timelineAll() Window { return Window{Method: MethodTimeline, Mode: ModeAll} }

func timelineCount(count int) Window {
	return Window{Method: MethodTimeline, Mode: ModeCount, Count: count}
}

func TestTimelineAllPagesToTheEndAndResumes(t *testing.T) {
	store := &memoryStore{stored: map[string]bool{"1900000000000000003": true}}
	timeline := &fakeTimeline{pages: map[string]xsource.TimelineStatuses{
		"":   timelinePage("c2", timelineStatus("1900000000000000005", "2026-03-05T03:00:00Z", "weekly schedule", "https://media.example.test/board.png")),
		"c2": timelinePage("c3", timelineStatus("1900000000000000003", "2026-03-03T03:00:00Z", "stored already")),
		"c3": timelinePage("", timelineStatus("1900000000000000001", "2025-01-01T03:00:00Z", "any age counts")),
	}}
	fetcher := timelineFetcher(store, timeline, 2)
	request := Request{Source: SourceX, Account: testAccount, Window: timelineAll()}
	result, err := fetcher.FetchTimeline(context.Background(), request)
	if err != nil || result.Requested != 2 || result.Listed != 2 || result.New != 1 || result.Stored != 1 || result.ScheduleAssets != 1 || !result.HasMore {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(result.Errors) != 1 || result.Errors[0] != "history_page_budget_reached; repeat this fetch to continue" {
		t.Fatalf("errors=%v", result.Errors)
	}
	if len(result.Items) != 1 || result.Items[0] != (Item{ID: "1900000000000000005", Title: "weekly schedule", Account: testAccount}) {
		t.Fatalf("items=%+v", result.Items)
	}
	first := store.pages[0]
	if first.Checkpoint != TimelineCheckpoint || !first.KeepStored || first.Next != "c2" || first.Window != timelineAll() {
		t.Fatalf("first page=%+v", first)
	}
	// Repeating the fetch continues at the committed cursor, and the end of
	// the timeline completes it without an error.
	result, err = fetcher.FetchTimeline(context.Background(), request)
	if err != nil || result.HasMore || result.Requested != 1 || result.Listed != 1 || result.New != 1 || len(result.Errors) != 0 {
		t.Fatalf("resume=%+v err=%v", result, err)
	}
	if last := store.pages[len(store.pages)-1]; last.Next != "" || last.Discovered != 3 {
		t.Fatalf("last page=%+v", last)
	}
	if strings.Join(timeline.cursors, ",") != ",c2,c3" || timeline.counts[0] != xsource.MaxTimelinePageSize {
		t.Fatalf("cursors=%v counts=%v", timeline.cursors, timeline.counts)
	}
	if _, ok := store.states["x-window/"+first.CheckpointKey]; ok {
		t.Fatal("a timeline cursor must not become an API next_token")
	}
	// A completed fetch starts again at the newest page.
	if _, err = fetcher.FetchTimeline(context.Background(), request); err != nil || timeline.cursors[3] != "" {
		t.Fatalf("restart cursors=%v err=%v", timeline.cursors, err)
	}
}

func TestTimelineCountStopsAfterTheNewestStatuses(t *testing.T) {
	store := &memoryStore{states: map[string]State{
		// The incremental cursor is far ahead; a fetch never stops at it.
		"x/" + testAccount: {Found: true, CursorID: "1900000000000000009"},
	}}
	timeline := &fakeTimeline{pages: map[string]xsource.TimelineStatuses{
		"":   timelinePage("c2", timelineStatus("1900000000000000009", "2026-03-09T03:00:00Z", "newest"), timelineStatus("1900000000000000008", "2026-03-08T03:00:00Z", "")),
		"c2": timelinePage("c3", timelineStatus("1900000000000000007", "2026-01-07T03:00:00Z", "any age counts"), timelineStatus("1900000000000000006", "2026-01-06T03:00:00Z", "beyond the count")),
	}}
	result, err := timelineFetcher(store, timeline, 4).FetchTimeline(context.Background(), Request{Source: SourceX, Account: testAccount, Window: timelineCount(3)})
	if err != nil || result.Requested != 2 || result.Listed != 3 || result.New != 2 || result.Failed != 1 || result.HasMore {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	// An entry without usable content is counted and reported, never stored.
	if len(result.Errors) != 1 || result.Errors[0] != "1900000000000000008: x_invalid_response" {
		t.Fatalf("errors=%v", result.Errors)
	}
	if strings.Join(timeline.cursors, ",") != ",c2" || timeline.counts[0] != 3 || timeline.counts[1] != 1 {
		t.Fatalf("cursors=%v counts=%v", timeline.cursors, timeline.counts)
	}
	if last := store.pages[1]; last.Next != "" || len(last.Posts) != 1 || last.Posts[0].ID != "1900000000000000007" || last.Window.Count != 3 {
		t.Fatalf("last page=%+v", last)
	}
	// A timeline that ends before the count says so.
	store = &memoryStore{}
	only := timelinePage("", timelineStatus("1900000000000000005", "2026-03-05T03:00:00Z", "only"))
	result, err = timelineFetcher(store, &fakeTimeline{pages: map[string]xsource.TimelineStatuses{"": only}}, 4).
		FetchTimeline(context.Background(), Request{Source: SourceX, Account: testAccount, Window: timelineCount(3)})
	if err != nil || result.HasMore || len(result.Errors) != 1 || result.Errors[0] != "x_timeline_end; the timeline lists no older posts" {
		t.Fatalf("end=%+v err=%v", result, err)
	}
}

func TestTimelineFreshnessKeepsOriginalHeadAcrossResumeAndFailedCursorReset(t *testing.T) {
	store := &memoryStore{}
	timeline := &fakeTimeline{pages: map[string]xsource.TimelineStatuses{
		"":      timelinePage("older", timelineStatus("1900000000000000003", "2026-03-09T03:00:00Z", "newest")),
		"older": timelinePage("stale", timelineStatus("1900000000000000002", "2026-03-08T03:00:00Z", "older")),
	}, fail: map[string]error{"stale": &xsource.Error{Code: xsource.ErrHTTP, Status: 400}}}
	fetcher := timelineFetcher(store, timeline, 1)
	request := Request{Source: SourceX, Account: testAccount, Window: timelineCount(3)}
	if result, err := fetcher.FetchTimeline(context.Background(), request); err != nil || !result.HasMore {
		t.Fatalf("head: %+v %v", result, err)
	}
	want := formatTime(fixedNow())
	if store.pages[0].HeadFetchedAt != want || store.pages[0].CheckpointOnly {
		t.Fatalf("head freshness=%+v", store.pages[0])
	}
	// A count fetch has no expiry. Reading its older continuation days later
	// must not claim the newest source content was read again.
	fetcher.Now = func() time.Time { return fixedNow().Add(72 * time.Hour) }
	if result, err := fetcher.FetchTimeline(context.Background(), request); err != nil || !result.HasMore {
		t.Fatalf("resume: %+v %v", result, err)
	}
	if resumed := store.pages[1]; resumed.HeadFetchedAt != want || resumed.CheckpointOnly {
		t.Fatalf("resumed freshness=%+v", resumed)
	}
	if _, err := fetcher.FetchTimeline(context.Background(), request); err == nil {
		t.Fatal("stale cursor was not reported")
	}
	if reset := store.pages[2]; !reset.CheckpointOnly || reset.Next != "" || reset.HeadFetchedAt != want {
		t.Fatalf("failed reset claimed success=%+v", reset)
	}
	// After clearing the stale cursor, a real successful head read advances it.
	if _, err := fetcher.FetchTimeline(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if head := store.pages[3]; head.CheckpointOnly || head.HeadFetchedAt != formatTime(fetcher.Now()) {
		t.Fatalf("fresh head did not advance=%+v", head)
	}
}

func TestTimelineFailuresRestartStaleCursorsAndKeepCommittedPages(t *testing.T) {
	ctx := context.Background()
	request := Request{Source: SourceX, Account: testAccount, Window: timelineAll()}
	first := timelineStatus("1900000000000000005", "2026-03-05T03:00:00Z", "kept")
	unavailable := &xsource.Error{Code: xsource.ErrServer, Status: 503}

	// A failed first page commits nothing and keeps the adapter's code.
	store := &memoryStore{}
	_, err := timelineFetcher(store, &fakeTimeline{fail: map[string]error{"": unavailable}}, 4).FetchTimeline(ctx, request)
	var controlled *Error
	if !errors.As(err, &controlled) || controlled.Code != "x_upstream_5xx" || controlled.Requested != 1 || len(store.pages) != 0 {
		t.Fatalf("err=%v pages=%d", err, len(store.pages))
	}

	// A resumed cursor that fails starts again from the newest page.
	key := timelineAll().checkpointKey(testAccount)
	store = &memoryStore{states: map[string]State{TimelineCheckpoint + "/" + key: {Found: true, CursorID: "stale", Metadata: map[string]any{
		"window": map[string]any{"method": "timeline", "mode": "all"}, "discovered": float64(4),
	}}}}
	timeline := &fakeTimeline{fail: map[string]error{"stale": &xsource.Error{Code: xsource.ErrHTTP, Status: 400}}, pages: map[string]xsource.TimelineStatuses{"": timelinePage("", first)}}
	result, err := timelineFetcher(store, timeline, 4).FetchTimeline(ctx, request)
	if err != nil || result.Requested != 2 || result.Listed != 1 || result.HasMore || len(result.Errors) != 0 {
		t.Fatalf("restart=%+v err=%v", result, err)
	}
	if strings.Join(timeline.cursors, ",") != "stale," || store.pages[0].Next != "" || len(store.pages[0].Posts) != 0 || store.pages[1].Discovered != 1 {
		t.Fatalf("cursors=%v pages=%+v", timeline.cursors, store.pages)
	}

	// A later failure keeps the committed page and its cursor.
	store = &memoryStore{}
	timeline = &fakeTimeline{fail: map[string]error{"c2": unavailable}, pages: map[string]xsource.TimelineStatuses{"": timelinePage("c2", first)}}
	result, err = timelineFetcher(store, timeline, 4).FetchTimeline(ctx, request)
	if err != nil || !result.HasMore || len(result.Errors) != 1 || result.Errors[0] != "x_upstream_5xx" || len(store.pages) != 1 || store.pages[0].Next != "c2" {
		t.Fatalf("later=%+v err=%v", result, err)
	}

	// Other methods, retired selections and YouTube never reach the network.
	timeline = &fakeTimeline{}
	fetcher := timelineFetcher(&memoryStore{}, timeline, 4)
	for _, window := range []Window{
		{Method: MethodAPI, Mode: ModeCount, Count: 5},
		{Mode: ModeCount, Count: 5},
		{Method: MethodTimeline, Mode: ModeRange, StartDate: "2026-03-01", EndDate: "2026-03-07"},
	} {
		if _, err = fetcher.FetchTimeline(ctx, Request{Source: SourceX, Account: testAccount, Window: window}); !errors.Is(err, ErrRetired) {
			t.Fatalf("%+v err=%v", window, err)
		}
	}
	if _, err = fetcher.FetchTimeline(ctx, Request{Source: SourceYoutube, Account: testChannel, Window: timelineAll()}); err == nil {
		t.Fatal("youtube fetch accepted")
	}
	if len(timeline.cursors) != 0 {
		t.Fatalf("requests=%v", timeline.cursors)
	}
}
