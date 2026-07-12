package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"pandapages/api/internal/db"
	"pandapages/api/internal/httpadmin"
	"pandapages/api/internal/httpapi"
)

const (
	listenAddress     = ":8080"
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 5 * time.Minute
	writeTimeout      = 6 * time.Minute
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 10 * time.Second
	maxHeaderBytes    = 1 << 20 // 1 MiB
)

func newServer(handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              listenAddress,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}
}

func run() error {
	store := db.MustOpen(os.Getenv("DATABASE_URL"))
	defer store.Close()

	pass := strings.TrimSpace(os.Getenv("PP_PASSCODE"))
	if pass == "" {
		return fmt.Errorf("PP_PASSCODE is required")
	}

	adminKey := strings.TrimSpace(os.Getenv("PP_ADMIN_KEY"))

	public := httpapi.New(httpapi.Config{
		Passcode:     pass,
		CookieSecure: os.Getenv("PP_COOKIE_SECURE") == "true",
		LogRequests:  os.Getenv("PP_LOG_LEVEL") == "debug",
	}, store)

	admin := httpadmin.New(httpadmin.Config{
		AdminKey:     adminKey,
		CookieSecure: os.Getenv("PP_COOKIE_SECURE") == "true",
		LogRequests:  os.Getenv("PP_LOG_LEVEL") == "debug",
	}, store)

	root := http.NewServeMux()
	root.Handle("/api/v1/admin/", admin)
	root.Handle("/", public)

	server := newServer(root)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	slog.Info("api listening", "addr", listenAddress)

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	}
}

func main() {
	if err := run(); err != nil {
		slog.Error("api stopped", "err", err)
		os.Exit(1)
	}
}
