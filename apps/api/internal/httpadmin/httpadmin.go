package httpadmin

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"pandapages/api/internal/model"
)

type Store interface {
	AdminDraftUpsert(accountID string, req model.AdminDraftUpsertRequest) (model.AdminDraftUpsertResponse, error)
	AdminPublish(accountID string, slug string, versionID string) error
	AdminPreview(req model.AdminPreviewRequest) (model.AdminPreviewResponse, error)

	AdminListStories(accountID string) (model.AdminStoriesListResponse, error)
}

const (
	cookieName        = "pp_unlocked"
	accountCookieName = "pp_aid"

	// Admin endpoints need a bigger body limit for large Gutenberg books.
	// Keep public APIs small; only admin gets this.
	maxJSONBodyBytes = 20 << 20 // 20MB
)

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type ctxKey string

const ctxAccountID ctxKey = "pp_account_id"

func accountIDFromCookie(r *http.Request) (string, error) {
	c, err := r.Cookie(accountCookieName)
	if err != nil {
		return "", errors.New("account required")
	}
	v := strings.TrimSpace(c.Value)
	if v == "" || !uuidRe.MatchString(v) {
		return "", errors.New("invalid account")
	}
	return v, nil
}

func accountIDFromCtx(r *http.Request) string {
	v, _ := r.Context().Value(ctxAccountID).(string)
	return v
}

func New(cfg Config, store Store) http.Handler {
	adminKey := strings.TrimSpace(cfg.AdminKey)
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

			// 2) require account cookie (bind admin actions to an account)
			aid, err := accountIDFromCookie(r)
			if err != nil {
				writeErr(w, http.StatusUnauthorized, "unauthorized", err.Error())
				return
			}

			// 3) require admin key (constant time compare)
			got := strings.TrimSpace(r.Header.Get("X-PP-Admin-Key"))
			if !adminKeyOK(got, adminKey) {
				writeErr(w, http.StatusForbidden, "forbidden", "admin key required")
				return
			}

			ctx := context.WithValue(r.Context(), ctxAccountID, aid)
			next(w, r.WithContext(ctx))
		}
	}

	// POST /api/v1/admin/preview
	mux.HandleFunc("POST /api/v1/admin/preview", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		var body model.AdminPreviewRequest
		if err := decodeJSON(w, r, &body); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
			return
		}

		out, err := store.AdminPreview(body)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "preview_failed", err.Error())
			return
		}

		noStore(w)
		writeJSON(w, http.StatusOK, out)
	}))

	// POST /api/v1/admin/stories/draft
	mux.HandleFunc("POST /api/v1/admin/stories/draft", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		var body model.AdminDraftUpsertRequest
		if err := decodeJSON(w, r, &body); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
			return
		}

		aid := accountIDFromCtx(r)
		out, err := store.AdminDraftUpsert(aid, body)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "draft_failed", err.Error())
			return
		}

		noStore(w)
		writeJSON(w, http.StatusOK, out)
	}))

	mux.HandleFunc("GET /api/v1/admin/stories", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		aid := accountIDFromCtx(r)

		out, err := store.AdminListStories(aid)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "list_failed", err.Error())
			return
		}

		noStore(w)
		writeJSON(w, http.StatusOK, out)
	}))

	// POST /api/v1/admin/stories/{slug}/publish
	mux.HandleFunc("POST /api/v1/admin/stories/{slug}/publish", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		slug := strings.TrimSpace(r.PathValue("slug"))
		if slug == "" {
			writeErr(w, http.StatusBadRequest, "bad_request", "slug required")
			return
		}

		var body struct {
			VersionID string `json:"versionId"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
			return
		}

		aid := accountIDFromCtx(r)
		if err := store.AdminPublish(aid, slug, strings.TrimSpace(body.VersionID)); err != nil {
			writeErr(w, http.StatusBadRequest, "publish_failed", err.Error())
			return
		}

		noStore(w)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))

	// Actually apply middleware stack (you already wrote these helpers)
	var h http.Handler = mux
	h = withSecurityHeaders(h)
	h = withRecover(h)
	h = withLog(h)

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
