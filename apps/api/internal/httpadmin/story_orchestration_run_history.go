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
	defaultStoryOrchestrationRunHistoryLimit = 50
	maxStoryOrchestrationRunHistoryLimit     = 100
)

// StoryOrchestrationRunHistoryReader returns bounded metadata-only history
// for one exact source version.
type StoryOrchestrationRunHistoryReader interface {
	ListCompletedStoryOrchestrationRuns(string, int) (model.AdminStoryOrchestrationRunsListResponse, error)
}

func storyOrchestrationRunHistoryHandler(reader StoryOrchestrationRunHistoryReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sourceVersionID, ok := httpbearer.CanonicalUUID(r.PathValue("sourceVersionID"))
		if !ok {
			writeErr(w, http.StatusBadRequest, "story_orchestration_run_history_invalid", "story source version ID is invalid")
			return
		}
		limit, err := storyOrchestrationRunHistoryLimit(r.URL.Query().Get("limit"))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "story_orchestration_run_history_invalid", "story orchestration run history request is invalid")
			return
		}
		if reader == nil {
			writeErr(w, http.StatusServiceUnavailable, "story_orchestration_runs_unavailable", "story orchestration runs are unavailable")
			return
		}

		out, err := reader.ListCompletedStoryOrchestrationRuns(sourceVersionID, limit)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeErr(w, http.StatusNotFound, "story_source_version_not_found", "story source version was not found")
			} else {
				slog.Error("admin story orchestration run history read failed", "source_version_id", sourceVersionID, "category", "read_failed")
				writeErr(w, http.StatusInternalServerError, "story_orchestration_run_history_failed", "story orchestration run history is unavailable")
			}
			return
		}

		noStore(w)
		writeJSON(w, http.StatusOK, out)
	}
}

func storyOrchestrationRunHistoryLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultStoryOrchestrationRunHistoryLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > maxStoryOrchestrationRunHistoryLimit {
		return 0, errors.New("story orchestration run history limit is invalid")
	}
	return limit, nil
}
