// Package feeds renders the public schedule and videos as iCalendar and Atom
// documents. It reads the same sanitized store types as the public JSON API
// and adds no field the board does not already show.
package feeds

import (
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// SiteName is the English base-locale name feeds carry.
const SiteName = "What’s Kano Up To?"

var japan = time.FixedZone("JST", 9*60*60)

// Calendar renders events as an iCalendar document (RFC 5545) with CRLF line
// endings and folded lines, worded for locale: titles resolve translations
// as the board does and the fixed strings come from the locale's messages.
// Times are UTC; date-only and unknown-time events become all-day entries on
// their Japan date. LAST-MODIFIED and SEQUENCE follow the stored update time
// so clients replace a changed entry.
func Calendar(events []sqlite.DashboardEvent, now time.Time, locale Locale) []byte {
	var b strings.Builder
	line := func(name, value string) { writeFolded(&b, name+":"+value) }
	line("BEGIN", "VCALENDAR")
	line("VERSION", "2.0")
	line("PRODID", "-//Kano Navi//Schedule//EN")
	line("CALSCALE", "GREGORIAN")
	line("METHOD", "PUBLISH")
	line("X-WR-CALNAME", escapeText(locale.messages().siteName))
	line("X-WR-TIMEZONE", "Asia/Tokyo")
	line("REFRESH-INTERVAL;VALUE=DURATION", "PT1H")
	line("X-PUBLISHED-TTL", "PT1H")
	stamp := now.UTC().Format("20060102T150405Z")
	for _, e := range events {
		day, err := time.ParseInLocation("2006-01-02", e.StartsOn, japan)
		if err != nil {
			continue
		}
		line("BEGIN", "VEVENT")
		line("UID", escapeText(e.ID+"@kano-navi"))
		line("DTSTAMP", stamp)
		if updated, ok := parseInstant(&e.UpdatedAt); ok {
			line("LAST-MODIFIED", updated.UTC().Format("20060102T150405Z"))
			line("SEQUENCE", strconv.FormatInt(sequence(updated), 10))
		}
		start, timed := eventStart(e)
		if timed {
			line("DTSTART", start.UTC().Format("20060102T150405Z"))
			end, ok := parseInstant(e.EndsAt)
			if !ok || !end.After(start) {
				end = start.Add(defaultDuration(e))
			}
			line("DTEND", end.UTC().Format("20060102T150405Z"))
		} else {
			line("DTSTART;VALUE=DATE", day.Format("20060102"))
			line("DTEND;VALUE=DATE", day.AddDate(0, 0, 1).Format("20060102"))
		}
		line("SUMMARY", escapeText(localizedTitle(e.Title, e.Translations, locale)))
		if description := eventDescription(e, timed, locale); description != "" {
			line("DESCRIPTION", escapeText(description))
		}
		if link := publicLink(e.URL); link != "" {
			line("URL", link)
		}
		line("STATUS", eventStatus(e))
		line("TRANSP", "TRANSPARENT")
		line("END", "VEVENT")
	}
	line("END", "VCALENDAR")
	return []byte(b.String())
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
	if link := publicLink(e.URL); link != "" {
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
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
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
