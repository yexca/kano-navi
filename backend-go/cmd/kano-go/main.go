package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

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
	store, err := sqlite.OpenReadOnly(cfg.DatabasePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "kano-go: database unavailable")
		os.Exit(1)
	}
	defer store.Close()

	server := httpapi.NewServer(httpapi.RequestLog(httpapi.NewHandler(store), os.Stdout), cfg)
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
