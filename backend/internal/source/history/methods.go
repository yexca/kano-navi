package history

import (
	"slices"
	"strings"

	xsource "github.com/yexca/kano-navi/backend/internal/source/x"
)

// Listing methods. The IDs are stable: runs freeze them, and the console and
// the timed source chains name methods by them.
const (
	// MethodTimeline pages the public FxTwitter timeline of an X account.
	MethodTimeline = "timeline"
	// MethodAPI is the source's official API: X full-archive search, or the
	// YouTube Data API's uploads playlist.
	MethodAPI = "api"
	// MethodRSS reads the YouTube channel's public feed.
	MethodRSS = "rss"
	// MethodYTDLP reads YouTube through the locally installed yt-dlp binary.
	MethodYTDLP = "ytdlp"
	// MethodCatalog pages the official shop's whole product catalog.
	MethodCatalog = "catalog"
	// MethodSpace pages the Bilibili account's space listing: its uploads,
	// or its dynamics with the login cookie.
	MethodSpace = "space"
)

// Method describes what one listing method of a source can serve.
type Method struct {
	ID string `json:"id"`
	// Modes are the selections it serves, in the order the console offers
	// them.
	Modes []string `json:"modes"`
	// MaxCount bounds a count selection per X account or for the channel.
	MaxCount int `json:"maxCount"`
	// PageSize is how many records one request lists at most.
	PageSize int `json:"pageSize"`
	// Requires names the environment variable holding the credential the
	// method needs, never its value; empty for public methods.
	Requires string `json:"requires,omitempty"`
	// Refresh reports whether a fetch may rewrite stored records it lists
	// again. X methods only fill gaps, so stored posts keep their extraction
	// fingerprints.
	Refresh bool `json:"refresh"`
	// Extract reports whether fetched records can go on to schedule
	// extraction in the same run.
	Extract bool `json:"extract"`
}

func (m Method) serves(mode string) bool { return slices.Contains(m.Modes, mode) }

// feedLimit is what the public YouTube feed lists.
const feedLimit = 15

var methods = map[string][]Method{
	SourceX: {
		{ID: MethodTimeline, Modes: []string{ModeCount, ModeAll}, MaxCount: MaxCount, PageSize: xsource.TimelinePageSize, Extract: true},
		{ID: MethodAPI, Modes: []string{ModeCount, ModeRange, ModeBefore}, MaxCount: MaxCount, PageSize: 100, Requires: "X_API_BEARER_TOKEN", Extract: true},
	},
	SourceYoutube: {
		{ID: MethodAPI, Modes: []string{ModeCount, ModeAll}, MaxCount: MaxCount, PageSize: 50, Requires: "YOUTUBE_API_KEY", Refresh: true},
		{ID: MethodRSS, Modes: []string{ModeCount}, MaxCount: feedLimit, PageSize: feedLimit, Refresh: true},
		{ID: MethodYTDLP, Modes: []string{ModeCount, ModeAll}, MaxCount: MaxCount, PageSize: MaxCount, Requires: "YTDLP_BINARY", Refresh: true},
	},
	// Every shop read rewrites the period groups it lists, so its fetch has
	// no refresh option.
	SourceShop: {
		{ID: MethodCatalog, Modes: []string{ModeAll}, PageSize: 250},
	},
	// A related account reads its FxTwitter timeline from its own cursor,
	// bounded as the x_related timer reads it.
	SourceXRelated: {
		{ID: MethodTimeline, Modes: []string{ModeIncremental}, PageSize: xsource.TimelinePageSize, Extract: true},
	},
	// Every Bilibili read rewrites the records it lists, as the timed reads
	// do, so these fetches have no refresh option either.
	SourceBilibili: {
		{ID: MethodSpace, Modes: []string{ModeCount, ModeAll}, MaxCount: MaxCount, PageSize: BilibiliUploadsPageSize},
	},
	SourceBilibiliDynamics: {
		{ID: MethodSpace, Modes: []string{ModeCount, ModeAll}, MaxCount: MaxCount, PageSize: BilibiliDynamicsPageSize, Requires: "BILIBILI_COOKIE or BILIBILI_SESSDATA", Extract: true},
	},
}

// Page sizes of the Bilibili space listings: the uploads listing is asked
// for 20 entries a page, as the timed read asks; the dynamics listing answers
// about 12 a page whatever is asked.
const (
	BilibiliUploadsPageSize  = 20
	BilibiliDynamicsPageSize = 12
)

// Methods lists a source's listing methods in the order the console offers
// them. The slices are copies.
func Methods(source string) []Method {
	out := []Method{}
	for _, method := range methods[source] {
		method.Modes = slices.Clone(method.Modes)
		out = append(out, method)
	}
	return out
}

// Lookup finds one of a source's methods by ID.
func Lookup(source, id string) (Method, bool) {
	for _, method := range Methods(source) {
		if method.ID == id {
			return method, true
		}
	}
	return Method{}, false
}

func methodNames(source string) string {
	names := []string{}
	for _, method := range methods[source] {
		names = append(names, method.ID)
	}
	return strings.Join(names, ", ")
}

// MethodStatus is a listing method and whether a process can run it now.
type MethodStatus struct {
	Method
	Available bool `json:"available"`
}

// UnavailableMessage tells the operator why a method cannot run.
func UnavailableMessage(source, id string) string {
	if method, ok := Lookup(source, id); ok && method.Requires != "" {
		return method.Requires + " is required for the " + id + " method"
	}
	return "the " + id + " method is unavailable"
}
