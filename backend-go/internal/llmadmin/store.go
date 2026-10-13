package llmadmin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/httpx"
	"github.com/yexca/kano-navi/backend-go/internal/secretstore"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// Querier is satisfied by *sql.DB, *sql.Tx and *sql.Conn. The store's pool
// holds a single connection, so every helper here drains and closes its rows
// before returning.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Provider is the public projection of an llm_providers row. It carries only
// whether a credential exists, never the credential.
type Provider struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Protocol         string   `json:"protocol"`
	BaseURL          string   `json:"baseUrl"`
	Model            string   `json:"model"`
	Enabled          bool     `json:"enabled"`
	VisionCapable    bool     `json:"visionCapable"`
	TimeoutMs        int64    `json:"timeoutMs"`
	MaxRetries       int64    `json:"maxRetries"`
	LastStatus       *string  `json:"lastStatus"`
	LastError        *string  `json:"lastError"`
	LastCheckedAt    *string  `json:"lastCheckedAt"`
	CreatedAt        string   `json:"createdAt"`
	UpdatedAt        string   `json:"updatedAt"`
	Capabilities     []string `json:"capabilities"`
	APIKeyConfigured bool     `json:"apiKeyConfigured"`
}

// Model is one catalog entry of a provider.
type Model struct {
	ProviderID   string   `json:"providerId"`
	ID           string   `json:"id"`
	Name         *string  `json:"name"`
	Tags         []string `json:"tags"`
	Capabilities []string `json:"capabilities"`
	Enabled      bool     `json:"enabled"`
	Origin       string   `json:"origin"`
	OwnedBy      *string  `json:"ownedBy"`
	CreatedAt    string   `json:"createdAt"`
	UpdatedAt    string   `json:"updatedAt"`
}

// RouteTarget is one ordered model choice of a route. An empty ModelID
// follows the provider's default model.
type RouteTarget struct {
	ProviderID string `json:"providerId"`
	ModelID    string `json:"modelId"`
}

// ScheduleExtraction is the persisted schedule pipeline configuration.
type ScheduleExtraction struct {
	Enabled        bool
	KeywordEnabled bool
	VisionEnabled  bool
	MessageEnabled bool
	Keywords       []string
}

type scanner func(dest ...any) error

func collect[T any](ctx context.Context, q Querier, scan func(scanner) (T, error), query string, args ...any) ([]T, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		value, scanErr := scan(rows.Scan)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, value)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return out, rows.Close()
}

func optional(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

// LookupAppSetting reports whether a setting row exists.
func LookupAppSetting(ctx context.Context, db Querier, key string) (value string, found bool, err error) {
	err = db.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}

// GetAppSetting returns the stored value or fallback when the row is absent.
func GetAppSetting(ctx context.Context, db Querier, key, fallback string) (string, error) {
	value, found, err := LookupAppSetting(ctx, db, key)
	if err != nil || !found {
		return fallback, err
	}
	return value, nil
}

func setAppSettingAt(ctx context.Context, db Querier, key, value, timestamp string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO app_settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, key, value, timestamp)
	return err
}

// SetAppSetting upserts a setting and returns the stored updated_at stamp.
func SetAppSetting(ctx context.Context, db Querier, key, value string) (updatedAt string, err error) {
	updatedAt = sqlite.ISOTime(time.Now())
	return updatedAt, setAppSettingAt(ctx, db, key, value, updatedAt)
}

// The credential column is reduced to a flag inside SQLite, so the ciphertext
// never reaches the projection code.
const providerColumns = `id, name, protocol, base_url, model, enabled, vision_capable,
	capabilities_json, timeout_ms, max_retries,
	(api_key_ciphertext IS NOT NULL AND api_key_ciphertext <> '') AS api_key_configured,
	last_status, last_error, last_checked_at, created_at, updated_at`

func scanProvider(scan scanner) (Provider, error) {
	var p Provider
	var enabled, vision, configured sql.NullInt64
	var capabilities, lastStatus, lastError, lastChecked sql.NullString
	err := scan(&p.ID, &p.Name, &p.Protocol, &p.BaseURL, &p.Model, &enabled, &vision,
		&capabilities, &p.TimeoutMs, &p.MaxRetries, &configured,
		&lastStatus, &lastError, &lastChecked, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return p, err
	}
	p.Enabled = enabled.Int64 != 0
	p.Capabilities = storedCapabilities(capabilities.String, vision.Int64 != 0)
	// Kept as a derived compatibility field for older admin clients.
	p.VisionCapable = slices.Contains(p.Capabilities, "image")
	p.APIKeyConfigured = configured.Int64 != 0
	p.LastStatus, p.LastError, p.LastCheckedAt = optional(lastStatus), optional(lastError), optional(lastChecked)
	return p, nil
}

// GetProvider returns nil when the provider does not exist.
func GetProvider(ctx context.Context, db Querier, id string) (*Provider, error) {
	provider, err := scanProvider(db.QueryRowContext(ctx, `SELECT `+providerColumns+` FROM llm_providers WHERE id = ?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &provider, nil
}

func ListProviders(ctx context.Context, db Querier) ([]Provider, error) {
	return collect(ctx, db, scanProvider, `SELECT `+providerColumns+` FROM llm_providers
		ORDER BY enabled DESC, updated_at DESC, id ASC`)
}

// ProviderAPIKey decrypts a provider credential for an outbound request. It
// returns "" when no usable credential is stored; callers must keep the
// result out of responses, errors and logs.
func ProviderAPIKey(ctx context.Context, db Querier, id, secretsKey string) (string, error) {
	var ciphertext sql.NullString
	err := db.QueryRowContext(ctx, `SELECT api_key_ciphertext FROM llm_providers WHERE id = ?`, id).Scan(&ciphertext)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && ciphertext.String == "") {
		return "", nil
	}
	if err != nil {
		return "", errors.New("llm provider credential lookup failed")
	}
	plaintext, decryptErr := secretstore.Decrypt(ciphertext.String, secretsKey)
	if decryptErr != nil {
		return "", nil
	}
	return plaintext, nil
}

const modelColumns = `provider_id, model_id, name, tags_json, enabled, origin, owned_by, created_at, updated_at`

func scanModel(scan scanner) (Model, error) {
	var m Model
	var name, tags, owner sql.NullString
	var enabled sql.NullInt64
	if err := scan(&m.ProviderID, &m.ID, &name, &tags, &enabled, &m.Origin, &owner, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return m, err
	}
	m.Name, m.OwnedBy = nullableString(name.String), nullableString(owner.String)
	m.Tags = normalizeModelTags(tags.String)
	m.Capabilities = routingCapabilities(m.Tags)
	m.Enabled = enabled.Int64 != 0
	return m, nil
}

// GetModel returns nil when the model is not in the provider catalog.
func GetModel(ctx context.Context, db Querier, providerID, modelID string) (*Model, error) {
	model, err := scanModel(db.QueryRowContext(ctx, `SELECT `+modelColumns+` FROM llm_models WHERE provider_id = ? AND model_id = ?`, providerID, modelID).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &model, nil
}

// ListModels lists one provider's catalog, or every catalog when providerID
// is empty.
func ListModels(ctx context.Context, db Querier, providerID string) ([]Model, error) {
	if providerID != "" {
		return collect(ctx, db, scanModel, `SELECT `+modelColumns+` FROM llm_models WHERE provider_id = ?
			ORDER BY model_id COLLATE NOCASE ASC`, providerID)
	}
	return collect(ctx, db, scanModel, `SELECT `+modelColumns+` FROM llm_models
		ORDER BY provider_id ASC, model_id COLLATE NOCASE ASC`)
}

// RouteTargets returns the targets configured for exactly this route.
func RouteTargets(ctx context.Context, db Querier, route string) ([]RouteTarget, error) {
	return collect(ctx, db, func(scan scanner) (RouteTarget, error) {
		var target RouteTarget
		var model sql.NullString
		err := scan(&target.ProviderID, &model)
		target.ModelID = model.String
		return target, err
	}, `SELECT r.provider_id, r.model_id FROM llm_route_targets r
		JOIN llm_providers p ON p.id = r.provider_id
		WHERE r.route = ? ORDER BY r.priority ASC, r.provider_id ASC, r.model_id ASC`, route)
}

// ScheduleRouteTargets returns the effective targets per route: message and
// board fall back to the vision route while they have none of their own.
func ScheduleRouteTargets(ctx context.Context, db Querier) (RouteMap[[]RouteTarget], error) {
	var out RouteMap[[]RouteTarget]
	var err error
	if out.Vision, err = RouteTargets(ctx, db, RouteVision); err != nil {
		return out, err
	}
	if out.Message, err = RouteTargets(ctx, db, RouteMessage); err != nil {
		return out, err
	}
	if out.Board, err = RouteTargets(ctx, db, RouteBoard); err != nil {
		return out, err
	}
	if len(out.Message) == 0 {
		out.Message = slices.Clone(out.Vision)
	}
	if len(out.Board) == 0 {
		out.Board = slices.Clone(out.Vision)
	}
	return out, nil
}

func configuredRouteTargets(ctx context.Context, db Querier) (RouteMap[[]RouteTarget], error) {
	var out RouteMap[[]RouteTarget]
	var err error
	if out.Message, err = RouteTargets(ctx, db, RouteMessage); err != nil {
		return out, err
	}
	if out.Board, err = RouteTargets(ctx, db, RouteBoard); err != nil {
		return out, err
	}
	out.Vision, err = RouteTargets(ctx, db, RouteVision)
	return out, err
}

func routeProviders(ctx context.Context, db Querier, route string) ([]string, error) {
	targets, err := RouteTargets(ctx, db, route)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(targets))
	for _, target := range targets {
		ids = append(ids, target.ProviderID)
	}
	return uniqueStrings(ids), nil
}

// ScheduleProviderOrders is the distinct provider order per route, kept for
// the legacy provider-only admin API.
func ScheduleProviderOrders(ctx context.Context, db Querier) (RouteMap[[]string], error) {
	var out RouteMap[[]string]
	var err error
	if out.Vision, err = routeProviders(ctx, db, RouteVision); err != nil {
		return out, err
	}
	if out.Message, err = routeProviders(ctx, db, RouteMessage); err != nil {
		return out, err
	}
	if out.Board, err = routeProviders(ctx, db, RouteBoard); err != nil {
		return out, err
	}
	if len(out.Message) == 0 {
		out.Message = slices.Clone(out.Vision)
	}
	if len(out.Board) == 0 {
		out.Board = slices.Clone(out.Vision)
	}
	return out, nil
}

func settingBoolean(value string, fallback bool) bool {
	switch strings.ToLower(jsTrim(value)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return fallback
}

func parseKeywordSetting(value string) []string {
	var items []string
	var parsed any
	if json.Unmarshal([]byte(value), &parsed) == nil {
		if array, ok := parsed.([]any); ok {
			for _, item := range array {
				items = append(items, jsString(item))
			}
		} else {
			items = strings.FieldsFunc(value, isKeywordSeparator)
		}
	} else {
		items = strings.FieldsFunc(value, isKeywordSeparator)
	}
	keywords := make([]string, 0, len(items))
	for _, item := range items {
		if keyword := jsTrim(item); keyword != "" && utf16Len(keyword) <= 80 {
			keywords = append(keywords, keyword)
		}
	}
	return uniqueStrings(keywords)
}

// ScheduleExtractionConfig reads the stage switches and keywords. A stored
// setting wins over its environment default; lookupEnv may be nil.
func ScheduleExtractionConfig(ctx context.Context, db Querier, lookupEnv func(string) (string, bool)) (ScheduleExtraction, error) {
	var config ScheduleExtraction
	// setting resolves stored value, then environment, then the given default.
	setting := func(key, env, fallback string) (string, error) {
		value, found, err := LookupAppSetting(ctx, db, key)
		if err != nil || found {
			return value, err
		}
		if lookupEnv != nil {
			if fromEnv, ok := lookupEnv(env); ok {
				return fromEnv, nil
			}
		}
		return fallback, nil
	}
	enabledSetting, err := setting("schedule_extraction_enabled", "SCHEDULE_EXTRACTION_ENABLED", "1")
	if err != nil {
		return config, err
	}
	legacyEnabled := settingBoolean(enabledSetting, true)
	stage := func(key, env string) (bool, error) {
		value, stageErr := setting(key, env, enabledSetting)
		return settingBoolean(value, legacyEnabled), stageErr
	}
	if config.KeywordEnabled, err = stage("schedule_keyword_enabled", "SCHEDULE_KEYWORD_ENABLED"); err != nil {
		return config, err
	}
	if config.VisionEnabled, err = stage("schedule_vision_enabled", "SCHEDULE_VISION_ENABLED"); err != nil {
		return config, err
	}
	if config.MessageEnabled, err = stage("schedule_message_enabled", "SCHEDULE_MESSAGE_ENABLED"); err != nil {
		return config, err
	}
	// The legacy switch only seeds the stage defaults: the message detector
	// can be enabled on its own.
	config.Enabled = config.MessageEnabled || (config.KeywordEnabled && config.VisionEnabled)

	const unset = "\x00unset"
	keywords, err := setting("schedule_keywords", "SCHEDULE_KEYWORDS", unset)
	if err != nil {
		return config, err
	}
	if keywords == unset {
		config.Keywords = slices.Clone(defaultScheduleKeywords)
	} else {
		config.Keywords = parseKeywordSetting(keywords)
	}
	return config, nil
}

func videoExists(ctx context.Context, db Querier, id string) (bool, error) {
	var one int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM videos WHERE id = ?`, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// FeaturedVideoID returns the operator's featured video, or the legacy focus
// row while no choice has been stored. "" means none; a video that no longer
// exists is never returned.
func FeaturedVideoID(ctx context.Context, db Querier) (string, error) {
	candidate, found, err := LookupAppSetting(ctx, db, "featured_video_id")
	if err != nil {
		return "", err
	}
	if !found {
		var legacy sql.NullString
		err = db.QueryRowContext(ctx, `SELECT video_id FROM focus ORDER BY id LIMIT 1`).Scan(&legacy)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
		candidate = legacy.String
	}
	if candidate == "" {
		return "", nil
	}
	exists, err := videoExists(ctx, db, candidate)
	if err != nil || !exists {
		return "", err
	}
	return candidate, nil
}

// --- writes -----------------------------------------------------------------

func (m *Module) stamp() string { return sqlite.ISOTime(m.now()) }

func (m *Module) setSetting(ctx context.Context, db Querier, key, value string) (string, error) {
	timestamp := m.stamp()
	return timestamp, setAppSettingAt(ctx, db, key, value, timestamp)
}

func (m *Module) setFeaturedVideoID(ctx context.Context, db Querier, id string) error {
	if id != "" {
		exists, err := videoExists(ctx, db, id)
		if err != nil {
			return err
		}
		if !exists {
			return httpx.Invalid("featured video does not exist")
		}
	}
	_, err := m.setSetting(ctx, db, "featured_video_id", id)
	return err
}

func (m *Module) upsertProvider(ctx context.Context, db Querier, input providerWrite) (*Provider, error) {
	name := utf16Prefix(jsTrim(input.Name), 120)
	if name == "" {
		name = input.ID
	}
	timeout := input.TimeoutMs
	if timeout == 0 {
		timeout = 30000
	}
	timeout = min(120000, max(1000, timeout))
	capabilities := input.Capabilities
	if len(capabilities) == 0 {
		capabilities = slices.Clone(llmCapabilities)
	}
	var credential any
	if input.ReplaceAPIKey && input.APIKeyCiphertext != "" {
		credential = input.APIKeyCiphertext
	}
	timestamp := m.stamp()
	_, err := db.ExecContext(ctx, `INSERT INTO llm_providers (
			id, name, protocol, base_url, model, enabled, vision_capable,
			capabilities_json, timeout_ms, max_retries, api_key_ciphertext,
			last_status, last_error, last_checked_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, NULL, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name, protocol=excluded.protocol, base_url=excluded.base_url,
			model=excluded.model, enabled=excluded.enabled,
			vision_capable=excluded.vision_capable,
			capabilities_json=excluded.capabilities_json,
			timeout_ms=excluded.timeout_ms,
			max_retries=excluded.max_retries,
			api_key_ciphertext=CASE WHEN ? = 1 THEN excluded.api_key_ciphertext
				ELSE llm_providers.api_key_ciphertext END,
			updated_at=excluded.updated_at`,
		input.ID, name, input.Protocol, input.BaseURL, input.Model, input.Enabled,
		slices.Contains(capabilities, "image"), compactJSON(capabilities), timeout, MaxRetries,
		credential, timestamp, timestamp, input.ReplaceAPIKey)
	if err != nil {
		return nil, errors.New("llm provider write failed")
	}
	if input.Model != "" {
		if err = m.syncDefaultModelCapabilities(ctx, db, input.ID, input.Model, capabilities); err != nil {
			return nil, err
		}
	}
	return GetProvider(ctx, db, input.ID)
}

// syncDefaultModelCapabilities keeps the default model's routing tags and
// the provider capability columns identical, whichever side is edited.
func (m *Module) syncDefaultModelCapabilities(ctx context.Context, db Querier, providerID, model string, capabilities []string) error {
	timestamp := m.stamp()
	existing, err := GetModel(ctx, db, providerID, model)
	if err != nil {
		return err
	}
	merged := []any{}
	for _, capability := range capabilities {
		merged = append(merged, capability)
	}
	if existing != nil {
		for _, tag := range existing.Tags {
			if !slices.Contains(llmCapabilities, tag) {
				merged = append(merged, tag)
			}
		}
	}
	tags := compactJSON(normalizeModelTags(merged))
	if existing != nil {
		_, err = db.ExecContext(ctx, `UPDATE llm_models SET tags_json = ?, updated_at = ? WHERE provider_id = ? AND model_id = ?`,
			tags, timestamp, providerID, model)
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO llm_models
		(provider_id, model_id, name, tags_json, enabled, origin, owned_by, created_at, updated_at)
		VALUES (?, ?, NULL, ?, 1, 'legacy', NULL, ?, ?)`, providerID, model, tags, timestamp, timestamp)
	return err
}

func (m *Module) updateProviderStatus(ctx context.Context, db Querier, id, status, message string) error {
	timestamp := m.stamp()
	_, err := db.ExecContext(ctx, `UPDATE llm_providers SET last_status=?, last_error=?,
		last_checked_at=?, updated_at=? WHERE id=?`,
		nullableString(status), nullableString(utf16Prefix(message, 500)), timestamp, timestamp, id)
	return err
}

func deleteProvider(ctx context.Context, db Querier, id string) (bool, error) {
	// Children are removed explicitly so the result does not depend on the
	// connection's foreign_keys pragma.
	for _, statement := range []string{
		`DELETE FROM llm_route_targets WHERE provider_id = ?`,
		`DELETE FROM llm_route_providers WHERE provider_id = ?`,
		`DELETE FROM llm_models WHERE provider_id = ?`,
	} {
		if _, err := db.ExecContext(ctx, statement, id); err != nil {
			return false, err
		}
	}
	result, err := db.ExecContext(ctx, `DELETE FROM llm_providers WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	return changed > 0, err
}

// upsertModels adds catalog models to a provider. Existing rows keep their
// operator tags and enabled flag unless the caller passes them explicitly.
func (m *Module) upsertModels(ctx context.Context, db Querier, provider *Provider, models []modelWrite) error {
	timestamp := m.stamp()
	for _, model := range models {
		existing, err := GetModel(ctx, db, provider.ID, model.ID)
		if err != nil {
			return err
		}
		name, owner, tags, enabled, origin := model.Name, model.OwnedBy, model.Tags, model.Enabled, model.Origin
		if !model.HasName {
			name = nil
			if existing != nil {
				name = existing.Name
			}
		}
		if !model.HasOwnedBy {
			owner = nil
			if existing != nil {
				owner = existing.OwnedBy
			}
		}
		if !model.HasTags {
			tags = []string{"text"}
			if existing != nil {
				tags = existing.Tags
			}
		}
		if !model.HasEnabled {
			enabled = existing == nil || existing.Enabled
		}
		if origin != "remote" && origin != "manual" && origin != "legacy" {
			origin = "manual"
			if existing != nil {
				origin = existing.Origin
			}
		}
		if tags == nil {
			tags = []string{}
		}
		_, err = db.ExecContext(ctx, `INSERT INTO llm_models
			(provider_id, model_id, name, tags_json, enabled, origin, owned_by, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(provider_id, model_id) DO UPDATE SET
				name=excluded.name, tags_json=excluded.tags_json,
				enabled=excluded.enabled, origin=excluded.origin,
				owned_by=excluded.owned_by, updated_at=excluded.updated_at`,
			provider.ID, model.ID, name, compactJSON(tags), enabled, origin, owner, timestamp, timestamp)
		if err != nil {
			return err
		}
		capabilities := routingCapabilities(tags)
		if model.ID == provider.Model && len(capabilities) > 0 {
			_, err = db.ExecContext(ctx, `UPDATE llm_providers SET capabilities_json = ?, vision_capable = ?, updated_at = ?
				WHERE id = ?`, compactJSON(capabilities), slices.Contains(capabilities, "image"), timestamp, provider.ID)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Module) deleteModels(ctx context.Context, db Querier, provider *Provider, ids []string) (int64, error) {
	var removed int64
	for _, id := range uniqueStrings(ids) {
		if id == "" {
			continue
		}
		result, err := db.ExecContext(ctx, `DELETE FROM llm_models WHERE provider_id = ? AND model_id = ?`, provider.ID, id)
		if err != nil {
			return 0, err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		removed += changed
		if _, err = db.ExecContext(ctx, `DELETE FROM llm_route_targets WHERE provider_id = ? AND model_id = ?`, provider.ID, id); err != nil {
			return 0, err
		}
		if id == provider.Model {
			if _, err = db.ExecContext(ctx, `UPDATE llm_providers SET model = '', updated_at = ? WHERE id = ?`, m.stamp(), provider.ID); err != nil {
				return 0, err
			}
		}
	}
	for _, route := range scheduleRoutes {
		targets, err := RouteTargets(ctx, db, route)
		if err != nil {
			return 0, err
		}
		if err = m.writeRouteTargets(ctx, db, route, targets); err != nil {
			return 0, err
		}
	}
	return removed, nil
}

func (m *Module) writeRouteTargets(ctx context.Context, db Querier, route string, targets []RouteTarget) error {
	timestamp := m.stamp()
	if _, err := db.ExecContext(ctx, `DELETE FROM llm_route_targets WHERE route = ?`, route); err != nil {
		return err
	}
	for priority, target := range targets {
		_, err := db.ExecContext(ctx, `INSERT INTO llm_route_targets (route, provider_id, model_id, priority, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)`, route, target.ProviderID, target.ModelID, priority, timestamp, timestamp)
		if err != nil {
			return err
		}
	}
	return nil
}

// setRouteTargets replaces a route's ordered targets. A target with an empty
// model follows the provider default; any other model must be in the
// provider's catalog. It must run inside a transaction.
func (m *Module) setRouteTargets(ctx context.Context, db Querier, route string, targets []RouteTarget) error {
	seen := map[RouteTarget]bool{}
	normalized := make([]RouteTarget, 0, len(targets))
	for _, target := range targets {
		target = RouteTarget{ProviderID: jsTrim(target.ProviderID), ModelID: jsTrim(target.ModelID)}
		if target.ProviderID == "" {
			continue
		}
		provider, err := GetProvider(ctx, db, target.ProviderID)
		if err != nil {
			return err
		}
		if provider == nil {
			return httpx.Invalid("LLM provider does not exist: %s", target.ProviderID)
		}
		if target.ModelID != "" {
			model, modelErr := GetModel(ctx, db, target.ProviderID, target.ModelID)
			if modelErr != nil {
				return modelErr
			}
			if model == nil {
				return httpx.Invalid("LLM model does not exist: %s/%s", target.ProviderID, target.ModelID)
			}
		}
		if seen[target] {
			continue
		}
		seen[target] = true
		normalized = append(normalized, target)
	}
	return m.writeRouteTargets(ctx, db, route, normalized)
}

// appendRouteProvider adds a provider-default target unless the provider is
// already routed.
func (m *Module) appendRouteProvider(ctx context.Context, db Querier, route, providerID string) error {
	current, err := RouteTargets(ctx, db, route)
	if err != nil {
		return err
	}
	for _, target := range current {
		if target.ProviderID == providerID {
			return nil
		}
	}
	return m.setRouteTargets(ctx, db, route, append(current, RouteTarget{ProviderID: providerID}))
}
