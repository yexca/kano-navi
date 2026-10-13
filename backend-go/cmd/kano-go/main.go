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

	"github.com/yexca/kano-navi/backend-go/internal/adminauth"
	"github.com/yexca/kano-navi/backend-go/internal/config"
	"github.com/yexca/kano-navi/backend-go/internal/httpapi"
	"github.com/yexca/kano-navi/backend-go/internal/mediadownload"
	xsource "github.com/yexca/kano-navi/backend-go/internal/source/x"
	ytsource "github.com/yexca/kano-navi/backend-go/internal/source/youtube"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend-go/internal/workflow"
)

func main() {
	cfg, err := config.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "kano-go: invalid configuration")
		os.Exit(2)
	}
	if cfg.HealthCheck {
		os.Exit(runHealthCheck(cfg))
	}
	if cfg.Standalone {
		os.Exit(runStandalone(cfg))
	}
	if cfg.EnableAdminWrites && os.Getenv("APP_MODE") != "development" {
		fmt.Fprintln(os.Stderr, "kano-go: admin writes require APP_MODE=development")
		os.Exit(2)
	}
	enableSources := cfg.EnableXSync || cfg.EnableYoutubeSync || cfg.EnableMediaDownloads || cfg.EnableScheduler
	if (enableSources || cfg.Initialize) && os.Getenv("APP_MODE") != "development" {
		fmt.Fprintln(os.Stderr, "kano-go: source and workflow writes require APP_MODE=development")
		os.Exit(2)
	}
	if cfg.Initialize && strings.TrimSpace(os.Getenv("LLM_SECRETS_KEY")) == "" {
		fmt.Fprintln(os.Stderr, "kano-go: initialization requires LLM_SECRETS_KEY")
		os.Exit(2)
	}
	var admin *adminauth.Auth
	// The public sidecar remains usable for the existing read-only smoke tests
	// when no admin environment is supplied. Supplying any admin setting opts
	// into the TypeScript-compatible production default and validates it before
	// the admin router is attached.
	if os.Getenv("APP_MODE") != "" || os.Getenv("ADMIN_PASSWORD") != "" || os.Getenv("ADMIN_ORIGIN") != "" {
		adminConfig, configErr := adminauth.ResolveEnv()
		if configErr != nil {
			fmt.Fprintln(os.Stderr, "kano-go: invalid admin configuration")
			os.Exit(2)
		}
		admin, err = adminauth.New(adminConfig)
		if err != nil {
			fmt.Fprintln(os.Stderr, "kano-go: invalid admin configuration")
			os.Exit(2)
		}
	}
	var store *sqlite.Store
	if cfg.Initialize {
		store, err = sqlite.OpenInitialized(cfg.DatabasePath, cfg.EnableAdminWrites, enableSources || cfg.EnableAdminWrites)
	} else if cfg.EnableAdminWrites {
		store, err = sqlite.OpenAdminWrites(cfg.DatabasePath)
	} else if enableSources {
		store, err = sqlite.OpenWorkflowCore(cfg.DatabasePath, true)
	} else {
		store, err = sqlite.OpenReadOnly(cfg.DatabasePath)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "kano-go: database unavailable")
		os.Exit(1)
	}
	defer store.Close()

	var queue *workflow.Queue
	triggers := map[string]httpapi.XSyncTrigger{}
	if enableSources {
		if err = store.EnsureSourceWorkflows(context.Background(), cfg.EnableXSync, cfg.EnableYoutubeSync, cfg.EnableMediaDownloads); err != nil {
			fmt.Fprintln(os.Stderr, "kano-go: workflow unavailable")
			os.Exit(1)
		}
		steps := &workflow.SourceSteps{Store: store}
		capabilities := map[string]bool{}
		if cfg.EnableXSync {
			opts := xsource.OptionsFromEnv()
			steps.X = &workflow.XRecentExecutor{Store: store, Adapter: xsource.NewAdapter(xsource.NewClient(&http.Client{Timeout: 10 * time.Second}), opts), Options: opts}
			capabilities["x"] = false
		}
		if cfg.EnableYoutubeSync {
			steps.Youtube = ytsource.NewAdapter(nil, ytsource.OptionsFromEnv())
			capabilities["youtube"] = false
		}
		if cfg.EnableMediaDownloads {
			steps.Media = mediadownload.NewClient(nil)
			capabilities["media"] = false
		}
		executor := &workflow.Executor{Store: store, Steps: steps, SyntheticSteps: capabilities}
		queue = &workflow.Queue{Store: store, Executor: executor}
		defer queue.Close()
		if err = queue.Recover(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, "kano-go: workflow recovery unavailable")
			os.Exit(1)
		}
		queue.StartRecovery()
		if cfg.EnableXSync {
			triggers["x"] = &workflow.RecentTrigger{Queue: queue, WorkflowID: "x-recent", Source: "x"}
		}
		if cfg.EnableYoutubeSync {
			triggers["youtube"] = &workflow.RecentTrigger{Queue: queue, WorkflowID: "youtube-recent", Source: "youtube"}
		}
	}
	scheduler := &workflow.Scheduler{Store: store, Queue: queue, Enabled: cfg.EnableScheduler}
	scheduler.Start()
	defer scheduler.Stop()
	server := httpapi.NewServer(httpapi.RequestLog(httpapi.NewHandlerWithSources(store, nil, admin, triggers, queue), os.Stdout), cfg)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		scheduler.Stop()
		if queue != nil {
			queue.Close()
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := httpapi.Shutdown(shutdownCtx, server); err != nil {
			fmt.Fprintln(os.Stderr, "kano-go: graceful shutdown failed")
			os.Exit(1)
		}
	case <-errCh:
		fmt.Fprintln(os.Stderr, "kano-go: HTTP server stopped unexpectedly")
		os.Exit(1)
	}
}
