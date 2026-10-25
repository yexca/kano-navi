package llmadmin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
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
		httpx.ReadOnly(w)
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

type handler = http.HandlerFunc

// AdminRoutes lists the extraction settings, provider catalog, model routing
// and featured-video endpoints. The router has already authenticated the
// operator and applied the origin guard.
func (m *Module) AdminRoutes() []httpx.Route {
	id := func(build func(string) handler) handler {
		return func(w http.ResponseWriter, r *http.Request) { build(r.PathValue("id"))(w, r) }
	}
	return []httpx.Route{
		{Method: http.MethodGet, Path: "/extraction", Handler: m.guard(m.getExtraction)},
		{Method: http.MethodPut, Path: "/extraction", Handler: m.guard(m.putExtraction)},
		{Method: http.MethodGet, Path: "/llm", Handler: m.guard(m.getLLM)},
		{Method: http.MethodPost, Path: "/llm/providers", Handler: m.guard(m.postProvider)},
		{Method: http.MethodPut, Path: "/llm/providers/{id}", Handler: m.guard(id(m.putProvider))},
		{Method: http.MethodDelete, Path: "/llm/providers/{id}", Handler: m.guard(id(m.deleteProvider))},
		{Method: http.MethodPost, Path: "/llm/providers/{id}/test", Handler: m.guard(id(m.testProvider))},
		{Method: http.MethodPost, Path: "/llm/providers/{id}/discover", Handler: m.guard(id(m.discoverModels))},
		{Method: http.MethodPost, Path: "/llm/providers/{id}/models", Handler: m.guard(id(m.postModels))},
		{Method: http.MethodPut, Path: "/llm/providers/{id}/models", Handler: m.guard(id(m.putModel))},
		{Method: http.MethodPost, Path: "/llm/providers/{id}/models/remove", Handler: m.guard(id(m.removeModels))},
		{Method: http.MethodPut, Path: "/llm/queue", Handler: m.guard(m.putQueue)},
		{Method: http.MethodPut, Path: "/videos/featured", Handler: m.guard(m.putFeatured)},
	}
}

// guard rejects every route without a database and every write against a
// read-only one before the handler parses its body.
func (m *Module) guard(next handler) handler {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case m.db == nil:
			httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		case r.Method != http.MethodGet && r.Method != http.MethodHead && !m.store.Writable():
			httpx.ReadOnly(w)
		default:
			next(w, r)
		}
	}
}

// --- extraction settings ----------------------------------------------------

type extractionResponse struct {
	// Enabled is derived: the message detector alone, or keyword plus vision.
	Enabled         bool     `json:"enabled"`
	KeywordEnabled  bool     `json:"keywordEnabled"`
	VisionEnabled   bool     `json:"visionEnabled"`
	MessageEnabled  bool     `json:"messageEnabled"`
	MessageAllPosts bool     `json:"messageAllPosts"`
	Keywords        []string `json:"keywords"`
}

func (m *Module) extractionPayload(ctx context.Context, db Querier) (extractionResponse, error) {
	schedule, err := ScheduleExtractionConfig(ctx, db, m.env)
	return extractionResponse(schedule), err
}

func (m *Module) getExtraction(w http.ResponseWriter, r *http.Request) {
	response, err := m.extractionPayload(r.Context(), m.db)
	if err != nil {
		m.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, response)
}

var extractionStages = []struct{ field, key string }{
	{"keywordEnabled", "schedule_keyword_enabled"},
	{"visionEnabled", "schedule_vision_enabled"},
	{"messageEnabled", "schedule_message_enabled"},
	{"messageAllPosts", "schedule_message_all_posts"},
}

func (m *Module) putExtraction(w http.ResponseWriter, r *http.Request) {
	body, err := m.body(w, r)
	if err != nil {
		m.fail(w, err)
		return
	}
	ctx := r.Context()
	var response extractionResponse
	err = m.write(ctx, func(tx *sql.Tx) error {
		for _, stage := range extractionStages {
			raw, present := body[stage.field]
			if !present {
				continue
			}
			enabled, flagErr := booleanValue(raw, stage.field)
			if flagErr != nil {
				return flagErr
			}
			value := "0"
			if enabled {
				value = "1"
			}
			if _, setErr := m.setSetting(ctx, tx, stage.key, value); setErr != nil {
				return setErr
			}
		}
		if raw, present := body["keywords"]; present {
			keywords, keywordErr := scheduleKeywords(raw)
			if keywordErr != nil {
				return keywordErr
			}
			if _, setErr := m.setSetting(ctx, tx, "schedule_keywords", compactJSON(keywords)); setErr != nil {
				return setErr
			}
		}
		var payloadErr error
		response, payloadErr = m.extractionPayload(ctx, tx)
		return payloadErr
	})
	if err != nil {
		m.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, response)
}

// --- featured video ---------------------------------------------------------

// putFeatured pins the board's featured video; null or "" restores the
// automatic choice. A change advances the dashboard revision.
func (m *Module) putFeatured(w http.ResponseWriter, r *http.Request) {
	body, err := m.body(w, r)
	if err != nil {
		m.fail(w, err)
		return
	}
	raw, present := body["videoId"]
	if !present {
		m.fail(w, httpx.Invalid("videoId is required"))
		return
	}
	ctx := r.Context()
	var featured string
	err = m.write(ctx, func(tx *sql.Tx) error {
		videoID, videoErr := text(raw, "videoId", false, 64)
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
		if featured, lookupErr = FeaturedVideoID(ctx, tx); lookupErr != nil {
			return lookupErr
		}
		if previous != featured {
			// Polling browsers reload the board when the revision moves.
			return sqlite.BumpRevisionTx(ctx, tx, m.stamp())
		}
		return nil
	})
	if err != nil {
		m.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]*string{"featuredVideoId": nullableString(featured)})
}

// --- catalog ----------------------------------------------------------------

type llmResponse struct {
	Providers []Provider    `json:"providers"`
	Models    []Model       `json:"models"`
	Queue     []RouteTarget `json:"queue"`
	// TranslationQueue is the separate translation route.
	TranslationQueue []RouteTarget `json:"translationQueue"`
	ModelTags        []string      `json:"modelTags"`
}

func (m *Module) getLLM(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	response := llmResponse{ModelTags: llmModelTags}
	var err error
	if response.Providers, err = ListProviders(ctx, m.db); err == nil {
		if response.Models, err = ListModels(ctx, m.db, ""); err == nil {
			if response.Queue, err = QueueTargets(ctx, m.db); err == nil {
				response.TranslationQueue, err = RouteTargets(ctx, m.db, TranslationRoute)
			}
		}
	}
	if err != nil {
		m.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, response)
}

// --- providers --------------------------------------------------------------

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
		// Queue targets name catalog models, so the operator queues them.
		provider, err = m.upsertProvider(ctx, tx, input)
		return err
	})
	if err != nil {
		m.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]*Provider{"provider": provider})
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

// --- queue ------------------------------------------------------------------

func (m *Module) putQueue(w http.ResponseWriter, r *http.Request) {
	body, err := m.body(w, r)
	if err != nil {
		m.fail(w, err)
		return
	}
	ctx := r.Context()
	// route selects the schedule queue (the default) or the translation queue.
	route := QueueRoute
	if value, present := body["route"]; present {
		if name, _ := value.(string); name == QueueRoute || name == TranslationRoute {
			route = name
		} else {
			m.fail(w, httpx.Invalid("route must be schedule or translate"))
			return
		}
	}
	var queue []RouteTarget
	err = m.write(ctx, func(tx *sql.Tx) error {
		targets, targetErr := routeTargetsInput(body["targets"])
		if targetErr != nil {
			return targetErr
		}
		if err := m.setQueue(ctx, tx, route, targets); err != nil {
			return err
		}
		var payloadErr error
		queue, payloadErr = RouteTargets(ctx, tx, route)
		return payloadErr
	})
	if err != nil {
		m.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"route": route, "queue": queue})
}
