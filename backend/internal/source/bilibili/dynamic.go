package bilibili

import (
	"encoding/json"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Dynamic is one space dynamic as stored in posts (source = 'bilibili').
type Dynamic struct {
	// ID is the dynamic's numeric ID, as it appears in t.bilibili.com URLs.
	ID string
	// Kind is KindDynamicPost or KindDynamicRepost.
	Kind string
	Text string
	URL  string
	// PublishedAt is the public release time, RFC 3339 in UTC.
	PublishedAt string
	// Author is the uploader's display name, which Bilibili calls the
	// account handle in posts.
	Author    string
	AuthorMid int64
	// Images are the pictures the dynamic carries, in the order shown, on
	// Bilibili's image CDN.
	Images []string
	// Original is the forwarded dynamic of a repost, nil otherwise.
	Original *Dynamic
}

const (
	// KindDynamicPost is a dynamic the account published itself.
	KindDynamicPost = "post"
	// KindDynamicRepost is a dynamic the account forwarded.
	KindDynamicRepost = "repost"
)

// maxDynamicText bounds one dynamic's text, in runes.
const maxDynamicText = 4000

// maxDynamicImages bounds the pictures kept from one dynamic. A post shows
// nine at most.
const maxDynamicImages = 9

// dynamicIDPattern accepts only the numeric ID a dynamic URL carries.
var dynamicIDPattern = regexp.MustCompile(`^[0-9]{1,32}$`)

// dynamicItem is one entry of the space feed. A forwarded dynamic carries the
// forwarded one in orig, in the same shape.
type dynamicItem struct {
	IDStr   string `json:"id_str"`
	Type    string `json:"type"`
	Modules struct {
		Author *struct {
			PubTS int64  `json:"pub_ts"`
			Name  string `json:"name"`
			Mid   int64  `json:"mid"`
		} `json:"module_author"`
		Dynamic *struct {
			Desc *struct {
				Text string `json:"text"`
			} `json:"desc"`
			Major *struct {
				Draw *struct {
					Items []struct {
						Src string `json:"src"`
					} `json:"items"`
				} `json:"draw"`
				Opus *struct {
					Title   string `json:"title"`
					Summary *struct {
						Text string `json:"text"`
					} `json:"summary"`
					Pics []struct {
						URL string `json:"url"`
					} `json:"pics"`
				} `json:"opus"`
				Archive *struct {
					BVID  string `json:"bvid"`
					Title string `json:"title"`
				} `json:"archive"`
			} `json:"major"`
		} `json:"module_dynamic"`
	} `json:"modules"`
	Orig *dynamicItem `json:"orig"`
}

// dynamic validates one entry; it reports false for one that is incomplete or
// is not a post: an upload published as a video dynamic is read by the
// uploads list, so storing it again as a post would duplicate it.
func (d dynamicItem) dynamic(account int64) (Dynamic, bool) {
	author := d.Modules.Author
	if !dynamicIDPattern.MatchString(d.IDStr) || author == nil || author.PubTS <= 0 {
		return Dynamic{}, false
	}
	if author.Mid != 0 && author.Mid != account {
		return Dynamic{}, false
	}
	if d.Type == dynamicTypeVideo {
		return Dynamic{}, false
	}
	text, images := d.body()
	if text == "" && len(images) == 0 {
		return Dynamic{}, false
	}
	out := Dynamic{
		ID: d.IDStr, Kind: KindDynamicPost, Text: text,
		URL:         "https://t.bilibili.com/" + d.IDStr,
		PublishedAt: time.Unix(author.PubTS, 0).UTC().Format(time.RFC3339),
		Author:      strings.TrimSpace(html.UnescapeString(author.Name)),
		AuthorMid:   author.Mid,
		Images:      images,
	}
	if d.Type == dynamicTypeForward && d.Orig != nil {
		// The forwarded dynamic belongs to whoever published it, so it is
		// read without the account restriction.
		mid := int64(0)
		if d.Orig.Modules.Author != nil {
			mid = d.Orig.Modules.Author.Mid
		}
		if original, ok := d.Orig.dynamic(mid); ok {
			out.Kind, out.Original = KindDynamicRepost, &original
		}
	}
	return out, true
}

// body is the text and the pictures a dynamic shows. Newer dynamics keep both
// in an opus, older ones in a desc with a draw album.
func (d dynamicItem) body() (string, []string) {
	var text, title string
	images := []string{}
	dynamic := d.Modules.Dynamic
	if dynamic == nil {
		return "", images
	}
	if dynamic.Desc != nil {
		text = dynamic.Desc.Text
	}
	if major := dynamic.Major; major != nil {
		if major.Opus != nil {
			title = major.Opus.Title
			if major.Opus.Summary != nil && strings.TrimSpace(major.Opus.Summary.Text) != "" {
				text = major.Opus.Summary.Text
			}
			for _, pic := range major.Opus.Pics {
				images = append(images, pic.URL)
			}
		}
		if major.Draw != nil {
			for _, item := range major.Draw.Items {
				images = append(images, item.Src)
			}
		}
		if major.Archive != nil && strings.TrimSpace(text) == "" {
			// A forwarded upload says nothing itself; its title is the text.
			title = major.Archive.Title
		}
	}
	text = strings.TrimSpace(html.UnescapeString(text))
	if text == "" {
		text = strings.TrimSpace(html.UnescapeString(title))
	}
	if utf8.RuneCountInString(text) > maxDynamicText {
		text = string([]rune(text)[:maxDynamicText])
	}
	kept := []string{}
	seen := map[string]bool{}
	for _, raw := range images {
		url := thumbnail(raw)
		if url == "" || seen[url] || len(kept) >= maxDynamicImages {
			continue
		}
		seen[url] = true
		kept = append(kept, url)
	}
	return text, kept
}

const (
	dynamicTypeVideo   = "DYNAMIC_TYPE_AV"
	dynamicTypeForward = "DYNAMIC_TYPE_FORWARD"
)

// ParseDynamics reads one space feed page. Entries of another account and
// entries that carry nothing showable are skipped, and so are repeated IDs.
func ParseDynamics(data json.RawMessage, account string) ([]Dynamic, error) {
	var page struct {
		Items []dynamicItem `json:"items"`
	}
	if json.Unmarshal(data, &page) != nil || page.Items == nil {
		return nil, &Error{"bilibili_invalid_response"}
	}
	owner, _ := strconv.ParseInt(account, 10, 64)
	out := []Dynamic{}
	seen := map[string]bool{}
	for _, item := range page.Items {
		if d, ok := item.dynamic(owner); ok && !seen[d.ID] {
			seen[d.ID] = true
			out = append(out, d)
		}
	}
	return out, nil
}
