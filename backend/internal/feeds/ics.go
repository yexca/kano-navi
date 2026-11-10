// Package feeds renders the public schedule and videos as iCalendar and Atom
// documents. It reads the same sanitized store types as the public JSON API
// and adds no field the board does not already show.
package feeds

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend/internal/urlguard"
)

// SiteName is the English base-locale name feeds carry.
const SiteName = "What’s Kano Up To?"

var japan = time.FixedZone("JST", 9*60*60)

// Calendar renders events as an iCalendar document (RFC 5545) with CRLF line
// endings and folded lines, worded for locale: titles resolve translations
// as the board does and the fixed strings come from the locale's messages.
// The document names no time zone: times are UTC, and date-only and
// unknown-time events become floating all-day entries on their Japan date, so
// every viewer sees the same day. LAST-MODIFIED and SEQUENCE follow the stored update time
// so clients replace a changed entry. Every DTSTAMP is stamp, which the caller
// keeps steady between polls so an unchanged document keeps its validator.
func Calendar(events []sqlite.DashboardEvent, stamp time.Time, locale Locale) []byte {
	var b strings.Builder
	line := func(name, value string) { writeFolded(&b, name+":"+value) }
	line("BEGIN", "VCALENDAR")
	line("VERSION", "2.0")
	line("PRODID", "-//Kano Navi//Schedule//EN")
	line("CALSCALE", "GREGORIAN")
	line("METHOD", "PUBLISH")
	line("X-WR-CALNAME", escapeText(locale.messages().siteName))
	line("REFRESH-INTERVAL;VALUE=DURATION", "PT1H")
	line("X-PUBLISHED-TTL", "PT1H")
	dtstamp := stamp.UTC().Format("20060102T150405Z")
	for _, e := range events {
		for _, entry := range calendarEntries(e) {
			day, err := time.ParseInLocation("2006-01-02", entry.day, japan)
			if err != nil {
				continue
			}
			line("BEGIN", "VEVENT")
			line("UID", escapeText(entry.uid+"@kano-navi"))
			line("DTSTAMP", dtstamp)
			if updated, ok := parseInstant(&e.UpdatedAt); ok {
				line("LAST-MODIFIED", updated.UTC().Format("20060102T150405Z"))
				line("SEQUENCE", strconv.FormatInt(sequence(updated), 10))
			}
			if entry.timed {
				line("DTSTART", entry.start.UTC().Format("20060102T150405Z"))
				line("DTEND", entry.end.UTC().Format("20060102T150405Z"))
			} else {
				line("DTSTART;VALUE=DATE", day.Format("20060102"))
				line("DTEND;VALUE=DATE", day.AddDate(0, 0, 1).Format("20060102"))
				// Outlook keeps an entry with this flag a single all-day event
				// instead of converting its dates into a span across two days.
				line("X-MICROSOFT-CDO-ALLDAYEVENT", "TRUE")
			}
			title := localizedTitle(e.Title, e.Translations, locale)
			switch entry.mark {
			case saleOpens:
				title = fmt.Sprintf(locale.messages().saleOpens, title)
			case saleCloses:
				title = fmt.Sprintf(locale.messages().saleCloses, title)
			}
			line("SUMMARY", escapeText(title))
			if description := eventDescription(e, entry.timed, locale); description != "" {
				line("DESCRIPTION", escapeText(description))
			}
			if link := publicLink(eventFeedLink(e)); link != "" {
				line("URL", link)
			}
			line("STATUS", eventStatus(e))
			line("TRANSP", "TRANSPARENT")
			line("END", "VEVENT")
		}
	}
	line("END", "VCALENDAR")
	return []byte(b.String())
}

// saleMark names which end of a sale period a calendar entry shows.
type saleMark int

const (
	noSaleMark saleMark = iota
	saleOpens
	saleCloses
)

// calendarEntry is one VEVENT: an event, or one end of a sale period.
type calendarEntry struct {
	uid        string
	day        string
	mark       saleMark
	timed      bool
	start, end time.Time
}

// calendarEntries turns an event into its VEVENTs. A sale period is shown at
// its ends, as the board shows it, rather than as one block across weeks: a
// period that closes on a later date gives an opening entry (the event's own
// UID) and a closing entry, and a deadline whose opening is not known (the
// close date is the start date and there is no start time) gives the closing
// entry alone. A closing time ends its entry, which starts half an hour
// earlier.
func calendarEntries(e sqlite.DashboardEvent) []calendarEntry {
	opening := calendarEntry{uid: e.ID, day: e.StartsOn}
	if start, ok := eventStart(e); ok {
		opening.timed, opening.start = true, start
		end, ok := parseInstant(e.EndsAt)
		if !ok || !end.After(start) || e.EndsOn != nil && *e.EndsOn != e.StartsOn {
			end = start.Add(defaultDuration(e))
		}
		opening.end = end
	}
	if !sqlite.SaleEvent(e.EventType) {
		return []calendarEntry{opening}
	}
	// A stored start time makes a same-day sale an opening whatever its
	// precision, as on the board; an unknown precision keeps it all-day.
	started := e.StartsAt != nil && strings.TrimSpace(*e.StartsAt) != ""
	if e.EndsOn == nil || *e.EndsOn < e.StartsOn || *e.EndsOn == e.StartsOn && started {
		opening.mark = saleOpens
		return []calendarEntry{opening}
	}
	closing := calendarEntry{uid: e.ID, day: *e.EndsOn, mark: saleCloses}
	if end, ok := parseInstant(e.EndsAt); ok {
		closing.timed, closing.start, closing.end = true, end.Add(-defaultEventDuration), end
	}
	if *e.EndsOn == e.StartsOn {
		return []calendarEntry{closing}
	}
	opening.mark, closing.uid = saleOpens, e.ID+"-close"
	return []calendarEntry{opening, closing}
}

// Default lengths for timed events without a stored end. RFC 5545 makes an
// event with a DTSTART time and no DTEND zero-length, which calendar apps draw
// as a sliver, so streams get an hour and other timed entries half an hour.
const (
	defaultStreamDuration = time.Hour
	defaultEventDuration  = 30 * time.Minute
)

// defaultDuration classifies streams the way the board does: the stream and
// member (membership stream) event types.
func defaultDuration(e sqlite.DashboardEvent) time.Duration {
	if e.EventType != nil {
		switch strings.TrimSpace(*e.EventType) {
		case "stream", "member":
			return defaultStreamDuration
		}
	}
	return defaultEventDuration
}

// sequenceEpoch anchors SEQUENCE. Every write to an event sets its updated_at,
// so whole seconds since a fixed epoch increase whenever the event changes and
// stay the same between polls, without a stored revision counter. Seconds
// since 2020 fit the 32-bit integers some clients use until 2088.
var sequenceEpoch = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

func sequence(updated time.Time) int64 {
	return max(0, int64(updated.Sub(sequenceEpoch)/time.Second))
}

func eventStart(e sqlite.DashboardEvent) (time.Time, bool) {
	if e.TimePrecision == "unknown" {
		return time.Time{}, false
	}
	return parseInstant(e.StartsAt)
}

func parseInstant(value *string) (time.Time, bool) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.000Z07:00"} {
		if parsed, err := time.Parse(layout, strings.TrimSpace(*value)); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// eventStatus maps cancellation the way the board does: only a cancelled
// status or an operator's confirmation cancels, a suspicion stays tentative.
func eventStatus(e sqlite.DashboardEvent) string {
	switch {
	case e.Cancelled():
		return "CANCELLED"
	case e.CancellationStatus == "llm_suspected":
		return "TENTATIVE"
	case e.Status != nil && strings.EqualFold(strings.TrimSpace(*e.Status), "tentative"):
		return "TENTATIVE"
	}
	return "CONFIRMED"
}

func eventDescription(e sqlite.DashboardEvent, timed bool, locale Locale) string {
	m := locale.messages()
	parts := []string{}
	if e.Detail != nil && strings.TrimSpace(*e.Detail) != "" {
		parts = append(parts, localizedDetail(strings.TrimSpace(*e.Detail), locale))
	}
	if timed && e.TimePrecision == "approximate" {
		parts = append(parts, m.approximate)
	}
	if e.Provenance == "manual" {
		parts = append(parts, m.confirmed)
	} else {
		parts = append(parts, m.automatic)
	}
	if link := publicLink(eventFeedLink(e)); link != "" {
		parts = append(parts, link)
	}
	parts = append(parts, m.unofficial)
	return strings.Join(parts, "\n")
}

// publicLink keeps only absolute HTTP(S) links, the same links the board opens.
func publicLink(value *string) string {
	if value == nil {
		return ""
	}
	parsed, err := url.Parse(strings.TrimSpace(*value))
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || urlguard.ContainsCredentials(parsed.String()) {
		return ""
	}
	return parsed.String()
}

// escapeText escapes an iCalendar TEXT value.
func escapeText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\n", `\n`).Replace(value)
}

// writeFolded writes one content line, folding it at 75 octets without
// splitting a UTF-8 sequence. Control characters other than tab are dropped.
func writeFolded(b *strings.Builder, content string) {
	width := 0
	for _, r := range content {
		if r == utf8.RuneError || (r < 0x20 && r != '\t') || r == 0x7f {
			continue
		}
		size := utf8.RuneLen(r)
		if width+size > 75 {
			b.WriteString("\r\n ")
			width = 1
		}
		b.WriteRune(r)
		width += size
	}
	b.WriteString("\r\n")
}

// eventFeedLink is the link a calendar entry carries: the entry's own, or,
// for an entry without one (a stored live-room link reads as none), its
// first platform link.
func eventFeedLink(e sqlite.DashboardEvent) *string {
	if e.URL != nil {
		return e.URL
	}
	for _, link := range e.Links {
		if link.URL != nil {
			return link.URL
		}
	}
	return e.LiveURL
}
