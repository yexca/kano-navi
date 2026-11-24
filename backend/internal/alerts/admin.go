package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/yexca/kano-navi/backend/internal/httpx"
	"github.com/yexca/kano-navi/backend/internal/secretstore"
)

// deliveryPage is how many deliveries the console lists.
const deliveryPage = 100

// Admin owns /api/admin/alerts: channels, rules, the heartbeat, open
// incidents and their acknowledgement, the delivery history and channel
// tests.
type Admin struct {
	service *Service
}

var _ httpx.AdminModule = (*Admin)(nil)

// NewAdmin binds the operator routes to service.
func NewAdmin(service *Service) *Admin { return &Admin{service: service} }

// AdminRoutes lists the alert endpoints. The router has already
// authenticated the operator and applied the origin guard.
func (a *Admin) AdminRoutes() []httpx.Route {
	return []httpx.Route{
		{Method: http.MethodGet, Path: "/alerts", Handler: a.get},
		{Method: http.MethodPut, Path: "/alerts", Handler: a.put},
		{Method: http.MethodGet, Path: "/alerts/deliveries", Handler: a.deliveries},
		{Method: http.MethodPost, Path: "/alerts/channels/{id}/test", Handler: a.testChannel},
		{Method: http.MethodPost, Path: "/alerts/heartbeat/test", Handler: a.testHeartbeat},
		{Method: http.MethodPost, Path: "/alerts/incidents/{id}/acknowledge", Handler: a.acknowledge(true)},
		{Method: http.MethodDelete, Path: "/alerts/incidents/{id}/acknowledge", Handler: a.acknowledge(false)},
	}
}

type channelResponse struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Type    ChannelType `json:"type"`
	Enabled bool        `json:"enabled"`
	Server  string      `json:"server"`
	Topic   string      `json:"topic"`
	ChatID  string      `json:"chatId"`
	// URLHost replaces the webhook address, which is write-only: it often
	// carries a token in its path.
	URLHost string `json:"urlHost"`
	Link    bool   `json:"link"`
	// HasSecret and SecretUnreadable replace the secret, which is
	// write-only.
	HasSecret        bool `json:"hasSecret"`
	SecretUnreadable bool `json:"secretUnreadable"`
}

// urlHost is the host of a webhook address, or "" when there is none.
func urlHost(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Host
}

type ruleResponse struct {
	Rule
	Setting RuleSetting `json:"setting"`
}

type heartbeatResponse struct {
	Enabled         bool    `json:"enabled"`
	IntervalMinutes int     `json:"intervalMinutes"`
	HasURL          bool    `json:"hasUrl"`
	URLUnreadable   bool    `json:"urlUnreadable"`
	LastOkAt        *string `json:"lastOkAt"`
	LastFailureAt   *string `json:"lastFailureAt"`
	LastError       *string `json:"lastError"`
}

type settingsResponse struct {
	State            string            `json:"state"`
	EvaluatedAt      *string           `json:"evaluatedAt"`
	EvaluationFailed bool              `json:"evaluationFailed"`
	HasOrigin        bool              `json:"hasOrigin"`
	ChannelTypes     []ChannelType     `json:"channelTypes"`
	Channels         []channelResponse `json:"channels"`
	Rules            []ruleResponse    `json:"rules"`
	Heartbeat        heartbeatResponse `json:"heartbeat"`
	Incidents        []incident        `json:"incidents"`
}

func instant(value time.Time) *string {
	if value.IsZero() {
		return nil
	}
	formatted := formatTime(value)
	return &formatted
}

func text(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func (a *Admin) response(ctx context.Context, settings Settings) (settingsResponse, error) {
	s := a.service
	s.mu.Lock()
	current := s.status
	s.mu.Unlock()
	out := settingsResponse{
		State: s.State(), EvaluatedAt: instant(current.evaluatedAt), EvaluationFailed: current.evaluationFailed,
		HasOrigin: s.origin != "", ChannelTypes: ChannelTypes, Channels: make([]channelResponse, 0, len(settings.Channels)),
		Rules: make([]ruleResponse, 0, len(Catalog)),
		Heartbeat: heartbeatResponse{
			Enabled: settings.Heartbeat.Enabled, IntervalMinutes: settings.Heartbeat.IntervalMinutes,
			HasURL: settings.Heartbeat.URL != "" || settings.Heartbeat.Unreadable, URLUnreadable: settings.Heartbeat.Unreadable,
			LastOkAt: instant(current.heartbeat.okAt), LastFailureAt: instant(current.heartbeat.failureAt), LastError: text(current.heartbeat.lastError),
		},
	}
	for _, channel := range settings.Channels {
		out.Channels = append(out.Channels, channelResponse{
			ID: channel.ID, Name: channel.Name, Type: channel.Type, Enabled: channel.Enabled, Server: channel.Server,
			Topic: channel.Topic, ChatID: channel.ChatID, URLHost: urlHost(channel.URL), Link: channel.Link,
			HasSecret: channel.Secret != "" || channel.Unreadable, SecretUnreadable: channel.Unreadable,
		})
	}
	for _, rule := range Catalog {
		out.Rules = append(out.Rules, ruleResponse{Rule: rule, Setting: settings.Rules[rule.Code]})
	}
	incidents, err := openIncidents(ctx, s.store.DB())
	if err != nil {
		return settingsResponse{}, err
	}
	out.Incidents = incidents
	return out, nil
}

func (a *Admin) get(w http.ResponseWriter, r *http.Request) {
	settings, err := a.service.Settings(r.Context())
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "alert_settings_unavailable")
		return
	}
	out, err := a.response(r.Context(), settings)
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// channelPayload omits Secret to keep the stored secret of the channel with
// the same id; an empty string clears it. A webhook omits URL to keep its
// stored address.
type channelPayload struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Type    ChannelType `json:"type"`
	Enabled bool        `json:"enabled"`
	Server  string      `json:"server"`
	Topic   string      `json:"topic"`
	ChatID  string      `json:"chatId"`
	URL     *string     `json:"url"`
	Link    bool        `json:"link"`
	Secret  *string     `json:"secret"`
}

// heartbeatPayload omits URL to keep the stored address.
type heartbeatPayload struct {
	Enabled         bool    `json:"enabled"`
	IntervalMinutes int     `json:"intervalMinutes"`
	URL             *string `json:"url"`
}

// put replaces the fields it carries: the whole channel list, the rules it
// names and the heartbeat.
func (a *Admin) put(w http.ResponseWriter, r *http.Request) {
	if !a.service.store.Writable() {
		httpx.ReadOnly(w)
		return
	}
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	a.service.writeMu.Lock()
	defer a.service.writeMu.Unlock()
	settings, err := a.apply(r.Context(), body)
	switch {
	case errors.Is(err, secretstore.ErrKeyRequired):
		httpx.WriteCode(w, http.StatusServiceUnavailable, "secrets_key_unavailable")
	case err != nil:
		httpx.WriteError(w, err)
	default:
		out, err := a.response(r.Context(), settings)
		if err != nil {
			httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}

func (a *Admin) apply(ctx context.Context, body map[string]json.RawMessage) (Settings, error) {
	s := a.service
	tx, err := s.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return Settings{}, err
	}
	defer tx.Rollback()
	previous, err := load(ctx, tx, s.key)
	if errors.Is(err, ErrUnavailable) {
		// Unparseable settings are replaced by this save.
		previous, err = Normalize(Settings{})
	}
	if err != nil {
		return Settings{}, err
	}
	next := Settings{Channels: previous.Channels, Rules: previous.Rules, Heartbeat: previous.Heartbeat}
	if raw, ok := body["channels"]; ok {
		var items []channelPayload
		if json.Unmarshal(raw, &items) != nil || items == nil {
			return Settings{}, httpx.Invalid("channels must be a list of channels")
		}
		kept := make(map[string]Channel, len(previous.Channels))
		for _, channel := range previous.Channels {
			kept[channel.ID] = channel
		}
		next.Channels = make([]Channel, 0, len(items))
		for _, item := range items {
			channel := Channel{ID: item.ID, Name: item.Name, Type: item.Type, Enabled: item.Enabled, Server: item.Server, Topic: item.Topic, ChatID: item.ChatID, Link: item.Link}
			old, saved := kept[item.ID]
			saved = saved && item.ID != ""
			switch {
			case item.URL != nil:
				channel.URL = *item.URL
			case saved && old.Type == TypeWebhook && item.Type == TypeWebhook:
				channel.URL = old.URL
			}
			if saved && item.Secret == nil && (old.Secret != "" || old.Unreadable) {
				// A saved secret is only ever sent to the address it was
				// entered for.
				if !sameDestination(old, channel) {
					return Settings{}, httpx.Invalid("the secret must be entered again when a channel's address changes")
				}
				channel.Secret, channel.Unreadable = old.Secret, old.Unreadable
			}
			if item.Secret != nil {
				channel.Secret = *item.Secret
			}
			next.Channels = append(next.Channels, channel)
		}
	}
	if raw, ok := body["rules"]; ok {
		var rules map[string]RuleSetting
		if json.Unmarshal(raw, &rules) != nil || rules == nil {
			return Settings{}, httpx.Invalid("rules must map rule codes to settings")
		}
		merged := make(map[string]RuleSetting, len(previous.Rules))
		for code, setting := range previous.Rules {
			merged[code] = setting
		}
		for code, setting := range rules {
			merged[code] = setting
		}
		next.Rules = merged
	}
	if raw, ok := body["heartbeat"]; ok {
		var item heartbeatPayload
		if json.Unmarshal(raw, &item) != nil {
			return Settings{}, httpx.Invalid("heartbeat must be an object")
		}
		heartbeat := Heartbeat{Enabled: item.Enabled, IntervalMinutes: item.IntervalMinutes, URL: previous.Heartbeat.URL, Unreadable: previous.Heartbeat.Unreadable}
		if item.URL != nil {
			heartbeat.URL, heartbeat.Unreadable = *item.URL, false
		}
		next.Heartbeat = heartbeat
	}
	settings, err := Normalize(next)
	if err != nil {
		return Settings{}, err
	}
	if err = save(ctx, tx, settings, s.key, s.now()); err != nil {
		return Settings{}, err
	}
	if err = tx.Commit(); err != nil {
		return Settings{}, err
	}
	s.set(settings)
	return settings, nil
}

func (a *Admin) deliveries(w http.ResponseWriter, r *http.Request) {
	items, err := listDeliveries(r.Context(), a.service.store.DB(), deliveryPage)
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"deliveries": items})
}

type testResult struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	LatencyMs int64  `json:"latencyMs"`
}

// testChannel sends a test message through one saved channel, whether or
// not it is enabled and whatever the state, and records the attempt.
func (a *Admin) testChannel(w http.ResponseWriter, r *http.Request) {
	if !a.service.store.Writable() {
		httpx.ReadOnly(w)
		return
	}
	settings, err := a.service.Settings(r.Context())
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "alert_settings_unavailable")
		return
	}
	for _, channel := range settings.Channels {
		if channel.ID != r.PathValue("id") {
			continue
		}
		if channel.Unreadable {
			httpx.WriteError(w, httpx.Invalid("the stored secret cannot be read; enter it again"))
			return
		}
		code, latency := a.service.TestChannel(r.Context(), channel)
		httpx.WriteJSON(w, http.StatusOK, testResult{OK: code == "", Error: code, LatencyMs: latency.Milliseconds()})
		return
	}
	httpx.WriteCode(w, http.StatusNotFound, "alert_channel_not_found")
}

// testHeartbeat pings the saved heartbeat address once, whether or not the
// heartbeat is enabled.
func (a *Admin) testHeartbeat(w http.ResponseWriter, r *http.Request) {
	settings, err := a.service.Settings(r.Context())
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "alert_settings_unavailable")
		return
	}
	heartbeat := settings.Heartbeat
	switch {
	case heartbeat.Unreadable:
		httpx.WriteError(w, httpx.Invalid("the stored address cannot be read; enter it again"))
		return
	case heartbeat.URL == "":
		httpx.WriteError(w, httpx.Invalid("save a heartbeat address first"))
		return
	}
	started := a.service.now()
	code := a.service.heartbeat(r.Context(), heartbeat, true)
	httpx.WriteJSON(w, http.StatusOK, testResult{OK: code == "", Error: code, LatencyMs: a.service.now().Sub(started).Milliseconds()})
}

// acknowledge sets (on) or clears an open incident's acknowledgement and
// returns the settings with the open incidents. Acknowledging changes no
// delivery: an incident's opened and resolved notices are sent as usual.
func (a *Admin) acknowledge(on bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.service.store.Writable() {
			httpx.ReadOnly(w)
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			httpx.WriteCode(w, http.StatusNotFound, "alert_incident_not_found")
			return
		}
		settings, err := a.service.Settings(r.Context())
		if err != nil {
			httpx.WriteCode(w, http.StatusServiceUnavailable, "alert_settings_unavailable")
			return
		}
		found, err := acknowledge(r.Context(), a.service.store.DB(), id, on, a.service.now())
		if err != nil {
			httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
			return
		}
		if !found {
			httpx.WriteCode(w, http.StatusNotFound, "alert_incident_not_found")
			return
		}
		out, err := a.response(r.Context(), settings)
		if err != nil {
			httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}
