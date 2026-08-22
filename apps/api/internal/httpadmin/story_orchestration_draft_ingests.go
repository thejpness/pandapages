package httpadmin

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"

	"pandapages/api/internal/httpbearer"
	"pandapages/api/internal/model"
)

const maxStoryOrchestrationDraftIngestBody = 1024

// StoryOrchestrationDraftIngestService owns the atomic server-side transition
// from immutable approved evidence to editable draft versions.
type StoryOrchestrationDraftIngestService interface {
	CreateStoryOrchestrationDraftIngest(model.AdminStoryOrchestrationDraftIngestInput) (model.AdminStoryOrchestrationDraftIngestResponse, error)
}

func storyOrchestrationDraftIngestHandler(service StoryOrchestrationDraftIngestService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runID, ok := canonicalStoryOrchestrationDraftIngestUUID(r.PathValue("runID"))
		if !ok {
			writeErr(w, http.StatusBadRequest, "story_orchestration_run_invalid", "story orchestration run ID is invalid")
			return
		}
		if service == nil {
			writeErr(w, http.StatusServiceUnavailable, "story_orchestration_draft_ingests_unavailable", "story orchestration draft ingests are unavailable")
			return
		}

		var body model.AdminStoryOrchestrationDraftIngestRequest
		if err := decodeJSONLimit(w, r, &body, maxStoryOrchestrationDraftIngestBody); err != nil {
			writeDecodeError(w, err)
			return
		}
		reviewID, ok := canonicalStoryOrchestrationDraftIngestUUID(body.EditorialReviewID)
		if !ok {
			writeErr(w, http.StatusBadRequest, "story_orchestration_draft_ingest_invalid", "story orchestration draft ingest is invalid")
			return
		}
		body.EditorialReviewID = reviewID
		if err := body.Validate(); err != nil {
			writeErr(w, http.StatusBadRequest, "story_orchestration_draft_ingest_invalid", "story orchestration draft ingest is invalid")
			return
		}

		out, err := service.CreateStoryOrchestrationDraftIngest(model.AdminStoryOrchestrationDraftIngestInput{
			RunID: runID, EditorialReviewID: body.EditorialReviewID,
		})
		if err != nil {
			switch {
			case errors.Is(err, model.ErrAdminStoryOrchestrationDraftIngestInvalid):
				writeErr(w, http.StatusBadRequest, "story_orchestration_draft_ingest_invalid", "story orchestration draft ingest is invalid")
			case errors.Is(err, sql.ErrNoRows):
				writeErr(w, http.StatusNotFound, "story_orchestration_draft_ingest_not_found", "story orchestration run or editorial review was not found")
			case errors.Is(err, model.ErrAdminStoryOrchestrationRunRepairRequired), errors.Is(err, model.ErrAdminStoryOrchestrationDraftIngestConflict):
				writeErr(w, http.StatusConflict, "story_orchestration_draft_ingest_conflict", "story orchestration run cannot be ingested into editable drafts")
			default:
				slog.Error("admin story orchestration draft ingest failed", "run_id", runID, "category", "create_failed")
				writeErr(w, http.StatusInternalServerError, "story_orchestration_draft_ingest_failed", "story orchestration draft ingest could not be created")
			}
			return
		}

		status := http.StatusCreated
		if out.Outcome == model.AdminStoryOrchestrationDraftIngestOutcomeReused {
			status = http.StatusOK
		}
		if out.Outcome != model.AdminStoryOrchestrationDraftIngestOutcomeCreated && out.Outcome != model.AdminStoryOrchestrationDraftIngestOutcomeReused {
			slog.Error("admin story orchestration draft ingest returned invalid outcome", "run_id", runID, "category", "invalid_response")
			writeErr(w, http.StatusInternalServerError, "story_orchestration_draft_ingest_failed", "story orchestration draft ingest could not be created")
			return
		}
		noStore(w)
		writeJSON(w, status, out)
	}
}

func canonicalStoryOrchestrationDraftIngestUUID(raw string) (string, bool) {
	value, ok := httpbearer.CanonicalUUID(raw)
	return value, ok && value != zeroStoryOrchestrationRunID
}
