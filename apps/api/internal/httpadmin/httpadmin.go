package httpadmin

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"pandapages/api/internal/httpauth"
	"pandapages/api/internal/model"
)

type Store interface {
	AccountExists(accountID string) (bool, error)

	AdminDraftUpsert(accountID string, req model.AdminDraftUpsertRequest) (model.AdminDraftUpsertResponse, error)
	AdminPublishStory(accountID string, slug string, versionID string) (model.AdminStoryStatusResponse, error)
	AdminUnpublish(accountID string, slug string) (model.AdminStoryStatusResponse, error)
	AdminPreview(req model.AdminPreviewRequest) (model.AdminPreviewResponse, error)

	AdminListStories(accountID string) (model.AdminStoriesListResponse, error)
	AdminGetStory(accountID string, slug string) (model.AdminStoryDetailResponse, error)
	AdminGetVersionSource(accountID string, slug string, versionID string) (model.AdminVersionSourceResponse, error)
}

const (
	// Admin endpoints need a bigger body limit for large Gutenberg books.
	// Keep public APIs small; only admin gets this.
	maxJSONBodyBytes = 20 << 20 // 20MB
)

var adminVersionIDPattern = regexp.MustCompile("(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$")

type ctxKey string

const ctxAccountID ctxKey = "pp_account_id"

func accountIDFromCtx(r *http.Request) string {
	v, _ := r.Context().Value(ctxAccountID).(string)
	return v
}

func New(cfg Config, store Store) http.Handler {
	adminKey := strings.TrimSpace(cfg.AdminKey)
	if adminKey == "" {
		panic("PP_ADMIN_KEY is required for admin routes")
	}
	authenticator := httpauth.New(cfg.Sessions, store)

	mux := http.NewServeMux()

	withAdmin := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			// 1) require the shared signed session and its existing account.
			aid, err := authenticator.Authenticate(r)
			if errors.Is(err, httpauth.ErrInvalidSession) {
				cfg.Sessions.Clear(w)
				writeErr(w, http.StatusUnauthorized, "unauthorized", "unlock required")
				return
			}
			if err != nil {
				writeErr(w, http.StatusServiceUnavailable, "session_unavailable", "session validation unavailable")
				return
			}

			// 2) retain the proxy-injected admin key boundary.
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
			writeDecodeError(w, err)
			return
		}

		out, err := store.AdminPreview(body)
		if err != nil {
			var validationErr *model.AdminValidationError
			if errors.As(err, &validationErr) {
				writeIssues(w, http.StatusBadRequest, "preview_invalid", "Story content is invalid", validationErr.Issues)
				return
			}
			slog.Error("admin story preview failed")
			writeErr(w, http.StatusInternalServerError, "preview_failed", "story preview failed")
			return
		}

		noStore(w)
		writeJSON(w, http.StatusOK, out)
	}))

	// POST /api/v1/admin/stories/draft
	mux.HandleFunc("POST /api/v1/admin/stories/draft", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		var body model.AdminDraftUpsertRequest
		if err := decodeJSON(w, r, &body); err != nil {
			writeDecodeError(w, err)
			return
		}

		aid := accountIDFromCtx(r)
		out, err := store.AdminDraftUpsert(aid, body)
		if err != nil {
			var validationErr *model.AdminValidationError
			if errors.As(err, &validationErr) {
				writeIssues(w, http.StatusBadRequest, "draft_invalid", "Story content is invalid", validationErr.Issues)
				return
			}
			if errors.Is(err, model.ErrAdminVersionRepairRequired) {
				writeErr(w, http.StatusConflict, "draft_repair_required", "stored story version requires repair")
				return
			}
			slog.Error("admin story draft failed")
			writeErr(w, http.StatusInternalServerError, "draft_failed", "story draft could not be saved")
			return
		}

		noStore(w)
		writeJSON(w, http.StatusOK, out)
	}))

	mux.HandleFunc("GET /api/v1/admin/stories", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		aid := accountIDFromCtx(r)

		out, err := store.AdminListStories(aid)
		if err != nil {
			slog.Error("admin story catalogue failed")
			writeErr(w, http.StatusInternalServerError, "list_failed", "story catalogue unavailable")
			return
		}

		noStore(w)
		writeJSON(w, http.StatusOK, out)
	}))

	// GET /api/v1/admin/stories/{slug}
	mux.HandleFunc("GET /api/v1/admin/stories/{slug}", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		slug := strings.TrimSpace(r.PathValue("slug"))
		out, err := store.AdminGetStory(accountIDFromCtx(r), slug)
		if err != nil {
			if errors.Is(err, model.ErrAdminStoryNotFound) {
				writeErr(w, http.StatusNotFound, "story_not_found", "story was not found")
				return
			}
			slog.Error("admin story detail failed")
			writeErr(w, http.StatusInternalServerError, "story_failed", "story details unavailable")
			return
		}
		noStore(w)
		writeJSON(w, http.StatusOK, out)
	}))

	// GET /api/v1/admin/stories/{slug}/versions/{versionId}
	mux.HandleFunc("GET /api/v1/admin/stories/{slug}/versions/{versionId}", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		slug := strings.TrimSpace(r.PathValue("slug"))
		versionID := strings.TrimSpace(r.PathValue("versionId"))
		out, err := store.AdminGetVersionSource(accountIDFromCtx(r), slug, versionID)
		if err != nil {
			switch {
			case errors.Is(err, model.ErrAdminStoryNotFound):
				writeErr(w, http.StatusNotFound, "version_not_found", "story version was not found")
			case errors.Is(err, model.ErrAdminVersionRepairRequired):
				writeErr(w, http.StatusConflict, "version_repair_required", "story version requires repair")
			default:
				slog.Error("admin story version source failed")
				writeErr(w, http.StatusInternalServerError, "version_failed", "story version unavailable")
			}
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
			writeDecodeError(w, err)
			return
		}

		aid := accountIDFromCtx(r)
		body.VersionID = strings.TrimSpace(body.VersionID)
		if !adminVersionIDPattern.MatchString(body.VersionID) {
			writeErr(w, http.StatusBadRequest, "publish_invalid", "versionId must be a valid identifier")
			return
		}
		out, err := store.AdminPublishStory(aid, slug, body.VersionID)
		if err != nil {
			if errors.Is(err, model.ErrAdminPublishNotFound) {
				writeErr(w, http.StatusNotFound, "publish_not_found", "story version was not found")
				return
			}
			if errors.Is(err, model.ErrAdminPublishInvalid) {
				writeErr(w, http.StatusConflict, "publish_repair_required", "story version is unavailable or unreadable")
				return
			}
			// Driver errors may contain connection or query detail. Keep both the
			// browser response and application logs on a fixed safe boundary.
			slog.Error("admin story publication failed")
			writeErr(w, http.StatusInternalServerError, "publish_failed", "story publication failed")
			return
		}

		noStore(w)
		writeJSON(w, http.StatusOK, out)
	}))

	// POST /api/v1/admin/stories/{slug}/unpublish
	mux.HandleFunc("POST /api/v1/admin/stories/{slug}/unpublish", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		slug := strings.TrimSpace(r.PathValue("slug"))
		out, err := store.AdminUnpublish(accountIDFromCtx(r), slug)
		if err != nil {
			if errors.Is(err, model.ErrAdminStoryNotFound) {
				writeErr(w, http.StatusNotFound, "unpublish_not_found", "story was not found")
				return
			}
			slog.Error("admin story unpublish failed")
			writeErr(w, http.StatusInternalServerError, "unpublish_failed", "story could not be unpublished")
			return
		}
		noStore(w)
		writeJSON(w, http.StatusOK, out)
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

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if !utf8.Valid(raw) {
		return errors.New("request body is not valid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return err
	}
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
	writeErr(w, http.StatusBadRequest, "bad_json", "request body must be valid JSON")
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

func writeIssues(w http.ResponseWriter, status int, code string, msg string, issues []model.AdminValidationIssue) {
	noStore(w)
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": msg,
			"issues":  issues,
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
