package bilibili

import (
	"bytes"
	"encoding/json"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Dynamic is one post (动态) of the account as stored in posts
// (source = 'bilibili').
type Dynamic struct {
	// IDStr is Bilibili's dynamic ID; the stored post ID is PostID().
	IDStr string
	// OwnerMid is the author's account.
	OwnerMid int64
	// Text joins the word, topic, mention and link nodes; emoji nodes are
	// left out, so an emoji-only dynamic has no text.
	Text string
	// PublishedAt is the publication time, RFC 3339 in UTC.
	PublishedAt string
	// Images are the dynamic's own pictures on Bilibili's image CDN, HTTPS.
	// A forward has none of its own.
	Images []string
	// Quote is the dynamic a forward forwards, nil for any other dynamic.
	Quote *Quote
	// Raw keeps the type and the source nodes, emoji included.
	Raw map[string]any
}

// Quote is the dynamic a forward (DYNAMIC_TYPE_FORWARD) forwards, in the
// shape of the stored quote projection (posts.quote_json) that an X quote
// also has. ID is the forwarded dynamic's post ID (PostPrefix and its
// dynamic ID), so a forward of a stored dynamic names that post.
// AuthorHandle is the author's mid. URL is the forwarded dynamic's opus
// page, or the video page of a forwarded upload. A forwarded dynamic the
// listing cannot show (deleted, or a kind that is neither a post nor an
// upload) is Unavailable with only its ID and URL; when even its ID is
// unknown, ID is empty and URL is the forward's own opus page. MediaURLs are
// its pictures, which the store links as quote-image media and never as
// schedule candidates.
type Quote struct {
	ID           string   `json:"id"`
	AuthorHandle string   `json:"authorHandle,omitempty"`
	AuthorName   string   `json:"authorName,omitempty"`
	Text         string   `json:"text,omitempty"`
	PublishedAt  string   `json:"publishedAt,omitempty"`
	URL          string   `json:"url"`
	Unavailable  bool     `json:"unavailable,omitempty"`
	MediaURLs    []string `json:"-"`
}

// PostPrefix keeps dynamic IDs apart from X status IDs in posts.id: both are
// decimal numbers of about 19 digits.
const PostPrefix = "bilibili-"

func (d Dynamic) PostID() string { return PostPrefix + d.IDStr }

// URL is the dynamic's public opus page.
func (d Dynamic) URL() string { return "https://" + wwwHost + "/opus/" + d.IDStr }

var dynamicIDPattern = regexp.MustCompile(`^[1-9][0-9]{8,24}$`)

// ParseDynamicID accepts a bilibili.com/opus/<id> or t.bilibili.com/<id>
// link, or a bare dynamic ID, and returns the ID.
func ParseDynamicID(input string) (string, bool) {
	input = strings.TrimSpace(input)
	if dynamicIDPattern.MatchString(input) {
		return input, true
	}
	u, err := url.Parse(input)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch strings.ToLower(u.Hostname()) {
	case "www.bilibili.com", "bilibili.com", "m.bilibili.com":
		if len(parts) == 2 && parts[0] == "opus" && dynamicIDPattern.MatchString(parts[1]) {
			return parts[1], true
		}
	case "t.bilibili.com":
		if len(parts) == 1 && dynamicIDPattern.MatchString(parts[0]) {
			return parts[0], true
		}
	}
	return "", false
}

// Dynamic types the listing keeps: text, picture and opus posts, an article
// only in its opus form, and forwards, which quote the dynamic they forward.
// Uploads and collection updates are the upload read's, and a live-start
// notice is not a post.
var keptDynamicTypes = map[string]bool{"DYNAMIC_TYPE_WORD": true, "DYNAMIC_TYPE_DRAW": true, "DYNAMIC_TYPE_ARTICLE": true, ForwardType: true}

// ForwardType is a forward's dynamic type, also kept in its raw_json.
const ForwardType = "DYNAMIC_TYPE_FORWARD"

// maxAuthorName bounds a forwarded dynamic's author name, in runes.
const maxAuthorName = 50

// keptCommentTypes are the comment areas of the kept types as an opus page
// names them: 11 a picture post, 12 an article, 17 a text post. A video (1)
// or another kind is not a post.
var keptCommentTypes = map[int]bool{11: true, 12: true, 17: true}

const emojiNode = "RICH_TEXT_NODE_TYPE_EMOJI"

// maxDynamicText bounds the stored text of one dynamic, in runes.
const maxDynamicText = 5000

// maxDynamicImages bounds the pictures kept from one dynamic.
const maxDynamicImages = 9

type richNode struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	OrigTxt string `json:"orig_text"`
}

// joinNodes keeps every node's text except emoji.
func joinNodes(nodes []richNode) string {
	var b strings.Builder
	for _, n := range nodes {
		if n.Type == emojiNode {
			continue
		}
		b.WriteString(n.Text)
	}
	return b.String()
}

func boundText(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if r := []rune(text); len(r) > maxDynamicText {
		text = string(r[:maxDynamicText])
	}
	return text
}

func images(urls []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, raw := range urls {
		if u := thumbnail(raw); u != "" && !seen[u] && len(out) < maxDynamicImages {
			seen[u] = true
			out = append(out, u)
		}
	}
	return out
}

// flexInt reads a number given as a JSON number or a decimal string.
type flexInt int64

func (f *flexInt) UnmarshalJSON(b []byte) error {
	b = bytes.Trim(b, `"`)
	n, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		*f = 0
		return nil
	}
	*f = flexInt(n)
	return nil
}

type spaceItem struct {
	IDStr   string `json:"id_str"`
	Type    string `json:"type"`
	Visible *bool  `json:"visible"`
	// Basic.IsOnlyFans marks a dynamic only the account's paying supporters
	// (充电专属) may read; a signed-in supporter's listing carries it in full.
	Basic struct {
		IsOnlyFans bool `json:"is_only_fans"`
	} `json:"basic"`
	Modules struct {
		Author struct {
			Mid   flexInt `json:"mid"`
			Name  string  `json:"name"`
			PubTS flexInt `json:"pub_ts"`
		} `json:"module_author"`
		Dynamic struct {
			Desc *struct {
				Nodes []richNode `json:"rich_text_nodes"`
				Text  string     `json:"text"`
			} `json:"desc"`
			Major *struct {
				Type string `json:"type"`
				Opus *struct {
					Title   string `json:"title"`
					Summary *struct {
						Nodes []richNode `json:"rich_text_nodes"`
						Text  string     `json:"text"`
					} `json:"summary"`
					Pics []struct {
						URL string `json:"url"`
					} `json:"pics"`
				} `json:"opus"`
				Draw *struct {
					Items []struct {
						Src string `json:"src"`
					} `json:"items"`
				} `json:"draw"`
				Archive *struct {
					BVID         string  `json:"bvid"`
					AID          flexInt `json:"aid"`
					Title        string  `json:"title"`
					Cover        string  `json:"cover"`
					Desc         string  `json:"desc"`
					DurationText string  `json:"duration_text"`
				} `json:"archive"`
			} `json:"major"`
		} `json:"module_dynamic"`
		Tag *struct {
			Text string `json:"text"`
		} `json:"module_tag"`
	} `json:"modules"`
	// Orig is the dynamic a forward forwards, carried in full by the listing.
	// It is decoded on its own (see original) so a bad one never fails the
	// page.
	Orig json.RawMessage `json:"orig"`
}

// original is the forwarded dynamic, or nil when the forward has none. One
// that does not decode is read for its ID alone, so its quote is
// unavailable.
func (it spaceItem) original() *spaceItem {
	if len(it.Orig) == 0 || string(it.Orig) == "null" {
		return nil
	}
	var orig spaceItem
	if json.Unmarshal(it.Orig, &orig) == nil {
		return &orig
	}
	var id struct {
		IDStr string `json:"id_str"`
	}
	if json.Unmarshal(it.Orig, &id) != nil {
		return nil
	}
	return &spaceItem{IDStr: id.IDStr}
}

// dynamic validates one listing item of the account; it reports false for a
// kind that is not a post, a supporters-only dynamic or an incomplete item. A forward keeps its own
// wording and quotes the dynamic it forwards, whether or not that dynamic can
// still be shown.
func (it spaceItem) dynamic(owner int64) (Dynamic, bool) {
	if !keptDynamicTypes[it.Type] || !dynamicIDPattern.MatchString(it.IDStr) || it.Modules.Author.PubTS <= 0 ||
		int64(it.Modules.Author.Mid) != owner || it.Visible != nil && !*it.Visible || it.Basic.IsOnlyFans {
		return Dynamic{}, false
	}
	m := it.Modules.Dynamic
	if it.Type == "DYNAMIC_TYPE_ARTICLE" && (m.Major == nil || m.Major.Type != "MAJOR_TYPE_OPUS") {
		return Dynamic{}, false
	}
	published := time.Unix(int64(it.Modules.Author.PubTS), 0).UTC().Format(time.RFC3339)
	if it.Type == ForwardType {
		nodes := []richNode{}
		if m.Desc != nil && m.Desc.Nodes != nil {
			nodes = m.Desc.Nodes
		}
		d := Dynamic{IDStr: it.IDStr, OwnerMid: owner, Text: boundText(joinNodes(nodes)), PublishedAt: published, Images: []string{}}
		// A forward without wording of its own whose dynamic is gone is still
		// kept: the store rewrites a stored one and leaves out a new one.
		forwarded := it.original()
		d.Quote = forwarded.quote(d.URL())
		orig := map[string]any{"type": "", "id": strings.TrimPrefix(d.Quote.ID, PostPrefix)}
		if forwarded != nil && len(forwarded.Type) <= 64 {
			orig["type"] = forwarded.Type
		}
		d.Raw = map[string]any{"type": it.Type, "nodes": nodes, "orig": orig}
		return d, true
	}
	text, nodes, title, pics := it.content()
	raw := map[string]any{"type": it.Type, "nodes": nodes}
	if title != "" {
		raw["title"] = title
	}
	if it.Modules.Tag != nil && it.Modules.Tag.Text != "" {
		raw["tag"] = it.Modules.Tag.Text
	}
	return Dynamic{IDStr: it.IDStr, OwnerMid: owner, Text: text, PublishedAt: published, Images: images(pics), Raw: raw}, true
}

// content is a text, picture or opus dynamic's bounded wording (an opus
// title on its first line), its rich-text nodes, its title and its pictures.
func (it spaceItem) content() (string, []richNode, string, []string) {
	m := it.Modules.Dynamic
	var nodes []richNode
	title := ""
	pics := []string{}
	if m.Major != nil {
		switch m.Major.Type {
		case "MAJOR_TYPE_OPUS":
			if o := m.Major.Opus; o != nil {
				title = strings.TrimSpace(o.Title)
				if o.Summary != nil {
					nodes = o.Summary.Nodes
				}
				for _, p := range o.Pics {
					pics = append(pics, p.URL)
				}
			}
		case "MAJOR_TYPE_DRAW":
			if d := m.Major.Draw; d != nil {
				for _, p := range d.Items {
					pics = append(pics, p.Src)
				}
			}
		case "", "MAJOR_TYPE_NONE":
		default:
			// Another card (a vote, a reservation or goods) leaves the text.
		}
	}
	if nodes == nil && m.Desc != nil {
		nodes = m.Desc.Nodes
	}
	text := joinNodes(nodes)
	if title != "" {
		text = title + "\n" + text
	}
	return boundText(text), nodes, title, pics
}

// quote projects the dynamic a forward forwards, from the listing item alone.
// It never fails: a deleted dynamic, a missing one or one of another kind (a
// live room, a collection, goods or a vote) is unavailable. A forwarded
// forward shows its own wording only. selfURL is the forward's own opus page,
// the link of an unavailable quote whose ID is unknown.
func (it *spaceItem) quote(selfURL string) *Quote {
	if it == nil || !dynamicIDPattern.MatchString(it.IDStr) {
		return &Quote{URL: selfURL, Unavailable: true}
	}
	q := &Quote{ID: PostPrefix + it.IDStr, URL: "https://" + wwwHost + "/opus/" + it.IDStr, Unavailable: true}
	m := it.Modules.Dynamic
	major := ""
	if m.Major != nil {
		major = m.Major.Type
	}
	if it.Modules.Author.Mid <= 0 || it.Modules.Author.PubTS <= 0 || it.Visible != nil && !*it.Visible || it.Basic.IsOnlyFans || major == "MAJOR_TYPE_NONE" {
		return q
	}
	text, pics, link := "", []string{}, q.URL
	switch {
	case it.Type == "DYNAMIC_TYPE_WORD" || it.Type == "DYNAMIC_TYPE_DRAW" || it.Type == "DYNAMIC_TYPE_ARTICLE" && major == "MAJOR_TYPE_OPUS":
		text, _, _, pics = it.content()
	case it.Type == ForwardType:
		if m.Desc != nil {
			text = boundText(joinNodes(m.Desc.Nodes))
		}
	case it.Type == "DYNAMIC_TYPE_AV" && major == "MAJOR_TYPE_ARCHIVE" && m.Major.Archive != nil && bvidPattern.MatchString(m.Major.Archive.BVID):
		a := m.Major.Archive
		link = "https://" + wwwHost + "/video/" + a.BVID
		text, pics = boundText(a.Title), []string{a.Cover}
	default:
		return q
	}
	media := images(pics)
	if text == "" && len(media) == 0 {
		return q
	}
	name := []rune(strings.TrimSpace(html.UnescapeString(it.Modules.Author.Name)))
	if len(name) > maxAuthorName {
		name = name[:maxAuthorName]
	}
	// Only an available upload links to its video page; an unavailable quote
	// keeps the opus page.
	q.Unavailable, q.URL = false, link
	q.AuthorHandle = strconv.FormatInt(int64(it.Modules.Author.Mid), 10)
	q.AuthorName = strings.TrimSpace(string(name))
	q.Text, q.MediaURLs = text, media
	q.PublishedAt = time.Unix(int64(it.Modules.Author.PubTS), 0).UTC().Format(time.RFC3339)
	return q
}

// upload reads a video dynamic of the account as the partial upload it
// publishes: the archive's BV ID, title, cover, description and length,
// published at the dynamic's time. It reports false for any other item or an
// incomplete one.
func (it spaceItem) upload(owner int64) (Video, bool) {
	m := it.Modules.Dynamic
	if it.Type != "DYNAMIC_TYPE_AV" || m.Major == nil || m.Major.Type != "MAJOR_TYPE_ARCHIVE" || m.Major.Archive == nil ||
		int64(it.Modules.Author.Mid) != owner || it.Visible != nil && !*it.Visible {
		return Video{}, false
	}
	a := m.Major.Archive
	duration, _ := json.Marshal(a.DurationText)
	v, ok := upload{
		BVID: a.BVID, AID: int64(a.AID), Title: a.Title, Pic: a.Cover, PubDate: int64(it.Modules.Author.PubTS),
		Duration: duration, Desc: a.Desc, UpMid: owner,
	}.video()
	v.Partial = ok
	return v, ok
}

// SpacePage is one page of the account's dynamics listing.
type SpacePage struct {
	Dynamics []Dynamic
	// Uploads are the account's video dynamics read as uploads.
	Uploads []Video
	// Seen counts every item the page listed, kept or not.
	Seen    int
	HasMore bool
	Offset  string
}

// ParseSpaceFeed reads a feed/space page. A v_voucher answer instead of
// items is Bilibili's risk control.
func ParseSpaceFeed(data json.RawMessage, account string) (SpacePage, error) {
	var page struct {
		Items    []spaceItem `json:"items"`
		HasMore  bool        `json:"has_more"`
		Offset   string      `json:"offset"`
		VVoucher string      `json:"v_voucher"`
	}
	if json.Unmarshal(data, &page) != nil {
		return SpacePage{}, &Error{"bilibili_invalid_response"}
	}
	if page.VVoucher != "" {
		return SpacePage{}, &Error{"bilibili_risk_control_voucher"}
	}
	if page.Items == nil {
		return SpacePage{}, &Error{"bilibili_invalid_response"}
	}
	owner, _ := strconv.ParseInt(account, 10, 64)
	out := SpacePage{Dynamics: []Dynamic{}, Uploads: []Video{}, Seen: len(page.Items), HasMore: page.HasMore, Offset: page.Offset}
	seen := map[string]bool{}
	for _, item := range page.Items {
		if d, ok := item.dynamic(owner); ok && !seen[d.IDStr] {
			seen[d.IDStr] = true
			out.Dynamics = append(out.Dynamics, d)
		}
		if v, ok := item.upload(owner); ok && !seen[v.BVID] {
			seen[v.BVID] = true
			out.Uploads = append(out.Uploads, v)
		}
	}
	return out, nil
}

// opusDetail is the item of the opus page's server state and of the
// opus/detail API: a module list rather than the listing's module object.
type opusDetail struct {
	IDStr string `json:"id_str"`
	Basic struct {
		CommentType *int `json:"comment_type"`
	} `json:"basic"`
	// Fallback is set when the page shows another kind of content instead.
	Fallback json.RawMessage `json:"fallback"`
	Modules  []struct {
		Type   string `json:"module_type"`
		Author *struct {
			Mid   flexInt `json:"mid"`
			PubTS flexInt `json:"pub_ts"`
		} `json:"module_author"`
		Title *struct {
			Text string `json:"text"`
		} `json:"module_title"`
		Content *struct {
			Paragraphs []struct {
				ParaType int `json:"para_type"`
				Text     *struct {
					Nodes []struct {
						Type string `json:"type"`
						Word *struct {
							Words string `json:"words"`
						} `json:"word"`
						Rich *struct {
							Type string `json:"type"`
							Text string `json:"text"`
						} `json:"rich"`
					} `json:"nodes"`
				} `json:"text"`
				Pic *struct {
					Pics []struct {
						URL string `json:"url"`
					} `json:"pics"`
				} `json:"pic"`
			} `json:"paragraphs"`
		} `json:"module_content"`
	} `json:"modules"`
}

func (d opusDetail) dynamic(id string) (Dynamic, error) {
	if d.IDStr != id {
		return Dynamic{}, &Error{"bilibili_invalid_response"}
	}
	if d.Basic.CommentType == nil || !keptCommentTypes[*d.Basic.CommentType] || len(d.Fallback) > 0 && string(d.Fallback) != "null" {
		return Dynamic{}, &Error{"bilibili_unsupported_dynamic"}
	}
	var owner, published int64
	title := ""
	var paragraphs []string
	nodes := []richNode{}
	pics := []string{}
	content := false
	for _, m := range d.Modules {
		switch {
		case m.Author != nil && m.Type == "MODULE_TYPE_AUTHOR":
			owner, published = int64(m.Author.Mid), int64(m.Author.PubTS)
		case m.Title != nil && m.Type == "MODULE_TYPE_TITLE":
			title = strings.TrimSpace(m.Title.Text)
		case m.Content != nil && m.Type == "MODULE_TYPE_CONTENT":
			content = true
			for _, p := range m.Content.Paragraphs {
				if p.Pic != nil {
					for _, pic := range p.Pic.Pics {
						pics = append(pics, pic.URL)
					}
				}
				if p.Text == nil {
					continue
				}
				var b strings.Builder
				for _, n := range p.Text.Nodes {
					switch {
					case n.Word != nil:
						b.WriteString(n.Word.Words)
						nodes = append(nodes, richNode{Type: "RICH_TEXT_NODE_TYPE_TEXT", Text: n.Word.Words})
					case n.Rich != nil:
						nodes = append(nodes, richNode{Type: n.Rich.Type, Text: n.Rich.Text})
						if n.Rich.Type != emojiNode {
							b.WriteString(n.Rich.Text)
						}
					}
				}
				paragraphs = append(paragraphs, strings.TrimRight(b.String(), "\n"))
			}
		}
	}
	if !content || owner <= 0 || published <= 0 {
		return Dynamic{}, &Error{"bilibili_invalid_response"}
	}
	text := strings.Join(paragraphs, "\n")
	if title != "" {
		text = title + "\n" + text
	}
	raw := map[string]any{"type": "opus", "nodes": nodes}
	if title != "" {
		raw["title"] = title
	}
	return Dynamic{
		IDStr: id, OwnerMid: owner, Text: boundText(text),
		PublishedAt: time.Unix(published, 0).UTC().Format(time.RFC3339),
		Images:      images(pics), Raw: raw,
	}, nil
}

var initialState = []byte("window.__INITIAL_STATE__=")

// ParseOpusPage reads the server-rendered state of a www.bilibili.com/opus
// page for dynamic id. A page without that state is the captcha or an error
// page, which starts the backoff like any risk-control answer.
func ParseOpusPage(body []byte, id string) (Dynamic, error) {
	start := bytes.Index(body, initialState)
	if start < 0 {
		return Dynamic{}, &Error{"bilibili_risk_control_page"}
	}
	var state struct {
		Detail *opusDetail `json:"detail"`
	}
	// The state is one JSON object followed by the page's script.
	if json.NewDecoder(bytes.NewReader(body[start+len(initialState):])).Decode(&state) != nil {
		return Dynamic{}, &Error{"bilibili_invalid_response"}
	}
	if state.Detail == nil {
		return Dynamic{}, &Error{"bilibili_risk_control_page"}
	}
	return state.Detail.dynamic(id)
}
