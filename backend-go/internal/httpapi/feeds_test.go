package httpapi

import (
	"compress/gzip"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/buildinfo"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend-go/internal/testutil"
)

func feedHandler(t *testing.T, now time.Time) http.Handler {
	t.Helper()
	return NewHandlerWithClock(feedStore(t), func() time.Time { return now })
}

func feedStore(t *testing.T) *sqlite.Store {
	t.Helper()
	path := testutil.NewSnapshot(t)
	testutil.Exec(t, path, `
		INSERT INTO events (id,source,title,detail,starts_on,starts_at,ends_at,timezone,time_precision,status,event_type,url,provenance,manual_locked,cancellation_status,cancellation_evidence,created_at,updated_at) VALUES
		 ('feed-timed','x','Night, stream; part 2','YouTube Live','2026-10-28','2026-10-28T11:00:00Z',NULL,'Asia/Tokyo','exact','scheduled','stream','https://example.invalid/post','manual',1,'none','private evidence','','2026-10-26T09:30:00.000Z'),
		 ('feed-day','x','Date only',NULL,'2026-10-29',NULL,NULL,'Asia/Tokyo','unknown','scheduled','event','javascript:alert(1)','automatic',0,'none',NULL,'',''),
		 ('feed-suspect','x','Suspected',NULL,'2026-10-30','2026-10-30T11:00:00Z',NULL,'Asia/Tokyo','exact','scheduled','event',NULL,'automatic',0,'llm_suspected','private evidence','',''),
		 ('feed-cancel','x','Cancelled',NULL,'2026-10-31','2026-10-31T11:00:00Z','2026-10-31T13:00:00Z','Asia/Tokyo','exact','scheduled','event',NULL,'manual',1,'manual_confirmed',NULL,'',''),
		 ('feed-old','x','Too old',NULL,'2026-01-01',NULL,NULL,'Asia/Tokyo','unknown','scheduled','event',NULL,'automatic',0,'none',NULL,'',''),
		 ('feed-yt','youtube','Upcoming stream','YouTube Live','2026-11-01','2026-11-01T11:00:00.000Z',NULL,'Asia/Tokyo','exact','scheduled','stream','https://www.youtube.com/watch?v=feedvid0002','automatic',0,'none',NULL,'','');
		INSERT INTO translations (owner_type,owner_id,field,locale,origin,text,source_hash,created_at,updated_at) VALUES
		 ('video','feedvid0002','title','en','manual','Upcoming stream EN','','',''),
		 ('video','feedvid0002','title','zh-CN','manual','即将开始的直播','','','');
		INSERT INTO videos (id,source,title,published_at,scheduled_at,url,kind,is_upcoming,raw_json) VALUES
		 ('feedvid0001','youtube','Cover <&> song','2026-10-20T03:00:00.000Z',NULL,'https://www.youtube.com/watch?v=feedvid0001','MUSIC',0,'{"secret":"raw"}'),
		 ('feedvid0002','youtube','Upcoming stream','2026-10-26T03:00:00.000Z','2026-11-01T11:00:00.000Z','https://www.youtube.com/watch?v=feedvid0002','LIVE',1,'{}');
		INSERT INTO app_settings (key,value,updated_at) VALUES ('dashboard_revision','7','');`)
	store, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestCalendarFeed(t *testing.T) {
	handler := feedHandler(t, time.Date(2026, 10, 27, 3, 0, 0, 0, time.UTC))
	response := request(t, handler, http.MethodGet, "/api/calendar.ics")
	body := response.Body.String()
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/calendar; charset=utf-8" || response.Header().Get("Cache-Control") != feedCacheControl {
		t.Fatalf("status=%d headers=%v", response.Code, response.Header())
	}
	for _, line := range strings.Split(strings.TrimSuffix(body, "\r\n"), "\r\n") {
		if len(line) > 75 {
			t.Fatalf("unfolded line %q", line)
		}
	}
	unfolded := strings.ReplaceAll(body, "\r\n ", "")
	for _, want := range []string{
		"BEGIN:VCALENDAR\r\n",
		// A stream without an end lasts an hour; LAST-MODIFIED and SEQUENCE
		// follow updated_at (whole seconds since 2020-01-01T00:00:00Z). A
		// timed entry carries no all-day flag.
		"UID:feed-timed@kano-navi\r\nDTSTAMP:20261026T150000Z\r\nLAST-MODIFIED:20261026T093000Z\r\nSEQUENCE:215170200\r\nDTSTART:20261028T110000Z\r\nDTEND:20261028T120000Z\r\nSUMMARY:Night\\, stream\\; part 2\r\n",
		"URL:https://example.invalid/post\r\nSTATUS:CONFIRMED\r\n", "Confirmed by the site maintainer.",
		"DTSTART;VALUE=DATE:20261029\r\nDTEND;VALUE=DATE:20261030\r\nX-MICROSOFT-CDO-ALLDAYEVENT:TRUE\r\n",
		// Another timed event without an end lasts half an hour, and one
		// without a parseable update time has no SEQUENCE.
		"UID:feed-suspect@kano-navi\r\nDTSTAMP:20261026T150000Z\r\nDTSTART:20261030T110000Z\r\nDTEND:20261030T113000Z\r\n", "STATUS:TENTATIVE\r\n",
		// A stored end is kept.
		"DTSTART:20261031T110000Z\r\nDTEND:20261031T130000Z\r\n", "STATUS:CANCELLED\r\n",
		"END:VCALENDAR\r\n",
	} {
		if !strings.Contains(unfolded, want) {
			t.Fatalf("calendar lacks %q:\n%s", want, unfolded)
		}
	}
	// The feed names no time zone, so all-day entries stay floating.
	for _, leaked := range []string{"private evidence", "javascript:", "Too old", "manualLocked", "X-WR-TIMEZONE"} {
		if strings.Contains(unfolded, leaked) {
			t.Fatalf("calendar contains %q", leaked)
		}
	}
	etag := response.Header().Get("ETag")
	if !strings.HasPrefix(etag, `W/"ics-en-`+buildinfo.Version+`-7-`) {
		t.Fatalf("etag %q", etag)
	}
	conditional := httptest.NewRequest(http.MethodGet, "/api/calendar.ics", nil)
	conditional.Header.Set("If-None-Match", etag)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, conditional)
	if recorder.Code != http.StatusNotModified || recorder.Body.Len() != 0 {
		t.Fatalf("conditional status=%d", recorder.Code)
	}
	if head := request(t, handler, http.MethodHead, "/api/calendar.ics"); head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("head status=%d", head.Code)
	}
	if post := request(t, handler, http.MethodPost, "/api/calendar.ics"); post.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post status=%d", post.Code)
	}
}

// feedGet requests a feed with an optional Accept-Encoding and If-None-Match.
func feedGet(handler http.Handler, path, encoding, etag string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if encoding != "" {
		request.Header.Set("Accept-Encoding", encoding)
	}
	if etag != "" {
		request.Header.Set("If-None-Match", etag)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// feedEncodings checks that a feed's identity and gzip answers carry the same
// weak validator, that gzip decodes to the identity body and that either
// encoding revalidates the tag without a body. It returns the tag.
func feedEncodings(t *testing.T, handler http.Handler, path string) string {
	t.Helper()
	identity := feedGet(handler, path, "", "")
	etag := identity.Header().Get("ETag")
	if identity.Code != http.StatusOK || identity.Header().Get("Content-Encoding") != "" || identity.Header().Get("Vary") != "Accept-Encoding" ||
		!strings.HasPrefix(etag, `W/"`) || identity.Body.Len() < compressThreshold {
		t.Fatalf("%s identity status=%d headers=%v length=%d", path, identity.Code, identity.Header(), identity.Body.Len())
	}
	gzipped := feedGet(handler, path, "gzip", "")
	if gzipped.Code != http.StatusOK || gzipped.Header().Get("Content-Encoding") != "gzip" || gzipped.Header().Get("ETag") != etag ||
		gzipped.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("%s gzip status=%d headers=%v", path, gzipped.Code, gzipped.Header())
	}
	reader, err := gzip.NewReader(gzipped.Body)
	if err != nil {
		t.Fatal(err)
	}
	if decoded, _ := io.ReadAll(reader); string(decoded) != identity.Body.String() {
		t.Fatalf("%s gzip body differs from the identity body", path)
	}
	feedRevalidates(t, handler, path, etag)
	return etag
}

// feedRevalidates checks that both encodings answer etag with a bodiless 304.
func feedRevalidates(t *testing.T, handler http.Handler, path, etag string) {
	t.Helper()
	for _, encoding := range []string{"", "gzip"} {
		unchanged := feedGet(handler, path, encoding, etag)
		if unchanged.Code != http.StatusNotModified || unchanged.Body.Len() != 0 || unchanged.Header().Get("ETag") != etag ||
			unchanged.Header().Get("Content-Encoding") != "" || unchanged.Header().Get("Vary") != "Accept-Encoding" {
			t.Fatalf("%s %q revalidation status=%d headers=%v", path, encoding, unchanged.Code, unchanged.Header())
		}
	}
}

func TestCalendarFeedRevalidatesWithinAJapanDayAcrossEncodings(t *testing.T) {
	var now time.Time
	handler := Compress(NewHandlerWithClock(feedStore(t), func() time.Time { return now }))
	now = time.Date(2026, 10, 27, 3, 0, 0, 0, time.UTC)
	etag := feedEncodings(t, handler, "/api/calendar.ics")
	// Later the same Japan day the tag still holds.
	now = time.Date(2026, 10, 27, 14, 59, 59, 0, time.UTC)
	feedRevalidates(t, handler, "/api/calendar.ics", etag)
	// The next Japan day moves the window and DTSTAMP.
	now = time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)
	next := feedGet(handler, "/api/calendar.ics", "", etag)
	if next.Code != http.StatusOK || next.Header().Get("ETag") == etag || !strings.Contains(next.Body.String(), "DTSTAMP:20261027T150000Z\r\n") {
		t.Fatalf("next day status=%d etag=%q", next.Code, next.Header().Get("ETag"))
	}
}

func TestAtomFeedRevalidatesAcrossEncodings(t *testing.T) {
	handler := Compress(feedHandler(t, time.Date(2026, 10, 27, 3, 0, 0, 0, time.UTC)))
	feedEncodings(t, handler, "/api/feed.xml")
}

func TestAtomFeed(t *testing.T) {
	handler := feedHandler(t, time.Date(2026, 10, 27, 3, 0, 0, 0, time.UTC))
	response := request(t, handler, http.MethodGet, "/api/feed.xml")
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/atom+xml; charset=utf-8" {
		t.Fatalf("status=%d headers=%v body=%s", response.Code, response.Header(), response.Body)
	}
	var feed struct {
		Updated string `xml:"updated"`
		Entries []struct {
			Title    string `xml:"title"`
			ID       string `xml:"id"`
			Updated  string `xml:"updated"`
			Summary  string `xml:"summary"`
			Category struct {
				Term string `xml:"term,attr"`
			} `xml:"category"`
			Link struct {
				Href string `xml:"href,attr"`
			} `xml:"link"`
		} `xml:"entry"`
	}
	if err := xml.Unmarshal(response.Body.Bytes(), &feed); err != nil {
		t.Fatal(err)
	}
	if len(feed.Entries) != 2 || feed.Updated != "2026-10-26T03:00:00Z" {
		t.Fatalf("feed=%+v", feed)
	}
	upcoming, cover := feed.Entries[0], feed.Entries[1]
	if upcoming.ID != "urn:kano-navi:video:feedvid0002" || upcoming.Summary != "Scheduled for 2026-11-01 20:00 JST." || upcoming.Category.Term != "LIVE" {
		t.Fatalf("upcoming=%+v", upcoming)
	}
	if cover.Title != "Cover <&> song" || cover.Link.Href != "https://www.youtube.com/watch?v=feedvid0001" || cover.Updated != "2026-10-20T03:00:00Z" {
		t.Fatalf("cover=%+v", cover)
	}
	if strings.Contains(response.Body.String(), "secret") {
		t.Fatal("feed leaked raw_json")
	}
	if body := response.Body.String(); strings.Contains(body, `rel="self"`) || !strings.Contains(body, `<link href="/" rel="alternate"`) {
		t.Fatalf("feed without PUBLIC_ORIGIN has absolute or self links:\n%s", body)
	}
}

func TestAtomFeedLinksWithPublicOrigin(t *testing.T) {
	now := time.Date(2026, 10, 27, 3, 0, 0, 0, time.UTC)
	handler := NewStandaloneHandler(feedStore(t), func() time.Time { return now }, Standalone{PublicOrigin: "https://kano.example.invalid"})
	request := httptest.NewRequest(http.MethodGet, "/api/feed.xml", nil)
	// The Host header never decides a link.
	request.Host = "attacker.example.invalid"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	body := response.Body.String()
	if response.Code != http.StatusOK ||
		!strings.Contains(body, `<link href="https://kano.example.invalid/" rel="alternate" type="text/html">`) ||
		!strings.Contains(body, `<link href="https://kano.example.invalid/api/feed.xml" rel="self" type="application/atom+xml">`) ||
		strings.Contains(body, "attacker") {
		t.Fatalf("status=%d body:\n%s", response.Code, body)
	}
}

func TestAtomFeedETagFollowsTheClockAndVersion(t *testing.T) {
	path := testutil.NewSnapshot(t)
	testutil.Exec(t, path, `
		INSERT INTO videos (id,source,title,published_at,scheduled_at,url,kind,is_upcoming,raw_json) VALUES
		 ('etagvid0001','youtube','Reserved stream',NULL,'2026-11-01T11:00:00.000Z','https://www.youtube.com/watch?v=etagvid0001','LIVE',1,'{}');
		INSERT INTO app_settings (key,value,updated_at) VALUES ('dashboard_revision','3','');`)
	store, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var now time.Time
	handler := NewHandlerWithClock(store, func() time.Time { return now })
	get := func(at time.Time, etag string) *httptest.ResponseRecorder {
		now = at
		request := httptest.NewRequest(http.MethodGet, "/api/feed.xml", nil)
		if etag != "" {
			request.Header.Set("If-None-Match", etag)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	// 10:00 and 12:00 JST on the reservation's day: the not-yet-published
	// entry is dated at the start of the Japan day, so nothing moves hourly.
	morning := get(time.Date(2026, 11, 1, 1, 0, 0, 0, time.UTC), "")
	noon := get(time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC), "")
	etag := morning.Header().Get("ETag")
	if morning.Code != http.StatusOK || etag == "" || noon.Header().Get("ETag") != etag || noon.Body.String() != morning.Body.String() ||
		!strings.Contains(morning.Body.String(), "<updated>2026-10-31T15:00:00Z</updated>") ||
		!strings.Contains(morning.Body.String(), "Scheduled for 2026-11-01 20:00 JST.") {
		t.Fatalf("morning=%d %q noon=%q\n%s", morning.Code, etag, noon.Header().Get("ETag"), morning.Body)
	}
	if cached := get(time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC), etag); cached.Code != http.StatusNotModified {
		t.Fatalf("same state status=%d", cached.Code)
	}
	// After the start, the same day and revision: the entry has aired and
	// takes its own time, so the old ETag no longer matches.
	aired := get(time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC), etag)
	if aired.Code != http.StatusOK || aired.Header().Get("ETag") == etag ||
		!strings.Contains(aired.Body.String(), "Aired 2026-11-01 20:00 JST.") ||
		!strings.Contains(aired.Body.String(), "<updated>2026-11-01T11:00:00Z</updated>") {
		t.Fatalf("aired=%d %q\n%s", aired.Code, aired.Header().Get("ETag"), aired.Body)
	}
	// An upgrade changes the ETag even when the body is the same.
	previous := buildinfo.Version
	t.Cleanup(func() { buildinfo.Version = previous })
	buildinfo.Version = "9.9.9-test"
	upgraded := get(time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC), etag)
	if upgraded.Code != http.StatusOK || upgraded.Header().Get("ETag") == etag || !strings.Contains(upgraded.Header().Get("ETag"), "-9.9.9-test-") {
		t.Fatalf("upgraded=%d %q", upgraded.Code, upgraded.Header().Get("ETag"))
	}
}

func TestCalendarFeedKeepsComingEventsUnderTheCap(t *testing.T) {
	path := testutil.NewSnapshot(t)
	// More past events than the cap, spread over 25 days up to yesterday,
	// and three coming ones.
	testutil.Exec(t, path, `
		WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < 520)
		INSERT INTO events (id,source,title,starts_on,timezone,time_precision,status,event_type,provenance,manual_locked,cancellation_status,created_at,updated_at)
		SELECT printf('cap-past-%03d',i),'x','Past',date('2026-10-26',printf('-%d days',i % 25)),'Asia/Tokyo','unknown','scheduled','event','automatic',0,'none','','' FROM n;
		INSERT INTO events (id,source,title,starts_on,starts_at,timezone,time_precision,status,event_type,provenance,manual_locked,cancellation_status,created_at,updated_at) VALUES
		 ('cap-today','x','Today','2026-10-27','2026-10-27T10:00:00Z','Asia/Tokyo','exact','scheduled','stream','automatic',0,'none','',''),
		 ('cap-next','x','Next','2026-11-20',NULL,'Asia/Tokyo','unknown','scheduled','event','automatic',0,'none','',''),
		 ('cap-last','x','Last','2027-01-25',NULL,'Asia/Tokyo','unknown','scheduled','event','automatic',0,'none','','');`)
	store, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	handler := NewHandlerWithClock(store, func() time.Time { return time.Date(2026, 10, 27, 3, 0, 0, 0, time.UTC) })
	response := request(t, handler, http.MethodGet, "/api/calendar.ics")
	body := strings.ReplaceAll(response.Body.String(), "\r\n ", "")
	uids := map[string]bool{}
	for _, line := range strings.Split(body, "\r\n") {
		if uid, ok := strings.CutPrefix(line, "UID:"); ok {
			if uids[uid] {
				t.Fatalf("duplicate %s", uid)
			}
			uids[uid] = true
		}
	}
	if response.Code != http.StatusOK || len(uids) != calendarFeedMaxEvents {
		t.Fatalf("status=%d events=%d", response.Code, len(uids))
	}
	for _, uid := range []string{"cap-today@kano-navi", "cap-next@kano-navi", "cap-last@kano-navi", "cap-past-025@kano-navi"} {
		if !uids[uid] {
			t.Fatalf("calendar lacks %s", uid)
		}
	}
	// The oldest past day (2026-10-02, every 25th event) gives way.
	if strings.Contains(body, "DTSTART;VALUE=DATE:20261002\r\n") || !strings.Contains(body, "DTSTART;VALUE=DATE:20261026\r\n") {
		t.Fatal("past events were not trimmed from the oldest")
	}
	// Events stay in date order: the past before today.
	if strings.Index(body, "UID:cap-past-025@") > strings.Index(body, "UID:cap-today@") {
		t.Fatal("past events follow the coming ones")
	}
}
