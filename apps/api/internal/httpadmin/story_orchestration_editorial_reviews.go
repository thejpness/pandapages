package httpadmin

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"pandapages/api/internal/httpbearer"
	"pandapages/api/internal/model"
)

const (
	defaultStoryOrchestrationEditorialReviewLimit = 50
	maxStoryOrchestrationEditorialReviewLimit     = 100
	maxStoryOrchestrationEditorialReviewBody      = 1024
	zeroStoryOrchestrationRunID                   = "00000000-0000-0000-0000-000000000000"
)

// StoryOrchestrationEditorialReviewService owns the exact-run validation
// before create and bounded metadata-only review history reads.
type StoryOrchestrationEditorialReviewService interface {
	Create(model.AdminStoryOrchestrationEditorialReviewCreateInput) (model.AdminStoryOrchestrationEditorialReview, error)
	List(string, int) (model.AdminStoryOrchestrationEditorialReviewsListResponse, error)
}

func storyOrchestrationEditorialReviewsHandler(service StoryOrchestrationEditorialReviewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runID, ok := canonicalStoryOrchestrationEditorialReviewRunID(r.PathValue("runID"))
		if !ok {
			writeErr(w, http.StatusBadRequest, "story_orchestration_run_invalid", "story orchestration run ID is invalid")
			return
		}
		if service == nil {
			writeErr(w, http.StatusServiceUnavailable, "story_orchestration_editorial_reviews_unavailable", "story orchestration editorial reviews are unavailable")
			return
		}

		switch r.Method {
		case http.MethodPost:
			createStoryOrchestrationEditorialReview(w, r, service, runID)
		case http.MethodGet:
			listStoryOrchestrationEditorialReviews(w, r, service, runID)
		default:
			writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed")
		}
	}
}

func canonicalStoryOrchestrationEditorialReviewRunID(raw string) (string, bool) {
	runID, ok := httpbearer.CanonicalUUID(raw)
	return runID, ok && runID != zeroStoryOrchestrationRunID
}

func createStoryOrchestrationEditorialReview(
	w http.ResponseWriter,
	r *http.Request,
	service StoryOrchestrationEditorialReviewService,
	runID string,
) {
	var body model.AdminStoryOrchestrationEditorialReviewCreateRequest
	if err := decodeJSONLimit(w, r, &body, maxStoryOrchestrationEditorialReviewBody); err != nil {
		writeDecodeError(w, err)
		return
	}
	if err := body.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, "story_orchestration_editorial_review_invalid", "story orchestration editorial review is invalid")
		return
	}
	account, ok := adminAccountFromRequest(r)
	if !ok {
		slog.Error("trusted admin account context was unavailable", "category", "editorial_review_context")
		writeErr(w, http.StatusInternalServerError, "story_orchestration_editorial_review_failed", "story orchestration editorial review could not be recorded")
		return
	}

	review, err := service.Create(model.AdminStoryOrchestrationEditorialReviewCreateInput{
		RunID:               runID,
		Decision:            body.Decision,
		ReviewerPrincipalID: account.PrincipalID,
		ReviewerAccountID:   account.AccountID,
	})
	if err != nil {
		switch {
		case errors.Is(err, model.ErrAdminStoryOrchestrationEditorialReviewInvalid):
			writeErr(w, http.StatusBadRequest, "story_orchestration_editorial_review_invalid", "story orchestration editorial review is invalid")
		case errors.Is(err, sql.ErrNoRows):
			writeErr(w, http.StatusNotFound, "story_orchestration_run_not_found", "story orchestration run was not found")
		case errors.Is(err, model.ErrAdminStoryOrchestrationRunRepairRequired):
			writeErr(w, http.StatusConflict, "story_orchestration_run_repair_required", "story orchestration run requires repair before editorial review")
		default:
			slog.Error("admin story orchestration editorial review create failed", "run_id", runID, "category", "create_failed")
			writeErr(w, http.StatusInternalServerError, "story_orchestration_editorial_review_failed", "story orchestration editorial review could not be recorded")
		}
		return
	}

	noStore(w)
	writeJSON(w, http.StatusCreated, review)
}

func listStoryOrchestrationEditorialReviews(
	w http.ResponseWriter,
	r *http.Request,
	service StoryOrchestrationEditorialReviewService,
	runID string,
) {
	limit, err := storyOrchestrationEditorialReviewLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "story_orchestration_editorial_review_history_invalid", "story orchestration editorial review history request is invalid")
		return
	}
	out, err := service.List(runID, limit)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "story_orchestration_run_not_found", "story orchestration run was not found")
		} else {
			slog.Error("admin story orchestration editorial review history failed", "run_id", runID, "category", "read_failed")
			writeErr(w, http.StatusInternalServerError, "story_orchestration_editorial_review_history_failed", "story orchestration editorial review history is unavailable")
		}
		return
	}

	noStore(w)
	writeJSON(w, http.StatusOK, out)
}

func storyOrchestrationEditorialReviewLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultStoryOrchestrationEditorialReviewLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > maxStoryOrchestrationEditorialReviewLimit {
		return 0, errors.New("story orchestration editorial review history limit is invalid")
	}
	return limit, nil
}
