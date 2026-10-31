package sqlite

import "encoding/json"

// SourceSync is how a source schedule reads one source (ADR-0002, ADR-0003):
// "incremental" reads newest first until a stored record, at most Max per X
// account or YouTube channel; "count" reads the newest Count every run.
type SourceSync struct {
	Mode  string `json:"mode"`
	Max   int    `json:"max,omitempty"`
	Count int    `json:"count,omitempty"`
}

const (
	SyncIncremental = "incremental"
	SyncCount       = "count"
	// DefaultSyncMax caps an incremental run of a source.
	DefaultSyncMax = 50
	// MaxSyncLimit bounds both a cap and a count: ten timeline pages of 20, or
	// four uploads-playlist pages of 50.
	MaxSyncLimit = 200
)

// SyncSources are the sources whose schedule has a sync setting.
var SyncSources = []string{"x", "youtube"}

// Valid reports whether the setting names one mode with a bound in range.
func (s SourceSync) Valid() bool {
	switch s.Mode {
	case SyncIncremental:
		return s.Max >= 1 && s.Max <= MaxSyncLimit && s.Count == 0
	case SyncCount:
		return s.Count >= 1 && s.Count <= MaxSyncLimit && s.Max == 0
	}
	return false
}

// DefaultSourceSync is the incremental default of every configured source.
func DefaultSourceSync() map[string]SourceSync {
	out := make(map[string]SourceSync, len(SyncSources))
	for _, source := range SyncSources {
		out[source] = SourceSync{Mode: SyncIncremental, Max: DefaultSyncMax}
	}
	return out
}

// readSourceSync parses a stored setting. Valid entries replace the
// defaults; anything unreadable keeps them, as '{}' does.
func readSourceSync(raw string) map[string]SourceSync {
	out := DefaultSourceSync()
	var stored map[string]SourceSync
	if json.Unmarshal([]byte(raw), &stored) == nil {
		for source, setting := range stored {
			if _, known := out[source]; known && setting.Valid() {
				out[source] = setting
			}
		}
	}
	return out
}
