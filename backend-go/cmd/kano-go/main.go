package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/yexca/kano-navi/backend-go/internal/adminauth"
	"github.com/yexca/kano-navi/backend-go/internal/config"
	"github.com/yexca/kano-navi/backend-go/internal/httpapi"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

func main() {
	cfg, err := config.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "kano-go: invalid configuration")
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
	store, err := sqlite.OpenReadOnly(cfg.DatabasePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "kano-go: database unavailable")
		os.Exit(1)
	}
	defer store.Close()

	server := httpapi.NewServer(httpapi.RequestLog(httpapi.NewHandlerWithAdmin(store, nil, admin), os.Stdout), cfg)
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
