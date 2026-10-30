package workflow

import (
	ytsource "github.com/yexca/kano-navi/backend-go/internal/source/youtube"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// ChainSource is the source whose method chain a step reads through
// (ADR-0004): the live-status probe reads YouTube's.
func ChainSource(step string) string {
	if step == "live" {
		return "youtube"
	}
	return step
}

// youtubeFor is the YouTube adapter and listing order a run reads with. A
// chain without the api method gets an adapter without the key, so the run
// makes no Data API call at all: no listing, details, per-format or Shorts
// playlists, and watch pages decide as they do without a key.
func (s *SourceSteps) youtubeFor(chain *sqlite.SourceChain) (*ytsource.Adapter, []string) {
	c := sqlite.DefaultSourceChain("youtube")
	if chain != nil {
		c = *chain
	}
	if c.Uses(ytsource.ListingAPI) {
		return s.Youtube, c.Enabled()
	}
	keyless := *s.Youtube
	keyless.Options.APIKey = ""
	return &keyless, c.Enabled()
}
