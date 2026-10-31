package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/yexca/kano-navi/backend/internal/buildinfo"
	"github.com/yexca/kano-navi/backend/internal/feeds"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

const (
	// The calendar feed covers past and coming Japan dates around today.
	calendarFeedPastDays   = 30
	calendarFeedFutureDays = 90
	// calendarFeedMaxEvents bounds the document whatever the event density;
	// coming events are kept before past ones.
	calendarFeedMaxEvents = 500
	// The Atom feed reads videos dated within this many days before and
	// after today, so reservations made for the coming days are listed.
	atomFeedPastDays   = 30
	atomFeedFutureDays = 11
	// Calendar clients and feed readers poll; a short shared cache spares the
	// database while the ETag keeps conditional requests small.
	feedCacheControl = "public, max-age=600"
)

var feedJapan = time.FixedZone("JST", 9*60*60)

// feedPreamble reads the optional lang query (missing or unknown is English)
// and sets the shared feed headers. It reports false when the response is
// already written.
func (h *Handler) feedPreamble(w http.ResponseWriter, r *http.Request) (time.Time, feeds.Locale, bool) {
	// The feeds negotiate gzip, so every answer varies by encoding.
	w.Header().Set("Vary", "Accept-Encoding")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeMethodNotAllowed(w, "GET, HEAD")
		return time.Time{}, "", false
	}
	w.Header().Set("Cache-Control", feedCacheControl)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	return h.now(), feeds.ParseLocale(r.URL.Query().Get("lang")), true
}

// feedETag names one feed variant. The build version is part of it, so an
// upgrade that rewords a feed is not hidden behind a cached copy. The tag is
// weak because gzip changes the transferred bytes; it validates the
// uncompressed body across encodings.
func feedETag(kind string, locale feeds.Locale, state string) string {
	return `W/"` + kind + "-" + string(locale) + "-" + buildinfo.Version + "-" + state + `"`
}

// notModified sets the ETag and answers a matching conditional request,
// comparing weakly as If-None-Match requires.
func notModified(w http.ResponseWriter, r *http.Request, etag string) bool {
	w.Header().Set("ETag", etag)
	opaque := strings.TrimPrefix(etag, "W/")
	for _, candidate := range strings.Split(r.Header.Get("If-None-Match"), ",") {
		if strings.TrimPrefix(strings.TrimSpace(candidate), "W/") == opaque {
			w.WriteHeader(http.StatusNotModified)
			return true
		}
	}
	return false
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
	now, locale, ok := h.feedPreamble(w, r)
	if !ok {
		return
	}
	revision, err := h.store.GetDashboardRevision(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database_unavailable"}, "")
		return
	}
	today, _ := time.ParseInLocation("2006-01-02", now.In(feedJapan).Format("2006-01-02"), feedJapan)
	events, err := h.calendarEvents(r, today)
	if err != nil {
		w.Header().Del("ETag")
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database_unavailable"}, "no-store")
		return
	}
	// DTSTAMP is the start of today's Japan date, so the body changes only
	// with the snapshot, the date and the build, and a poll in between is
	// answered 304.
	body := feeds.Calendar(events, today, locale)
	sum := sha256.Sum256(body)
	state := strconv.FormatInt(revision, 10) + "-" + hex.EncodeToString(sum[:12])
	if notModified(w, r, feedETag("ics", locale, state)) {
		return
	}
	w.Header().Set("Content-Disposition", `inline; filename="kano-schedule.ics"`)
	writeFeed(w, r, "text/calendar; charset=utf-8", body)
}

// calendarEvents reads the calendar's events in date order. The coming days
// fill the cap first and the most recent past events take what is left, so a
// busy month behind never pushes the schedule out. Each event appears once.
func (h *Handler) calendarEvents(r *http.Request, today time.Time) ([]sqlite.DashboardEvent, error) {
	seen := map[string]bool{}
	upcoming, err := h.feedEvents(r, today, today.AddDate(0, 0, calendarFeedFutureDays), calendarFeedMaxEvents, seen)
	if err != nil {
		return nil, err
	}
	past, err := h.recentFeedEvents(r, today.AddDate(0, 0, -calendarFeedPastDays), today.AddDate(0, 0, -1), calendarFeedMaxEvents-len(upcoming), seen)
	if err != nil {
		return nil, err
	}
	return append(past, upcoming...), nil
}

// feedEventWindow is the span of one read, the board's month view's.
const feedEventWindow = 42

// feedEvents reads up to limit events on inclusive Japan dates, oldest
// first, in calendar-sized windows, the same bounded reads the board's month
// view makes. Events already in seen are skipped and the rest are added.
func (h *Handler) feedEvents(r *http.Request, from, to time.Time, limit int, seen map[string]bool) ([]sqlite.DashboardEvent, error) {
	events := []sqlite.DashboardEvent{}
	for start := from; !start.After(to) && len(events) < limit; start = start.AddDate(0, 0, feedEventWindow) {
		end := start.AddDate(0, 0, feedEventWindow-1)
		if end.After(to) {
			end = to
		}
		for page := 1; ; page++ {
			calendar, err := h.store.GetCalendarPage(r.Context(), start.Format("2006-01-02"), end.Format("2006-01-02"), page, 100)
			if err != nil {
				return nil, err
			}
			for _, event := range calendar.Items {
				if len(events) == limit {
					return events, nil
				}
				if !seen[event.ID] {
					seen[event.ID] = true
					events = append(events, event)
				}
			}
			if !calendar.HasNext {
				break
			}
		}
	}
	return events, nil
}

// recentFeedEvents reads the newest limit events on inclusive Japan dates,
// newest window and page first, and returns them oldest first. Events
// already in seen are skipped and the rest are added.
func (h *Handler) recentFeedEvents(r *http.Request, from, to time.Time, limit int, seen map[string]bool) ([]sqlite.DashboardEvent, error) {
	newest := []sqlite.DashboardEvent{}
	for end := to; !end.Before(from) && len(newest) < limit; end = end.AddDate(0, 0, -feedEventWindow) {
		start := end.AddDate(0, 0, 1-feedEventWindow)
		if start.Before(from) {
			start = from
		}
		// A page past the end answers the last page, which names its number.
		for page := math.MaxInt32; page >= 1 && len(newest) < limit; page-- {
			calendar, err := h.store.GetCalendarPage(r.Context(), start.Format("2006-01-02"), end.Format("2006-01-02"), page, 100)
			if err != nil {
				return nil, err
			}
			page = calendar.Page
			for i := len(calendar.Items) - 1; i >= 0 && len(newest) < limit; i-- {
				if event := calendar.Items[i]; !seen[event.ID] {
					seen[event.ID] = true
					newest = append(newest, event)
				}
			}
		}
	}
	slices.Reverse(newest)
	return newest, nil
}

// atomFeed serves recent videos and reservations as an Atom feed.
func (h *Handler) atomFeed(w http.ResponseWriter, r *http.Request) {
	now, locale, ok := h.feedPreamble(w, r)
	if !ok {
		return
	}
	today := now.In(feedJapan)
	page, err := h.store.GetVideoArchive(r.Context(), today.AddDate(0, 0, -atomFeedPastDays).Format("2006-01-02"),
		today.AddDate(0, 0, atomFeedFutureDays).Format("2006-01-02"), 1, feeds.MaxEntries, now)
	var body []byte
	if err == nil {
		body, err = feeds.Atom(page.Items, now, h.publicOrigin, locale)
	}
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database_unavailable"}, "no-store")
		return
	}
	// Besides the snapshot and the date, the clock decides when a
	// reservation turns into an aired video, so the ETag covers the body.
	sum := sha256.Sum256(body)
	if notModified(w, r, feedETag("atom", locale, hex.EncodeToString(sum[:12]))) {
		return
	}
	writeFeed(w, r, "application/atom+xml; charset=utf-8", body)
}
