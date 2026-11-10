package shop

import (
	"encoding/json"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Tag is the product tag of every product Kano takes part in. This board is
// hers, so it is a constant.
const Tag = "鹿乃まほろ"

// Product is what the source reads of one storefront product: no price,
// variant, stock or image is decoded.
type Product struct {
	ID          int64
	Handle      string
	Title       string
	Tags        []string
	PublishedAt time.Time
	// Body is the description HTML; only the period line is kept from it.
	Body string
}

// Collection is one entry of the collection list.
type Collection struct {
	Handle string
	Title  string
}

var (
	handlePattern = regexp.MustCompile(`^[^\s/?#%\\]{1,255}$`)
	htmlTag       = regexp.MustCompile(`<[^>]*>`)
	// periodPattern is a labelled period with a dated opening and close on
	// one line: ＜受注期間＞ 2026年10月24日 〜 2026年11月24日. The close may omit
	// its year; a weekday or time may follow the opening.
	periodPattern = regexp.MustCompile(`(抽選受付期間|受注期間|販売期間|受付期間)[^0-9]{0,8}?(\d{4})\s*年\s*(\d{1,2})\s*月\s*(\d{1,2})\s*日[^〜～~年\n]{0,24}?[〜～~][ \t]*(?:(\d{4})\s*年\s*)?(\d{1,2})\s*月\s*(\d{1,2})\s*日`)
	// openedPattern is a labelled opening without a close, such as a
	// permanent item's 販売期間 2026年10月1日～.
	openedPattern = regexp.MustCompile(`(抽選受付期間|受注期間|販売期間|受付期間)[^0-9]{0,8}?\d{4}\s*年\s*\d{1,2}\s*月\s*\d{1,2}\s*日`)
	// spaceSeparators are the Unicode spaces, such as the U+00A0 that &nbsp;
	// decodes to and the ideographic U+3000; a description reads each as a
	// plain space.
	spaceSeparators = regexp.MustCompile(`\p{Zs}`)
	wideDigits      = strings.NewReplacer("０", "0", "１", "1", "２", "2", "３", "3", "４", "4", "５", "5", "６", "6", "７", "7", "８", "8", "９", "9", "：", ":")
	japan           = time.FixedZone("JST", 9*3600)
)

type productJSON struct {
	ID          int64           `json:"id"`
	Handle      string          `json:"handle"`
	Title       string          `json:"title"`
	Tags        json.RawMessage `json:"tags"`
	PublishedAt string          `json:"published_at"`
	Body        string          `json:"body_html"`
}

// ParseProducts reads a products.json answer. A product without a usable ID,
// handle, title or publication time is left out.
func ParseProducts(body []byte) ([]Product, error) {
	products, _, err := parseProductPage(body)
	return products, err
}

// parseProductPage reads a products.json answer as ParseProducts does, with
// the number of products the answer lists before unusable ones are left out.
func parseProductPage(body []byte) ([]Product, int, error) {
	var out struct {
		Products *[]productJSON `json:"products"`
	}
	if err := decode(body, &out); err != nil {
		return nil, 0, err
	}
	if out.Products == nil {
		return nil, 0, &Error{Code: "shop_invalid_json"}
	}
	products := []Product{}
	for _, p := range *out.Products {
		title := cleanText(p.Title, 200)
		published, err := time.Parse(time.RFC3339, p.PublishedAt)
		if p.ID <= 0 || !handlePattern.MatchString(p.Handle) || title == "" || err != nil {
			continue
		}
		products = append(products, Product{ID: p.ID, Handle: p.Handle, Title: title, Tags: parseTags(p.Tags), PublishedAt: published, Body: p.Body})
	}
	return products, len(*out.Products), nil
}

// parseTags reads tags as an array, or as the comma-separated string some
// Shopify answers give.
func parseTags(raw json.RawMessage) []string {
	var list []string
	if json.Unmarshal(raw, &list) != nil {
		var joined string
		if json.Unmarshal(raw, &joined) != nil {
			return nil
		}
		list = strings.Split(joined, ",")
	}
	out := []string{}
	for _, tag := range list {
		if tag = strings.TrimSpace(tag); tag != "" {
			out = append(out, tag)
		}
	}
	return out
}

// ParseCollections reads a collections.json answer.
func ParseCollections(body []byte) ([]Collection, error) {
	var out struct {
		Collections *[]struct {
			Handle string `json:"handle"`
			Title  string `json:"title"`
		} `json:"collections"`
	}
	if err := decode(body, &out); err != nil {
		return nil, err
	}
	if out.Collections == nil {
		return nil, &Error{Code: "shop_invalid_json"}
	}
	collections := []Collection{}
	for _, c := range *out.Collections {
		if title := cleanText(c.Title, 200); title != "" && handlePattern.MatchString(c.Handle) {
			collections = append(collections, Collection{Handle: c.Handle, Title: title})
		}
	}
	return collections, nil
}

// Tagged reports a product Kano takes part in.
func (p Product) Tagged() bool {
	for _, tag := range p.Tags {
		if tag == Tag {
			return true
		}
	}
	return false
}

// Period is a dated sale period: Japan dates, both inclusive, and the line
// it was read from.
type Period struct {
	Opens, Closes string
	Line          string
}

// Key is the period's identity, <opens>/<closes>.
func (p Period) Key() string { return p.Opens + "/" + p.Closes }

// Reasons a product is not a sale period.
const (
	SkipUntagged  = "untagged"
	SkipNoPeriod  = "no_period"
	SkipOpenEnded = "open_ended"
	SkipInvalid   = "invalid_period"
)

// ParsePeriod reads the first labelled period of a description. A period
// without a close (a permanent item) or with impossible dates is not one,
// and the reason says why.
func ParsePeriod(body string) (Period, string) {
	text := descriptionText(body)
	match := periodPattern.FindStringSubmatch(text)
	if match == nil {
		if openedPattern.MatchString(text) {
			return Period{}, SkipOpenEnded
		}
		return Period{}, SkipNoPeriod
	}
	opens, ok := date(match[2], match[3], match[4])
	if !ok {
		return Period{}, SkipInvalid
	}
	// A close without a year falls on or after the opening: in the opening's
	// year, or the next one when its month and day come earlier.
	closeYear := match[5]
	if closeYear == "" {
		year := opens.Year()
		month, _ := strconv.Atoi(match[6])
		day, _ := strconv.Atoi(match[7])
		if month < int(opens.Month()) || month == int(opens.Month()) && day < opens.Day() {
			year++
		}
		closeYear = strconv.Itoa(year)
	}
	closes, ok := date(closeYear, match[6], match[7])
	if !ok || closes.Before(opens) || closes.After(opens.AddDate(2, 0, 0)) {
		return Period{}, SkipInvalid
	}
	return Period{Opens: opens.Format(time.DateOnly), Closes: closes.Format(time.DateOnly), Line: cleanText(match[0], 120)}, ""
}

func date(year, month, day string) (time.Time, bool) {
	y, errY := strconv.Atoi(year)
	m, errM := strconv.Atoi(month)
	d, errD := strconv.Atoi(day)
	if errY != nil || errM != nil || errD != nil || y < 2000 || y > 2100 {
		return time.Time{}, false
	}
	t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, japan)
	return t, t.Year() == y && int(t.Month()) == m && t.Day() == d
}

// descriptionText is the description as lines of text: tags become line
// breaks, entities are decoded, every Unicode space reads as a plain space
// and full-width digits read as digits.
func descriptionText(body string) string {
	text := html.UnescapeString(htmlTag.ReplaceAllString(body, "\n"))
	return wideDigits.Replace(spaceSeparators.ReplaceAllString(text, " "))
}

// cleanText decodes entities, composes decomposed kana, collapses whitespace
// and cuts to limit runes. A title is kept as the shop writes it apart from
// that composition, which changes no character a reader sees but lets the
// stored title match posts and other titles of the same goods.
func cleanText(value string, limit int) string {
	text := strings.Join(strings.Fields(composeKana(html.UnescapeString(value))), " ")
	if runes := []rune(text); len(runes) > limit {
		text = strings.TrimSpace(string(runes[:limit]))
	}
	return text
}

// normalize is a title's identity: kana composed, without whitespace, ASCII
// letters in lower case, so 鹿乃まほろ 加入記念グッズ and 鹿乃まほろ加入記念グッズ
// agree, and so do ボ and ホ followed by U+3099.
func normalize(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		if r < unicode.MaxASCII {
			return unicode.ToLower(r)
		}
		return r
	}, composeKana(value))
}

// Candidate is a tagged product with a dated period.
type Candidate struct {
	Product
	Period Period
}

// Candidates keeps the tagged products with a dated period, and counts why
// each other product is not one.
func Candidates(products []Product) ([]Candidate, map[string]int) {
	out, skipped := []Candidate{}, map[string]int{}
	for _, p := range products {
		if !p.Tagged() {
			skipped[SkipUntagged]++
			continue
		}
		period, reason := ParsePeriod(p.Body)
		if reason != "" {
			skipped[reason]++
			continue
		}
		out = append(out, Candidate{Product: p, Period: period})
	}
	return out, skipped
}
