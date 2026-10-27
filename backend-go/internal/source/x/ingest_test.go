package x

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// statusFixture is one synthetic status that both the vxtwitter detail answer
// and an FxTwitter timeline page can describe.
type statusFixture struct {
	id, text                string
	at                      time.Time
	likes, reposts, replies int
	media                   []mediaFixture
	quote                   *statusFixture
	author                  string
}

// mediaFixture is a photo ("photo"), video or GIF; file is a photo's image or
// a video's file, and thumb a video's or GIF's thumbnail.
type mediaFixture struct{ kind, file, thumb string }

func (f statusFixture) handle() string {
	if f.author != "" {
		return f.author
	}
	return "kano"
}

// detail is the status as api.vxtwitter.com answers it.
func (f statusFixture) detail() map[string]any {
	urls, extended := []any{}, []any{}
	for _, m := range f.media {
		kind, thumb := m.kind, m.thumb
		if kind == "photo" {
			kind, thumb = "image", m.file
		}
		urls = append(urls, m.file)
		extended = append(extended, map[string]any{"type": kind, "url": m.file, "thumbnail_url": thumb, "altText": nil})
	}
	out := map[string]any{
		"tweetID": f.id, "text": f.text, "date": f.at.Format("Mon Jan 02 15:04:05 +0000 2006"), "date_epoch": f.at.Unix(),
		"likes": f.likes, "retweets": f.reposts, "replies": f.replies,
		"tweetURL": "https://x.com/" + f.handle() + "/status/" + f.id, "user_screen_name": f.handle(), "user_name": "Synthetic",
		"mediaURLs": urls, "media_extended": extended, "hasMedia": len(urls) > 0, "qrt": nil,
	}
	if f.quote != nil {
		out["qrt"] = f.quote.detail()
	}
	return out
}

// timeline is the status as an FxTwitter timeline page lists it.
func (f statusFixture) timeline() map[string]any {
	all := []any{}
	for _, m := range f.media {
		item := map[string]any{"type": m.kind, "url": m.file + "?name=orig"}
		if m.kind != "photo" {
			item = map[string]any{"type": m.kind, "url": m.file, "thumbnail_url": m.thumb}
		}
		all = append(all, item)
	}
	out := map[string]any{
		"type": "status", "id": f.id, "url": "https://x.com/" + f.handle() + "/status/" + f.id, "text": f.text,
		"created_at": f.at.Format("Mon Jan 02 15:04:05 +0000 2006"), "created_timestamp": f.at.Unix(),
		"likes": f.likes, "reposts": f.reposts, "replies": f.replies, "is_note_tweet": len([]rune(f.text)) > 280,
		"author": map[string]any{"screen_name": f.handle(), "name": "Synthetic", "description": "repeated profile"},
	}
	if len(all) > 0 {
		out["media"] = map[string]any{"all": all}
	}
	if f.quote != nil {
		out["quote"] = f.quote.timeline()
	}
	return out
}

// decoded round-trips a fixture through JSON, as the client reads it.
func decoded(t *testing.T, v any) []byte {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func fxPage(t *testing.T, next string, statuses ...map[string]any) string {
	t.Helper()
	results := []any{}
	for _, s := range statuses {
		results = append(results, s)
	}
	return string(decoded(t, map[string]any{"code": 200, "results": results, "cursor": map[string]any{"bottom": next}}))
}

// TestTimelineAndDetailStoreTheSameValues compares both paths for one status:
// a post first stored from a timeline page and refreshed through the detail
// request later must keep its text, time, URL, media and declared images, or
// its extraction fingerprint would change.
func TestTimelineAndDetailStoreTheSameValues(t *testing.T) {
	at := time.Date(2026, 10, 20, 3, 4, 5, 0, time.UTC)
	photo := func(name string) mediaFixture {
		return mediaFixture{kind: "photo", file: "https://pbs.twimg.com/media/" + name + ".jpg"}
	}
	quoted := &statusFixture{id: "1900000000000000001", text: "quoted", at: at.Add(-time.Hour), author: "other", media: []mediaFixture{photo("quoted")}}
	for name, f := range map[string]statusFixture{
		"text only":       {text: "plain text"},
		"long text":       {text: strings.Repeat("長い本文の行です。\n", 60)},
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
			fromTimeline := page.Statuses[0].Post
			for field, pair := range map[string][2]any{
				"text":      {fromDetail.Text, fromTimeline.Text},
				"published": {fromDetail.PublishedAt, fromTimeline.PublishedAt},
				"url":       {fromDetail.URL, fromTimeline.URL},
				"type":      {fromDetail.Type + fromDetail.Label, fromTimeline.Type + fromTimeline.Label},
				"counters":  {[3]int{fromDetail.Likes, fromDetail.Reposts, fromDetail.Replies}, [3]int{fromTimeline.Likes, fromTimeline.Reposts, fromTimeline.Replies}},
				"media":     {strings.Join(fromDetail.MediaURLs, " "), strings.Join(fromTimeline.MediaURLs, " ")},
				// Extraction declares the images raw_json lists as mediaURLs.
				"declared": {string(decoded(t, fromDetail.Raw["mediaURLs"])), string(decoded(t, fromTimeline.Raw["mediaURLs"]))},
			} {
				if pair[0] != pair[1] {
					t.Errorf("%s: detail %v, timeline %v", field, pair[0], pair[1])
				}
			}
			// Videos and GIFs keep their thumbnail, never the file.
			if slices.ContainsFunc(fromDetail.MediaURLs, func(u string) bool { return strings.Contains(u, "video.twimg.com") || strings.Contains(u, "?") }) {
				t.Errorf("media=%v", fromDetail.MediaURLs)
			}
		})
	}
}

func TestDetailPhotoURLDropsOnlyFxTwitterSize(t *testing.T) {
	for in, want := range map[string]string{
		"https://pbs.twimg.com/media/a.jpg?name=orig":             "https://pbs.twimg.com/media/a.jpg",
		"https://pbs.twimg.com/media/a.png?name=large":            "https://pbs.twimg.com/media/a.png",
		"https://pbs.twimg.com/media/a?format=jpg&name=orig":      "https://pbs.twimg.com/media/a?format=jpg&name=orig",
		"https://pbs.twimg.com/ext_tw_video_thumb/1/a.jpg?name=x": "https://pbs.twimg.com/ext_tw_video_thumb/1/a.jpg?name=x",
		"https://example.test/media/a.jpg?name=orig":              "https://example.test/media/a.jpg?name=orig",
		"https://pbs.twimg.com/media/a.jpg":                       "https://pbs.twimg.com/media/a.jpg",
		"https://pbs.twimg.com/media/a.jpg?name=orig&name=small":  "https://pbs.twimg.com/media/a.jpg?name=orig&name=small",
	} {
		if got := detailPhotoURL(in); got != want {
			t.Errorf("detailPhotoURL(%s)=%s", in, got)
		}
	}
}

// detailFor answers a detail request with the status of that ID.
func detailFor(t *testing.T, fixtures map[string]statusFixture) func(string) *http.Response {
	return func(id string) *http.Response {
		f, ok := fixtures[id]
		if !ok {
			return response(404, `{"error":"not found"}`)
		}
		return response(200, string(decoded(t, f.detail())))
	}
}

func TestIncrementalRunStoresTimelineStatusesAndRequestsOnlyTheRest(t *testing.T) {
	at := func(h int) time.Time { return timelineNow.Add(-time.Duration(h) * time.Hour) }
	status := func(h int, text string, media ...mediaFixture) statusFixture {
		return statusFixture{id: statusAt(at(h)), text: text, at: at(h).Truncate(time.Second), media: media}
	}
	schedule := status(1, "今週の配信予定", mediaFixture{kind: "photo", file: "https://pbs.twimg.com/media/week.jpg"})
	plain := status(2, "plain")
	wasPending := status(3, "listed again")
	empty := status(4, "only the detail has it")
	known := status(30, "stored")
	pendingOld := status(100, "fell off the timeline")
	// The timeline lists the empty status without content.
	emptyEntry := map[string]any{"id": empty.id, "author": map[string]any{"screen_name": "kano"}}
	server := func() *timelineServer {
		s := &timelineServer{bodies: map[string]string{"": fxPage(t, "page-2", schedule.timeline(), plain.timeline(), wasPending.timeline(), emptyEntry, known.timeline())}}
		s.detail = detailFor(t, map[string]statusFixture{empty.id: empty, known.id: known, pendingOld.id: pendingOld})
		return s
	}
	opts := Options{BootstrapDays: 7, DiscoveryLimit: 50, MaxDetailRequests: 5, RefreshKnown: 1, ScheduleKeywords: []string{"配信予定"},
		CursorID: known.id, KnownIDs: map[string]bool{known.id: true}, PendingIDs: []string{wasPending.id, pendingOld.id}}

	s := server()
	a := s.adapter(opts)
	got, err := a.FetchRecent(context.Background(), "kano", opts)
	if err != nil {
		t.Fatal(err)
	}
	// Only the pending status the page no longer lists, the status listed
	// without content and the known refresh take a detail request.
	if strings.Join(s.details, ",") != strings.Join([]string{pendingOld.id, empty.id, known.id}, ",") || got.RequestedCount != 3 {
		t.Fatalf("details=%v requested=%d", s.details, got.RequestedCount)
	}
	if len(got.Posts) != 6 || len(got.PendingIDs) != 0 || len(got.FromTimeline) != 3 || !got.FromTimeline[schedule.id] || !got.FromTimeline[wasPending.id] || got.FromTimeline[known.id] {
		t.Fatalf("posts=%d pending=%v fromTimeline=%v", len(got.Posts), got.PendingIDs, got.FromTimeline)
	}
	if len(got.ScheduleCandidates) != 1 || got.ScheduleCandidates[0].MediaURL != "https://pbs.twimg.com/media/week.jpg" || got.NextCursor != schedule.id {
		t.Fatalf("candidates=%+v cursor=%s", got.ScheduleCandidates, got.NextCursor)
	}

	// Without a detail budget the page's statuses are still stored and the
	// rest stays pending in queue order.
	s = server()
	opts.MaxDetailRequests = 0
	a = s.adapter(opts)
	got, err = a.FetchRecent(context.Background(), "kano", opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.details) != 0 || len(got.Posts) != 3 || got.NextCursor != schedule.id || strings.Join(got.PendingIDs, ",") != strings.Join([]string{pendingOld.id, empty.id, known.id}, ",") {
		t.Fatalf("details=%v posts=%d cursor=%s pending=%v", s.details, len(got.Posts), got.NextCursor, got.PendingIDs)
	}
}

func TestPendingStatusesBackOffAndAreDropped(t *testing.T) {
	at := func(h int) string { return statusAt(timelineNow.Add(-time.Duration(h) * time.Hour)) }
	known, gone, flaky, foreign := at(1), at(50), at(60), at(70)
	s := &timelineServer{bodies: map[string]string{"": timelineBody("", known+"@kano")}}
	s.detail = func(id string) *http.Response {
		switch id {
		case gone:
			return response(404, `{"error":"not found"}`)
		case foreign:
			return response(200, string(decoded(t, statusFixture{id: foreign, text: "another account", at: timelineNow, author: "other"}.detail())))
		}
		return response(503, "unavailable")
	}
	opts := Options{BootstrapDays: 7, DiscoveryLimit: 50, MaxDetailRequests: 5, CursorID: known, KnownIDs: map[string]bool{known: true}, PendingIDs: []string{gone, flaky, foreign}}
	a := s.adapter(opts)
	start := timelineNow
	run := func(after time.Duration) Result {
		t.Helper()
		s.details = nil
		a.Now = func() time.Time { return start.Add(after) }
		// Every detail request failing fails the account, with the state kept.
		got, _ := a.FetchRecent(context.Background(), "kano", opts)
		opts.PendingIDs, opts.PendingRetries = got.PendingIDs, got.PendingRetries
		return got
	}
	dropped := func(r Result) string {
		out := []string{}
		for _, d := range r.Dropped {
			out = append(out, d.ID+":"+d.Code)
		}
		return strings.Join(out, ",")
	}

	first := run(0)
	if len(s.details) != 3 || len(first.Dropped) != 0 || first.PendingRetries[gone].Gone != 1 || first.PendingRetries[flaky].RetryAt != start.Add(time.Hour).Format(time.RFC3339Nano) {
		t.Fatalf("details=%v retries=%+v", s.details, first.PendingRetries)
	}
	// Within the first hour nothing is requested and nothing is lost.
	held := run(30 * time.Minute)
	if len(s.details) != 0 || held.Held != 3 || len(held.PendingIDs) != 3 || held.PendingRetries[flaky].Failures != 1 {
		t.Fatalf("details=%v held=%d pending=%v", s.details, held.Held, held.PendingIDs)
	}
	// A second 404 or a second answer for another account drops the status.
	second := run(time.Hour)
	if dropped(second) != gone+":"+DroppedGone+","+foreign+":"+DroppedGone || strings.Join(second.PendingIDs, ",") != flaky || second.PendingRetries[flaky].Failures != 2 {
		t.Fatalf("dropped=%v pending=%v retries=%+v", second.Dropped, second.PendingIDs, second.PendingRetries)
	}
	// Other failures wait 1, 6 and 24 hours; the fourth drops the status.
	third := run(7 * time.Hour)
	if len(s.details) != 1 || third.PendingRetries[flaky].RetryAt != start.Add(31*time.Hour).Format(time.RFC3339Nano) {
		t.Fatalf("details=%v retries=%+v", s.details, third.PendingRetries)
	}
	if held = run(30 * time.Hour); len(s.details) != 0 || held.Held != 1 {
		t.Fatalf("details=%v held=%d", s.details, held.Held)
	}
	last := run(31 * time.Hour)
	if dropped(last) != flaky+":"+DroppedRetryLimit || len(last.PendingIDs) != 0 || len(last.PendingRetries) != 0 {
		t.Fatalf("dropped=%v pending=%v retries=%+v", last.Dropped, last.PendingIDs, last.PendingRetries)
	}
}

func TestCancelledRunCountsNoFailure(t *testing.T) {
	at := func(h int) string { return statusAt(timelineNow.Add(-time.Duration(h) * time.Hour)) }
	known, first, second := at(1), at(50), at(60)
	ctx, cancel := context.WithCancel(context.Background())
	s := &timelineServer{bodies: map[string]string{"": timelineBody("", known+"@kano")}}
	s.detail = func(string) *http.Response {
		cancel()
		return response(503, "unavailable")
	}
	opts := Options{BootstrapDays: 7, DiscoveryLimit: 50, MaxDetailRequests: 5, CursorID: known, KnownIDs: map[string]bool{known: true}, PendingIDs: []string{first, second}}
	got, err := s.adapter(opts).FetchRecent(ctx, "kano", opts)
	if err == nil || len(s.details) != 1 || len(got.PendingRetries) != 0 || len(got.Dropped) != 0 || strings.Join(got.PendingIDs, ",") != first+","+second {
		t.Fatalf("err=%v details=%v result=%+v", err, s.details, got)
	}
}
