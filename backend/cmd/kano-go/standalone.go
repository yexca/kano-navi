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
	"github.com/yexca/kano-navi/backend/internal/schedule"
	bilisource "github.com/yexca/kano-navi/backend/internal/source/bilibili"
	"github.com/yexca/kano-navi/backend/internal/source/history"
	xsource "github.com/yexca/kano-navi/backend/internal/source/x"
	ytsource "github.com/yexca/kano-navi/backend/internal/source/youtube"
	"github.com/yexca/kano-navi/backend/internal/source/youtube/websub"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend/internal/workflow"
)

// stepSkipFlags are the process-wide kill switches for each workflow step.
// SKIP_LLM stops every model call: schedule extraction and translation.
var stepSkipFlags = map[string]string{"x": "SKIP_X", "youtube": "SKIP_YOUTUBE", "bilibili": "SKIP_BILIBILI", "media": "SKIP_MEDIA", "schedule": "SKIP_LLM", jobs.StepTranslate: "SKIP_LLM"}

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

	run := newPipeline(store, secretsKey)
	modules := standaloneModules(store, adminConfig.Mode, secretsKey)
	queue := &workflow.Queue{Store: store, Executor: run.executor}
	defer queue.Close()
	if err = queue.Recover(context.Background()); err != nil {
		return fail("workflow recovery unavailable", 1)
	}
	queue.StartRecovery()
	// A Bilibili interval below a raised floor is raised before the first
	// tick (ADR-0003).
	floor := int(run.steps.Bilibili.Options.MinInterval / time.Minute)
	if err = store.PrepareSourceSchedules(context.Background(), floor, time.Now()); err != nil {
		return fail("source schedules unavailable", 1)
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
	push := websub.New(ytsource.OptionsFromEnv().ChannelID, nil, youtubePushTrigger(manager))
	pushAdmin := websub.NewAdmin(store, push, secretsKey, os.Getenv(stepSkipFlags["youtube"]) != "1")
	if err = pushAdmin.Load(context.Background()); err != nil {
		return fail("YouTube push settings unavailable", 1)
	}
	handler := httpapi.NewStandaloneHandler(store, nil, httpapi.Standalone{
		Admin: adminapi.New(adminapi.Options{
			Store:        store,
			Auth:         auth,
			Runs:         manager,
			Modules:      append(modules, pushAdmin),
			Scheduler:    scheduler.Status,
			YoutubePush:  pushAdmin.Status,
			LiveProbe:    probe.Status,
			RunPruner:    pruner.Status,
			YoutubeYTDLP: run.steps.Youtube.YTDLP,
			SkippedSteps: run.skipped,
			XHandles:     run.xOptions.Handles,
			Bilibili:     run.steps.Bilibili,
			FetchMethods: run.steps.History.Methods,
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
// LLM_SECRETS_KEY that seals provider credentials.
func standaloneModules(store *sqlite.Store, mode, key string) []httpx.AdminModule {
	return []httpx.AdminModule{
		llmadmin.New(store, llmadmin.Options{Mode: mode, SecretsKey: key}),
		contentadmin.New(store, contentadmin.Options{}),
	}
}

// pipeline is the synchronization machinery shared by the service and the
// one-shot sync command: every catalog step with its real adapter.
type pipeline struct {
	steps    *workflow.SourceSteps
	executor *workflow.Executor
	skipped  []string
	xOptions xsource.Options
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
	// The live-status probe is part of the YouTube source.
	skip[jobs.StepLive] = skip["youtube"]
	xOptions := xsource.OptionsFromEnv()
	youtubeOptions := ytsource.OptionsFromEnv()
	xAdapter := xsource.NewAdapter(xsource.NewClient(&http.Client{Timeout: 10 * time.Second}), xOptions)
	youtubeAdapter := ytsource.NewAdapter(nil, youtubeOptions)
	// Manual fetches list through the source adapters' clients, apart from
	// X full-archive search.
	fetcher := history.NewFetcher(store, history.DefaultOptions(), nil)
	fetcher.Timeline, fetcher.Uploads = xAdapter, youtubeAdapter
	// The timed X chain's API method reads through the same client as the
	// manual one; without the credential the chain skips it.
	xToken := strings.TrimSpace(os.Getenv("X_API_BEARER_TOKEN"))
	if xToken != "" {
		xAdapter.API = &history.XAPI{Fetcher: fetcher, Credential: xToken}
	}
	steps := &workflow.SourceSteps{
		Store:     store,
		Skip:      skip,
		X:         &workflow.XRecentExecutor{Store: store, Adapter: xAdapter, Options: xOptions},
		Youtube:   youtubeAdapter,
		Bilibili:  bilisource.NewAdapter(nil, bilisource.OptionsFromEnv()),
		Media:     mediadownload.NewClient(nil),
		Schedule:  scheduleStep{schedule.New(store, schedule.Options{SecretsKey: key})},
		Translate: translateStep{schedule.NewTranslator(store, schedule.Options{SecretsKey: key})},
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
	executor := &workflow.Executor{
		Store: store,
		Steps: steps,
		// Every catalog step has a real adapter here; none is synthetic.
		SyntheticSteps: map[string]bool{"x": false, "youtube": false, "bilibili": false, jobs.StepLive: false, "media": false, "schedule": false, jobs.StepTranslate: false, "reconcile": false},
		// One unavailable source keeps the other source and post-processing,
		// and a failed translation keeps event reconciliation.
		TolerateFailure: map[string]bool{"x": true, "youtube": true, "bilibili": true, jobs.StepTranslate: true},
	}
	return pipeline{steps: steps, executor: executor, skipped: skipped, xOptions: xOptions}
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
