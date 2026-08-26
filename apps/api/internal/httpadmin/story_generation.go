package httpadmin

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"pandapages/api/internal/httpbearer"
	"pandapages/api/internal/model"
)

const maxGenerationRequestBytes = 1024

// StoryGenerationJobService is the durable lifecycle boundary. It intentionally
// never exposes partial model output as completed orchestration evidence.
type StoryGenerationJobService interface {
	Enqueue(context.Context, model.AdminStoryGenerationJobCreateInput) (model.AdminStoryGenerationJob, error)
	Get(context.Context, string) (model.AdminStoryGenerationJob, error)
	GetActiveForSourceVersion(context.Context, string) (model.AdminStoryGenerationJob, error)
}

func storyGenerationJobCreateHandler(service StoryGenerationJobService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sourceVersionID, ok := httpbearer.CanonicalUUID(r.PathValue("sourceVersionID"))
		if !ok {
			writeErr(w, http.StatusBadRequest, "generation_source_version_invalid", "source version ID is invalid")
			return
		}
		if !emptyGenerationRequest(w, r) {
			return
		}
		if service == nil {
			writeErr(w, http.StatusServiceUnavailable, "generation_unavailable", "story generation is unavailable")
			return
		}
		account, ok := adminAccountFromRequest(r)
		if !ok {
			writeErr(w, http.StatusForbidden, "forbidden", "admin authorization required")
			return
		}

		job, err := service.Enqueue(r.Context(), model.AdminStoryGenerationJobCreateInput{
			SourceVersionID:      sourceVersionID,
			RequesterPrincipalID: account.PrincipalID,
			RequesterAccountID:   account.AccountID,
		})
		if err != nil {
			writeStoryGenerationJobError(w, r, sourceVersionID, err)
			return
		}
		noStore(w)
		writeJSON(w, http.StatusAccepted, job)
	}
}

func storyGenerationJobHandler(service StoryGenerationJobService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jobID, ok := httpbearer.CanonicalUUID(r.PathValue("jobID"))
		if !ok {
			writeErr(w, http.StatusBadRequest, "generation_job_invalid", "generation job ID is invalid")
			return
		}
		if service == nil {
			writeErr(w, http.StatusServiceUnavailable, "generation_unavailable", "story generation is unavailable")
			return
		}
		job, err := service.Get(r.Context(), jobID)
		if err != nil {
			writeStoryGenerationJobError(w, r, "", err)
			return
		}
		noStore(w)
		writeJSON(w, http.StatusOK, job)
	}
}

func storyGenerationActiveJobHandler(service StoryGenerationJobService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sourceVersionID, ok := httpbearer.CanonicalUUID(r.PathValue("sourceVersionID"))
		if !ok {
			writeErr(w, http.StatusBadRequest, "generation_source_version_invalid", "source version ID is invalid")
			return
		}
		if service == nil {
			writeErr(w, http.StatusServiceUnavailable, "generation_unavailable", "story generation is unavailable")
			return
		}
		job, err := service.GetActiveForSourceVersion(r.Context(), sourceVersionID)
		if err != nil {
			writeStoryGenerationJobError(w, r, sourceVersionID, err)
			return
		}
		noStore(w)
		writeJSON(w, http.StatusOK, job)
	}
}

func emptyGenerationRequest(w http.ResponseWriter, r *http.Request) bool {
	if r.Body == nil {
		return true
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxGenerationRequestBytes)
	defer r.Body.Close()
	raw, err := io.ReadAll(r.Body)
	if err != nil || len(bytes.TrimSpace(raw)) != 0 {
		writeErr(w, http.StatusBadRequest, "generation_request_invalid", "story generation request must be empty")
		return false
	}
	return true
}

func writeStoryGenerationJobError(w http.ResponseWriter, r *http.Request, sourceVersionID string, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		if sourceVersionID != "" {
			writeErr(w, http.StatusNotFound, "generation_source_version_not_found", "source version was not found")
		} else {
			writeErr(w, http.StatusNotFound, "generation_job_not_found", "generation job was not found")
		}
		return
	}
	slog.ErrorContext(r.Context(), "admin story generation job request failed", "source_version_id", sourceVersionID, "category", "job_store")
	writeErr(w, http.StatusInternalServerError, "generation_job_failed", "story generation job is unavailable")
}
