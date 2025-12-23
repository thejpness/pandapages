package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"pandapages/api/internal/model"
)

type Config struct {
	Passcode     string
	CookieSecure bool
	LogRequests  bool
}

type Store interface {
	// Phase A: derive an account id from today's unlock mechanism.
	EnsureDefaultAccount() (string, error)

	Library(accountID string) ([]model.StoryItem, error)
	StoryLatest(accountID, slug string) (model.StoryPayload, error)
	StorySegments(accountID, slug string) (model.StorySegmentsPayload, error)

	ProgressGet(accountID, slug string) (model.ProgressState, error)
	ProgressPut(accountID, slug string, version int, locator json.RawMessage, percent float64) error

	ContinueRecent(accountID string, limit int) ([]model.ContinueItem, error)

	SettingsGet(accountID string) (model.SettingsPayload, error)
	SettingsPut(accountID string, payload model.SettingsUpsert) (model.SettingsPayload, error)
}

const (
	cookieName        = "pp_unlocked"
	accountCookieName = "pp_aid"

	maxJSONBodyBytes   = 1 << 20 // 1MB
	defaultContinueLim = 3
	maxContinueLim     = 10

	// Cookie MaxAge is seconds. Keep this a const to avoid InvalidConstInit.
	sessionMaxAgeSeconds = 30 * 24 * 60 * 60 // 30 days
)

func New(cfg Config, store Store) http.Handler {
	pass := strings.TrimSpace(cfg.Passcode)
	if pass == "" {
		panic("PP_PASSCODE is required")
	}

	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Unlock -> cookies
	mux.HandleFunc("/api/v1/auth/unlock", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, []string{http.MethodPost})
			return
		}

		var body struct {
			Passcode string `json:"passcode"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
			return
		}

		body.Passcode = strings.TrimSpace(body.Passcode)
		if len(body.Passcode) != 6 || body.Passcode != pass {
			writeErr(w, http.StatusUnauthorized, "unauthorized", "invalid passcode")
			return
		}

		accountID, err := store.EnsureDefaultAccount()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "db", "account init failed")
			return
		}

		// IMPORTANT: set BOTH cookies so isUnlocked() passes.
		http.SetCookie(w, &http.Cookie{
			Name:     cookieName,
			Value:    "1",
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			Secure:   cfg.CookieSecure,
			MaxAge:   sessionMaxAgeSeconds,
		})
		http.SetCookie(w, &http.Cookie{
			Name:     accountCookieName,
			Value:    accountID,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			Secure:   cfg.CookieSecure,
			MaxAge:   sessionMaxAgeSeconds,
		})

		noStore(w)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	// Optional status endpoint (handy for UI)
	mux.HandleFunc("/api/v1/auth/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, []string{http.MethodGet})
			return
		}
		noStore(w)
		writeJSON(w, http.StatusOK, map[string]any{"unlocked": isUnlocked(r)})
	})

	type authedHandler func(w http.ResponseWriter, r *http.Request, accountID string)

	withUnlock := func(next authedHandler) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !isUnlocked(r) {
				writeErr(w, http.StatusUnauthorized, "unauthorized", "unlock required")
				return
			}
			accountID := mustAccountID(r)
			if accountID == "" {
				// Should never happen if isUnlocked() is correct, but keep it safe.
				writeErr(w, http.StatusUnauthorized, "unauthorized", "unlock required")
				return
			}
			next(w, r, accountID)
		}
	}

	// Library
	mux.HandleFunc("/api/v1/library", withUnlock(func(w http.ResponseWriter, r *http.Request, accountID string) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, []string{http.MethodGet})
			return
		}

		items, err := store.Library(accountID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "db", "library query failed")
			return
		}

		noStore(w)
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}))

	// Story (+ segments)
	mux.HandleFunc("/api/v1/story/", withUnlock(func(w http.ResponseWriter, r *http.Request, accountID string) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, []string{http.MethodGet})
			return
		}

		path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/story/"), "/")
		if path == "" {
			writeErr(w, http.StatusBadRequest, "slug", "missing slug")
			return
		}

		parts := strings.Split(path, "/")
		slug := parts[0]
		if slug == "" {
			writeErr(w, http.StatusBadRequest, "slug", "missing slug")
			return
		}

		// /api/v1/story/{slug}/segments
		if len(parts) == 2 && parts[1] == "segments" {
			p, err := store.StorySegments(accountID, slug)
			if errors.Is(err, sql.ErrNoRows) {
				writeErr(w, http.StatusNotFound, "not_found", "story not found")
				return
			}
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "db", "segments query failed")
				return
			}
			noStore(w)
			writeJSON(w, http.StatusOK, p)
			return
		}

		// /api/v1/story/{slug}
		if len(parts) != 1 {
			writeErr(w, http.StatusBadRequest, "path", "invalid story path")
			return
		}

		p, err := store.StoryLatest(accountID, slug)
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "not_found", "story not found")
			return
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "db", "story query failed")
			return
		}

		noStore(w)
		writeJSON(w, http.StatusOK, p)
	}))

	// Progress
	mux.HandleFunc("/api/v1/progress/", withUnlock(func(w http.ResponseWriter, r *http.Request, accountID string) {
		slug := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/progress/"), "/")
		if slug == "" {
			writeErr(w, http.StatusBadRequest, "slug", "missing slug")
			return
		}

		switch r.Method {
		case http.MethodGet:
			st, err := store.ProgressGet(accountID, slug)
			if errors.Is(err, sql.ErrNoRows) {
				noStore(w)
				writeJSON(w, http.StatusOK, model.ProgressState{Version: 0, Locator: nil, Percent: 0})
				return
			}
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "db", "progress query failed")
				return
			}

			noStore(w)
			writeJSON(w, http.StatusOK, st)
			return

		case http.MethodPut:
			var body struct {
				Version int             `json:"version"`
				Locator json.RawMessage `json:"locator"`
				Percent float64         `json:"percent"`
			}
			if err := decodeJSON(w, r, &body); err != nil {
				writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
				return
			}
			if body.Version <= 0 {
				writeErr(w, http.StatusBadRequest, "version", "version must be > 0")
				return
			}
			if body.Percent < 0 {
				body.Percent = 0
			}
			if body.Percent > 1 {
				body.Percent = 1
			}

			err := store.ProgressPut(accountID, slug, body.Version, body.Locator, body.Percent)
			if errors.Is(err, sql.ErrNoRows) {
				writeErr(w, http.StatusNotFound, "not_found", "story/version not found")
				return
			}
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "db", "progress update failed")
				return
			}

			noStore(w)
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return

		default:
			methodNotAllowed(w, []string{http.MethodGet, http.MethodPut})
			return
		}
	}))

	// Continue (top N recent)
	mux.HandleFunc("/api/v1/continue", withUnlock(func(w http.ResponseWriter, r *http.Request, accountID string) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, []string{http.MethodGet})
			return
		}

		limit := defaultContinueLim
		if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				limit = n
			}
		}
		if limit < 1 {
			limit = 1
		}
		if limit > maxContinueLim {
			limit = maxContinueLim
		}

		items, err := store.ContinueRecent(accountID, limit)
		if err != nil {
			// For v1: treat "no rows" as empty list; anything else is 500.
			if errors.Is(err, sql.ErrNoRows) {
				items = []model.ContinueItem{}
			} else {
				writeErr(w, http.StatusInternalServerError, "db", "continue query failed")
				return
			}
		}

		noStore(w)
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}))

	// Settings / Journey
	mux.HandleFunc("/api/v1/settings", withUnlock(func(w http.ResponseWriter, r *http.Request, accountID string) {
		switch r.Method {
		case http.MethodGet:
			out, err := store.SettingsGet(accountID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					out = model.SettingsPayload{}
				} else {
					writeErr(w, http.StatusInternalServerError, "db", "settings query failed")
					return
				}
			}
			noStore(w)
			writeJSON(w, http.StatusOK, out)
			return

		case http.MethodPut:
			var body model.SettingsUpsert
			if err := decodeJSON(w, r, &body); err != nil {
				writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
				return
			}
			out, err := store.SettingsPut(accountID, body)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "db", "settings update failed")
				return
			}
			noStore(w)
			writeJSON(w, http.StatusOK, out)
			return

		default:
			methodNotAllowed(w, []string{http.MethodGet, http.MethodPut})
			return
		}
	}))

	// middleware wrapping
	var h http.Handler = mux
	h = withSecurityHeaders(h)
	h = withRecover(h)
	if cfg.LogRequests {
		h = withLog(h)
	}

	return h
}

/* -------------------- helpers & middleware -------------------- */

func mustAccountID(r *http.Request) string {
	c, err := r.Cookie(accountCookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(c.Value)
}

func isUnlocked(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value != "1" {
		return false
	}
	a, err := r.Cookie(accountCookieName)
	return err == nil && strings.TrimSpace(a.Value) != ""
}

func noStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
}

func methodNotAllowed(w http.ResponseWriter, allow []string) {
	w.Header().Set("Allow", strings.Join(allow, ", "))
	writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return err
	}

	// Must be EOF after the first object (prevents trailing junk)
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("unexpected extra json")
	}
	return nil
}

func writeErr(w http.ResponseWriter, status int, code string, msg string) {
	noStore(w)
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": msg,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic", "path", r.URL.Path, "err", rec)
				writeErr(w, http.StatusInternalServerError, "panic", "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func withLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Debug("http", "method", r.Method, "path", r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// sane defaults
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "interest-cohort=()")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}
