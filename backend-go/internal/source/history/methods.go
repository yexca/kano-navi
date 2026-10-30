package history

import (
	"slices"
	"strings"
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
		{ID: MethodTimeline, Modes: []string{ModeCount, ModeAll}, MaxCount: MaxCount, PageSize: 100, Extract: true},
		{ID: MethodAPI, Modes: []string{ModeCount, ModeRange, ModeBefore}, MaxCount: MaxCount, PageSize: 100, Requires: "X_API_BEARER_TOKEN", Extract: true},
	},
	SourceYoutube: {
		{ID: MethodAPI, Modes: []string{ModeCount, ModeAll}, MaxCount: MaxCount, PageSize: 50, Requires: "YOUTUBE_API_KEY", Refresh: true},
		{ID: MethodRSS, Modes: []string{ModeCount}, MaxCount: feedLimit, PageSize: feedLimit, Refresh: true},
	},
}

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
