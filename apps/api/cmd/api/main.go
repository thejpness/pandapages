package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"pandapages/api/internal/appidentity"
	"pandapages/api/internal/db"
	"pandapages/api/internal/httpadmin"
	"pandapages/api/internal/httpapi"
	"pandapages/api/internal/httpbearer"
	"pandapages/api/internal/httpidentity"
	"pandapages/api/internal/httpmiddleware"
	"pandapages/api/internal/httpprofile"
	"pandapages/api/internal/sourceeligibility"
	"pandapages/api/internal/sourceprovider"
	"pandapages/api/internal/sourceprovider/gutenberg"
	"pandapages/api/internal/supabaseauth"
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
	databaseURL  string
	adminKey     string
	logLevel     slog.Level
	supabaseAuth supabaseauth.Config
}

func loadRuntimeConfig(getenv func(string) string) (runtimeConfig, error) {
	logLevel, err := parseLogLevel(getenv("PP_LOG_LEVEL"))
	if err != nil {
		return runtimeConfig{}, err
	}

	supabaseConfig := supabaseauth.Config{
		Provider: appidentity.ProviderSupabase,
		Issuer:   getenv("PP_SUPABASE_ISSUER"),
		Audience: getenv("PP_SUPABASE_AUDIENCE"),
		JWKSURL:  getenv("PP_SUPABASE_JWKS_URL"),
	}
	if _, err := supabaseauth.New(supabaseConfig); err != nil {
		return runtimeConfig{}, fmt.Errorf("Supabase bearer configuration is invalid: %w", err)
	}

	return runtimeConfig{
		databaseURL:  getenv("DATABASE_URL"),
		adminKey:     strings.TrimSpace(getenv("PP_ADMIN_KEY")),
		logLevel:     logLevel,
		supabaseAuth: supabaseConfig,
	}, nil
}

func parseLogLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("PP_LOG_LEVEL must be one of debug, info, warn, error")
	}
}

func newLogger(output io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(output, &slog.HandlerOptions{Level: level}))
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

func newRootHandler(public, identity, admin http.Handler) http.Handler {
	root := http.NewServeMux()
	root.Handle("/api/v1/admin/", admin)
	root.Handle("/api/auth/", identity)
	root.Handle("/", public)

	// One outer boundary also observes ServeMux redirects and path cleaning.
	return httpmiddleware.Observe(root)
}

func run() error {
	cfg, err := loadRuntimeConfig(os.Getenv)
	if err != nil {
		return err
	}

	slog.SetDefault(newLogger(os.Stderr, cfg.logLevel))
	slog.Debug("logging configured", "level", cfg.logLevel.String())

	store := db.MustOpen(cfg.databaseURL)
	defer store.Close()

	verifier, err := supabaseauth.New(cfg.supabaseAuth)
	if err != nil {
		return fmt.Errorf("configure Supabase bearer verifier: %w", err)
	}
	bearerAuthenticator := httpbearer.New(verifier, store)
	sourceDiscovery, err := sourceprovider.NewRegistry(gutenberg.New(gutenberg.Config{}))
	if err != nil {
		return fmt.Errorf("configure source providers: %w", err)
	}
	sourceEligibility, err := sourceeligibility.New(sourceeligibility.Config{Gateway: sourceDiscovery})
	if err != nil {
		return fmt.Errorf("configure source eligibility: %w", err)
	}

	public := httpapi.New(httpapi.Config{
		BearerAuthenticator: bearerAuthenticator,
		ProfileResolver:     httpprofile.New(store),
	}, store)
	identity := httpidentity.New(bearerAuthenticator, store)

	admin := httpadmin.New(httpadmin.Config{
		AdminKey:            cfg.adminKey,
		BearerAuthenticator: bearerAuthenticator,
		SourceDiscovery:     sourceDiscovery,
		SourceAcquisition:   sourceDiscovery,
		SourceEligibility:   sourceEligibility,
	}, store)

	server := newServer(newRootHandler(public, identity, admin))
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
