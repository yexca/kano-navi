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

	"github.com/yexca/kano-navi/backend-go/internal/adminapi"
	"github.com/yexca/kano-navi/backend-go/internal/adminauth"
	"github.com/yexca/kano-navi/backend-go/internal/config"
	"github.com/yexca/kano-navi/backend-go/internal/contentadmin"
	"github.com/yexca/kano-navi/backend-go/internal/httpapi"
	"github.com/yexca/kano-navi/backend-go/internal/httpx"
	"github.com/yexca/kano-navi/backend-go/internal/jobs"
	"github.com/yexca/kano-navi/backend-go/internal/llmadmin"
	"github.com/yexca/kano-navi/backend-go/internal/mcpapi"
	"github.com/yexca/kano-navi/backend-go/internal/mediadownload"
	"github.com/yexca/kano-navi/backend-go/internal/schedule"
	"github.com/yexca/kano-navi/backend-go/internal/source/history"
	xsource "github.com/yexca/kano-navi/backend-go/internal/source/x"
	ytsource "github.com/yexca/kano-navi/backend-go/internal/source/youtube"
	"github.com/yexca/kano-navi/backend-go/internal/source/youtube/websub"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend-go/internal/workflow"
)

// stepSkipFlags are the process-wide kill switches for each workflow step.
var stepSkipFlags = map[string]string{"x": "SKIP_X", "youtube": "SKIP_YOUTUBE", "media": "SKIP_MEDIA", "schedule": "SKIP_LLM"}

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

	run := newPipeline(store, secretsKey)
	modules := standaloneModules(store, adminConfig.Mode, secretsKey)
	queue := &workflow.Queue{Store: store, Executor: run.executor}
	defer queue.Close()
	if err = queue.Recover(context.Background()); err != nil {
		return fail("workflow recovery unavailable", 1)
	}
	queue.StartRecovery()
	// Saved workflows with a schedule are started by this process only.
	scheduler := &workflow.Scheduler{Store: store, Queue: queue, Enabled: os.Getenv("WORKFLOW_SCHEDULER_ENABLED") != "0"}
	scheduler.Start()
	defer scheduler.Stop()

	manager := &jobs.QueueManager{Store: store, Queue: queue}
	// YouTube push is configured in /admin; it stays inactive until enabled
	// there and while SKIP_YOUTUBE disables the source.
	push := websub.New(ytsource.OptionsFromEnv().ChannelID, nil, youtubePushTrigger(manager))
	pushAdmin := websub.NewAdmin(store, push, secretsKey, os.Getenv(stepSkipFlags["youtube"]) != "1")
	if err = pushAdmin.Load(context.Background()); err != nil {
		return fail("YouTube push settings unavailable", 1)
	}
	handler := httpapi.NewStandaloneHandler(store, nil, httpapi.Standalone{
		Admin: adminapi.New(adminapi.Options{
			Store:            store,
			Auth:             auth,
			Runs:             manager,
			Modules:          append(modules, pushAdmin),
			Scheduler:        scheduler.Status,
			YoutubePush:      pushAdmin.Status,
			SkippedSteps:     run.skipped,
			XHandles:         run.xOptions.Handles,
			HistoryAvailable: run.steps.History.Available,
		}),
		Static: httpapi.NewStaticSite(cfg.StaticDirectory),
		MCP:    standaloneMCP(store, manager),
		WebSub: push,
	})
	server := httpapi.NewServer(httpapi.RequestLog(httpapi.Compress(handler), os.Stdout), cfg)
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
	xOptions := xsource.OptionsFromEnv()
	youtubeOptions := ytsource.OptionsFromEnv()
	steps := &workflow.SourceSteps{
		Store:    store,
		Skip:     skip,
		X:        &workflow.XRecentExecutor{Store: store, Adapter: xsource.NewAdapter(xsource.NewClient(&http.Client{Timeout: 10 * time.Second}), xOptions), Options: xOptions},
		Youtube:  ytsource.NewAdapter(nil, youtubeOptions),
		Media:    mediadownload.NewClient(nil),
		Schedule: scheduleStep{schedule.New(store, schedule.Options{SecretsKey: key})},
		History: &workflow.HistorySource{
			Fetcher:      history.NewFetcher(store, history.OptionsFromEnv(os.Getenv), nil),
			Store:        store,
			XToken:       strings.TrimSpace(os.Getenv("X_API_BEARER_TOKEN")),
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
		SyntheticSteps: map[string]bool{"x": false, "youtube": false, "media": false, "schedule": false},
		// One unavailable source keeps the other source and post-processing.
		TolerateFailure: map[string]bool{"x": true, "youtube": true},
	}
	return pipeline{steps: steps, executor: executor, skipped: skipped, xOptions: xOptions}
}

// scheduleStep adapts the schedule extractor to the workflow step contract.
type scheduleStep struct{ extractor *schedule.Extractor }

func (s scheduleStep) RunSchedule(ctx context.Context) (string, map[string]any, error) {
	result, err := s.extractor.Run(ctx)
	return result.Status, result.Counts, err
}

// youtubePushTrigger queues the YouTube source and its thumbnail downloads
// through the operator job path, so a push shares the single-flight queue and
// is labelled "websub" in the run history. A busy queue leaves it pending.
func youtubePushTrigger(manager *jobs.QueueManager) websub.TriggerFunc {
	return func(ctx context.Context) (bool, error) {
		result, err := manager.Start(ctx, jobs.KindWorkflow, "websub", jobs.Options{Steps: []string{"youtube", "media"}})
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
