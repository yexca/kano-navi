package adminapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/yexca/kano-navi/backend/internal/httpx"
	"github.com/yexca/kano-navi/backend/internal/llmadmin"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

const (
	visitorRegionSettingKey        = "visitor_region_enabled"
	visitorRegionProviderNameKey   = "visitor_region_provider_name"
	visitorRegionProviderURLKey    = "visitor_region_provider_url"
	visitorRegionMirrorURLKey      = "visitor_region_mirror_url"
	visitorRegionProviderNameLimit = 120
)

type visitorRegionSettings struct {
	Enabled      bool   `json:"enabled"`
	ProviderName string `json:"providerName"`
	ProviderURL  string `json:"providerUrl"`
	MirrorURL    string `json:"mirrorUrl"`
}

func (a *api) visitorRegionSettingsValue(ctx context.Context) (visitorRegionSettings, error) {
	values := visitorRegionSettings{}
	read := func(key string) (string, error) {
		return llmadmin.GetAppSetting(ctx, a.Store.DB(), key, "")
	}
	enabled, err := read(visitorRegionSettingKey)
	if err != nil {
		return values, err
	}
	values.Enabled = settingEnabled(enabled)
	if values.ProviderName, err = read(visitorRegionProviderNameKey); err != nil {
		return values, err
	}
	if values.ProviderURL, err = read(visitorRegionProviderURLKey); err != nil {
		return values, err
	}
	if values.MirrorURL, err = read(visitorRegionMirrorURLKey); err != nil {
		return values, err
	}
	return values, nil
}

func settingEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (a *api) visitorRegionSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := a.visitorRegionSettingsValue(r.Context())
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, settings)
}

func settingString(body map[string]json.RawMessage, key, fallback string) (string, error) {
	raw, ok := body[key]
	if !ok {
		return fallback, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", httpx.Invalid("%s must be a string", key)
	}
	return strings.TrimSpace(value), nil
}

func validateVisitorRegionURL(key, raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", httpx.Invalid("%s must be an absolute HTTP(S) URL without credentials or a fragment", key)
	}
	return parsed.String(), nil
}

func setVisitorRegionSetting(ctx context.Context, tx *sql.Tx, key, value, timestamp string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO app_settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, key, value, timestamp)
	return err
}

func (a *api) updateVisitorRegionSettings(w http.ResponseWriter, r *http.Request) {
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	raw, ok := body["enabled"]
	if !ok {
		httpx.WriteError(w, httpx.Invalid("enabled is required"))
		return
	}
	var enabled bool
	if err := json.Unmarshal(raw, &enabled); err != nil {
		httpx.WriteError(w, httpx.Invalid("enabled must be a boolean"))
		return
	}
	current, err := a.visitorRegionSettingsValue(r.Context())
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	current.Enabled = enabled
	if current.ProviderName, err = settingString(body, "providerName", current.ProviderName); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if current.ProviderURL, err = settingString(body, "providerUrl", current.ProviderURL); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if current.MirrorURL, err = settingString(body, "mirrorUrl", current.MirrorURL); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if utf8.RuneCountInString(current.ProviderName) > visitorRegionProviderNameLimit {
		httpx.WriteError(w, httpx.Invalid("providerName is too long"))
		return
	}
	if current.ProviderURL, err = validateVisitorRegionURL("providerUrl", current.ProviderURL); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if current.MirrorURL, err = validateVisitorRegionURL("mirrorUrl", current.MirrorURL); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if current.Enabled && (current.ProviderName == "" || current.ProviderURL == "" || current.MirrorURL == "") {
		httpx.WriteError(w, httpx.Invalid("providerName, providerUrl and mirrorUrl are required when enabled"))
		return
	}
	if !a.Store.Writable() {
		httpx.ReadOnly(w)
		return
	}
	tx, err := a.Store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		httpx.WriteCode(w, http.StatusInternalServerError, "internal_error")
		return
	}
	defer tx.Rollback()
	stamp := sqlite.ISOTime(a.Now())
	stored := map[string]string{
		visitorRegionSettingKey:      map[bool]string{true: "1", false: "0"}[current.Enabled],
		visitorRegionProviderNameKey: current.ProviderName,
		visitorRegionProviderURLKey:  current.ProviderURL,
		visitorRegionMirrorURLKey:    current.MirrorURL,
	}
	for key, value := range stored {
		if err := setVisitorRegionSetting(r.Context(), tx, key, value, stamp); err != nil {
			httpx.WriteCode(w, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		httpx.WriteCode(w, http.StatusInternalServerError, "internal_error")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, current)
}
