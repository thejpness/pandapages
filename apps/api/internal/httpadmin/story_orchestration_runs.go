package httpadmin

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"pandapages/api/internal/httpbearer"
	"pandapages/api/internal/storygeneration"
	"pandapages/api/internal/storyorchestration"
	"pandapages/api/internal/storyvalidation"
)

// StoryOrchestrationRunReader returns fully validated persisted orchestration
// evidence. HTTP deliberately does not decode or validate retained artifacts.
type StoryOrchestrationRunReader interface {
	GetCompletedStoryOrchestrationRun(string) (storyorchestration.PersistedRun, error)
}

type storyOrchestrationRunResponse struct {
	ID                 string                                     `json:"id"`
	SourceVersionID    string                                     `json:"sourceVersionId"`
	SourceSHA256       string                                     `json:"sourceSha256"`
	SemanticResult     string                                     `json:"semanticResult"`
	CreatedAt          string                                     `json:"createdAt"`
	AnalysisArtifact   storygeneration.StoryAnalysisArtifact      `json:"analysisArtifact"`
	Editions           []storygeneration.GeneratedEditionArtifact `json:"editions"`
	EditionAssessments []storyvalidation.AssessmentArtifact       `json:"editionAssessments"`
	BundleAssessment   storyvalidation.AssessmentArtifact         `json:"bundleAssessment"`
}

func storyOrchestrationRunHandler(reader StoryOrchestrationRunReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runID, ok := httpbearer.CanonicalUUID(r.PathValue("runID"))
		if !ok {
			writeErr(w, http.StatusBadRequest, "story_orchestration_run_invalid", "story orchestration run ID is invalid")
			return
		}
		if reader == nil {
			writeErr(w, http.StatusServiceUnavailable, "story_orchestration_runs_unavailable", "story orchestration runs are unavailable")
			return
		}

		persisted, err := reader.GetCompletedStoryOrchestrationRun(runID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeErr(w, http.StatusNotFound, "story_orchestration_run_not_found", "story orchestration run was not found")
			} else {
				slog.Error("admin story orchestration run read failed", "run_id", runID, "category", "read_failed")
				writeErr(w, http.StatusInternalServerError, "story_orchestration_run_failed", "story orchestration run is unavailable")
			}
			return
		}

		noStore(w)
		writeJSON(w, http.StatusOK, storyOrchestrationRunResponse{
			ID:                 persisted.ID,
			SourceVersionID:    persisted.SourceVersionID,
			SourceSHA256:       persisted.Result.SourceSHA256,
			SemanticResult:     string(persisted.Result.SemanticResult),
			CreatedAt:          persisted.CreatedAt.UTC().Format(time.RFC3339Nano),
			AnalysisArtifact:   persisted.Result.AnalysisArtifact,
			Editions:           persisted.Result.Editions,
			EditionAssessments: persisted.Result.EditionAssessments,
			BundleAssessment:   persisted.Result.BundleAssessment,
		})
	}
}
