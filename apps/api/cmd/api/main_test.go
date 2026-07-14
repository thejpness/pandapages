package main

import (
	"net/http"
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
	if !cfg.logRequests {
		t.Error("logRequests = false, want true")
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
