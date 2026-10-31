package sqlite

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// SyncSettings are the operator's bounds of source reads and the translate
// step. Every run reads them when it starts, so a change applies to the next
// run.
type SyncSettings struct {
	// XBootstrapDays is the age limit of an X account's first run.
	XBootstrapDays int `json:"xBootstrapDays"`
	// XDiscoveryLimit caps unstored X statuses per account in a full sync.
	XDiscoveryLimit int `json:"xDiscoveryLimit"`
	// XMaxStatusRequests is the X detail-request budget of one run, shared by
	// all accounts, and the page budget of a manual X fetch.
	XMaxStatusRequests int `json:"xMaxStatusRequests"`
	// XRefreshKnown is the number of known X posts refreshed per run.
	XRefreshKnown int `json:"xRefreshKnown"`
	// YoutubeMaxDetailRequests is the reservation pages inspected per run.
	YoutubeMaxDetailRequests int `json:"youtubeMaxDetailRequests"`
	// FetchMaxPages bounds one manual fetch per X account or for the channel.
	FetchMaxPages int `json:"fetchMaxPages"`
	// FetchTimeoutMS and FetchRequestDelayMS bound the requests of manual
	// fetches and the X chain's API method.
	FetchTimeoutMS      int `json:"fetchTimeoutMs"`
	FetchRequestDelayMS int `json:"fetchRequestDelayMs"`
	// TranslationScanLimit and TranslationLookbackDays bound the translate
	// step.
	TranslationScanLimit    int `json:"translationScanLimit"`
	TranslationLookbackDays int `json:"translationLookbackDays"`
}

// SyncSettingSpec describes one setting: its API field, its app_settings key,
// the environment variable a first start imports it from, its default and
// its inclusive bounds.
type SyncSettingSpec struct {
	Field   string `json:"field"`
	key     string
	env     string
	Default int `json:"default"`
	Min     int `json:"min"`
	Max     int `json:"max"`
	// clamp imports an out-of-range environment value at the nearest bound,
	// as the variable was read; otherwise such a value is not imported.
	clamp bool
	value func(*SyncSettings) *int
}

// SyncSettingSpecs lists the settings in console order.
var SyncSettingSpecs = []SyncSettingSpec{
	{Field: "xBootstrapDays", key: "sync_x_bootstrap_days", env: "X_BOOTSTRAP_DAYS", Default: 7, Min: 1, Max: 30, value: func(s *SyncSettings) *int { return &s.XBootstrapDays }},
	{Field: "xDiscoveryLimit", key: "sync_x_discovery_limit", env: "X_DISCOVERY_LIMIT", Default: 50, Min: 1, Max: 200, value: func(s *SyncSettings) *int { return &s.XDiscoveryLimit }},
	{Field: "xMaxStatusRequests", key: "sync_x_max_status_requests", env: "X_MAX_STATUS_REQUESTS", Default: 12, Min: 0, Max: 50, value: func(s *SyncSettings) *int { return &s.XMaxStatusRequests }},
	{Field: "xRefreshKnown", key: "sync_x_refresh_known", env: "X_REFRESH_KNOWN", Default: 1, Min: 0, Max: 5, value: func(s *SyncSettings) *int { return &s.XRefreshKnown }},
	{Field: "youtubeMaxDetailRequests", key: "sync_youtube_max_detail_requests", env: "YOUTUBE_MAX_DETAIL_REQUESTS", Default: 12, Min: 1, Max: 50, value: func(s *SyncSettings) *int { return &s.YoutubeMaxDetailRequests }},
	{Field: "fetchMaxPages", key: "sync_fetch_max_pages", env: "SOURCE_HISTORY_MAX_PAGES", Default: 4, Min: 1, Max: 20, clamp: true, value: func(s *SyncSettings) *int { return &s.FetchMaxPages }},
	{Field: "fetchTimeoutMs", key: "sync_fetch_timeout_ms", env: "SYNC_TIMEOUT_MS", Default: 7000, Min: 1000, Max: 60000, clamp: true, value: func(s *SyncSettings) *int { return &s.FetchTimeoutMS }},
	{Field: "fetchRequestDelayMs", key: "sync_fetch_request_delay_ms", env: "SYNC_REQUEST_DELAY_MS", Default: 0, Min: 0, Max: 10000, clamp: true, value: func(s *SyncSettings) *int { return &s.FetchRequestDelayMS }},
	{Field: "translationScanLimit", key: "sync_translation_scan_limit", env: "TRANSLATION_SCAN_LIMIT", Default: 20, Min: 1, Max: 200, clamp: true, value: func(s *SyncSettings) *int { return &s.TranslationScanLimit }},
	{Field: "translationLookbackDays", key: "sync_translation_lookback_days", env: "TRANSLATION_LOOKBACK_DAYS", Default: 30, Min: 1, Max: 3650, clamp: true, value: func(s *SyncSettings) *int { return &s.TranslationLookbackDays }},
}

// DefaultSyncSettings are the settings before an operator changes any.
func DefaultSyncSettings() SyncSettings {
	var out SyncSettings
	for _, spec := range SyncSettingSpecs {
		*spec.value(&out) = spec.Default
	}
	return out
}

// Set changes settings by their API fields; an unknown field changes nothing
// and fails validation. SaveSyncSettings checks the bounds.
func (s *SyncSettings) Set(values map[string]int) error {
	next := *s
	for field, n := range values {
		found := false
		for _, spec := range SyncSettingSpecs {
			if spec.Field == field {
				*spec.value(&next), found = n, true
			}
		}
		if !found {
			return ErrWorkflowValidation
		}
	}
	*s = next
	return nil
}

func syncSettingKeys() []any {
	keys := make([]any, 0, len(SyncSettingSpecs))
	for _, spec := range SyncSettingSpecs {
		keys = append(keys, spec.key)
	}
	return keys
}

// SyncSettings reads the stored settings; an absent or out-of-range value
// uses its default, as does a snapshot without the table.
func (s *Store) SyncSettings(ctx context.Context) (SyncSettings, error) {
	out := DefaultSyncSettings()
	if s == nil || s.db == nil {
		return out, ErrUnavailable
	}
	rows, err := s.db.QueryContext(ctx, `SELECT key,value FROM app_settings WHERE key IN (?`+strings.Repeat(",?", len(SyncSettingSpecs)-1)+`)`, syncSettingKeys()...)
	if err != nil {
		return out, dbErr(err)
	}
	defer rows.Close()
	stored := map[string]string{}
	for rows.Next() {
		var key, value string
		if err = rows.Scan(&key, &value); err != nil {
			return out, dbErr(err)
		}
		stored[key] = value
	}
	if err = rows.Err(); err != nil {
		return out, dbErr(err)
	}
	for _, spec := range SyncSettingSpecs {
		if n, e := strconv.Atoi(stored[spec.key]); e == nil && n >= spec.Min && n <= spec.Max {
			*spec.value(&out) = n
		}
	}
	return out, nil
}

// SaveSyncSettings stores every setting after checking its bounds.
func (s *Store) SaveSyncSettings(ctx context.Context, settings SyncSettings) error {
	if !s.Writable() {
		return ErrReadOnly
	}
	for _, spec := range SyncSettingSpecs {
		if n := *spec.value(&settings); n < spec.Min || n > spec.Max {
			return ErrWorkflowValidation
		}
	}
	now := workflowTime(time.Now())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrUnavailable
	}
	defer tx.Rollback()
	for _, spec := range SyncSettingSpecs {
		if _, err = tx.ExecContext(ctx, `INSERT INTO app_settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, spec.key, strconv.Itoa(*spec.value(&settings)), now); err != nil {
			return ErrUnavailable
		}
	}
	if tx.Commit() != nil {
		return ErrUnavailable
	}
	return nil
}

// ImportSyncSettings stores, once, each setting an earlier release read from
// the environment: a variable with a usable integer becomes the stored value
// when the setting has none yet. Stored settings are never replaced, so the
// variables have no effect after the first start that sees them.
func (s *Store) ImportSyncSettings(ctx context.Context, getenv func(string) string) error {
	if !s.Writable() {
		return nil
	}
	now := workflowTime(time.Now())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrUnavailable
	}
	defer tx.Rollback()
	for _, spec := range SyncSettingSpecs {
		n, e := strconv.Atoi(strings.TrimSpace(getenv(spec.env)))
		if e != nil {
			continue
		}
		if n < spec.Min || n > spec.Max {
			if !spec.clamp {
				continue
			}
			n = min(max(n, spec.Min), spec.Max)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO app_settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO NOTHING`, spec.key, strconv.Itoa(n), now); err != nil {
			return ErrUnavailable
		}
	}
	if tx.Commit() != nil {
		return ErrUnavailable
	}
	return nil
}
