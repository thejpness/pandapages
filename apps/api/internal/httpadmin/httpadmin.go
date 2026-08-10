package httpadmin

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"pandapages/api/internal/appidentity"
	"pandapages/api/internal/model"
	"pandapages/api/internal/sourceprovider"
)

type Store interface {
	AdminDraftUpsert(req model.AdminDraftUpsertRequest) (model.AdminDraftUpsertResponse, error)
	AdminEditionBundleUpsert(req model.AdminEditionBundleUpsertRequest) (model.AdminEditionBundleUpsertResponse, error)
	AdminCreateRelease(slug string, req model.AdminCreateReleaseRequest) (model.AdminCreateReleaseResponse, error)
	AdminUnpublish(slug string) (model.AdminStoryStatusResponse, error)
	AdminPreview(req model.AdminPreviewRequest) (model.AdminPreviewResponse, error)

	AdminListStories() (model.AdminStoriesListResponse, error)
	AdminGetStory(slug string) (model.AdminStoryDetailResponse, error)
	AdminGetVersionSource(slug string, versionID string) (model.AdminVersionSourceResponse, error)
	AdminGetEditionVersionSource(slug string, editionKey model.AdminStoryEditionKey, versionID string) (model.AdminVersionSourceResponse, error)

	AdminSourceUpsert(slug string, req model.AdminSourceUpsertRequest) (model.AdminSourceUpsertResponse, error)
	AdminGetSource(slug string) (model.AdminSourceDetailResponse, error)
	AdminGetSourceVersion(slug string, versionID string) (model.AdminSourceVersionResponse, error)
}

const (
	// Admin endpoints need a bigger body limit for large Gutenberg books.
	// Keep public APIs small; only admin gets this.
	maxJSONBodyBytes           = 20 << 20 // 20MB
	sourceProviderMaximumLimit = 20
)

func New(cfg Config, store Store) http.Handler {
	adminKey := strings.TrimSpace(cfg.AdminKey)
	if adminKey == "" {
		panic("PP_ADMIN_KEY is required for admin routes")
	}
	if cfg.BearerAuthenticator == nil {
		panic("bearer account authenticator is required")
	}

	discovery := cfg.SourceDiscovery
	mux := http.NewServeMux()

	withAdmin := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			account, ok := cfg.BearerAuthenticator.RequireAccount(w, r)
			if !ok {
				return
			}
			if account.Role != appidentity.RoleOwner {
				writeErr(w, http.StatusForbidden, "forbidden", "admin authorization required")
				return
			}

			// Retain the proxy-injected admin key as an additional capability boundary.
			got := strings.TrimSpace(r.Header.Get("X-PP-Admin-Key"))
			if !adminKeyOK(got, adminKey) {
				writeErr(w, http.StatusForbidden, "forbidden", "admin authorization required")
				return
			}

			next(w, r)
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

		out, err := store.AdminDraftUpsert(body)
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

	// POST /api/v1/admin/stories/editions/ingest
	mux.HandleFunc("POST /api/v1/admin/stories/editions/ingest", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		var body model.AdminEditionBundleUpsertRequest
		if err := decodeJSON(w, r, &body); err != nil {
			writeDecodeError(w, err)
			return
		}

		out, err := store.AdminEditionBundleUpsert(body)
		if err != nil {
			var validationErr *model.AdminValidationError
			switch {
			case errors.As(err, &validationErr):
				writeIssues(w, http.StatusBadRequest, "edition_ingest_invalid", "Five-edition bundle is invalid", validationErr.Issues)
			case errors.Is(err, model.ErrAdminVersionRepairRequired):
				writeErr(w, http.StatusConflict, "edition_ingest_repair_required", "a stored edition version requires repair")
			default:
				slog.Error("admin five-edition ingest failed")
				writeErr(w, http.StatusInternalServerError, "edition_ingest_failed", "five-edition bundle could not be saved")
			}
			return
		}

		noStore(w)
		writeJSON(w, http.StatusOK, out)
	}))

	mux.HandleFunc("GET /api/v1/admin/source-providers/{provider}/search", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		if discovery == nil {
			writeErr(w, http.StatusServiceUnavailable, "source_provider_unavailable", "source provider is unavailable")
			return
		}
		limit, err := sourceProviderLimit(r.URL.Query().Get("limit"))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "source_provider_query_invalid", "source provider search is invalid")
			return
		}
		out, err := discovery.Search(r.Context(), sourceprovider.ID(strings.TrimSpace(r.PathValue("provider"))), r.URL.Query().Get("q"), limit)
		if err != nil {
			writeSourceProviderError(w, err, false)
			return
		}
		noStore(w)
		writeJSON(w, http.StatusOK, out)
	}))

	mux.HandleFunc("GET /api/v1/admin/source-providers/{provider}/works/{externalID}", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		if discovery == nil {
			writeErr(w, http.StatusServiceUnavailable, "source_provider_unavailable", "source provider is unavailable")
			return
		}
		out, err := discovery.GetWork(r.Context(), sourceprovider.ID(strings.TrimSpace(r.PathValue("provider"))), strings.TrimSpace(r.PathValue("externalID")))
		if err != nil {
			writeSourceProviderError(w, err, true)
			return
		}
		noStore(w)
		writeJSON(w, http.StatusOK, out)
	}))

	mux.HandleFunc("GET /api/v1/admin/stories", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		out, err := store.AdminListStories()
		if err != nil {
			slog.Error("admin story catalogue failed")
			writeErr(w, http.StatusInternalServerError, "list_failed", "story catalogue unavailable")
			return
		}

		noStore(w)
		writeJSON(w, http.StatusOK, out)
	}))

	// PUT /api/v1/admin/stories/{slug}/source
	mux.HandleFunc("PUT /api/v1/admin/stories/{slug}/source", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		slug := strings.TrimSpace(r.PathValue("slug"))
		var body model.AdminSourceUpsertRequest
		if err := decodeJSON(w, r, &body); err != nil {
			writeDecodeError(w, err)
			return
		}
		out, err := store.AdminSourceUpsert(slug, body)
		if err != nil {
			var validationErr *model.AdminValidationError
			switch {
			case errors.As(err, &validationErr):
				writeIssues(w, http.StatusBadRequest, "source_invalid", "Canonical source is invalid", validationErr.Issues)
			case errors.Is(err, model.ErrAdminSourceRepairRequired):
				writeErr(w, http.StatusConflict, "source_repair_required", "canonical source requires repair")
			default:
				slog.Error("admin canonical source save failed")
				writeErr(w, http.StatusInternalServerError, "source_failed", "canonical source could not be saved")
			}
			return
		}
		noStore(w)
		writeJSON(w, http.StatusOK, out)
	}))

	// GET /api/v1/admin/stories/{slug}/source
	mux.HandleFunc("GET /api/v1/admin/stories/{slug}/source", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		slug := strings.TrimSpace(r.PathValue("slug"))
		out, err := store.AdminGetSource(slug)
		if err != nil {
			if errors.Is(err, model.ErrAdminSourceNotFound) {
				writeErr(w, http.StatusNotFound, "source_not_found", "canonical source was not found")
				return
			}
			slog.Error("admin canonical source detail failed")
			writeErr(w, http.StatusInternalServerError, "source_failed", "canonical source unavailable")
			return
		}
		noStore(w)
		writeJSON(w, http.StatusOK, out)
	}))

	// GET /api/v1/admin/stories/{slug}/source/versions/{versionId}
	mux.HandleFunc("GET /api/v1/admin/stories/{slug}/source/versions/{versionId}", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		slug := strings.TrimSpace(r.PathValue("slug"))
		versionID := strings.TrimSpace(r.PathValue("versionId"))
		out, err := store.AdminGetSourceVersion(slug, versionID)
		if err != nil {
			switch {
			case errors.Is(err, model.ErrAdminSourceNotFound):
				writeErr(w, http.StatusNotFound, "source_version_not_found", "canonical source version was not found")
			case errors.Is(err, model.ErrAdminSourceRepairRequired):
				writeErr(w, http.StatusConflict, "source_repair_required", "canonical source requires repair")
			default:
				slog.Error("admin canonical source version failed")
				writeErr(w, http.StatusInternalServerError, "source_failed", "canonical source unavailable")
			}
			return
		}
		noStore(w)
		writeJSON(w, http.StatusOK, out)
	}))

	// GET /api/v1/admin/stories/{slug}
	mux.HandleFunc("GET /api/v1/admin/stories/{slug}", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		slug := strings.TrimSpace(r.PathValue("slug"))
		out, err := store.AdminGetStory(slug)
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

	// GET /api/v1/admin/stories/{slug}/editions/{editionKey}/versions/{versionId}
	mux.HandleFunc("GET /api/v1/admin/stories/{slug}/editions/{editionKey}/versions/{versionId}", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		slug := strings.TrimSpace(r.PathValue("slug"))
		editionKey, ok := parseAdminStoryEditionKey(r.PathValue("editionKey"))
		if !ok {
			writeErr(w, http.StatusBadRequest, "edition_invalid", "reading edition is not supported")
			return
		}
		versionID := strings.TrimSpace(r.PathValue("versionId"))
		out, err := store.AdminGetEditionVersionSource(slug, editionKey, versionID)
		if err != nil {
			switch {
			case errors.Is(err, model.ErrAdminStoryNotFound):
				writeErr(w, http.StatusNotFound, "version_not_found", "story version was not found")
			case errors.Is(err, model.ErrAdminVersionRepairRequired):
				writeErr(w, http.StatusConflict, "version_repair_required", "story version requires repair")
			default:
				slog.Error("admin story edition version source failed")
				writeErr(w, http.StatusInternalServerError, "version_failed", "story version unavailable")
			}
			return
		}
		noStore(w)
		writeJSON(w, http.StatusOK, out)
	}))

	// GET /api/v1/admin/stories/{slug}/versions/{versionId}
	mux.HandleFunc("GET /api/v1/admin/stories/{slug}/versions/{versionId}", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		slug := strings.TrimSpace(r.PathValue("slug"))
		versionID := strings.TrimSpace(r.PathValue("versionId"))
		out, err := store.AdminGetVersionSource(slug, versionID)
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

	// POST /api/v1/admin/stories/{slug}/releases
	mux.HandleFunc("POST /api/v1/admin/stories/{slug}/releases", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		slug := strings.TrimSpace(r.PathValue("slug"))
		var body model.AdminCreateReleaseRequest
		if err := decodeJSON(w, r, &body); err != nil {
			writeDecodeError(w, err)
			return
		}

		out, err := store.AdminCreateRelease(slug, body)
		if err != nil {
			var validationErr *model.AdminValidationError
			switch {
			case errors.As(err, &validationErr):
				writeIssues(w, http.StatusBadRequest, "release_invalid", "Story release is invalid", validationErr.Issues)
			case errors.Is(err, model.ErrAdminReleaseNotFound), errors.Is(err, model.ErrAdminStoryNotFound):
				writeErr(w, http.StatusNotFound, "release_not_found", "story edition version was not found")
			case errors.Is(err, model.ErrAdminReleaseInvalid):
				writeErr(w, http.StatusConflict, "release_repair_required", "stored release state or edition version requires repair")
			default:
				slog.Error("admin story release failed")
				writeErr(w, http.StatusInternalServerError, "release_failed", "story release could not be published")
			}
			return
		}

		noStore(w)
		writeJSON(w, http.StatusOK, out)
	}))

	// POST /api/v1/admin/stories/{slug}/unpublish
	mux.HandleFunc("POST /api/v1/admin/stories/{slug}/unpublish", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		slug := strings.TrimSpace(r.PathValue("slug"))
		out, err := store.AdminUnpublish(slug)
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

	// Security headers remain local to application responses. The root server
	// owns the single shared request-observability boundary.
	h := withSecurityHeaders(mux)

	return h

}

/* ------------------------------ helpers ------------------------------ */

func parseAdminStoryEditionKey(raw string) (model.AdminStoryEditionKey, bool) {
	key := model.AdminStoryEditionKey(strings.TrimSpace(raw))
	return key, model.ValidAdminStoryEditionKey(key)
}

func adminKeyOK(got, want string) bool {
	if got == "" || want == "" {
		return false
	}
	if len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func sourceProviderLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > sourceProviderMaximumLimit {
		return 0, sourceprovider.ErrQueryInvalid
	}
	return limit, nil
}

func writeSourceProviderError(w http.ResponseWriter, err error, isWork bool) {
	switch {
	case errors.Is(err, sourceprovider.ErrUnknownProvider):
		writeErr(w, http.StatusNotFound, "source_provider_invalid", "source provider is not supported")
	case errors.Is(err, sourceprovider.ErrQueryInvalid), errors.Is(err, sourceprovider.ErrWorkIDInvalid):
		writeErr(w, http.StatusBadRequest, "source_provider_query_invalid", "source provider request is invalid")
	case isWork && errors.Is(err, sourceprovider.ErrWorkNotFound):
		writeErr(w, http.StatusNotFound, "source_provider_work_not_found", "source provider work was not found")
	case errors.Is(err, sourceprovider.ErrTimeout):
		writeErr(w, http.StatusGatewayTimeout, "source_provider_timeout", "source provider did not respond in time")
	case errors.Is(err, sourceprovider.ErrResponseInvalid):
		slog.Error("source provider response was invalid")
		writeErr(w, http.StatusBadGateway, "source_provider_response_invalid", "source provider returned an invalid response")
	default:
		slog.Error("source provider request failed")
		writeErr(w, http.StatusBadGateway, "source_provider_unavailable", "source provider is unavailable")
	}
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

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "interest-cohort=()")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}
