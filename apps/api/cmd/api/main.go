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
	"pandapages/api/internal/session"
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

type runtimeConfig struct {
	databaseURL   string
	passcode      string
	adminKey      string
	cookieSecure  bool
	logRequests   bool
	sessionSigner *session.Manager
}

func loadRuntimeConfig(getenv func(string) string) (runtimeConfig, error) {
	passcode := getenv("PP_PASSCODE")
	if !validPasscode(passcode) {
		return runtimeConfig{}, fmt.Errorf("PP_PASSCODE must be exactly six ASCII decimal digits")
	}

	cookieSecure := getenv("PP_COOKIE_SECURE") == "true"
	sessionSigner, err := session.New(getenv("PP_SESSION_SECRET"), cookieSecure)
	if err != nil {
		return runtimeConfig{}, fmt.Errorf("PP_SESSION_SECRET is invalid: %w", err)
	}

	return runtimeConfig{
		databaseURL:   getenv("DATABASE_URL"),
		passcode:      passcode,
		adminKey:      strings.TrimSpace(getenv("PP_ADMIN_KEY")),
		cookieSecure:  cookieSecure,
		logRequests:   getenv("PP_LOG_LEVEL") == "debug",
		sessionSigner: sessionSigner,
	}, nil
}

func validPasscode(passcode string) bool {
	if len(passcode) != 6 {
		return false
	}
	for index := range passcode {
		if passcode[index] < '0' || passcode[index] > '9' {
			return false
		}
	}
	return true
}

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
	cfg, err := loadRuntimeConfig(os.Getenv)
	if err != nil {
		return err
	}

	store := db.MustOpen(cfg.databaseURL)
	defer store.Close()

	public := httpapi.New(httpapi.Config{
		Passcode:    cfg.passcode,
		LogRequests: cfg.logRequests,
		Sessions:    cfg.sessionSigner,
	}, store)

	admin := httpadmin.New(httpadmin.Config{
		AdminKey:    cfg.adminKey,
		LogRequests: cfg.logRequests,
		Sessions:    cfg.sessionSigner,
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
