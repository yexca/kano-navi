package llmadmin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/httpx"
	"github.com/yexca/kano-navi/backend-go/internal/jsonbody"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// Options configures the module. Mode is "production" unless it is exactly
// "development". SecretsKey is the LLM_SECRETS_KEY value. HTTPClient carries
// provider requests; redirects are always refused, whatever client is given.
type Options struct {
	Mode       string
	SecretsKey string
	HTTPClient *http.Client
	// LookupEnv supplies the SCHEDULE_* defaults; nil uses os.LookupEnv.
	LookupEnv func(string) (string, bool)
	// Now is the clock for stored timestamps; nil uses time.Now.
	Now func() time.Time
}

// Module implements httpx.AdminModule for the LLM configuration routes.
type Module struct {
	store  *sqlite.Store
	db     *sql.DB
	mode   string
	key    string
	client *http.Client
	env    func(string) (string, bool)
	now    func() time.Time
}

var _ httpx.AdminModule = (*Module)(nil)

func New(store *sqlite.Store, opts Options) *Module {
	client := &http.Client{}
	if opts.HTTPClient != nil {
		copied := *opts.HTTPClient
		client = &copied
	}
	client.CheckRedirect = refuseRedirect
	module := &Module{
		store:  store,
		db:     store.DB(),
		mode:   "production",
		key:    opts.SecretsKey,
		client: client,
		env:    opts.LookupEnv,
		now:    opts.Now,
	}
	if opts.Mode == "development" {
		module.mode = "development"
	}
	if module.env == nil {
		module.env = os.LookupEnv
	}
	if module.now == nil {
		module.now = time.Now
	}
	return module
}

type notFound string

func (e notFound) Error() string { return string(e) }

const (
	providerNotFound = notFound("llm_provider_not_found")
	modelNotFound    = notFound("llm_model_not_found")
)

func (m *Module) fail(w http.ResponseWriter, err error) {
	var missing notFound
	switch {
	case errors.As(err, &missing):
		httpx.WriteCode(w, http.StatusNotFound, string(missing))
	case errors.Is(err, sqlite.ErrReadOnly):
		httpx.WriteCode(w, http.StatusNotImplemented, "capability_not_migrated")
	default:
		httpx.WriteError(w, err)
	}
}

// write runs fn in one transaction, so a rejected request leaves no partial
// change. fn must not perform network requests.
func (m *Module) write(ctx context.Context, fn func(tx *sql.Tx) error) error {
	if !m.store.Writable() {
		return sqlite.ErrReadOnly
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (m *Module) body(w http.ResponseWriter, r *http.Request) (map[string]any, error) {
	raw, err := httpx.Body(w, r)
	if err != nil {
		return nil, err
	}
	body := make(map[string]any, len(raw))
	for key, member := range raw {
		var value any
		if json.Unmarshal(member, &value) != nil {
			return nil, jsonbody.ErrInvalid
		}
		body[key] = value
	}
	return body, nil
}

func segments(path string) ([]string, bool) {
	if !strings.HasPrefix(path, "/") {
		return nil, false
	}
	trimmed := strings.TrimSuffix(path[1:], "/")
	if trimmed == "" {
		return nil, false
	}
	parts := strings.Split(trimmed, "/")
	if slices.Contains(parts, "") {
		return nil, false
	}
	return parts, true
}

type handler func(http.ResponseWriter, *http.Request)

// ServeAdmin routes the LLM configuration paths. The caller has already
// authenticated the operator and applied the origin guard.
func (m *Module) ServeAdmin(w http.ResponseWriter, r *http.Request, path string) bool {
	parts, ok := segments(path)
	if !ok {
		return false
	}
	is := func(index int, name string) bool { return strings.EqualFold(parts[index], name) }
	var routes map[string]handler
	switch {
	case len(parts) == 1 && is(0, "config"):
		routes = map[string]handler{http.MethodGet: m.getConfig, http.MethodPut: m.putConfig}
	case len(parts) == 1 && is(0, "providers"):
		routes = map[string]handler{http.MethodGet: m.getProviders, http.MethodPost: m.postProvider}
	case len(parts) == 1 && is(0, "models"):
		routes = map[string]handler{http.MethodGet: m.getModels}
	case len(parts) == 1 && is(0, "routes"):
		routes = map[string]handler{http.MethodGet: m.getRoutes}
	case len(parts) == 2 && is(0, "routes"):
		routes = map[string]handler{http.MethodPut: m.putRoute(parts[1])}
	case len(parts) == 2 && is(0, "providers"):
		routes = map[string]handler{http.MethodPut: m.putProvider(parts[1]), http.MethodDelete: m.deleteProvider(parts[1])}
		if is(1, "order") {
			routes[http.MethodPut] = m.putProviderOrder
		}
	case len(parts) == 3 && is(0, "providers") && is(2, "test"):
		test := m.testProvider(parts[1])
		routes = map[string]handler{http.MethodPut: test, http.MethodPost: test}
	case len(parts) == 3 && is(0, "providers") && is(2, "models"):
		routes = map[string]handler{http.MethodPost: m.postModels(parts[1]), http.MethodPut: m.putModel(parts[1])}
	case len(parts) == 4 && is(0, "providers") && is(2, "models") && is(3, "discover"):
		routes = map[string]handler{http.MethodPost: m.discoverModels(parts[1])}
	case len(parts) == 4 && is(0, "providers") && is(2, "models") && is(3, "remove"):
		routes = map[string]handler{http.MethodPost: m.removeModels(parts[1])}
	default:
		return false
	}
	method := r.Method
	if method == http.MethodHead {
		method = http.MethodGet
	}
	handle, known := routes[method]
	switch {
	case m.db == nil:
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
	case !known:
		allowed := make([]string, 0, len(routes))
		for name := range routes {
			allowed = append(allowed, name)
		}
		sort.Strings(allowed)
		httpx.MethodNotAllowed(w, strings.Join(allowed, ", "))
	case method != http.MethodGet && !m.store.Writable():
		m.fail(w, sqlite.ErrReadOnly)
	default:
		handle(w, r)
	}
	return true
}

// --- configuration ----------------------------------------------------------

type configResponse struct {
	LLMModel                  string                  `json:"llmModel"`
	OpenAIKeyConfigured       bool                    `json:"openAiKeyConfigured"`
	ScheduleExtractionEnabled bool                    `json:"scheduleExtractionEnabled"`
	ScheduleKeywordEnabled    bool                    `json:"scheduleKeywordEnabled"`
	ScheduleVisionEnabled     bool                    `json:"scheduleVisionEnabled"`
	ScheduleMessageEnabled    bool                    `json:"scheduleMessageEnabled"`
	ScheduleKeywords          []string                `json:"scheduleKeywords"`
	ProviderOrder             []string                `json:"providerOrder"`
	ProviderOrders            RouteMap[[]string]      `json:"providerOrders"`
	Routes                    RouteMap[[]string]      `json:"routes"`
	Providers                 []Provider              `json:"providers"`
	Models                    []Model                 `json:"models"`
	ModelTags                 []string                `json:"modelTags"`
	RouteTargets              RouteMap[[]RouteTarget] `json:"routeTargets"`
	ConfiguredRouteTargets    RouteMap[[]RouteTarget] `json:"configuredRouteTargets"`
	FeaturedVideoID           *string                 `json:"featuredVideoId"`
	UpdatedAt                 string                  `json:"updatedAt,omitempty"`
}

func (m *Module) configPayload(ctx context.Context, db Querier, updatedAt string) (configResponse, error) {
	response := configResponse{ModelTags: llmModelTags, UpdatedAt: updatedAt}
	schedule, err := ScheduleExtractionConfig(ctx, db, m.env)
	if err != nil {
		return response, err
	}
	response.ScheduleExtractionEnabled = schedule.Enabled
	response.ScheduleKeywordEnabled = schedule.KeywordEnabled
	response.ScheduleVisionEnabled = schedule.VisionEnabled
	response.ScheduleMessageEnabled = schedule.MessageEnabled
	response.ScheduleKeywords = schedule.Keywords
	if response.ProviderOrders, err = ScheduleProviderOrders(ctx, db); err != nil {
		return response, err
	}
	response.Routes = response.ProviderOrders
	response.ProviderOrder = response.ProviderOrders.Vision
	if response.Providers, err = ListProviders(ctx, db); err != nil {
		return response, err
	}
	for _, provider := range response.Providers {
		if provider.ID == "openai-default" {
			response.OpenAIKeyConfigured = provider.APIKeyConfigured
		}
	}
	if response.LLMModel, err = GetAppSetting(ctx, db, "llm_model", DefaultScheduleModel); err != nil {
		return response, err
	}
	if response.Models, err = ListModels(ctx, db, ""); err != nil {
		return response, err
	}
	if response.RouteTargets, err = ScheduleRouteTargets(ctx, db); err != nil {
		return response, err
	}
	if response.ConfiguredRouteTargets, err = configuredRouteTargets(ctx, db); err != nil {
		return response, err
	}
	featured, err := FeaturedVideoID(ctx, db)
	response.FeaturedVideoID = nullableString(featured)
	return response, err
}

func (m *Module) getConfig(w http.ResponseWriter, r *http.Request) {
	response, err := m.configPayload(r.Context(), m.db, "")
	if err != nil {
		m.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, response)
}

func (m *Module) putConfig(w http.ResponseWriter, r *http.Request) {
	body, err := m.body(w, r)
	if err != nil {
		m.fail(w, err)
		return
	}
	ctx := r.Context()
	var response configResponse
	err = m.write(ctx, func(tx *sql.Tx) error {
		updatedAt := ""
		flag := func(field string, keys ...string) error {
			raw, present := body[field]
			if !present {
				return nil
			}
			enabled, flagErr := booleanValue(raw, field)
			if flagErr != nil {
				return flagErr
			}
			value := "0"
			if enabled {
				value = "1"
			}
			for index, key := range keys {
				stamp, setErr := m.setSetting(ctx, tx, key, value)
				if setErr != nil {
					return setErr
				}
				if index == 0 {
					updatedAt = stamp
				}
			}
			return nil
		}
		if raw, present := body["llmModel"]; present {
			model, modelErr := text(raw, "llmModel", true, 128)
			if modelErr != nil {
				return modelErr
			}
			if !llmModelPattern.MatchString(model) {
				return httpx.Invalid("llmModel contains unsupported characters")
			}
			if updatedAt, err = m.setSetting(ctx, tx, "llm_model", model); err != nil {
				return err
			}
		}
		// The legacy switch writes every stage, then explicit stage fields win.
		if err := flag("scheduleExtractionEnabled", "schedule_extraction_enabled",
			"schedule_keyword_enabled", "schedule_vision_enabled", "schedule_message_enabled"); err != nil {
			return err
		}
		if err := flag("scheduleKeywordEnabled", "schedule_keyword_enabled"); err != nil {
			return err
		}
		if err := flag("scheduleVisionEnabled", "schedule_vision_enabled"); err != nil {
			return err
		}
		if err := flag("scheduleMessageEnabled", "schedule_message_enabled"); err != nil {
			return err
		}
		if raw, present := body["scheduleKeywords"]; present {
			keywords, keywordErr := scheduleKeywords(raw)
			if keywordErr != nil {
				return keywordErr
			}
			if updatedAt, err = m.setSetting(ctx, tx, "schedule_keywords", compactJSON(keywords)); err != nil {
				return err
			}
		}
		if raw, present := body["featuredVideoId"]; present {
			videoID, videoErr := text(raw, "featuredVideoId", false, 64)
			if videoErr != nil {
				return videoErr
			}
			previous, lookupErr := FeaturedVideoID(ctx, tx)
			if lookupErr != nil {
				return lookupErr
			}
			if err := m.setFeaturedVideoID(ctx, tx, videoID); err != nil {
				return err
			}
			current, lookupErr := FeaturedVideoID(ctx, tx)
			if lookupErr != nil {
				return lookupErr
			}
			updatedAt = m.stamp()
			if previous != current {
				// Polling browsers reload the board when the revision moves.
				if err := sqlite.BumpRevisionTx(ctx, tx, updatedAt); err != nil {
					return err
				}
			}
		}
		var payloadErr error
		response, payloadErr = m.configPayload(ctx, tx, updatedAt)
		return payloadErr
	})
	if err != nil {
		m.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, response)
}

// --- providers --------------------------------------------------------------

type providersResponse struct {
	Providers      []Provider         `json:"providers"`
	Route          string             `json:"route"`
	ProviderOrder  []string           `json:"providerOrder"`
	ProviderOrders RouteMap[[]string] `json:"providerOrders"`
	Routes         RouteMap[[]string] `json:"routes"`
}

func (m *Module) providersPayload(ctx context.Context, db Querier, route string) (providersResponse, error) {
	response := providersResponse{Route: route}
	var err error
	if response.ProviderOrders, err = ScheduleProviderOrders(ctx, db); err != nil {
		return response, err
	}
	response.Routes = response.ProviderOrders
	response.ProviderOrder = response.ProviderOrders.Get(route)
	response.Providers, err = ListProviders(ctx, db)
	return response, err
}

func (m *Module) getProviders(w http.ResponseWriter, r *http.Request) {
	route, err := validProviderRoute(strings.Join(r.URL.Query()["route"], ","))
	if err != nil {
		m.fail(w, err)
		return
	}
	response, err := m.providersPayload(r.Context(), m.db, route)
	if err != nil {
		m.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, response)
}

func (m *Module) postProvider(w http.ResponseWriter, r *http.Request) {
	body, err := m.body(w, r)
	if err != nil {
		m.fail(w, err)
		return
	}
	ctx := r.Context()
	var provider *Provider
	err = m.write(ctx, func(tx *sql.Tx) error {
		input, inputErr := m.providerInput(body, nil)
		if inputErr != nil {
			return inputErr
		}
		if provider, err = m.upsertProvider(ctx, tx, input); err != nil {
			return err
		}
		// Legacy clients create a provider with one model and expect it to
		// join every route. Providers without a default model are routed
		// explicitly from the detection settings.
		if provider == nil || provider.Model == "" {
			return nil
		}
		for _, route := range []string{RouteVision, RouteMessage, RouteBoard} {
			if err := m.appendRouteProvider(ctx, tx, route, provider.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		m.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]*Provider{"provider": provider})
}

func (m *Module) putProviderOrder(w http.ResponseWriter, r *http.Request) {
	body, err := m.body(w, r)
	if err != nil {
		m.fail(w, err)
		return
	}
	ctx := r.Context()
	var response providersResponse
	err = m.write(ctx, func(tx *sql.Tx) error {
		route, routeErr := validProviderRoute(body["route"])
		if routeErr != nil {
			return routeErr
		}
		ids, isArray := coalesce(body["providerOrder"], body["ids"]).([]any)
		if !isArray || len(ids) > 100 {
			return httpx.Invalid("providerOrder must be an array")
		}
		targets := make([]RouteTarget, 0, len(ids))
		for _, id := range ids {
			targets = append(targets, RouteTarget{ProviderID: jsTrim(jsString(id))})
		}
		if err := m.setRouteTargets(ctx, tx, route, targets); err != nil {
			return err
		}
		var payloadErr error
		if response, payloadErr = m.providersPayload(ctx, tx, route); payloadErr != nil {
			return payloadErr
		}
		// The saved route's own order, without the vision fallback.
		response.ProviderOrder, payloadErr = routeProviders(ctx, tx, route)
		return payloadErr
	})
	if err != nil {
		m.fail(w, err)
		return
	}
	// Same fields as the listing, in the order the save reply always used.
	httpx.WriteJSON(w, http.StatusOK, struct {
		Route          string             `json:"route"`
		ProviderOrder  []string           `json:"providerOrder"`
		ProviderOrders RouteMap[[]string] `json:"providerOrders"`
		Routes         RouteMap[[]string] `json:"routes"`
		Providers      []Provider         `json:"providers"`
	}{response.Route, response.ProviderOrder, response.ProviderOrders, response.Routes, response.Providers})
}

func (m *Module) putProvider(id string) handler {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := m.body(w, r)
		if err != nil {
			m.fail(w, err)
			return
		}
		ctx := r.Context()
		var provider *Provider
		err = m.write(ctx, func(tx *sql.Tx) error {
			existing, lookupErr := GetProvider(ctx, tx, id)
			if lookupErr != nil {
				return lookupErr
			}
			if existing == nil {
				return providerNotFound
			}
			body["id"] = id
			input, inputErr := m.providerInput(body, existing)
			if inputErr != nil {
				return inputErr
			}
			// Edits never re-add a provider that an operator removed from a route.
			provider, err = m.upsertProvider(ctx, tx, input)
			return err
		})
		if err != nil {
			m.fail(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]*Provider{"provider": provider})
	}
}

func (m *Module) deleteProvider(id string) handler {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		err := m.write(ctx, func(tx *sql.Tx) error {
			deleted, deleteErr := deleteProvider(ctx, tx, id)
			if deleteErr == nil && !deleted {
				return providerNotFound
			}
			return deleteErr
		})
		if err != nil {
			m.fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// discover lists the provider's remote models and records the outcome on the
// provider row. The request runs outside any transaction.
func (m *Module) discover(ctx context.Context, provider *Provider) ([]RemoteModel, error) {
	apiKey, err := ProviderAPIKey(ctx, m.db, provider.ID, m.key)
	if err != nil {
		return nil, err
	}
	if apiKey == "" {
		return nil, httpx.Invalid("provider API key is not configured")
	}
	remote, fetchErr := m.fetchRemoteModels(ctx, provider, apiKey)
	if fetchErr != nil {
		if err = m.updateProviderStatus(ctx, m.db, provider.ID, "failed", fetchErr.Error()); err != nil {
			return nil, err
		}
		return nil, httpx.Invalid("%s", fetchErr.Error())
	}
	return remote, m.updateProviderStatus(ctx, m.db, provider.ID, "success", "")
}

type testResponse struct {
	OK               bool      `json:"ok"`
	Method           string    `json:"method"`
	Model            string    `json:"model,omitempty"`
	RemoteModelCount *int      `json:"remoteModelCount,omitempty"`
	Provider         *Provider `json:"provider"`
}

func (m *Module) testProvider(id string) handler {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := m.body(w, r)
		if err != nil {
			m.fail(w, err)
			return
		}
		// The outcome is recorded even if the operator's browser goes away;
		// the provider deadline still bounds the work.
		ctx := context.WithoutCancel(r.Context())
		response, err := m.runProviderTest(ctx, id, body)
		if err != nil {
			m.fail(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, response)
	}
}

func (m *Module) runProviderTest(ctx context.Context, id string, body map[string]any) (testResponse, error) {
	var response testResponse
	provider, err := GetProvider(ctx, m.db, id)
	if err != nil {
		return response, err
	}
	if provider == nil {
		return response, providerNotFound
	}
	model, err := modelID(body["model"], "model", false)
	if err != nil {
		return response, err
	}
	if model != "" {
		catalog, lookupErr := GetModel(ctx, m.db, provider.ID, model)
		if lookupErr != nil {
			return response, lookupErr
		}
		if catalog == nil {
			return response, httpx.Invalid("model is not in the provider catalog")
		}
	}
	apiKey, err := ProviderAPIKey(ctx, m.db, provider.ID, m.key)
	if err != nil {
		return response, err
	}
	if apiKey == "" {
		return response, httpx.Invalid("provider API key is not configured")
	}
	if model == "" {
		model = provider.Model
	}
	response.OK = true
	if model == "" {
		// Without any model, listing models is the cheapest authenticated check.
		remote, discoverErr := m.discover(ctx, provider)
		if discoverErr != nil {
			return response, discoverErr
		}
		count := len(remote)
		response.Method, response.RemoteModelCount = "models", &count
	} else {
		if probeErr := m.probeInference(ctx, provider, apiKey, model); probeErr != nil {
			if err = m.updateProviderStatus(ctx, m.db, provider.ID, "failed", probeErr.Error()); err != nil {
				return response, err
			}
			return response, httpx.Invalid("%s", probeErr.Error())
		}
		if err = m.updateProviderStatus(ctx, m.db, provider.ID, "success", ""); err != nil {
			return response, err
		}
		response.Method, response.Model = "inference", model
	}
	response.Provider, err = GetProvider(ctx, m.db, provider.ID)
	return response, err
}

// --- models -----------------------------------------------------------------

type discoveredModel struct {
	RemoteModel
	SuggestedTags []string `json:"suggestedTags"`
	Added         bool     `json:"added"`
	Tags          []string `json:"tags"`
}

func (m *Module) getModels(w http.ResponseWriter, r *http.Request) {
	models, err := ListModels(r.Context(), m.db, strings.Join(r.URL.Query()["providerId"], ","))
	if err != nil {
		m.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Models    []Model  `json:"models"`
		ModelTags []string `json:"modelTags"`
	}{models, llmModelTags})
}

func (m *Module) discoverModels(id string) handler {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithoutCancel(r.Context())
		provider, err := GetProvider(ctx, m.db, id)
		if err == nil && provider == nil {
			err = providerNotFound
		}
		if err != nil {
			m.fail(w, err)
			return
		}
		remote, err := m.discover(ctx, provider)
		if err != nil {
			m.fail(w, err)
			return
		}
		catalog, err := ListModels(ctx, m.db, provider.ID)
		if err != nil {
			m.fail(w, err)
			return
		}
		added := make(map[string][]string, len(catalog))
		for _, model := range catalog {
			added[model.ID] = model.Tags
		}
		models := make([]discoveredModel, 0, len(remote))
		for _, model := range remote {
			tags, isAdded := added[model.ID]
			models = append(models, discoveredModel{RemoteModel: model, SuggestedTags: InferModelTags(model.ID), Added: isAdded, Tags: tags})
		}
		current, err := GetProvider(ctx, m.db, provider.ID)
		if err != nil {
			m.fail(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, struct {
			Provider *Provider         `json:"provider"`
			Models   []discoveredModel `json:"models"`
		}{current, models})
	}
}

type modelsResponse struct {
	Removed  *int64    `json:"removed,omitempty"`
	Provider *Provider `json:"provider"`
	Model    *Model    `json:"model,omitempty"`
	Models   []Model   `json:"models"`
}

func (m *Module) modelsPayload(ctx context.Context, db Querier, providerID string) (modelsResponse, error) {
	var response modelsResponse
	var err error
	if response.Provider, err = GetProvider(ctx, db, providerID); err != nil {
		return response, err
	}
	response.Models, err = ListModels(ctx, db, providerID)
	return response, err
}

func existingProvider(ctx context.Context, db Querier, id string) (*Provider, error) {
	provider, err := GetProvider(ctx, db, id)
	if err == nil && provider == nil {
		err = providerNotFound
	}
	return provider, err
}

func (m *Module) postModels(id string) handler {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := m.body(w, r)
		if err != nil {
			m.fail(w, err)
			return
		}
		ctx := r.Context()
		var response modelsResponse
		err = m.write(ctx, func(tx *sql.Tx) error {
			provider, lookupErr := existingProvider(ctx, tx, id)
			if lookupErr != nil {
				return lookupErr
			}
			values, isArray := body["models"].([]any)
			if !isArray || len(values) == 0 || len(values) > 500 {
				return httpx.Invalid("models must be a non-empty array")
			}
			inputs := make([]modelWrite, 0, len(values))
			for _, value := range values {
				input, inputErr := modelInput(value)
				if inputErr != nil {
					return inputErr
				}
				existing, modelErr := GetModel(ctx, tx, provider.ID, input.ID)
				if modelErr != nil {
					return modelErr
				}
				if !input.HasTags && existing == nil {
					input.Tags, input.HasTags = InferModelTags(input.ID), true
				}
				switch {
				case existing != nil && existing.Origin != "":
					input.Origin = existing.Origin
				case body["origin"] == "remote":
					input.Origin = "remote"
				default:
					input.Origin = "manual"
				}
				inputs = append(inputs, input)
			}
			if err := m.upsertModels(ctx, tx, provider, inputs); err != nil {
				return err
			}
			if provider.Model == "" {
				// The first catalog model becomes the default test model.
				first, modelErr := GetModel(ctx, tx, provider.ID, inputs[0].ID)
				if modelErr != nil {
					return modelErr
				}
				capabilities := provider.Capabilities
				if first != nil && len(first.Capabilities) > 0 {
					capabilities = first.Capabilities
				}
				if _, err := m.upsertProvider(ctx, tx, providerWrite{
					ID: provider.ID, Name: provider.Name, Protocol: provider.Protocol,
					BaseURL: provider.BaseURL, Model: inputs[0].ID, Enabled: provider.Enabled,
					Capabilities: capabilities, TimeoutMs: provider.TimeoutMs,
				}); err != nil {
					return err
				}
			}
			var payloadErr error
			response, payloadErr = m.modelsPayload(ctx, tx, provider.ID)
			return payloadErr
		})
		if err != nil {
			m.fail(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, response)
	}
}

func (m *Module) putModel(id string) handler {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := m.body(w, r)
		if err != nil {
			m.fail(w, err)
			return
		}
		ctx := r.Context()
		var response modelsResponse
		err = m.write(ctx, func(tx *sql.Tx) error {
			provider, lookupErr := existingProvider(ctx, tx, id)
			if lookupErr != nil {
				return lookupErr
			}
			input, inputErr := modelInput(body)
			if inputErr != nil {
				return inputErr
			}
			existing, modelErr := GetModel(ctx, tx, provider.ID, input.ID)
			if modelErr != nil {
				return modelErr
			}
			if existing == nil {
				return modelNotFound
			}
			if err := m.upsertModels(ctx, tx, provider, []modelWrite{input}); err != nil {
				return err
			}
			var payloadErr error
			if response, payloadErr = m.modelsPayload(ctx, tx, provider.ID); payloadErr != nil {
				return payloadErr
			}
			response.Model, payloadErr = GetModel(ctx, tx, provider.ID, input.ID)
			return payloadErr
		})
		if err != nil {
			m.fail(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, response)
	}
}

func (m *Module) removeModels(id string) handler {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := m.body(w, r)
		if err != nil {
			m.fail(w, err)
			return
		}
		ctx := r.Context()
		var response modelsResponse
		err = m.write(ctx, func(tx *sql.Tx) error {
			provider, lookupErr := existingProvider(ctx, tx, id)
			if lookupErr != nil {
				return lookupErr
			}
			values, isArray := body["ids"].([]any)
			if !isArray || len(values) == 0 || len(values) > 500 {
				return httpx.Invalid("ids must be a non-empty array")
			}
			ids := make([]string, 0, len(values))
			for _, value := range values {
				model, idErr := modelID(value, "model id", true)
				if idErr != nil {
					return idErr
				}
				ids = append(ids, model)
			}
			removed, deleteErr := m.deleteModels(ctx, tx, provider, ids)
			if deleteErr != nil {
				return deleteErr
			}
			var payloadErr error
			response, payloadErr = m.modelsPayload(ctx, tx, provider.ID)
			response.Removed = &removed
			return payloadErr
		})
		if err != nil {
			m.fail(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, response)
	}
}

// --- routes -----------------------------------------------------------------

func (m *Module) getRoutes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	effective, err := ScheduleRouteTargets(ctx, m.db)
	if err != nil {
		m.fail(w, err)
		return
	}
	configured, err := configuredRouteTargets(ctx, m.db)
	if err != nil {
		m.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Routes     RouteMap[[]RouteTarget] `json:"routes"`
		Configured RouteMap[[]RouteTarget] `json:"configured"`
	}{effective, configured})
}

func (m *Module) putRoute(name string) handler {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := m.body(w, r)
		if err != nil {
			m.fail(w, err)
			return
		}
		ctx := r.Context()
		var response configResponse
		err = m.write(ctx, func(tx *sql.Tx) error {
			route, routeErr := validProviderRoute(name)
			if routeErr != nil {
				return routeErr
			}
			targets, targetErr := routeTargetsInput(body["targets"])
			if targetErr != nil {
				return targetErr
			}
			if err := m.setRouteTargets(ctx, tx, route, targets); err != nil {
				return err
			}
			var payloadErr error
			response, payloadErr = m.configPayload(ctx, tx, m.stamp())
			return payloadErr
		})
		if err != nil {
			m.fail(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, response)
	}
}
