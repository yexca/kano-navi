package sqlite

import (
	"encoding/json"
	"slices"
)

// ChainMethod is one listing method of a source's fallback chain
// (ADR-0004), by its stable ID.
type ChainMethod struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}

// SourceChain is the ordered fallback over a source's listing methods that
// timed reads try (ADR-0004). APIGate and APIDailyCap bound X's official API
// in the chain: it runs only after the timeline failed APIGate consecutive
// runs of an account, and reads at most APIDailyCap posts a Japan day.
type SourceChain struct {
	Methods     []ChainMethod `json:"methods"`
	APIGate     int           `json:"apiGate,omitempty"`
	APIDailyCap int           `json:"apiDailyCap,omitempty"`
}

const (
	// DefaultXAPIGate is how many consecutive timeline failures of an
	// account open the X API in its chain.
	DefaultXAPIGate = 3
	MaxXAPIGate     = 100
	// DefaultXAPIDailyCap bounds the posts timed X API reads return a Japan
	// day; see docs/architecture/sources.md#x-api-in-the-chain.
	DefaultXAPIDailyCap = 100
	MaxXAPIDailyCap     = 10000
)

// chainMethods are each source's listing methods in their default order,
// with whether the default chain enables them. The default is the order the
// recent steps used before chains, so an upgrade changes nothing: X reads
// the timeline, then the profile page, and leaves the API off; YouTube tries
// the Data API (with a key), the feed, then the uploads tab. Bilibili has one
// fixed method.
var chainMethods = map[string][]ChainMethod{
	"x":        {{ID: "timeline", Enabled: true}, {ID: "profile", Enabled: true}, {ID: "api", Enabled: false}},
	"youtube":  {{ID: "api", Enabled: true}, {ID: "rss", Enabled: true}, {ID: "videos_page", Enabled: true}},
	"bilibili": {{ID: "list", Enabled: true}},
}

// ChainSources are the sources whose chain the operator orders.
var ChainSources = []string{"x", "youtube"}

// DefaultSourceChain is a source's chain before the operator saves one.
func DefaultSourceChain(source string) SourceChain {
	chain := SourceChain{Methods: slices.Clone(chainMethods[source])}
	if source == "x" {
		chain.APIGate, chain.APIDailyCap = DefaultXAPIGate, DefaultXAPIDailyCap
	}
	return chain
}

// ChainMethodIDs lists a source's chain method IDs in their default order.
func ChainMethodIDs(source string) []string {
	out := []string{}
	for _, method := range chainMethods[source] {
		out = append(out, method.ID)
	}
	return out
}

// Chain validation failures, which the admin API reports as they are.
const (
	ChainUnknownMethod   = "unknown method"
	ChainDuplicateMethod = "duplicate method"
	ChainMissingMethod   = "missing method"
	ChainAllDisabled     = "no enabled method"
	ChainFixed           = "fixed chain"
	ChainBounds          = "setting out of range"
)

// Problem reports why a chain cannot be stored for source, or "". A chain
// lists every method of the source exactly once, at least one enabled;
// only X carries the API gate and cap.
func (c SourceChain) Problem(source string) string {
	if !slices.Contains(ChainSources, source) {
		return ChainFixed
	}
	known := ChainMethodIDs(source)
	seen := map[string]bool{}
	enabled := false
	for _, method := range c.Methods {
		switch {
		case !slices.Contains(known, method.ID):
			return ChainUnknownMethod
		case seen[method.ID]:
			return ChainDuplicateMethod
		}
		seen[method.ID] = true
		enabled = enabled || method.Enabled
	}
	if len(seen) != len(known) {
		return ChainMissingMethod
	}
	if !enabled {
		return ChainAllDisabled
	}
	if source == "x" {
		if c.APIGate < 0 || c.APIGate > MaxXAPIGate || c.APIDailyCap < 1 || c.APIDailyCap > MaxXAPIDailyCap {
			return ChainBounds
		}
	} else if c.APIGate != 0 || c.APIDailyCap != 0 {
		return ChainBounds
	}
	return ""
}

// Enabled lists the enabled method IDs in chain order.
func (c SourceChain) Enabled() []string {
	out := []string{}
	for _, method := range c.Methods {
		if method.Enabled {
			out = append(out, method.ID)
		}
	}
	return out
}

// Uses reports whether the chain enables the method.
func (c SourceChain) Uses(id string) bool { return slices.Contains(c.Enabled(), id) }

// readSourceChain parses a stored chain. A chain that is unreadable or no
// longer valid, as '{}' is, reads as the default.
func readSourceChain(source, raw string) SourceChain {
	var stored SourceChain
	if json.Unmarshal([]byte(raw), &stored) == nil && stored.Problem(source) == "" {
		return stored
	}
	return DefaultSourceChain(source)
}
