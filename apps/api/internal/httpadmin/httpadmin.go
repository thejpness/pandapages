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
	"time"
	"unicode/utf8"

	"pandapages/api/internal/appidentity"
	"pandapages/api/internal/copyrighteligibility"
	"pandapages/api/internal/evidenceresolver"
	"pandapages/api/internal/model"
	"pandapages/api/internal/sourceeligibility"
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

	AdminPersistEligibleSourceAcquisition(sourceeligibility.Evaluation) (model.AdminSourceAcquisitionPersistResponse, error)
	AdminListSourceAcquisitions(limit int) (model.AdminSourceAcquisitionsListResponse, error)
	AdminGetSourceAcquisition(id string) (model.AdminSourceAcquisitionDetail, error)
	AdminPromoteSourceAcquisition(id string, req model.AdminSourceAcquisitionPromotionRequest) (model.AdminSourceAcquisitionPromotionResponse, error)
	AdminUpdateSourceAcquisitionSourceQualityReview(id string, req model.AdminSourceQualityReviewUpdateRequest) (model.AdminSourceAcquisitionSummary, error)
}

const (
	// Admin endpoints need a bigger body limit for large Gutenberg books.
	// Keep public APIs small; only admin gets this.
	maxJSONBodyBytes           = 20 << 20 // 20MB
	sourceProviderMaximumLimit = 20
	maxSourceEligibilityBody   = 64 << 10
	maxSourcePromotionBody     = 32 << 10
)

var errSourceEligibilityInput = errors.New("source eligibility evidence is invalid")

func New(cfg Config, store Store) http.Handler {
	adminKey := strings.TrimSpace(cfg.AdminKey)
	if adminKey == "" {
		panic("PP_ADMIN_KEY is required for admin routes")
	}
	if cfg.BearerAuthenticator == nil {
		panic("bearer account authenticator is required")
	}

	discovery := cfg.SourceDiscovery
	acquisition := cfg.SourceAcquisition
	eligibility := cfg.SourceEligibility
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

	mux.HandleFunc("POST /api/v1/admin/source-versions/{sourceVersionID}/generate", withAdmin(storyGenerationHandler(cfg.StoryGeneration)))

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

	mux.HandleFunc("POST /api/v1/admin/source-providers/{provider}/works/{externalID}/candidate", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		if acquisition == nil {
			writeErr(w, http.StatusServiceUnavailable, "source_provider_unavailable", "source provider is unavailable")
			return
		}
		out, err := acquisition.Acquire(r.Context(), sourceprovider.ID(strings.TrimSpace(r.PathValue("provider"))), strings.TrimSpace(r.PathValue("externalID")))
		if err != nil {
			writeSourceProviderError(w, err, true)
			return
		}
		noStore(w)
		writeJSON(w, http.StatusOK, out)
	}))

	// POST /api/v1/admin/source-providers/{provider}/works/{externalID}/copyright-eligibility
	// evaluates current provider material without creating any database state.
	mux.HandleFunc("POST /api/v1/admin/source-providers/{provider}/works/{externalID}/copyright-eligibility", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		if eligibility == nil {
			writeErr(w, http.StatusServiceUnavailable, "source_eligibility_unavailable", "copyright eligibility is unavailable")
			return
		}
		human, err := decodeSourceEligibilityHumanEvidence(w, r)
		if err != nil {
			writeSourceEligibilityInputError(w, err)
			return
		}
		evaluation, err := eligibility.Evaluate(r.Context(), sourceprovider.ID(strings.TrimSpace(r.PathValue("provider"))), strings.TrimSpace(r.PathValue("externalID")), human)
		if err != nil {
			writeSourceEligibilityError(w, err)
			return
		}
		noStore(w)
		writeJSON(w, http.StatusOK, sourceEligibilityResponse(evaluation))
	}))

	mux.HandleFunc("POST /api/v1/admin/source-providers/{provider}/works/{externalID}/acquisitions", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		if eligibility == nil {
			writeErr(w, http.StatusServiceUnavailable, "source_eligibility_unavailable", "copyright eligibility is unavailable")
			return
		}
		human, err := decodeSourceEligibilityHumanEvidence(w, r)
		if err != nil {
			writeSourceEligibilityInputError(w, err)
			return
		}
		evaluation, err := eligibility.Evaluate(r.Context(), sourceprovider.ID(strings.TrimSpace(r.PathValue("provider"))), strings.TrimSpace(r.PathValue("externalID")), human)
		if err != nil {
			writeSourceEligibilityError(w, err)
			return
		}
		if evaluation.Assessment.Overall != "eligible" {
			noStore(w)
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"error":       map[string]string{"code": "source_eligibility_blocked", "message": "copyright eligibility is blocked"},
				"eligibility": sourceEligibilityResponse(evaluation),
			})
			return
		}
		out, err := store.AdminPersistEligibleSourceAcquisition(evaluation)
		if err != nil {
			writeSourceAcquisitionError(w, err, false)
			return
		}
		noStore(w)
		status := http.StatusOK
		if out.Outcome == model.AdminSourceAcquisitionOutcomeCreated {
			status = http.StatusCreated
		}
		writeJSON(w, status, out)
	}))

	mux.HandleFunc("GET /api/v1/admin/source-acquisitions", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		limit, err := sourceAcquisitionLimit(r.URL.Query().Get("limit"))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "source_acquisition_invalid", "source acquisition list request is invalid")
			return
		}
		out, err := store.AdminListSourceAcquisitions(limit)
		if err != nil {
			writeSourceAcquisitionError(w, err, false)
			return
		}
		noStore(w)
		writeJSON(w, http.StatusOK, out)
	}))

	mux.HandleFunc("GET /api/v1/admin/source-acquisitions/{acquisitionID}", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		out, err := store.AdminGetSourceAcquisition(strings.TrimSpace(r.PathValue("acquisitionID")))
		if err != nil {
			writeSourceAcquisitionError(w, err, false)
			return
		}
		noStore(w)
		writeJSON(w, http.StatusOK, out)
	}))

	mux.HandleFunc("POST /api/v1/admin/source-acquisitions/{acquisitionID}/promote", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		var body model.AdminSourceAcquisitionPromotionRequest
		if err := decodeJSONLimit(w, r, &body, maxSourcePromotionBody); err != nil {
			writeDecodeError(w, err)
			return
		}
		out, err := store.AdminPromoteSourceAcquisition(strings.TrimSpace(r.PathValue("acquisitionID")), body)
		if err != nil {
			writeSourceAcquisitionError(w, err, false)
			return
		}
		noStore(w)
		status := http.StatusOK
		if out.Outcome == model.AdminSourceAcquisitionPromotionCreated {
			status = http.StatusCreated
		}
		writeJSON(w, status, out)
	}))

	mux.HandleFunc("PUT /api/v1/admin/source-acquisitions/{acquisitionID}/source-quality-review", withAdmin(func(w http.ResponseWriter, r *http.Request) {
		var body model.AdminSourceQualityReviewUpdateRequest
		if err := decodeJSON(w, r, &body); err != nil {
			writeDecodeError(w, err)
			return
		}
		out, err := store.AdminUpdateSourceAcquisitionSourceQualityReview(strings.TrimSpace(r.PathValue("acquisitionID")), body)
		if err != nil {
			writeSourceAcquisitionError(w, err, true)
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

func sourceAcquisitionLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > 100 {
		return 0, errors.New("source acquisition list limit is invalid")
	}
	return limit, nil
}

func writeSourceAcquisitionError(w http.ResponseWriter, err error, review bool) {
	switch {
	case errors.Is(err, model.ErrAdminSourceAcquisitionNotFound):
		writeErr(w, http.StatusNotFound, "source_acquisition_not_found", "source acquisition was not found")
	case errors.Is(err, model.ErrAdminSourceAcquisitionNotReady):
		writeErr(w, http.StatusConflict, "source_acquisition_not_ready", "source acquisition is not ready for promotion")
	case errors.Is(err, model.ErrAdminSourceAcquisitionAlreadyPromoted):
		writeErr(w, http.StatusConflict, "source_acquisition_already_promoted", "source acquisition is already promoted to another story")
	case errors.Is(err, model.ErrAdminSourceAcquisitionPromotionTarget):
		writeErr(w, http.StatusNotFound, "source_acquisition_promotion_target_not_found", "promotion target was not found")
	case errors.Is(err, model.ErrAdminSourceAcquisitionPromotionConflict):
		writeErr(w, http.StatusConflict, "source_acquisition_promotion_conflict", "source acquisition promotion conflicts")
	default:
		var validationErr *model.AdminValidationError
		if errors.As(err, &validationErr) {
			code := "source_acquisition_invalid"
			message := "source acquisition is invalid"
			if review {
				code = "source_acquisition_review_invalid"
				message = "source acquisition review is invalid"
			}
			writeIssues(w, http.StatusBadRequest, code, message, validationErr.Issues)
			return
		}
		slog.Error("source acquisition operation failed", "error", err)
		writeErr(w, http.StatusInternalServerError, "source_acquisition_failed", "source acquisition operation failed")
	}
}

func writeSourceEligibilityInputError(w http.ResponseWriter, err error) {
	var validationErr *model.AdminValidationError
	if errors.As(err, &validationErr) {
		writeIssues(w, http.StatusBadRequest, "source_eligibility_invalid", "copyright evidence is invalid", validationErr.Issues)
		return
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeErr(w, http.StatusRequestEntityTooLarge, "source_eligibility_invalid", "copyright evidence is too large")
		return
	}
	writeErr(w, http.StatusBadRequest, "source_eligibility_invalid", "copyright evidence is invalid")
}

func writeSourceEligibilityError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sourceeligibility.ErrHumanEvidenceConflict):
		writeErr(w, http.StatusUnprocessableEntity, "source_eligibility_blocked", "provider evidence conflicts with submitted facts")
	case errors.Is(err, sourceeligibility.ErrProviderEvidenceInvalid), errors.Is(err, sourceprovider.ErrEvidenceInvalid), errors.Is(err, sourceprovider.ErrEvidenceIdentityMismatch):
		slog.Error("source eligibility evidence was invalid")
		writeErr(w, http.StatusBadGateway, "source_eligibility_evidence_failed", "copyright evidence could not be verified")
	case errors.Is(err, sourceprovider.ErrEvidenceTooLarge):
		writeErr(w, http.StatusBadGateway, "source_eligibility_evidence_failed", "copyright evidence could not be verified")
	case errors.Is(err, sourceprovider.ErrEvidenceTimeout), errors.Is(err, sourceprovider.ErrTimeout):
		writeErr(w, http.StatusGatewayTimeout, "source_eligibility_evidence_timeout", "copyright evidence did not respond in time")
	case errors.Is(err, sourceprovider.ErrUnknownProvider), errors.Is(err, sourceprovider.ErrWorkIDInvalid):
		writeErr(w, http.StatusBadRequest, "source_provider_query_invalid", "source provider request is invalid")
	case errors.Is(err, sourceprovider.ErrWorkNotFound):
		writeErr(w, http.StatusNotFound, "source_provider_work_not_found", "source provider work was not found")
	default:
		slog.Error("source eligibility evaluation failed")
		writeErr(w, http.StatusBadGateway, "source_eligibility_evidence_failed", "copyright evidence is unavailable")
	}
}

func sourceEligibilityResponse(evaluation sourceeligibility.Evaluation) model.AdminSourceEligibility {
	contributors := make([]model.AdminCopyrightContributorEvidence, 0, len(evaluation.ProviderEvidence.Contributors))
	for _, contributor := range evaluation.ProviderEvidence.Contributors {
		contributors = append(contributors, model.AdminCopyrightContributorEvidence{Name: contributor.Name, Role: contributor.Role, BirthYear: contributor.BirthYear, DeathYear: contributor.DeathYear})
	}
	return model.AdminSourceEligibility{
		PolicyVersion:  evaluation.Assessment.PolicyVersion,
		EvaluationDate: evaluation.EvaluationDate.UTC().Format("2006-01-02"),
		EvaluatedAt:    evaluation.EvaluatedAt.UTC().Format(time.RFC3339Nano),
		US:             model.AdminCopyrightJurisdiction{Status: string(evaluation.Assessment.US.Status), Reason: string(evaluation.Assessment.US.Reason)},
		UK:             model.AdminCopyrightJurisdiction{Status: string(evaluation.Assessment.UK.Status), Reason: string(evaluation.Assessment.UK.Reason)},
		Overall:        string(evaluation.Assessment.Overall),
		OverallReason:  string(evaluation.Assessment.OverallReason),
		OPDSRights:     string(evaluation.OPDSRights), RDFRights: string(evaluation.ProviderEvidence.Rights), HeaderRights: string(evaluation.HeaderRights),
		ProviderTitle: evaluation.ProviderEvidence.Title, Contributors: contributors, RDFDigest: evaluation.ProviderEvidence.EvidenceDigest,
		EffectiveUK:         sourceEligibilityEffectiveUK(evaluation.EffectiveUKEvidence),
		AutomaticResolution: sourceEligibilityAutomaticResolution(evaluation.Resolution),
	}
}

func sourceEligibilityAutomaticResolution(value evidenceresolver.Resolution) *model.AdminSourceEligibilityAutomaticResolution {
	status := func(value evidenceresolver.ResolutionStatus) string {
		switch value {
		case evidenceresolver.ResolutionEstablished, evidenceresolver.ResolutionConflicting:
			return string(value)
		default:
			return string(evidenceresolver.ResolutionInsufficient)
		}
	}
	return &model.AdminSourceEligibilityAutomaticResolution{
		WorkCategory:                  status(value.WorkCategory.Status),
		Authorship:                    status(value.Authorship.Status),
		Author:                        status(value.Author.Status),
		FirstPublication:              status(value.FirstPublication.Status),
		Translation:                   status(value.Translation.Status),
		AdditionalTextualContribution: status(value.AdditionalTextual.Status),
		UnpublishedAtEnd1988:          status(value.UnpublishedAtEnd1988.Status),
	}
}

func sourceEligibilityEffectiveUK(value copyrighteligibility.UKEvidence) model.AdminSourceEligibilityEffectiveUKEvidence {
	return model.AdminSourceEligibilityEffectiveUKEvidence{WorkTitle: value.WorkTitle, WorkCategory: string(value.WorkCategory), WorkCategoryReferences: sourceEligibilityResponseReferences(value.WorkCategoryReferences), Authorship: string(value.Authorship), AuthorshipReferences: sourceEligibilityResponseReferences(value.AuthorshipReferences), AuthorName: value.Author.Name, AuthorDeathYear: value.Author.DeathYear, AuthorReferences: sourceEligibilityResponseReferences(value.Author.References), FirstPublicationYear: value.FirstPublication.Year, FirstPublicationRefs: sourceEligibilityResponseReferences(value.FirstPublication.References), Translation: sourceEligibilityResponseFact(value.Translation), AdditionalTextual: sourceEligibilityResponseFact(value.AdditionalTextualContribution), UnpublishedAtEnd1988: sourceEligibilityResponseFact(value.UnpublishedAtEnd1988)}
}

func sourceEligibilityResponseFact(value copyrighteligibility.FactEvidence) model.AdminCopyrightFactEvidence {
	return model.AdminCopyrightFactEvidence{State: string(value.State), References: sourceEligibilityResponseReferences(value.References)}
}

func sourceEligibilityResponseReferences(values []copyrighteligibility.EvidenceReference) []model.AdminCopyrightEvidenceReference {
	result := make([]model.AdminCopyrightEvidenceReference, 0, len(values))
	for _, value := range values {
		result = append(result, model.AdminCopyrightEvidenceReference{Source: value.Source, Fact: value.Fact, Locator: eligibilityOptionalString(value.Locator), Identifier: eligibilityOptionalString(value.Identifier), Digest: eligibilityOptionalString(value.Digest)})
	}
	return result
}

func eligibilityOptionalString(value string) *string {
	if value == "" {
		return nil
	}
	result := value
	return &result
}

func requireEmptyBody(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1)
	defer r.Body.Close()
	var buffer [1]byte
	_, err := r.Body.Read(buffer[:])
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("unexpected request body")
}

func decodeSourceEligibilityHumanEvidence(w http.ResponseWriter, r *http.Request) (sourceeligibility.HumanUKEvidence, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSourceEligibilityBody)
	defer r.Body.Close()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return sourceeligibility.HumanUKEvidence{}, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return sourceeligibility.HumanUKEvidence{}, nil
	}
	if !utf8.Valid(raw) {
		return sourceeligibility.HumanUKEvidence{}, errSourceEligibilityInput
	}
	var body model.AdminSourceEligibilityHumanEvidence
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return sourceeligibility.HumanUKEvidence{}, errSourceEligibilityInput
	}
	return sourceEligibilityHumanEvidence(body)
}

func sourceEligibilityHumanEvidence(body model.AdminSourceEligibilityHumanEvidence) (sourceeligibility.HumanUKEvidence, error) {
	workCategory := copyrighteligibility.WorkCategory(strings.TrimSpace(body.WorkCategory))
	if workCategory == "" {
		workCategory = copyrighteligibility.WorkCategoryUnknown
	}
	if workCategory != copyrighteligibility.WorkCategoryOrdinaryLiterary && workCategory != copyrighteligibility.WorkCategoryUnknown {
		return sourceeligibility.HumanUKEvidence{}, errSourceEligibilityInput
	}
	workCategoryReferences, err := sourceEligibilityReferences(body.WorkCategoryReferences)
	if err != nil {
		return sourceeligibility.HumanUKEvidence{}, err
	}
	authorDeathReferences, err := sourceEligibilityReferences(body.AuthorDeathReferences)
	if err != nil {
		return sourceeligibility.HumanUKEvidence{}, err
	}
	publicationReferences, err := sourceEligibilityReferences(body.FirstPublicationRefs)
	if err != nil {
		return sourceeligibility.HumanUKEvidence{}, err
	}
	translation, err := sourceEligibilityFactEvidence(body.Translation)
	if err != nil {
		return sourceeligibility.HumanUKEvidence{}, err
	}
	additional, err := sourceEligibilityFactEvidence(body.AdditionalTextual)
	if err != nil {
		return sourceeligibility.HumanUKEvidence{}, err
	}
	unpublished, err := sourceEligibilityFactEvidence(body.UnpublishedAtEnd1988)
	if err != nil {
		return sourceeligibility.HumanUKEvidence{}, err
	}
	if (body.AuthorDeathYear != nil && (*body.AuthorDeathYear < -9999 || *body.AuthorDeathYear > 9999)) || body.FirstPublicationYear < -9999 || body.FirstPublicationYear > 9999 {
		return sourceeligibility.HumanUKEvidence{}, errSourceEligibilityInput
	}
	return sourceeligibility.HumanUKEvidence{WorkCategory: workCategory, WorkCategoryReferences: workCategoryReferences, AuthorDeathYear: body.AuthorDeathYear, AuthorDeathReferences: authorDeathReferences, FirstPublication: copyrighteligibility.PublicationEvidence{Year: body.FirstPublicationYear, References: publicationReferences}, Translation: translation, AdditionalTextual: additional, UnpublishedAtEnd1988: unpublished}, nil
}

func sourceEligibilityFactEvidence(body model.AdminCopyrightFactEvidence) (copyrighteligibility.FactEvidence, error) {
	state := copyrighteligibility.FactState(strings.TrimSpace(body.State))
	if state == "" {
		state = copyrighteligibility.FactUnknown
	}
	if state != copyrighteligibility.FactNoneConfirmed && state != copyrighteligibility.FactPresent && state != copyrighteligibility.FactUnknown {
		return copyrighteligibility.FactEvidence{}, errSourceEligibilityInput
	}
	references, err := sourceEligibilityReferences(body.References)
	if err != nil {
		return copyrighteligibility.FactEvidence{}, err
	}
	return copyrighteligibility.FactEvidence{State: state, References: references}, nil
}

func sourceEligibilityReferences(values []model.AdminCopyrightEvidenceReference) ([]copyrighteligibility.EvidenceReference, error) {
	if len(values) > 8 {
		return nil, errSourceEligibilityInput
	}
	result := make([]copyrighteligibility.EvidenceReference, 0, len(values))
	for _, value := range values {
		reference := copyrighteligibility.EvidenceReference{Source: strings.TrimSpace(value.Source), Fact: strings.TrimSpace(value.Fact)}
		if value.Locator != nil {
			reference.Locator = strings.TrimSpace(*value.Locator)
		}
		if value.Identifier != nil {
			reference.Identifier = strings.TrimSpace(*value.Identifier)
		}
		if value.Digest != nil {
			reference.Digest = strings.TrimSpace(*value.Digest)
		}
		if !validEligibilityText(reference.Source, 500) || !validEligibilityText(reference.Fact, 1000) || (reference.Locator != "" && !validEligibilityText(reference.Locator, 2048)) || (reference.Identifier != "" && !validEligibilityText(reference.Identifier, 256)) || (reference.Digest != "" && !sourceEligibilityDigest(reference.Digest)) {
			return nil, errSourceEligibilityInput
		}
		result = append(result, reference)
	}
	return result, nil
}

func validEligibilityText(value string, maximum int) bool {
	return utf8.ValidString(value) && value == strings.TrimSpace(value) && value != "" && len(value) <= maximum
}

func sourceEligibilityDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, runeValue := range value {
		if !(runeValue >= '0' && runeValue <= '9' || runeValue >= 'a' && runeValue <= 'f') {
			return false
		}
	}
	return true
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
	case errors.Is(err, sourceprovider.ErrRepresentationUnavailable):
		writeErr(w, http.StatusUnprocessableEntity, "source_provider_representation_unavailable", "source provider has no supported plain-text representation")
	case errors.Is(err, sourceprovider.ErrContentTooLarge):
		writeErr(w, http.StatusRequestEntityTooLarge, "source_provider_content_too_large", "source provider content is too large")
	case errors.Is(err, sourceprovider.ErrContentInvalid):
		slog.Error("source provider content was invalid")
		writeErr(w, http.StatusBadGateway, "source_provider_content_invalid", "source provider content is invalid")
	case errors.Is(err, sourceprovider.ErrNormalisationFailed):
		writeErr(w, http.StatusUnprocessableEntity, "source_provider_normalisation_failed", "source provider content could not be normalised")
	default:
		slog.Error("source provider request failed")
		writeErr(w, http.StatusBadGateway, "source_provider_unavailable", "source provider is unavailable")
	}
}

func noStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	return decodeJSONLimit(w, r, dst, maxJSONBodyBytes)
}

func decodeJSONLimit(w http.ResponseWriter, r *http.Request, dst any, limit int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
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
