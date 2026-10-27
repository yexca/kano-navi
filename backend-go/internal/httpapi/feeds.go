package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/feeds"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

const (
	// The calendar feed covers past and coming Japan dates around today.
	calendarFeedPastDays   = 30
	calendarFeedFutureDays = 90
	// calendarFeedMaxEvents bounds the document whatever the event density.
	calendarFeedMaxEvents = 500
	// The Atom feed reads videos dated within this many days before and
	// after today, so reservations made for the coming days are listed.
	atomFeedPastDays   = 30
	atomFeedFutureDays = 11
	// Calendar clients and feed readers poll; a short shared cache spares the
	// database while the revision ETag keeps conditional requests cheap.
	feedCacheControl = "public, max-age=600"
)

var feedJapan = time.FixedZone("JST", 9*60*60)

// feedPreamble answers a conditional request from the snapshot revision and
// the Japan date, which together decide every feed's content. It reports
// false when the response is already written.
func (h *Handler) feedPreamble(w http.ResponseWriter, r *http.Request, kind string) (time.Time, bool) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeMethodNotAllowed(w, "GET, HEAD")
		return time.Time{}, false
	}
	revision, err := h.store.GetDashboardRevision(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database_unavailable"}, "")
		return time.Time{}, false
	}
	now := h.now()
	etag := `"` + kind + "-" + strconv.FormatInt(revision, 10) + "-" + now.In(feedJapan).Format("20060102") + `"`
	w.Header().Set("Cache-Control", feedCacheControl)
	w.Header().Set("ETag", etag)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	for _, candidate := range strings.Split(r.Header.Get("If-None-Match"), ",") {
		if strings.TrimPrefix(strings.TrimSpace(candidate), "W/") == etag {
			w.WriteHeader(http.StatusNotModified)
			return time.Time{}, false
		}
	}
	return now, true
}

func writeFeed(w http.ResponseWriter, r *http.Request, contentType string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

// calendarFeed serves the schedule as an iCalendar subscription.
func (h *Handler) calendarFeed(w http.ResponseWriter, r *http.Request) {
	now, ok := h.feedPreamble(w, r, "ics")
	if !ok {
		return
	}
	today, _ := time.ParseInLocation("2006-01-02", now.In(feedJapan).Format("2006-01-02"), feedJapan)
	events, err := h.feedEvents(r, today.AddDate(0, 0, -calendarFeedPastDays), today.AddDate(0, 0, calendarFeedFutureDays))
	if err != nil {
		w.Header().Del("ETag")
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database_unavailable"}, "no-store")
		return
	}
	w.Header().Set("Content-Disposition", `inline; filename="kano-schedule.ics"`)
	writeFeed(w, r, "text/calendar; charset=utf-8", feeds.Calendar(events, now))
}

// feedEvents reads inclusive Japan dates in calendar-sized windows, the
// same bounded reads the board's month view makes.
func (h *Handler) feedEvents(r *http.Request, from, to time.Time) ([]sqlite.DashboardEvent, error) {
	const window = 42
	events := []sqlite.DashboardEvent{}
	for start := from; !start.After(to); start = start.AddDate(0, 0, window) {
		end := start.AddDate(0, 0, window-1)
		if end.After(to) {
			end = to
		}
		for page := 1; ; page++ {
			calendar, err := h.store.GetCalendarPage(r.Context(), start.Format("2006-01-02"), end.Format("2006-01-02"), page, 100)
			if err != nil {
				return nil, err
			}
			for _, event := range calendar.Items {
				if len(events) == calendarFeedMaxEvents {
					return events, nil
				}
				events = append(events, event)
			}
			if !calendar.HasNext {
				break
			}
		}
	}
	return events, nil
}

// atomFeed serves recent videos and reservations as an Atom feed.
func (h *Handler) atomFeed(w http.ResponseWriter, r *http.Request) {
	now, ok := h.feedPreamble(w, r, "atom")
	if !ok {
		return
	}
	today := now.In(feedJapan)
	page, err := h.store.GetVideoArchive(r.Context(), today.AddDate(0, 0, -atomFeedPastDays).Format("2006-01-02"),
		today.AddDate(0, 0, atomFeedFutureDays).Format("2006-01-02"), 1, feeds.MaxEntries, now)
	var body []byte
	if err == nil {
		body, err = feeds.Atom(page.Items, now)
	}
	if err != nil {
		w.Header().Del("ETag")
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database_unavailable"}, "no-store")
		return
	}
	writeFeed(w, r, "application/atom+xml; charset=utf-8", body)
}
