package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/adminauth"
	"github.com/yexca/kano-navi/backend-go/internal/config"
	"github.com/yexca/kano-navi/backend-go/internal/httpapi"
	xsource "github.com/yexca/kano-navi/backend-go/internal/source/x"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend-go/internal/workflow"
)

func main() {
	cfg, err := config.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "kano-go: invalid configuration")
		os.Exit(2)
	}
	if cfg.EnableAdminWrites && os.Getenv("APP_MODE") != "development" {
		fmt.Fprintln(os.Stderr, "kano-go: admin writes require APP_MODE=development")
		os.Exit(2)
	}
	if cfg.EnableXSync && os.Getenv("APP_MODE") != "development" {
		fmt.Fprintln(os.Stderr, "kano-go: X source writes require APP_MODE=development")
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
	if cfg.EnableXSync {
		store, err = sqlite.OpenWorkflowCore(cfg.DatabasePath, true)
	} else if cfg.EnableAdminWrites {
		store, err = sqlite.OpenAdminWrites(cfg.DatabasePath)
	} else {
		store, err = sqlite.OpenReadOnly(cfg.DatabasePath)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "kano-go: database unavailable")
		os.Exit(1)
	}
	defer store.Close()

	var trigger *workflow.XRecentTrigger
	if cfg.EnableXSync {
		if err = store.EnsureXRecentWorkflow(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, "kano-go: workflow unavailable")
			os.Exit(1)
		}
		opts := xsource.OptionsFromEnv()
		adapter := xsource.NewAdapter(xsource.NewClient(&http.Client{Timeout: 10 * time.Second}), opts)
		xexec := &workflow.XRecentExecutor{Store: store, Adapter: adapter, Options: opts}
		executor := &workflow.Executor{Store: store, Steps: xexec, SyntheticSteps: map[string]bool{"x": false}}
		trigger = &workflow.XRecentTrigger{Executor: executor, WorkflowID: "x-recent"}
	}
	server := httpapi.NewServer(httpapi.RequestLog(httpapi.NewHandlerWithAdminAndX(store, nil, admin, trigger), os.Stdout), cfg)
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
