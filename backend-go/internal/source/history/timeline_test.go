package history

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	xsource "github.com/yexca/kano-navi/backend-go/internal/source/x"
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
		return nil, errors.New("timeline windows never call the source APIs")
	}, pages)
	fetcher.Timeline = timeline
	return fetcher
}

func TestTimelineRangePagesBackToItsStartAndResumes(t *testing.T) {
	store := &memoryStore{}
	timeline := &fakeTimeline{pages: map[string]xsource.TimelineStatuses{
		"": timelinePage("c2",
			timelineStatus("1900000000000000009", "2026-03-09T03:00:00Z", "newer than the window"),
			timelineStatus("1900000000000000005", "2026-03-05T03:00:00Z", "weekly schedule", "https://media.example.test/board.png")),
		"c2": timelinePage("c3", timelineStatus("1900000000000000003", "2026-03-03T03:00:00Z", "in the window")),
		// The window's first instant is inside it; the older status ends it.
		"c3": timelinePage("c4",
			timelineStatus("1900000000000000002", "2026-02-28T15:00:00Z", "first instant"),
			timelineStatus("1900000000000000001", "2026-02-27T03:00:00Z", "before the window")),
	}}
	fetcher := timelineFetcher(store, timeline, 2)
	request := Request{Source: SourceX, Account: testAccount, Window: Window{Mode: ModeRange, StartDate: "2026-03-01", EndDate: "2026-03-07"}}
	result, err := fetcher.FetchTimeline(context.Background(), request)
	if err != nil || result.Requested != 2 || result.Discovered != 2 || result.Count != 2 || result.ScheduleAssets != 1 || !result.HasMore {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(result.Errors) != 1 || result.Errors[0] != "history_page_budget_reached; repeat this fetch to continue" {
		t.Fatalf("errors=%v", result.Errors)
	}
	first := store.pages[0]
	if first.Checkpoint != TimelineCheckpoint || !first.KeepStored || first.Next != "c2" || len(first.Posts) != 1 || first.Posts[0].ID != "1900000000000000005" {
		t.Fatalf("first page=%+v", first)
	}
	if first.Window.StartTime != "2026-02-28T15:00:00.000Z" || first.Window.EndTime != "2026-03-07T15:00:00.000Z" {
		t.Fatalf("window=%+v", first.Window)
	}
	// Repeating the window continues at the committed cursor.
	result, err = fetcher.FetchTimeline(context.Background(), request)
	if err != nil || result.HasMore || result.Requested != 1 || result.Discovered != 1 || len(result.Errors) != 0 {
		t.Fatalf("resume=%+v err=%v", result, err)
	}
	last := store.pages[len(store.pages)-1]
	if last.Next != "" || last.Discovered != 3 || len(last.Posts) != 1 || last.Posts[0].ID != "1900000000000000002" {
		t.Fatalf("last page=%+v", last)
	}
	if strings.Join(timeline.cursors, ",") != ",c2,c3" || timeline.counts[0] != xsource.MaxTimelinePageSize {
		t.Fatalf("cursors=%v counts=%v", timeline.cursors, timeline.counts)
	}
	if _, ok := store.states["x-window/"+first.CheckpointKey]; ok {
		t.Fatal("a timeline cursor must not become an API next_token")
	}
}

func TestTimelineRecentWindowIgnoresTheIncrementalCursor(t *testing.T) {
	key := Window{Mode: ModeRecent, Days: 3}.checkpointKey(testAccount)
	store := &memoryStore{states: map[string]State{
		// The incremental cursor is far ahead; a window never stops at it.
		"x/" + testAccount: {Found: true, CursorID: "1900000000000000009"},
		// A stale rolling interval is dropped instead of resumed.
		TimelineCheckpoint + "/" + key: {Found: true, CursorID: "stale", Metadata: map[string]any{"window": map[string]any{
			"mode": "recent", "days": float64(3), "startTime": "2026-02-20T00:00:00.000Z", "endTime": "2026-02-23T00:00:00.000Z",
		}, "discovered": float64(5)}},
	}}
	timeline := &fakeTimeline{pages: map[string]xsource.TimelineStatuses{
		"":   timelinePage("c2", timelineStatus("1900000000000000009", "2026-03-09T03:00:00Z", "stored by the timed sync"), timelineStatus("1900000000000000008", "2026-03-08T03:00:00Z", "missed")),
		"c2": timelinePage("c3", timelineStatus("1900000000000000007", "2026-03-07T03:00:00Z", "missed"), timelineStatus("1900000000000000006", "2026-03-06T03:00:00Z", "too old")),
	}}
	result, err := timelineFetcher(store, timeline, 4).FetchTimeline(context.Background(), Request{Source: SourceX, Account: testAccount, Window: Window{Mode: ModeRecent, Days: 3}})
	if err != nil || result.Requested != 2 || result.Discovered != 3 || result.HasMore || len(result.Errors) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if strings.Join(timeline.cursors, ",") != ",c2" || store.pages[1].Discovered != 3 || result.Window.StartTime != "2026-03-07T00:00:00.000Z" {
		t.Fatalf("cursors=%v pages=%+v", timeline.cursors, store.pages)
	}
}

func TestTimelineCountStopsAfterTheNewestStatuses(t *testing.T) {
	store := &memoryStore{}
	timeline := &fakeTimeline{pages: map[string]xsource.TimelineStatuses{
		"":   timelinePage("c2", timelineStatus("1900000000000000009", "2026-03-09T03:00:00Z", "newest"), timelineStatus("1900000000000000008", "2026-03-08T03:00:00Z", "")),
		"c2": timelinePage("c3", timelineStatus("1900000000000000007", "2026-01-07T03:00:00Z", "any age counts"), timelineStatus("1900000000000000006", "2026-01-06T03:00:00Z", "beyond the count")),
	}}
	result, err := timelineFetcher(store, timeline, 4).FetchTimeline(context.Background(), Request{Source: SourceX, Account: testAccount, Window: Window{Mode: ModeCount, Count: 3}})
	if err != nil || result.Requested != 2 || result.Discovered != 3 || result.Count != 2 || result.HasMore {
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
}

func TestTimelineFreshnessKeepsOriginalHeadAcrossResumeAndFailedCursorReset(t *testing.T) {
	store := &memoryStore{}
	timeline := &fakeTimeline{pages: map[string]xsource.TimelineStatuses{
		"":      timelinePage("older", timelineStatus("1900000000000000003", "2026-03-09T03:00:00Z", "newest")),
		"older": timelinePage("stale", timelineStatus("1900000000000000002", "2026-03-08T03:00:00Z", "older")),
	}, fail: map[string]error{"stale": &xsource.Error{Code: xsource.ErrHTTP, Status: 400}}}
	fetcher := timelineFetcher(store, timeline, 1)
	request := Request{Source: SourceX, Account: testAccount, Window: Window{Mode: ModeCount, Count: 3}}
	if result, err := fetcher.FetchTimeline(context.Background(), request); err != nil || !result.HasMore {
		t.Fatalf("head: %+v %v", result, err)
	}
	want := formatTime(fixedNow())
	if store.pages[0].HeadFetchedAt != want || store.pages[0].CheckpointOnly {
		t.Fatalf("head freshness=%+v", store.pages[0])
	}
	// A count window has no expiry. Reading its older continuation days later
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
	window := Window{Mode: ModeRange, StartDate: "2026-03-01", EndDate: "2026-03-07"}
	request := Request{Source: SourceX, Account: testAccount, Window: window}
	inside := timelineStatus("1900000000000000005", "2026-03-05T03:00:00Z", "kept")
	older := timelineStatus("1900000000000000001", "2026-02-27T03:00:00Z", "before the window")
	unavailable := &xsource.Error{Code: xsource.ErrServer, Status: 503}

	// A failed first page commits nothing and keeps the adapter's code.
	store := &memoryStore{}
	_, err := timelineFetcher(store, &fakeTimeline{fail: map[string]error{"": unavailable}}, 4).FetchTimeline(ctx, request)
	var controlled *Error
	if !errors.As(err, &controlled) || controlled.Code != "x_upstream_5xx" || controlled.Requested != 1 || len(store.pages) != 0 {
		t.Fatalf("err=%v pages=%d", err, len(store.pages))
	}

	// A resumed cursor that fails starts again from the newest page.
	key := window.checkpointKey(testAccount)
	store = &memoryStore{states: map[string]State{TimelineCheckpoint + "/" + key: {Found: true, CursorID: "stale", Metadata: map[string]any{"window": map[string]any{
		"mode": "range", "startDate": "2026-03-01", "endDate": "2026-03-07", "startTime": "2026-02-28T15:00:00.000Z", "endTime": "2026-03-07T15:00:00.000Z",
	}, "discovered": float64(4)}}}}
	timeline := &fakeTimeline{fail: map[string]error{"stale": &xsource.Error{Code: xsource.ErrHTTP, Status: 400}}, pages: map[string]xsource.TimelineStatuses{"": timelinePage("c2", inside, older)}}
	result, err := timelineFetcher(store, timeline, 4).FetchTimeline(ctx, request)
	if err != nil || result.Requested != 2 || result.Discovered != 1 || result.HasMore || len(result.Errors) != 0 {
		t.Fatalf("restart=%+v err=%v", result, err)
	}
	if strings.Join(timeline.cursors, ",") != "stale," || store.pages[0].Next != "" || len(store.pages[0].Posts) != 0 || store.pages[1].Discovered != 1 {
		t.Fatalf("cursors=%v pages=%+v", timeline.cursors, store.pages)
	}

	// A later failure keeps the committed page and its cursor.
	store = &memoryStore{}
	timeline = &fakeTimeline{fail: map[string]error{"c2": unavailable}, pages: map[string]xsource.TimelineStatuses{"": timelinePage("c2", inside)}}
	result, err = timelineFetcher(store, timeline, 4).FetchTimeline(ctx, request)
	if err != nil || !result.HasMore || len(result.Errors) != 1 || result.Errors[0] != "x_upstream_5xx" || len(store.pages) != 1 || store.pages[0].Next != "c2" {
		t.Fatalf("later=%+v err=%v", result, err)
	}

	// A timeline that ends before the window's start says so.
	store = &memoryStore{}
	result, err = timelineFetcher(store, &fakeTimeline{pages: map[string]xsource.TimelineStatuses{"": timelinePage("", inside)}}, 4).FetchTimeline(ctx, request)
	if err != nil || result.HasMore || len(result.Errors) != 1 || result.Errors[0] != "x_timeline_end; the timeline lists no older posts" {
		t.Fatalf("end=%+v err=%v", result, err)
	}

	// Backfills, YouTube and a missing timeline never reach the network.
	timeline = &fakeTimeline{}
	fetcher := timelineFetcher(&memoryStore{}, timeline, 4)
	if _, err = fetcher.FetchTimeline(ctx, Request{Source: SourceX, Account: testAccount, Window: Window{Mode: ModeBefore, Days: 7}}); err == nil || err.Error() != "X_API_BEARER_TOKEN is required for history search" {
		t.Fatalf("before err=%v", err)
	}
	if _, err = fetcher.FetchTimeline(ctx, Request{Source: SourceYoutube, Account: testChannel, Window: window}); err == nil {
		t.Fatal("youtube window accepted")
	}
	if _, err = fetcher.Fetch(ctx, Request{Source: SourceX, Account: testAccount, Credential: testCredential, Window: Window{Mode: ModeCount, Count: 5}}); err == nil || !IsValidation(err) {
		t.Fatalf("api count err=%v", err)
	}
	if len(timeline.cursors) != 0 {
		t.Fatalf("requests=%v", timeline.cursors)
	}
}
