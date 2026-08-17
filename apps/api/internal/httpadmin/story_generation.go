package httpadmin

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"pandapages/api/internal/httpbearer"
	"pandapages/api/internal/storygeneration"
	"pandapages/api/internal/storyorchestration"
)

const (
	adminStoryGenerationTimeout = 5 * time.Minute
	maxGenerationRequestBytes   = 1024
)

// StoryGenerationService is the application boundary for one trusted source
// version generation run. HTTP does not know about model or transport details.
type StoryGenerationService interface {
	Run(context.Context, string) (storyorchestration.PersistedRun, error)
}

type storyGenerationResponse struct {
	ID              string `json:"id"`
	SourceVersionID string `json:"sourceVersionId"`
	SemanticResult  string `json:"semanticResult"`
	CreatedAt       string `json:"createdAt"`
}

func storyGenerationHandler(service StoryGenerationService) http.HandlerFunc {
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

		ctx, cancel := context.WithTimeout(r.Context(), adminStoryGenerationTimeout)
		defer cancel()
		persisted, err := service.Run(ctx, sourceVersionID)
		if err != nil {
			writeStoryGenerationError(w, r, ctx, sourceVersionID, err)
			return
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			writeStoryGenerationError(w, r, ctx, sourceVersionID, ctx.Err())
			return
		}

		slog.InfoContext(r.Context(), "admin story generation completed",
			"source_version_id", sourceVersionID,
			"story_orchestration_run_id", persisted.ID,
			"semantic_result", persisted.Result.SemanticResult,
		)
		noStore(w)
		writeJSON(w, http.StatusCreated, storyGenerationResponse{
			ID:              persisted.ID,
			SourceVersionID: persisted.SourceVersionID,
			SemanticResult:  string(persisted.Result.SemanticResult),
			CreatedAt:       persisted.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
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

func writeStoryGenerationError(
	w http.ResponseWriter,
	r *http.Request,
	ctx context.Context,
	sourceVersionID string,
	err error,
) {
	category := "internal"
	status := http.StatusInternalServerError
	code := "generation_failed"
	message := "story generation failed"
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(ctx.Err(), context.DeadlineExceeded):
		category = "deadline"
		status = http.StatusGatewayTimeout
		code = "generation_timeout"
		message = "story generation timed out"
	case errors.Is(err, storygeneration.ErrOpenAIRateLimited):
		category = "rate_limited"
		status = http.StatusTooManyRequests
		code = "generation_rate_limited"
		message = "story generation is temporarily rate limited"
	case errors.Is(err, storygeneration.ErrOpenAIUnavailable), errors.Is(err, storygeneration.ErrOpenAIUnauthorized):
		category = "upstream_unavailable"
		status = http.StatusServiceUnavailable
		code = "generation_unavailable"
		message = "story generation is unavailable"
	case errors.Is(err, storygeneration.ErrOpenAIResponseInvalid), errors.Is(err, storygeneration.ErrOpenAIResponseIncomplete), errors.Is(err, storygeneration.ErrOpenAIResponseRefused):
		category = "upstream_invalid"
		status = http.StatusBadGateway
		code = "generation_upstream_invalid"
		message = "story generation provider returned an invalid response"
	}
	slog.ErrorContext(r.Context(), "admin story generation failed",
		"source_version_id", sourceVersionID,
		"category", category,
	)
	writeErr(w, status, code, message)
}
