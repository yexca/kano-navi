// Package adminapi serves the operator API under /api/admin. It owns the one
// route table, the session and origin guards and the shared response
// conventions described in docs/architecture/admin-api.md. Feature packages
// contribute routes through httpx.AdminModule.
package adminapi

import (
	"context"
	"errors"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/yexca/kano-navi/backend/internal/adminauth"
	"github.com/yexca/kano-navi/backend/internal/httpx"
	"github.com/yexca/kano-navi/backend/internal/jobs"
	bilisource "github.com/yexca/kano-navi/backend/internal/source/bilibili"
	"github.com/yexca/kano-navi/backend/internal/source/history"
	ytsource "github.com/yexca/kano-navi/backend/internal/source/youtube"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

// RunManager is the durable single-flight queue as the console sees it.
// jobs.QueueManager implements it.
type RunManager interface {
	Start(ctx context.Context, kind, triggeredBy string, options jobs.Options) (jobs.StartResult, error)
	Get(ctx context.Context, id string) (*jobs.Job, error)
	Active(ctx context.Context) (*jobs.Job, error)
	Page(ctx context.Context, page, pageSize int) ([]jobs.Job, int, error)
	Cancel(ctx context.Context, id string) (*jobs.Job, error)
	Retry(ctx context.Context, id string) (*jobs.Job, error)
}

// Options are the runtime dependencies. Store and Auth are required; a nil
// Runs answers the run routes with 503 unavailable.
type Options struct {
	Store   *sqlite.Store
	Auth    *adminauth.Auth
	Runs    RunManager
	Modules []httpx.AdminModule
	// Scheduler reports the in-process source schedule timer.
	Scheduler func() map[string]any
	// YoutubePush reports the WebSub settings and subscription.
	YoutubePush func() map[string]any
	// LiveProbe reports the in-process YouTube live-status probe timer.
	LiveProbe func() map[string]any
	// SkippedSteps lists steps disabled for the process through SKIP_* flags.
	SkippedSteps []string
	XHandles     []string
	// Bilibili reads single uploads for operators; nil disables the routes
	// that need it.
	Bilibili *bilisource.Adapter
	// YoutubeYTDLP is the optional local yt-dlp runner used by the YouTube
	// source and its operator update route.
	YoutubeYTDLP *ytsource.YTDLP
	// FetchMethods describes a source's manual fetch methods and which of
	// them can run now. Without it no method can run, which keeps a read-only
	// sidecar from accepting work its queue cannot run.
	FetchMethods func(source string) []history.MethodStatus
	// Now is the clock for Japan-date aggregates; nil uses time.Now.
	Now func() time.Time
}

type api struct{ Options }

var japanZone = time.FixedZone("JST", 9*3600)

// New builds the /api/admin handler.
func New(options Options) http.Handler {
	a := &api{options}
	if a.Now == nil {
		a.Now = time.Now
	}
	groups := [][]httpx.Route{a.routes()}
	for _, module := range a.Modules {
		if module != nil {
			groups = append(groups, module.AdminRoutes())
		}
	}
	mux := httpx.NewAdminMux(groups...)
	protected := a.Auth.Require(a.Auth.OriginGuard(a.writable(mux)))
	session := a.Auth.OriginGuard(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, no-transform")
		switch {
		case a.Auth == nil || a.Store == nil:
			httpx.WriteCode(w, http.StatusServiceUnavailable, "admin_unavailable")
		case r.URL.Path == httpx.AdminPrefix+"/session":
			session.ServeHTTP(w, r)
		default:
			protected.ServeHTTP(w, r)
		}
	})
}

func (a *api) routes() []httpx.Route {
	return []httpx.Route{
		{Method: http.MethodGet, Path: "/session", Handler: a.Auth.Session},
		{Method: http.MethodPost, Path: "/session", Handler: a.Auth.Login},
		{Method: http.MethodDelete, Path: "/session", Handler: a.Auth.Logout},

		{Method: http.MethodGet, Path: "/overview", Handler: a.overview},
		{Method: http.MethodGet, Path: "/settings/visitor-region", Handler: a.visitorRegionSettings},
		{Method: http.MethodPut, Path: "/settings/visitor-region", Handler: a.updateVisitorRegionSettings},

		{Method: http.MethodGet, Path: "/runs", Handler: a.needRuns(a.listRuns)},
		{Method: http.MethodPost, Path: "/runs", Handler: a.needRuns(a.startRun)},
		{Method: http.MethodGet, Path: "/runs/steps", Handler: a.stepCatalog},
		{Method: http.MethodGet, Path: "/runs/{id}", Handler: a.needRuns(a.getRun)},
		{Method: http.MethodPost, Path: "/runs/{id}/cancel", Handler: a.needRuns(a.cancelRun)},
		{Method: http.MethodPost, Path: "/runs/{id}/retry", Handler: a.needRuns(a.retryRun)},

		{Method: http.MethodGet, Path: "/sources", Handler: a.sources},
		{Method: http.MethodPut, Path: "/sources/{source}/schedule", Handler: a.updateSourceSchedule},
		{Method: http.MethodPost, Path: "/sources/{source}/run", Handler: a.needRuns(a.runSource)},
		{Method: http.MethodGet, Path: "/sources/youtube/live-probe", Handler: a.liveProbe},
		{Method: http.MethodPut, Path: "/sources/youtube/live-probe", Handler: a.updateLiveProbe},
		{Method: http.MethodGet, Path: "/sources/youtube/ytdlp", Handler: a.ytdlpStatus},
		{Method: http.MethodPost, Path: "/sources/youtube/ytdlp/update", Handler: a.updateYTDLP},
		{Method: http.MethodGet, Path: "/sync-settings", Handler: a.syncSettings},
		{Method: http.MethodPut, Path: "/sync-settings", Handler: a.updateSyncSettings},

		{Method: http.MethodGet, Path: "/events", Handler: a.listEvents},
		{Method: http.MethodPost, Path: "/events", Handler: a.createEvent},
		{Method: http.MethodPut, Path: "/events/{id}", Handler: a.updateEvent},
		{Method: http.MethodPost, Path: "/events/{id}/confirm", Handler: a.confirmEvent},
		{Method: http.MethodPost, Path: "/events/{id}/merge", Handler: a.mergeEvent},
		{Method: http.MethodPost, Path: "/events/{id}/unmerge", Handler: a.unmergeEvent},
		{Method: http.MethodDelete, Path: "/events/{id}", Handler: a.deleteEvent},

		{Method: http.MethodGet, Path: "/posts", Handler: a.listPosts},
		{Method: http.MethodGet, Path: "/videos", Handler: a.listVideos},
		{Method: http.MethodGet, Path: "/videos/{id}/history", Handler: a.videoHistory},
		{Method: http.MethodPut, Path: "/videos/{id}", Handler: a.updateVideo},
		{Method: http.MethodPost, Path: "/videos/{id}/refresh", Handler: a.refreshVideo},
		{Method: http.MethodPost, Path: "/videos/bilibili", Handler: a.addBilibiliVideo},
		{Method: http.MethodGet, Path: "/videos/merges", Handler: a.videoMerges},
		{Method: http.MethodGet, Path: "/videos/{id}/merges", Handler: a.uploadMerges},
		{Method: http.MethodPost, Path: "/videos/{id}/merge", Handler: a.mergeVideo},
		{Method: http.MethodPost, Path: "/videos/{id}/unmerge", Handler: a.unmergeVideo},

		{Method: http.MethodGet, Path: "/translations", Handler: a.translationSummary},
		{Method: http.MethodPost, Path: "/translations/request", Handler: a.requestTranslations},
		{Method: http.MethodPut, Path: "/translations/{owner}/{id}/{locale}", Handler: a.putManualTranslation},
		{Method: http.MethodDelete, Path: "/translations/{owner}/{id}/{locale}", Handler: a.deleteManualTranslation},
	}
}

// writable answers unsafe methods against a read-only database before any
// handler parses a body.
func (a *api) writable(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !a.Store.Writable() {
			httpx.ReadOnly(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *api) needRuns(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.Runs == nil {
			httpx.WriteCode(w, http.StatusServiceUnavailable, "unavailable")
			return
		}
		next(w, r)
	}
}

func (a *api) skipped(step string) bool { return slices.Contains(a.SkippedSteps, step) }

// fetchMethods lists a source's manual fetch methods with their availability.
func (a *api) fetchMethods(source string) []history.MethodStatus {
	if a.FetchMethods != nil {
		return a.FetchMethods(source)
	}
	out := []history.MethodStatus{}
	for _, method := range history.Methods(source) {
		out = append(out, history.MethodStatus{Method: method})
	}
	return out
}

func (a *api) methodAvailable(source, id string) bool {
	for _, method := range a.fetchMethods(source) {
		if method.ID == id {
			return method.Available
		}
	}
	return false
}

// workflowError maps durable queue and definition failures to the wire codes.
func workflowError(w http.ResponseWriter, err error, notFound string) {
	switch {
	case errors.Is(err, sqlite.ErrWorkflowBusy):
		httpx.WriteCode(w, http.StatusConflict, "busy")
	case errors.Is(err, sqlite.ErrWorkflowNotFound):
		httpx.WriteCode(w, http.StatusNotFound, notFound)
	case errors.Is(err, sqlite.ErrWorkflowValidation):
		httpx.WriteError(w, httpx.Invalid("request is invalid"))
	case errors.Is(err, sqlite.ErrWorkflowUnavailable):
		httpx.WriteCode(w, http.StatusServiceUnavailable, "unavailable")
	case errors.Is(err, sqlite.ErrReadOnly):
		httpx.ReadOnly(w)
	default:
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
	}
}

// queryInt reads a positive integer parameter, falling back when it is
// absent or malformed.
func queryInt(r *http.Request, name string, fallback int) int {
	value, err := strconv.ParseFloat(strings.TrimSpace(r.URL.Query().Get(name)), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 1 {
		return fallback
	}
	if value > 1e6 {
		return 1e6
	}
	return int(value)
}

func queryFlag(r *http.Request, name string) bool {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get(name))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func query(r *http.Request, name string) string { return strings.TrimSpace(r.URL.Query().Get(name)) }

func pageOf[T any](items []T, page, pageSize, total int) sqlite.AdminPage[T] {
	totalPages := max(1, (total+pageSize-1)/pageSize)
	return sqlite.AdminPage[T]{Items: items, Page: min(page, totalPages), PageSize: pageSize, Total: total, TotalPages: totalPages}
}
