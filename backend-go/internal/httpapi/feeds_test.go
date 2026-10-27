package httpapi

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend-go/internal/testutil"
)

func feedHandler(t *testing.T, now time.Time) http.Handler {
	t.Helper()
	path := testutil.NewSnapshot(t)
	testutil.Exec(t, path, `
		INSERT INTO events (id,source,title,detail,starts_on,starts_at,timezone,time_precision,status,event_type,url,provenance,manual_locked,cancellation_status,cancellation_evidence,created_at,updated_at) VALUES
		 ('feed-timed','x','Night, stream; part 2','YouTube Live','2026-10-28','2026-10-28T11:00:00Z','Asia/Tokyo','exact','scheduled','stream','https://example.invalid/post','manual',1,'none','private evidence','',''),
		 ('feed-day','x','Date only',NULL,'2026-10-29',NULL,'Asia/Tokyo','unknown','scheduled','event','javascript:alert(1)','automatic',0,'none',NULL,'',''),
		 ('feed-suspect','x','Suspected',NULL,'2026-10-30','2026-10-30T11:00:00Z','Asia/Tokyo','exact','scheduled','event',NULL,'automatic',0,'llm_suspected','private evidence','',''),
		 ('feed-cancel','x','Cancelled',NULL,'2026-10-31','2026-10-31T11:00:00Z','Asia/Tokyo','exact','scheduled','event',NULL,'manual',1,'manual_confirmed',NULL,'',''),
		 ('feed-old','x','Too old',NULL,'2026-01-01',NULL,'Asia/Tokyo','unknown','scheduled','event',NULL,'automatic',0,'none',NULL,'','');
		INSERT INTO videos (id,source,title,published_at,scheduled_at,url,kind,is_upcoming,raw_json) VALUES
		 ('feedvid0001','youtube','Cover <&> song','2026-10-20T03:00:00.000Z',NULL,'https://www.youtube.com/watch?v=feedvid0001','MUSIC',0,'{"secret":"raw"}'),
		 ('feedvid0002','youtube','Upcoming stream','2026-10-26T03:00:00.000Z','2026-11-01T11:00:00.000Z','https://www.youtube.com/watch?v=feedvid0002','LIVE',1,'{}');
		INSERT INTO app_settings (key,value,updated_at) VALUES ('dashboard_revision','7','');`)
	store, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return NewHandlerWithClock(store, func() time.Time { return now })
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
		"BEGIN:VCALENDAR\r\n", "X-WR-TIMEZONE:Asia/Tokyo\r\n",
		"UID:feed-timed@kano-navi\r\nDTSTAMP:20261027T030000Z\r\nDTSTART:20261028T110000Z\r\nSUMMARY:Night\\, stream\\; part 2\r\n",
		"URL:https://example.invalid/post\r\nSTATUS:CONFIRMED\r\n", "Confirmed by the site maintainer.",
		"DTSTART;VALUE=DATE:20261029\r\nDTEND;VALUE=DATE:20261030\r\n",
		"UID:feed-suspect@kano-navi", "STATUS:TENTATIVE\r\n",
		"UID:feed-cancel@kano-navi", "STATUS:CANCELLED\r\n",
		"END:VCALENDAR\r\n",
	} {
		if !strings.Contains(unfolded, want) {
			t.Fatalf("calendar lacks %q:\n%s", want, unfolded)
		}
	}
	for _, leaked := range []string{"private evidence", "javascript:", "Too old", "manualLocked"} {
		if strings.Contains(unfolded, leaked) {
			t.Fatalf("calendar contains %q", leaked)
		}
	}
	etag := response.Header().Get("ETag")
	if etag != `"ics-7-20261027"` {
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
}
