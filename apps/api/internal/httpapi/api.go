package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"pandapages/api/internal/httpbearer"
	"pandapages/api/internal/httpprofile"
	"pandapages/api/internal/model"
	"pandapages/api/internal/readercontract"
	"pandapages/api/internal/readiness"
)

type Config struct {
	BearerAuthenticator *httpbearer.Authenticator
	ProfileResolver     *httpprofile.Resolver
}

type Store interface {
	CheckReadiness(context.Context) error

	Library(accountID string) (model.LibraryReadModel, error)
	ReaderStory(accountID, slug string) (model.ReaderStory, error)

	ProgressGet(accountID, profileID, slug string) (model.ProgressResponse, error)
	ProgressPut(accountID, profileID, slug string, version int, locator readercontract.Locator, percent float64) error

	ContinueRecent(accountID, profileID string, limit int) ([]model.ContinueItem, error)
	Profiles(accountID string) ([]model.ReaderProfile, error)
	CreateProfile(accountID, name string) (model.ReaderProfile, error)
	UpdateProfile(accountID, profileID, name string) (model.ReaderProfile, error)
	DeleteProfile(accountID, profileID string) error

	SettingsGet(accountID string) (model.SettingsPayload, error)
	SettingsPut(accountID string, payload model.SettingsUpsert) (model.SettingsPayload, error)
}

const (
	maxJSONBodyBytes    = 1 << 20 // 1MB
	defaultContinueLim  = 3
	maxContinueLim      = 10
	readinessTimeout    = 2 * time.Second
	maxProfileNameRunes = 80
)

func New(cfg Config, store Store) http.Handler {
	if cfg.BearerAuthenticator == nil {
		panic("bearer account authenticator is required")
	}
	if cfg.ProfileResolver == nil {
		panic("profile resolver is required")
	}

	mux := http.NewServeMux()

	// Liveness is deliberately dependency-free: reaching this handler proves
	// only that the Go process and HTTP listener can answer.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		noStore(w)
		if r.Method != http.MethodGet {
			methodNotAllowed(w, []string{http.MethodGet})
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Readiness uses one strict deadline for both connectivity and Goose schema
	// state. It never applies migrations and never exposes the underlying error.
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		noStore(w)
		if r.Method != http.MethodGet {
			methodNotAllowed(w, []string{http.MethodGet})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
		defer cancel()

		err := store.CheckReadiness(ctx)
		switch {
		case err == nil:
			writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
		case errors.Is(err, readiness.ErrSchemaNotReady):
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "reason": "schema_not_ready"})
		default:
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "reason": "database_unavailable"})
		}
	})

	type authedHandler func(w http.ResponseWriter, r *http.Request, accountID string)

	withBearerAccount := func(next authedHandler) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			account, ok := cfg.BearerAuthenticator.RequireAccount(w, r)
			if !ok {
				return
			}
			next(w, r, account.AccountID)
		}
	}

	type profileHandler func(w http.ResponseWriter, r *http.Request, profile httpprofile.Context)

	withBearerProfile := func(next profileHandler) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			account, ok := cfg.BearerAuthenticator.RequireAccount(w, r)
			if !ok {
				return
			}
			profile, ok := cfg.ProfileResolver.RequireProfile(w, r, account)
			if !ok {
				return
			}
			next(w, r, profile)
		}
	}

	// Library
	mux.HandleFunc("/api/v1/library", withBearerAccount(func(w http.ResponseWriter, r *http.Request, accountID string) {
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
	mux.HandleFunc("/api/v1/reader/", withBearerAccount(func(w http.ResponseWriter, r *http.Request, accountID string) {
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
	mux.HandleFunc("/api/v1/progress/", withBearerProfile(func(w http.ResponseWriter, r *http.Request, profile httpprofile.Context) {
		slug := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/progress/"), "/")
		if slug == "" {
			writeErr(w, http.StatusBadRequest, "slug", "missing slug")
			return
		}

		switch r.Method {
		case http.MethodGet:
			st, err := store.ProgressGet(profile.AccountID, profile.ProfileID, slug)
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

			err := store.ProgressPut(profile.AccountID, profile.ProfileID, slug, body.Version, *body.Locator, *body.Percent)
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
	mux.HandleFunc("/api/v1/continue", withBearerProfile(func(w http.ResponseWriter, r *http.Request, profile httpprofile.Context) {
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

		items, err := store.ContinueRecent(profile.AccountID, profile.ProfileID, limit)
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

	// Profiles are managed only within an already-authorized explicit account.
	// A reader profile is never inferred or provisioned outside an explicit
	// create request.
	mux.HandleFunc("/api/v1/profiles", withBearerAccount(func(w http.ResponseWriter, r *http.Request, accountID string) {
		switch r.Method {
		case http.MethodGet:
			profiles, err := store.Profiles(accountID)
			if err != nil {
				writeErr(w, http.StatusServiceUnavailable, "profile_unavailable", "profiles are temporarily unavailable")
				return
			}
			noStore(w)
			writeJSON(w, http.StatusOK, map[string]any{"profiles": profiles})
		case http.MethodPost:
			name, ok := decodeProfileName(w, r)
			if !ok {
				return
			}
			profile, err := store.CreateProfile(accountID, name)
			if errors.Is(err, model.ErrProfileNameConflict) {
				writeErr(w, http.StatusBadRequest, "invalid_profile_name", "a profile with that name already exists")
				return
			}
			if err != nil {
				writeErr(w, http.StatusServiceUnavailable, "profile_unavailable", "profiles are temporarily unavailable")
				return
			}
			noStore(w)
			writeJSON(w, http.StatusCreated, profile)
		default:
			methodNotAllowed(w, []string{http.MethodGet, http.MethodPost})
		}
	}))

	mux.HandleFunc("/api/v1/profiles/", withBearerAccount(func(w http.ResponseWriter, r *http.Request, accountID string) {
		profileID, ok := profileIDFromPath(r.URL.Path)
		if !ok {
			writeErr(w, http.StatusBadRequest, "invalid_profile", "profile id is invalid")
			return
		}

		switch r.Method {
		case http.MethodPatch:
			name, ok := decodeProfileName(w, r)
			if !ok {
				return
			}
			profile, err := store.UpdateProfile(accountID, profileID, name)
			if errors.Is(err, sql.ErrNoRows) {
				writeErr(w, http.StatusForbidden, "profile_forbidden", "profile is not available in this account")
				return
			}
			if errors.Is(err, model.ErrProfileNameConflict) {
				writeErr(w, http.StatusBadRequest, "invalid_profile_name", "a profile with that name already exists")
				return
			}
			if err != nil {
				writeErr(w, http.StatusServiceUnavailable, "profile_unavailable", "profiles are temporarily unavailable")
				return
			}
			noStore(w)
			writeJSON(w, http.StatusOK, profile)
		case http.MethodDelete:
			err := store.DeleteProfile(accountID, profileID)
			if errors.Is(err, sql.ErrNoRows) {
				writeErr(w, http.StatusForbidden, "profile_forbidden", "profile is not available in this account")
				return
			}
			if err != nil {
				writeErr(w, http.StatusServiceUnavailable, "profile_unavailable", "profiles are temporarily unavailable")
				return
			}
			noStore(w)
			w.WriteHeader(http.StatusNoContent)
		default:
			methodNotAllowed(w, []string{http.MethodPatch, http.MethodDelete})
		}
	}))

	// Settings / Journey
	mux.HandleFunc("/api/v1/settings", withBearerAccount(func(w http.ResponseWriter, r *http.Request, accountID string) {
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
	h := withSecurityHeaders(mux)

	return h
}

/* -------------------- helpers & middleware -------------------- */

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

func decodeProfileName(w http.ResponseWriter, r *http.Request) (string, bool) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeDecodeError(w, err)
		return "", false
	}
	name := strings.TrimSpace(body.Name)
	if name == "" || utf8.RuneCountInString(name) > maxProfileNameRunes {
		writeErr(w, http.StatusBadRequest, "invalid_profile_name", "profile name must be between 1 and 80 characters")
		return "", false
	}
	if strings.IndexFunc(name, unicode.IsControl) >= 0 {
		writeErr(w, http.StatusBadRequest, "invalid_profile_name", "profile name contains unsupported characters")
		return "", false
	}
	return name, true
}

func profileIDFromPath(path string) (string, bool) {
	raw := strings.TrimPrefix(path, "/api/v1/profiles/")
	if raw == "" || strings.Contains(raw, "/") {
		return "", false
	}
	return httpbearer.CanonicalUUID(raw)
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
