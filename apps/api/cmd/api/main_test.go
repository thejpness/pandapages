package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewServerHasBoundedTimeouts(t *testing.T) {
	server := newServer(http.NotFoundHandler())

	if server.ReadHeaderTimeout != readHeaderTimeout {
		t.Errorf("ReadHeaderTimeout = %v, want %v", server.ReadHeaderTimeout, readHeaderTimeout)
	}
	if server.ReadTimeout != readTimeout {
		t.Errorf("ReadTimeout = %v, want %v", server.ReadTimeout, readTimeout)
	}
	if server.WriteTimeout != writeTimeout {
		t.Errorf("WriteTimeout = %v, want %v", server.WriteTimeout, writeTimeout)
	}
	if server.IdleTimeout != idleTimeout {
		t.Errorf("IdleTimeout = %v, want %v", server.IdleTimeout, idleTimeout)
	}
	if server.MaxHeaderBytes != maxHeaderBytes {
		t.Errorf("MaxHeaderBytes = %d, want %d", server.MaxHeaderBytes, maxHeaderBytes)
	}
}

func TestNewRootHandlerObservesServeMuxRedirectsExactlyOnce(t *testing.T) {
	previousLogger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	publicCalls := 0
	adminCalls := 0
	handler := newRootHandler(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			publicCalls++
			w.WriteHeader(http.StatusNoContent)
		}),
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			adminCalls++
			w.WriteHeader(http.StatusNoContent)
		}),
	)

	tests := []struct {
		name      string
		target    string
		requestID string
	}{
		{name: "admin subtree slash", target: "/api/v1/admin?oauth_code=private-query", requestID: "root.admin-redirect_1"},
		{name: "cleaned path", target: "/api//v1/library?oauth_code=private-query", requestID: "root.path-redirect_2"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))

			request := httptest.NewRequest(http.MethodGet, test.target, nil)
			request.Header.Set("X-Request-ID", test.requestID)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusTemporaryRedirect {
				t.Fatalf("redirect status = %d, want 307; body = %s", response.Code, response.Body.String())
			}
			if got := response.Header().Get("X-Request-ID"); got != test.requestID {
				t.Fatalf("response request ID = %q, want %q", got, test.requestID)
			}
			if count := strings.Count(logs.String(), `"msg":"http request completed"`); count != 1 {
				t.Fatalf("completion log count = %d, want 1; logs = %s", count, logs.String())
			}
			if !strings.Contains(logs.String(), `"status":307`) || strings.Contains(logs.String(), "oauth_code") || strings.Contains(logs.String(), "private-query") {
				t.Fatalf("redirect completion log is unsafe or incomplete: %s", logs.String())
			}
		})
	}

	if publicCalls != 0 || adminCalls != 0 {
		t.Fatalf("ServeMux redirects reached application handlers: public=%d admin=%d", publicCalls, adminCalls)
	}
}

func TestLoadRuntimeConfigAcceptsValidAuthenticationSettings(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		"DATABASE_URL":      "test-database-url",
		"PP_PASSCODE":       "012345",
		"PP_SESSION_SECRET": strings.Repeat("s", 32),
		"PP_ADMIN_KEY":      "  admin-key  ",
		"PP_COOKIE_SECURE":  "true",
		"PP_LOG_LEVEL":      "debug",
	}
	cfg, err := loadRuntimeConfig(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error = %v", err)
	}
	if cfg.databaseURL != "test-database-url" {
		t.Errorf("databaseURL = %q", cfg.databaseURL)
	}
	if cfg.passcode != "012345" {
		t.Errorf("passcode = %q", cfg.passcode)
	}
	if cfg.adminKey != "admin-key" {
		t.Errorf("adminKey = %q", cfg.adminKey)
	}
	if !cfg.cookieSecure {
		t.Error("cookieSecure = false, want true")
	}
	if cfg.logLevel != slog.LevelDebug {
		t.Errorf("logLevel = %v, want debug", cfg.logLevel)
	}
	if cfg.sessionSigner == nil {
		t.Fatal("sessionSigner = nil")
	}
}

func TestLoadRuntimeConfigRejectsInvalidPasscodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		passcode string
	}{
		{name: "empty", passcode: ""},
		{name: "short", passcode: "12345"},
		{name: "long", passcode: "1234567"},
		{name: "alphabetic", passcode: "12345a"},
		{name: "Unicode digits", passcode: "１２３４５６"},
		{name: "leading whitespace", passcode: " 123456"},
		{name: "trailing whitespace", passcode: "123456 "},
		{name: "embedded whitespace", passcode: "123 56"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			values := map[string]string{
				"PP_PASSCODE":       test.passcode,
				"PP_SESSION_SECRET": strings.Repeat("s", 32),
			}
			_, err := loadRuntimeConfig(func(key string) string { return values[key] })
			if err == nil {
				t.Fatal("loadRuntimeConfig() error = nil, want passcode validation error")
			}
			if !strings.Contains(err.Error(), "PP_PASSCODE") {
				t.Errorf("error = %q, want PP_PASSCODE context", err)
			}
			if test.passcode != "" && strings.Contains(err.Error(), test.passcode) {
				t.Errorf("error leaks configured passcode: %q", err)
			}
		})
	}
}

func TestLoadRuntimeConfigRejectsInvalidSessionSecrets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		secret string
	}{
		{name: "missing", secret: ""},
		{name: "short", secret: strings.Repeat("s", 31)},
		{name: "leading whitespace", secret: " " + strings.Repeat("s", 32)},
		{name: "trailing whitespace", secret: strings.Repeat("s", 32) + "\t"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			values := map[string]string{
				"PP_PASSCODE":       "123456",
				"PP_SESSION_SECRET": test.secret,
			}
			_, err := loadRuntimeConfig(func(key string) string { return values[key] })
			if err == nil {
				t.Fatal("loadRuntimeConfig() error = nil, want session-secret validation error")
			}
			if !strings.Contains(err.Error(), "PP_SESSION_SECRET") {
				t.Errorf("error = %q, want PP_SESSION_SECRET context", err)
			}
			if test.secret != "" && strings.Contains(err.Error(), test.secret) {
				t.Errorf("error leaks configured session secret: %q", err)
			}
		})
	}
}

func TestParseLogLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw  string
		want slog.Level
	}{
		{raw: "", want: slog.LevelInfo},
		{raw: "info", want: slog.LevelInfo},
		{raw: " INFO ", want: slog.LevelInfo},
		{raw: "debug", want: slog.LevelDebug},
		{raw: "DeBuG", want: slog.LevelDebug},
		{raw: "warn", want: slog.LevelWarn},
		{raw: "error", want: slog.LevelError},
	}
	for _, test := range tests {
		got, err := parseLogLevel(test.raw)
		if err != nil {
			t.Errorf("parseLogLevel(%q) error = %v", test.raw, err)
			continue
		}
		if got != test.want {
			t.Errorf("parseLogLevel(%q) = %v, want %v", test.raw, got, test.want)
		}
	}
}

func TestLoadRuntimeConfigRejectsInvalidLogLevel(t *testing.T) {
	t.Parallel()

	const invalid = "verbose-private-value"
	values := map[string]string{
		"PP_PASSCODE":       "123456",
		"PP_SESSION_SECRET": strings.Repeat("s", 32),
		"PP_LOG_LEVEL":      invalid,
	}
	_, err := loadRuntimeConfig(func(key string) string { return values[key] })
	if err == nil {
		t.Fatal("loadRuntimeConfig() error = nil, want log-level validation error")
	}
	if !strings.Contains(err.Error(), "PP_LOG_LEVEL") || strings.Contains(err.Error(), invalid) {
		t.Fatalf("log-level error is not safe and actionable: %q", err)
	}
}

func TestNewLoggerHonoursConfiguredLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		level     slog.Level
		wantDebug bool
		wantInfo  bool
		wantWarn  bool
	}{
		{name: "debug", level: slog.LevelDebug, wantDebug: true, wantInfo: true, wantWarn: true},
		{name: "info", level: slog.LevelInfo, wantInfo: true, wantWarn: true},
		{name: "warn", level: slog.LevelWarn, wantWarn: true},
		{name: "error", level: slog.LevelError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := newLogger(&output, test.level)
			logger.Debug("debug-record")
			logger.Info("info-record")
			logger.Warn("warn-record")
			logger.Error("error-record")

			logs := output.String()
			for marker, want := range map[string]bool{
				"debug-record": test.wantDebug,
				"info-record":  test.wantInfo,
				"warn-record":  test.wantWarn,
				"error-record": true,
			} {
				if got := strings.Contains(logs, marker); got != want {
					t.Errorf("log contains %q = %v, want %v; output = %s", marker, got, want, logs)
				}
			}
		})
	}
}
