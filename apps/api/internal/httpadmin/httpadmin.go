package httpadmin

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"pandapages/api/internal/model"
)

type Config struct {
	AdminKey     string
	CookieSecure bool
	LogRequests  bool
}

type Store interface {
	AdminDraftUpsert(req model.AdminDraftUpsertRequest) (model.AdminDraftUpsertResponse, error)
	AdminPublish(slug string, versionID string) error
	AdminPreview(req model.AdminPreviewRequest) (model.AdminPreviewResponse, error)
}

const (
	cookieName       = "pp_unlocked"
	maxJSONBodyBytes = 1 << 20 // 1MB
)

func New(cfg Config, store Store) http.Handler {
	adminKey := strings.TrimSpace(cfg.AdminKey)
	// Fail-closed: if you forgot to set PP_ADMIN_KEY, admin endpoints should not work.
	if adminKey == "" {
		panic("PP_ADMIN_KEY is required for admin routes")
	}

	mux := http.NewServeMux()

	withAdmin := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			// 1) require unlock cookie
			c, err := r.Cookie(cookieName)
			if err != nil || c.Value != "1" {
				writeErr(w, http.StatusUnauthorized, "unauthorized", "unlock required")
				return
			}

			// 2) require admin key (constant time compare)
			got := strings.TrimSpace(r.Header.Get("X-PP-Admin-Key"))
			if !adminKeyOK(got, adminKey) {
				writeErr(w, http.StatusForbidden, "forbidden", "admin key required")
				return
			}

			next(w, r)
		}
	}

	// POST /api/v1/admin/preview
	mux.HandleFunc("/api/v1/admin/preview", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, []string{http.MethodPost})
			return
		}

		var body model.AdminPreviewRequest
		if err := decodeJSON(w, r, &body); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
			return
		}

		out, err := store.AdminPreview(body)
		if err != nil {
			// Treat as validation / render errors
			writeErr(w, http.StatusBadRequest, "preview_failed", err.Error())
			return
		}

		noStore(w)
		writeJSON(w, http.StatusOK, out)
	}))

	// POST /api/v1/admin/stories/draft
	mux.HandleFunc("/api/v1/admin/stories/draft", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, []string{http.MethodPost})
			return
		}

		var body model.AdminDraftUpsertRequest
		if err := decodeJSON(w, r, &body); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
			return
		}

		out, err := store.AdminDraftUpsert(body)
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "not_found", "story not found")
			return
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, "draft_failed", err.Error())
			return
		}

		noStore(w)
		writeJSON(w, http.StatusOK, out)
	}))

	// POST /api/v1/admin/stories/:slug/publish
	mux.HandleFunc("/api/v1/admin/stories/", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, []string{http.MethodPost})
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/stories/")
		path = strings.Trim(path, "/")
		parts := strings.Split(path, "/")

		if len(parts) != 2 || parts[0] == "" || parts[1] != "publish" {
			writeErr(w, http.StatusNotFound, "not_found", "unknown admin route")
			return
		}

		slug := parts[0]

		var body struct {
			VersionID string `json:"versionId"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
			return
		}
		if strings.TrimSpace(body.VersionID) == "" {
			writeErr(w, http.StatusBadRequest, "versionId", "versionId required")
			return
		}

		if err := store.AdminPublish(slug, body.VersionID); err != nil {
			// If your store returns sql.ErrNoRows for missing story/version, map to 404:
			if errors.Is(err, sql.ErrNoRows) {
				writeErr(w, http.StatusNotFound, "not_found", "story/version not found")
				return
			}
			writeErr(w, http.StatusBadRequest, "publish_failed", err.Error())
			return
		}

		noStore(w)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))

	var h http.Handler = mux
	h = withSecurityHeaders(h)
	h = withRecover(h)
	if cfg.LogRequests {
		h = withLog(h)
	}
	return h
}

/* ------------------------------ helpers ------------------------------ */

func adminKeyOK(got, want string) bool {
	if got == "" || want == "" {
		return false
	}
	if len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
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
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "interest-cohort=()")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}
