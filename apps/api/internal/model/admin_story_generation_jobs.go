package model

import (
	"fmt"
	"strings"
)

// AdminStoryGenerationJobStatus is durable operational state, not an
// editorial result or completed orchestration evidence state.
type AdminStoryGenerationJobStatus string

const (
	AdminStoryGenerationJobQueued    AdminStoryGenerationJobStatus = "queued"
	AdminStoryGenerationJobRunning   AdminStoryGenerationJobStatus = "running"
	AdminStoryGenerationJobCompleted AdminStoryGenerationJobStatus = "completed"
	AdminStoryGenerationJobFailed    AdminStoryGenerationJobStatus = "failed"
)

// AdminStoryGenerationJobStage reports the last durable operation boundary.
// It deliberately contains no prompt, provider, or generated-content detail.
type AdminStoryGenerationJobStage string

const (
	AdminStoryGenerationJobStageQueued                     AdminStoryGenerationJobStage = "queued"
	AdminStoryGenerationJobStageAnalysingSource            AdminStoryGenerationJobStage = "analysing_source"
	AdminStoryGenerationJobStageGeneratingConfidentReaders AdminStoryGenerationJobStage = "generating_confident_readers"
	AdminStoryGenerationJobStageGeneratingGrowingReaders   AdminStoryGenerationJobStage = "generating_growing_readers"
	AdminStoryGenerationJobStageGeneratingStoryExplorers   AdminStoryGenerationJobStage = "generating_story_explorers"
	AdminStoryGenerationJobStageGeneratingLittleListeners  AdminStoryGenerationJobStage = "generating_little_listeners"
	AdminStoryGenerationJobStageValidatingConfidentReaders AdminStoryGenerationJobStage = "validating_confident_readers"
	AdminStoryGenerationJobStageValidatingGrowingReaders   AdminStoryGenerationJobStage = "validating_growing_readers"
	AdminStoryGenerationJobStageValidatingStoryExplorers   AdminStoryGenerationJobStage = "validating_story_explorers"
	AdminStoryGenerationJobStageValidatingLittleListeners  AdminStoryGenerationJobStage = "validating_little_listeners"
	AdminStoryGenerationJobStageValidatingBundle           AdminStoryGenerationJobStage = "validating_bundle"
	AdminStoryGenerationJobStageCompleted                  AdminStoryGenerationJobStage = "completed"
	AdminStoryGenerationJobStageFailed                     AdminStoryGenerationJobStage = "failed"
)

// AdminStoryGenerationJob is safe operational metadata for the admin API.
// Requester provenance is retained internally but is never serialised.
type AdminStoryGenerationJob struct {
	ID                   string                        `json:"id"`
	SourceVersionID      string                        `json:"sourceVersionId"`
	Status               AdminStoryGenerationJobStatus `json:"status"`
	Stage                AdminStoryGenerationJobStage  `json:"stage"`
	FailureCode          *string                       `json:"failureCode,omitempty"`
	CompletedRunID       *string                       `json:"completedRunId,omitempty"`
	CreatedAt            string                        `json:"createdAt"`
	StartedAt            *string                       `json:"startedAt,omitempty"`
	CompletedAt          *string                       `json:"completedAt,omitempty"`
	RequesterPrincipalID string                        `json:"-"`
	RequesterAccountID   string                        `json:"-"`
}

// AdminStoryGenerationJobCreateInput consists only of server-authorised facts.
type AdminStoryGenerationJobCreateInput struct {
	SourceVersionID      string
	RequesterPrincipalID string
	RequesterAccountID   string
}

func (input AdminStoryGenerationJobCreateInput) Validate() error {
	if !validAdminStoryGenerationJobUUID(input.SourceVersionID) ||
		!validAdminStoryGenerationJobUUID(input.RequesterPrincipalID) ||
		!validAdminStoryGenerationJobUUID(input.RequesterAccountID) {
		return fmt.Errorf("story generation job input is invalid")
	}
	return nil
}

func ValidAdminStoryGenerationJobStage(stage AdminStoryGenerationJobStage) bool {
	switch stage {
	case AdminStoryGenerationJobStageQueued,
		AdminStoryGenerationJobStageAnalysingSource,
		AdminStoryGenerationJobStageGeneratingConfidentReaders,
		AdminStoryGenerationJobStageGeneratingGrowingReaders,
		AdminStoryGenerationJobStageGeneratingStoryExplorers,
		AdminStoryGenerationJobStageGeneratingLittleListeners,
		AdminStoryGenerationJobStageValidatingConfidentReaders,
		AdminStoryGenerationJobStageValidatingGrowingReaders,
		AdminStoryGenerationJobStageValidatingStoryExplorers,
		AdminStoryGenerationJobStageValidatingLittleListeners,
		AdminStoryGenerationJobStageValidatingBundle,
		AdminStoryGenerationJobStageCompleted,
		AdminStoryGenerationJobStageFailed:
		return true
	default:
		return false
	}
}

// ValidAdminStoryGenerationJobFailureCode limits operational failure metadata
// to the API-safe categories; raw provider and database error text is never
// durable job state.
func ValidAdminStoryGenerationJobFailureCode(code string) bool {
	switch code {
	case "generation_timeout",
		"generation_unavailable",
		"generation_rate_limited",
		"generation_upstream_invalid",
		"generation_failed":
		return true
	default:
		return false
	}
}

func validAdminStoryGenerationJobUUID(value string) bool {
	return value == strings.ToLower(value) && validAdminStoryOrchestrationEditorialReviewUUID(value)
}
