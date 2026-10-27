package feeds

import (
	"encoding/xml"
	"sort"
	"strings"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

type atomFeed struct {
	XMLName  xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
	Title    string      `xml:"title"`
	Subtitle string      `xml:"subtitle"`
	ID       string      `xml:"id"`
	Updated  string      `xml:"updated"`
	Author   atomPerson  `xml:"author"`
	Links    []atomLink  `xml:"link"`
	Entries  []atomEntry `xml:"entry"`
}

type atomPerson struct {
	Name string `xml:"name"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr,omitempty"`
	Type string `xml:"type,attr,omitempty"`
}

type atomCategory struct {
	Term string `xml:"term,attr"`
}

type atomEntry struct {
	Title      string         `xml:"title"`
	ID         string         `xml:"id"`
	Updated    string         `xml:"updated"`
	Links      []atomLink     `xml:"link"`
	Categories []atomCategory `xml:"category"`
	Summary    string         `xml:"summary"`
}

// MaxEntries bounds the Atom feed.
const MaxEntries = 50

// Atom renders videos, reservations included, as an Atom 1.0 feed, newest
// first. An entry is dated when its video or reservation was published, never
// in the future; the schedule itself belongs to the calendar feed. Entry IDs
// are URNs over public video IDs, so they stay stable whichever host serves
// the feed, and the feed has no self link because the server does not trust a
// request's Host header.
func Atom(videos []sqlite.Video, now time.Time) ([]byte, error) {
	type dated struct {
		at    time.Time
		entry atomEntry
	}
	items := []dated{}
	for _, v := range videos {
		link := publicLink(&v.URL)
		scheduled, hasSchedule := parseInstant(v.ScheduledAt)
		at, ok := parseInstant(v.PublishedAt)
		if !ok && hasSchedule {
			at, ok = scheduled, true
		}
		if link == "" || !ok {
			continue
		}
		if at.After(now) {
			at = now.Truncate(time.Hour)
		}
		summary := "Published " + japanTime(at) + "."
		if v.IsUpcoming && hasSchedule {
			summary = "Scheduled for " + japanTime(scheduled) + "."
		} else if hasSchedule {
			summary = "Aired " + japanTime(scheduled) + "."
		}
		if v.IsCancelled {
			summary = "Cancelled. " + summary
		}
		entry := atomEntry{
			Title:      v.Title,
			ID:         "urn:kano-navi:video:" + v.ID,
			Updated:    at.UTC().Format(time.RFC3339),
			Links:      []atomLink{{Href: link, Rel: "alternate", Type: "text/html"}},
			Categories: []atomCategory{{Term: videoKind(v)}},
			Summary:    summary,
		}
		for _, alternate := range v.Alternates {
			if href := publicLink(&alternate.URL); href != "" {
				entry.Links = append(entry.Links, atomLink{Href: href, Rel: "related", Type: "text/html"})
			}
		}
		items = append(items, dated{at, entry})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].at.Equal(items[j].at) {
			return items[i].at.After(items[j].at)
		}
		return items[i].entry.ID < items[j].entry.ID
	})
	if len(items) > MaxEntries {
		items = items[:MaxEntries]
	}
	feed := atomFeed{
		Title:    SiteName,
		Subtitle: "Unofficial fan-made updates: Kano Mahoro's videos and streams.",
		ID:       "urn:kano-navi:feed",
		Updated:  now.UTC().Truncate(time.Hour).Format(time.RFC3339),
		Author:   atomPerson{Name: "Kano Navi"},
		Links:    []atomLink{{Href: "/", Rel: "alternate", Type: "text/html"}},
	}
	if len(items) > 0 {
		feed.Updated = items[0].entry.Updated
	}
	for _, item := range items {
		feed.Entries = append(feed.Entries, item.entry)
	}
	body, err := xml.MarshalIndent(feed, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), append(body, '\n')...), nil
}

func videoKind(v sqlite.Video) string {
	if v.IsShort != nil && *v.IsShort {
		return "short"
	}
	if v.Kind != nil && strings.TrimSpace(*v.Kind) != "" {
		return strings.TrimSpace(*v.Kind)
	}
	return "video"
}

func japanTime(at time.Time) string {
	return at.In(japan).Format("2006-01-02 15:04") + " JST"
}
