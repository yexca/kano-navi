package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/yexca/kano-navi/backend/internal/adminapi"
	"github.com/yexca/kano-navi/backend/internal/adminauth"
	"github.com/yexca/kano-navi/backend/internal/config"
	"github.com/yexca/kano-navi/backend/internal/contentadmin"
	"github.com/yexca/kano-navi/backend/internal/httpapi"
	"github.com/yexca/kano-navi/backend/internal/httpx"
	"github.com/yexca/kano-navi/backend/internal/jobs"
	"github.com/yexca/kano-navi/backend/internal/llmadmin"
	"github.com/yexca/kano-navi/backend/internal/mcpapi"
	"github.com/yexca/kano-navi/backend/internal/mediadownload"
	"github.com/yexca/kano-navi/backend/internal/outboundproxy"
	"github.com/yexca/kano-navi/backend/internal/schedule"
	bilisource "github.com/yexca/kano-navi/backend/internal/source/bilibili"
	"github.com/yexca/kano-navi/backend/internal/source/history"
	"github.com/yexca/kano-navi/backend/internal/source/niconico"
	shopsource "github.com/yexca/kano-navi/backend/internal/source/shop"
	xsource "github.com/yexca/kano-navi/backend/internal/source/x"
	ytsource "github.com/yexca/kano-navi/backend/internal/source/youtube"
	"github.com/yexca/kano-navi/backend/internal/source/youtube/websub"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend/internal/variants"
	"github.com/yexca/kano-navi/backend/internal/workflow"
	"github.com/yexca/kano-navi/backend/internal/works"
)

// stepSkipFlags are the process-wide kill switches for each workflow step.
// SKIP_LLM stops every model call: schedule extraction and translation.
var stepSkipFlags = map[string]string{"x": "SKIP_X", sqlite.XRelatedStep: "SKIP_X", "youtube": "SKIP_YOUTUBE", "bilibili": "SKIP_BILIBILI", sqlite.BilibiliDynamicsSource: "SKIP_BILIBILI", sqlite.ShopSource: "SKIP_SHOP", "media": "SKIP_MEDIA", "schedule": "SKIP_LLM", jobs.StepTranslate: "SKIP_LLM"}

func fail(message string, code int) int {
	fmt.Fprintln(os.Stderr, "kano-go: "+message)
	return code
}

// runStandalone serves the whole site from this process: public reads, the
// operator console, workflows and timers, MCP and the built frontend.
// Credentials are validated before the runtime database is opened or created.
func runStandalone(cfg config.Config) int {
	adminConfig, err := adminauth.ResolveEnv()
	if err != nil {
		return fail("invalid admin configuration", 2)
	}
	auth, err := adminauth.New(adminConfig)
	if err != nil {
		return fail("invalid admin configuration", 2)
	}
	secretsKey := strings.TrimSpace(os.Getenv("LLM_SECRETS_KEY"))
	if secretsKey == "" {
		return fail("LLM_SECRETS_KEY is not configured; refusing to start", 2)
	}
	store, err := sqlite.OpenStandalone(cfg.DatabasePath)
	if err != nil {
		return fail("database unavailable", 1)
	}
	defer store.Close()
	// Read bounds once set in the environment become stored sync settings on
	// the first start that sees them; /admin owns them from then on.
	if err = store.ImportSyncSettings(context.Background(), os.Getenv); err != nil {
		return fail("sync settings unavailable", 1)
	}
	// Stage flags an earlier release saved become one of the Detection page's
	// two modes on the first start.
	if err = llmadmin.SettleDetectionMode(context.Background(), store, os.LookupEnv); err != nil {
		return fail("detection settings unavailable", 1)
	}

	run := newPipeline(store, secretsKey)
	// The variants step and the Site page's copies status read the archives
	// under the data directory.
	run.steps.Variants = &variants.Roots{Data: store.DataRoot()}
	modules := standaloneModules(store, adminConfig.Mode, secretsKey, run.proxies)
	queue := &workflow.Queue{Store: store, Executor: run.executor}
	defer queue.Close()
	if err = queue.Recover(context.Background()); err != nil {
		return fail("workflow recovery unavailable", 1)
	}
	queue.StartRecovery()
	// A Bilibili interval below a raised floor is raised before the first
	// tick.
	floor := int(run.steps.Bilibili.Options.MinInterval / time.Minute)
	if err = store.PrepareSourceSchedules(context.Background(), floor, time.Now()); err != nil {
		return fail("source schedules unavailable", 1)
	}
	// An account named in X_HANDLES is an own account only.
	if err = store.SettleXRelatedRoles(context.Background(), run.xOptions.Handles); err != nil {
		return fail("related accounts unavailable", 1)
	}
	// Source schedules are started by this process only.
	scheduler := &workflow.Scheduler{
		Store: store, Queue: queue, Enabled: os.Getenv("WORKFLOW_SCHEDULER_ENABLED") != "0",
		SkipSources: run.steps.Skip, BilibiliFloor: floor,
	}
	scheduler.Start()
	defer scheduler.Stop()

	manager := &jobs.QueueManager{Store: store, Queue: queue}
	// The live-status probe has its own operator interval but runs through the
	// same queue, and only in this process.
	probe := &workflow.LiveProbe{
		Store:   store,
		Submit:  liveProbeTrigger(manager),
		Enabled: scheduler.Enabled && !run.steps.Skip[jobs.StepLive],
		// Each part of the probe stops with its source.
		SkipYoutube: run.steps.Skip["youtube"],
	}
	if run.steps.Bilibili != nil && !run.steps.Skip["bilibili"] {
		probe.BilibiliAccount = run.steps.Bilibili.Options.Account
	}
	probe.Start()
	defer probe.Stop()
	// Run history retention is local database work and runs whatever the
	// timers' switch says.
	pruner := &workflow.RunPruner{Store: store}
	pruner.Start()
	defer pruner.Stop()
	// YouTube push is configured in /admin; it stays inactive until enabled
	// there and while SKIP_YOUTUBE disables the source.
	push := websub.New(ytsource.OptionsFromEnv().ChannelID, run.proxies.Client(outboundproxy.AreaYoutube), youtubePushTrigger(manager))
	pushAdmin := websub.NewAdmin(store, push, secretsKey, os.Getenv(stepSkipFlags["youtube"]) != "1")
	if err = pushAdmin.Load(context.Background()); err != nil {
		return fail("YouTube push settings unavailable", 1)
	}
	handler := httpapi.NewStandaloneHandler(store, nil, httpapi.Standalone{
		Admin: adminapi.New(adminapi.Options{
			Store:     store,
			Auth:      auth,
			Runs:      manager,
			Modules:   append(modules, pushAdmin),
			Scheduler: scheduler.Status,
			// A timer the operator starts is submitted at once when the
			// queue is free.
			TickScheduler: func(ctx context.Context) { _, _ = scheduler.Tick(ctx) },
			YoutubePush:   pushAdmin.Status,
			LiveProbe:     probe.Status,
			RunPruner:     pruner.Status,
			YoutubeYTDLP:  run.steps.Youtube.YTDLP,
			SkippedSteps:  run.skipped,
			XHandles:      run.xOptions.Handles,
			Bilibili:      run.steps.Bilibili,
			FetchMethods:  run.steps.History.Methods,
			Variants:      run.steps.Variants,
			Works:         run.steps.Works,
			Queues:        run.queues,
			Youtube:       run.steps.Youtube,
		}),
		Static:       httpapi.NewStaticSite(cfg.StaticDirectory, cfg.PublicOrigin),
		MCP:          standaloneMCP(store, manager),
		WebSub:       push,
		PublicOrigin: cfg.PublicOrigin,
	})
	server := httpapi.NewServer(httpapi.RequestLog(httpapi.Compress(handler), os.Stdout), cfg)
	// Revision streams stay open until the client leaves; end them when
	// shutdown begins so it can finish.
	server.RegisterOnShutdown(handler.CloseStreams)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	failed := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			failed <- err
		}
	}()
	fmt.Fprintf(os.Stderr, "kano-go: standalone service listening on %s\n", cfg.ListenAddress)
	// The hub verifies the callback asynchronously, so subscribe once the
	// listener is running.
	push.Start()
	defer push.Stop()
	select {
	case <-ctx.Done():
		push.Stop()
		probe.Stop()
		pruner.Stop()
		scheduler.Stop()
		queue.Close()
		shutdown, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := httpapi.Shutdown(shutdown, server); err != nil {
			return fail("graceful shutdown failed", 1)
		}
	case <-failed:
		return fail("HTTP server stopped unexpectedly", 1)
	}
	return 0
}

// runHealthCheck asks the local service for its health document.
func runHealthCheck(cfg config.Config) int {
	address := cfg.ListenAddress
	if strings.HasPrefix(address, ":") {
		address = "127.0.0.1" + address
	}
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get("http://" + address + "/api/health")
	if err != nil {
		return 1
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

// standaloneModules attaches the operator feature modules. key is the
// LLM_SECRETS_KEY that seals provider and proxy credentials; proxies carries
// the modules' outbound requests.
func standaloneModules(store *sqlite.Store, mode, key string, proxies *outboundproxy.Pool) []httpx.AdminModule {
	return []httpx.AdminModule{
		llmadmin.New(store, llmadmin.Options{Mode: mode, SecretsKey: key, HTTPClient: proxies.Client(outboundproxy.AreaLLM)}),
		contentadmin.New(store, contentadmin.Options{HTTPClient: proxies.Client(outboundproxy.AreaMedia)}),
		outboundproxy.NewAdmin(store, proxies),
	}
}

// pipeline is the synchronization machinery shared by the service and the
// one-shot sync command: every catalog step with its real adapter.
type pipeline struct {
	steps    *workflow.SourceSteps
	executor *workflow.Executor
	skipped  []string
	xOptions xsource.Options
	// proxies routes every outbound request by area.
	proxies *outboundproxy.Pool
	// queues previews what the schedule and translate steps would work
	// through, with the same extractor and translator.
	queues adminapi.QueuePreviews
}

// newPipeline wires the source, media and schedule steps. key is the
// LLM_SECRETS_KEY that unseals provider credentials for schedule extraction.
// Source API credentials are read from the environment and stay in memory.
func newPipeline(store *sqlite.Store, key string) pipeline {
	skip := map[string]bool{}
	skipped := []string{}
	for _, step := range jobs.StepOrder {
		if os.Getenv(stepSkipFlags[step]) == "1" {
			skip[step] = true
			skipped = append(skipped, step)
		}
	}
	// The live-status probe checks the YouTube videos and the Bilibili room;
	// each part stops with its source.
	skip[jobs.StepLive] = skip["youtube"] && skip["bilibili"]
	xOptions := xsource.OptionsFromEnv()
	youtubeOptions := ytsource.OptionsFromEnv()
	// Each client's outbound requests follow its area's proxy route.
	proxies := outboundproxy.NewPool(store.DB(), key)
	xAdapter := xsource.NewAdapter(xsource.NewClient(&http.Client{Timeout: 10 * time.Second, Transport: proxies.Transport(outboundproxy.AreaX)}), xOptions)
	youtubeAdapter := ytsource.NewAdapter(ytsource.NewClient(proxies.Client(outboundproxy.AreaYoutube)), youtubeOptions)
	if youtubeAdapter.YTDLP != nil {
		youtubeAdapter.YTDLP.Routes = ytdlpRoutes(proxies)
	}
	// Manual fetches list through the source adapters' clients, apart from
	// X full-archive search.
	fetcher := history.NewFetcher(store, history.DefaultOptions(), proxies.Transport(outboundproxy.AreaX))
	fetcher.Timeline, fetcher.Uploads = xAdapter, youtubeAdapter
	// The timed X chain's API method reads through the same client as the
	// manual one; without the credential the chain skips it.
	xToken := strings.TrimSpace(os.Getenv("X_API_BEARER_TOKEN"))
	if xToken != "" {
		xAdapter.API = &history.XAPI{Fetcher: fetcher, Credential: xToken}
	}
	extractor := schedule.New(store, schedule.Options{SecretsKey: key, HTTPClient: proxies.Client(outboundproxy.AreaLLM)})
	translator := schedule.NewTranslator(store, schedule.Options{SecretsKey: key, HTTPClient: proxies.Client(outboundproxy.AreaLLM)})
	worksIndex := works.Packaged()
	steps := &workflow.SourceSteps{
		Store:     store,
		Skip:      skip,
		X:         &workflow.XRecentExecutor{Store: store, Adapter: xAdapter, Options: xOptions},
		Youtube:   youtubeAdapter,
		Bilibili:  bilisource.NewAdapter(bilisource.NewClient(proxies.Client(outboundproxy.AreaBilibili)), bilisource.OptionsFromEnv()),
		Shop:      shopsource.NewAdapter(shopsource.NewClient(proxies.Client(outboundproxy.AreaShop))),
		Media:     mediadownload.NewClient(proxies.Client(outboundproxy.AreaMedia)),
		Niconico:  niconico.NewClient(proxies.Client(outboundproxy.AreaMedia)),
		Works:     &worksIndex,
		Schedule:  scheduleStep{extractor},
		Translate: translateStep{translator},
		History: &workflow.HistorySource{
			Fetcher:      fetcher,
			Store:        store,
			XToken:       xToken,
			YoutubeKey:   strings.TrimSpace(os.Getenv("YOUTUBE_API_KEY")),
			Handles:      xOptions.Handles,
			ChannelID:    youtubeOptions.ChannelID,
			RequestLimit: max(1, xOptions.MaxDetailRequests),
		},
	}
	steps.History.Bilibili = steps.Bilibili
	executor := &workflow.Executor{
		Store: store,
		Steps: steps,
		// Every catalog step has a real adapter here; none is synthetic.
		SyntheticSteps: map[string]bool{"x": false, sqlite.XRelatedStep: false, "youtube": false, "bilibili": false, sqlite.BilibiliDynamicsSource: false, sqlite.ShopSource: false, jobs.StepLive: false, "media": false, "schedule": false, jobs.StepTranslate: false, jobs.StepVariants: false, "reconcile": false},
		// One unavailable source keeps the other source and post-processing,
		// and a failed translation keeps event reconciliation.
		TolerateFailure: map[string]bool{"x": true, sqlite.XRelatedStep: true, "youtube": true, "bilibili": true, sqlite.BilibiliDynamicsSource: true, sqlite.ShopSource: true, jobs.StepTranslate: true},
	}
	queues := adminapi.QueuePreviews{Schedule: extractor.PreviewQueue, Translation: translator.PreviewQueue}
	return pipeline{steps: steps, executor: executor, skipped: skipped, xOptions: xOptions, proxies: proxies, queues: queues}
}

// ytdlpRoutes runs yt-dlp's YouTube reads through the YouTube proxy route.
func ytdlpRoutes(proxies *outboundproxy.Pool) func(context.Context) ([]ytsource.ProcessRoute, error) {
	return func(ctx context.Context) ([]ytsource.ProcessRoute, error) {
		attempts, err := proxies.ProcessAttempts(ctx, outboundproxy.AreaYoutube)
		if err != nil {
			return nil, err
		}
		routes := make([]ytsource.ProcessRoute, 0, len(attempts))
		for _, attempt := range attempts {
			routes = append(routes, ytsource.ProcessRoute{Env: attempt.Env, Done: attempt.Done})
		}
		return routes, nil
	}
}

// translateStep adapts the translator to the workflow step contract.
type translateStep struct{ translator *schedule.Translator }

func (s translateStep) RunTranslate(ctx context.Context) (string, map[string]any, error) {
	result, err := s.translator.Run(ctx)
	return result.Status, result.Counts, err
}

// scheduleStep adapts the schedule extractor to the workflow step contract.
type scheduleStep struct{ extractor *schedule.Extractor }

func (s scheduleStep) RunSchedule(ctx context.Context) (string, map[string]any, error) {
	result, err := s.extractor.Run(ctx)
	return result.Status, result.Counts, err
}

// youtubePushTrigger queues the YouTube source chain through the operator
// job path, which reads YouTube as its schedule does, so a push shares the
// single-flight queue and is labelled "websub" in the run history. A busy
// queue leaves it pending.
func youtubePushTrigger(manager *jobs.QueueManager) websub.TriggerFunc {
	return func(ctx context.Context) (bool, error) {
		options := jobs.Options{Steps: workflow.SourceChains["youtube"]}
		result, err := manager.Start(ctx, jobs.KindWorkflow, "websub", options)
		return result.Accepted, err
	}
}

// liveProbeTrigger queues the YouTube live-status probe through the operator
// job path, labelled "live-probe" in the run history. A busy queue leaves it
// for the next tick.
func liveProbeTrigger(manager *jobs.QueueManager) func(context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		result, err := manager.Start(ctx, jobs.KindLive, jobs.TriggeredByLiveProbe, jobs.Options{})
		return result.Accepted, err
	}
}

// standaloneMCP mounts the public MCP reads; control tools additionally need
// the separate MCP_CONTROL_TOKEN.
func standaloneMCP(store *sqlite.Store, manager jobs.Manager) http.Handler {
	return mcpapi.New(store, mcpapi.Options{
		Enabled:      os.Getenv("MCP_ENABLED") != "0",
		ControlToken: os.Getenv("MCP_CONTROL_TOKEN"),
		Jobs:         manager,
	})
}
