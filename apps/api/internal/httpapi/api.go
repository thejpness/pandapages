package httpapi

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"pandapages/api/internal/httpauth"
	"pandapages/api/internal/model"
	"pandapages/api/internal/readercontract"
	"pandapages/api/internal/session"
)

type Config struct {
	Passcode    string
	LogRequests bool
	Sessions    *session.Manager
}

type Store interface {
	// Phase A: derive an account id from today's unlock mechanism.
	EnsureDefaultAccount() (string, error)
	AccountExists(accountID string) (bool, error)

	Library(accountID string) (model.LibraryReadModel, error)
	ReaderStory(accountID, slug string) (model.ReaderStory, error)

	ProgressGet(accountID, slug string) (model.ProgressResponse, error)
	ProgressPut(accountID, slug string, version int, locator readercontract.Locator, percent float64) error

	ContinueRecent(accountID string, limit int) ([]model.ContinueItem, error)

	SettingsGet(accountID string) (model.SettingsPayload, error)
	SettingsPut(accountID string, payload model.SettingsUpsert) (model.SettingsPayload, error)
}

const (
	maxJSONBodyBytes   = 1 << 20 // 1MB
	defaultContinueLim = 3
	maxContinueLim     = 10
)

func New(cfg Config, store Store) http.Handler {
	pass := cfg.Passcode
	if !validPasscode(pass) {
		panic("a six-digit ASCII passcode is required")
	}
	authenticator := httpauth.New(cfg.Sessions, store)

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
			writeDecodeError(w, err)
			return
		}

		if len(body.Passcode) != len(pass) || subtle.ConstantTimeCompare([]byte(body.Passcode), []byte(pass)) != 1 {
			writeErr(w, http.StatusUnauthorized, "unauthorized", "invalid passcode")
			return
		}

		accountID, err := store.EnsureDefaultAccount()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "db", "account init failed")
			return
		}

		if err := cfg.Sessions.Set(w, accountID); err != nil {
			writeErr(w, http.StatusInternalServerError, "session", "session creation failed")
			return
		}

		noStore(w)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	// Status distinguishes a definitively invalid session from unavailable
	// account storage. The frontend must not turn the latter into signed-out.
	mux.HandleFunc("/api/v1/auth/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, []string{http.MethodGet})
			return
		}
		_, err := authenticator.Authenticate(r)
		if errors.Is(err, httpauth.ErrInvalidSession) {
			cfg.Sessions.Clear(w)
			noStore(w)
			writeJSON(w, http.StatusOK, map[string]any{"unlocked": false})
			return
		}
		if err != nil {
			writeErr(w, http.StatusServiceUnavailable, "session_unavailable", "session validation unavailable")
			return
		}

		noStore(w)
		writeJSON(w, http.StatusOK, map[string]any{"unlocked": true})
	})

	// Logout is deliberately browser-local and does not need a valid session or
	// a working database in order to expire authentication cookies.
	mux.HandleFunc("/api/v1/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, []string{http.MethodPost})
			return
		}
		cfg.Sessions.Clear(w)
		noStore(w)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	type authedHandler func(w http.ResponseWriter, r *http.Request, accountID string)

	withUnlock := func(next authedHandler) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			accountID, err := authenticator.Authenticate(r)
			if errors.Is(err, httpauth.ErrInvalidSession) {
				cfg.Sessions.Clear(w)
				writeErr(w, http.StatusUnauthorized, "unauthorized", "unlock required")
				return
			}
			if err != nil {
				writeErr(w, http.StatusServiceUnavailable, "session_unavailable", "session validation unavailable")
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

		library, err := store.Library(accountID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "db", "library query failed")
			return
		}

		noStore(w)
		writeJSON(w, http.StatusOK, library)
	}))

	// Reader 2: one coherent published-version payload.
	mux.HandleFunc("/api/v1/reader/", withUnlock(func(w http.ResponseWriter, r *http.Request, accountID string) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, []string{http.MethodGet})
			return
		}

		slug := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/reader/"), "/")
		if slug == "" {
			writeErr(w, http.StatusBadRequest, "slug", "missing slug")
			return
		}
		if strings.Contains(slug, "/") {
			writeErr(w, http.StatusNotFound, "not_found", "reader story not found")
			return
		}

		p, err := store.ReaderStory(accountID, slug)
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "not_found", "story not found")
			return
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "db", "reader query failed")
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
				writeErr(w, http.StatusNotFound, "not_found", "story not found")
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
				Version int                     `json:"version"`
				Locator *readercontract.Locator `json:"locator"`
				Percent *float64                `json:"percent"`
			}
			if err := decodeJSON(w, r, &body); err != nil {
				writeDecodeError(w, err)
				return
			}
			if body.Version <= 0 {
				writeErr(w, http.StatusBadRequest, "version", "version must be > 0")
				return
			}
			if body.Locator == nil {
				writeErr(w, http.StatusBadRequest, "locator_invalid", "locator is required")
				return
			}
			if err := body.Locator.Validate(); err != nil {
				writeErr(w, http.StatusBadRequest, "locator_invalid", "invalid Reader locator")
				return
			}
			if body.Percent == nil {
				writeErr(w, http.StatusBadRequest, "percent", "percent is required")
				return
			}
			if *body.Percent < 0 || *body.Percent > 1 {
				writeErr(w, http.StatusBadRequest, "percent", "percent must be between 0 and 1")
				return
			}

			err := store.ProgressPut(accountID, slug, body.Version, *body.Locator, *body.Percent)
			if errors.Is(err, sql.ErrNoRows) {
				writeErr(w, http.StatusNotFound, "not_found", "story/version not found")
				return
			}
			if errors.Is(err, readercontract.ErrLocatorMismatch) {
				writeErr(w, http.StatusBadRequest, "locator_mismatch", "locator does not match the selected story version")
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
				writeDecodeError(w, err)
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

func writeDecodeError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeErr(w, http.StatusRequestEntityTooLarge, "body_too_large", "request body too large")
		return
	}
	writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
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
