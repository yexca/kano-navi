// Package feeds renders the public schedule and videos as iCalendar and Atom
// documents. It reads the same sanitized store types as the public JSON API
// and adds no field the board does not already show.
package feeds

import (
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// SiteName is the English base-locale name feeds carry.
const SiteName = "What’s Kano Up To?"

var japan = time.FixedZone("JST", 9*60*60)

// Calendar renders events as an iCalendar document (RFC 5545) with CRLF line
// endings and folded lines. Times are UTC; date-only and unknown-time events
// become all-day entries on their Japan date.
func Calendar(events []sqlite.DashboardEvent, now time.Time) []byte {
	var b strings.Builder
	line := func(name, value string) { writeFolded(&b, name+":"+value) }
	line("BEGIN", "VCALENDAR")
	line("VERSION", "2.0")
	line("PRODID", "-//Kano Navi//Schedule//EN")
	line("CALSCALE", "GREGORIAN")
	line("METHOD", "PUBLISH")
	line("X-WR-CALNAME", escapeText(SiteName))
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
		start, timed := eventStart(e)
		if timed {
			line("DTSTART", start.UTC().Format("20060102T150405Z"))
			if end, ok := parseInstant(e.EndsAt); ok && end.After(start) {
				line("DTEND", end.UTC().Format("20060102T150405Z"))
			}
		} else {
			line("DTSTART;VALUE=DATE", day.Format("20060102"))
			line("DTEND;VALUE=DATE", day.AddDate(0, 0, 1).Format("20060102"))
		}
		line("SUMMARY", escapeText(e.Title))
		if description := eventDescription(e, timed); description != "" {
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

func eventDescription(e sqlite.DashboardEvent, timed bool) string {
	parts := []string{}
	if e.Detail != nil && strings.TrimSpace(*e.Detail) != "" {
		parts = append(parts, strings.TrimSpace(*e.Detail))
	}
	if timed && e.TimePrecision == "approximate" {
		parts = append(parts, "The start time is approximate.")
	}
	if e.Provenance == "manual" {
		parts = append(parts, "Confirmed by the site maintainer.")
	} else {
		parts = append(parts, "Read automatically from the original source; check it for changes.")
	}
	if link := publicLink(e.URL); link != "" {
		parts = append(parts, link)
	}
	parts = append(parts, "Unofficial fan-made schedule.")
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
