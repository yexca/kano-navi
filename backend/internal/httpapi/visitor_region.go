package httpapi

import (
	"net/http"
	"strings"

	"github.com/yexca/kano-navi/backend/internal/llmadmin"
)

// Country headers are supplied by a configured edge or reverse proxy. The
// response only contains the booleans the public UI needs; it never exposes
// the address or the raw header value.
var visitorCountryHeaders = []string{
	"CF-IPCountry",
	"X-Country-Code",
	"X-Geo-Country",
	"X-Visitor-Country",
}

const (
	visitorRegionEnabledKey      = "visitor_region_enabled"
	visitorRegionProviderNameKey = "visitor_region_provider_name"
	visitorRegionProviderURLKey  = "visitor_region_provider_url"
	visitorRegionMirrorURLKey    = "visitor_region_mirror_url"
)

type visitorRegionConfig struct {
	enabled      bool
	providerName string
	providerURL  string
	mirrorURL    string
}

func (h *Handler) visitorRegion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, no-transform")
	w.Header().Set("Vary", strings.Join(visitorCountryHeaders, ", "))
	config := visitorRegionConfig{}
	if h.store != nil {
		read := func(key string) string {
			value, err := llmadmin.GetAppSetting(r.Context(), h.store.DB(), key, "")
			if err != nil {
				return ""
			}
			return value
		}
		config.enabled = settingEnabled(read(visitorRegionEnabledKey))
		config.providerName = strings.TrimSpace(read(visitorRegionProviderNameKey))
		config.providerURL = strings.TrimSpace(read(visitorRegionProviderURLKey))
		config.mirrorURL = strings.TrimSpace(read(visitorRegionMirrorURLKey))
	}
	writeJSON(w, http.StatusOK, visitorRegionPayload(r, config), "")
}

func visitorRegionPayload(r *http.Request, config visitorRegionConfig) map[string]any {
	country := ""
	for _, name := range visitorCountryHeaders {
		value := strings.TrimSpace(strings.SplitN(r.Header.Get(name), ",", 2)[0])
		if value != "" {
			country = strings.ToUpper(value)
			break
		}
	}
	usable := config.enabled && config.providerName != "" && config.providerURL != "" && config.mirrorURL != ""
	return map[string]any{
		"enabled":       usable,
		"mainlandChina": usable && country == "CN",
		"providerName":  config.providerName,
		"providerUrl":   config.providerURL,
		"mirrorUrl":     config.mirrorURL,
	}
}

func settingEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
