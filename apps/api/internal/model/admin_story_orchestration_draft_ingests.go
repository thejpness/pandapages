package model

import "errors"

var (
	ErrAdminStoryOrchestrationDraftIngestInvalid  = errors.New("story orchestration draft ingest is invalid")
	ErrAdminStoryOrchestrationDraftIngestConflict = errors.New("story orchestration draft ingest conflicts with retained state")
)

// AdminStoryOrchestrationDraftIngestRequest is the entire browser-controlled
// request. The exact run comes from the route; all content and target-story
// facts are loaded from retained server-side evidence.
type AdminStoryOrchestrationDraftIngestRequest struct {
	EditorialReviewID string `json:"editorialReviewId"`
}

func (request AdminStoryOrchestrationDraftIngestRequest) Validate() error {
	if !validAdminStoryOrchestrationEditorialReviewUUID(request.EditorialReviewID) {
		return ErrAdminStoryOrchestrationDraftIngestInvalid
	}
	return nil
}

// AdminStoryOrchestrationDraftIngestInput is validated server-owned input to
// the single transactional ingest boundary.
type AdminStoryOrchestrationDraftIngestInput struct {
	RunID             string
	EditorialReviewID string
}

func (input AdminStoryOrchestrationDraftIngestInput) Validate() error {
	if !validAdminStoryOrchestrationEditorialReviewUUID(input.RunID) ||
		!validAdminStoryOrchestrationEditorialReviewUUID(input.EditorialReviewID) {
		return ErrAdminStoryOrchestrationDraftIngestInvalid
	}
	return nil
}

type AdminStoryOrchestrationDraftIngestOutcome string

const (
	AdminStoryOrchestrationDraftIngestOutcomeCreated AdminStoryOrchestrationDraftIngestOutcome = "created"
	AdminStoryOrchestrationDraftIngestOutcomeReused  AdminStoryOrchestrationDraftIngestOutcome = "reused"
)

// AdminStoryOrchestrationDraftIngestEdition records one exact initial editable
// story-version snapshot created by an immutable ingest event.
type AdminStoryOrchestrationDraftIngestEdition struct {
	EditionKey     AdminStoryEditionKey `json:"editionKey"`
	EditionID      string               `json:"editionId"`
	StoryVersionID string               `json:"storyVersionId"`
}

// AdminStoryOrchestrationDraftIngestResponse deliberately omits generated
// content, orchestration artifacts, reviewer provenance, and release state.
// It proves only that editable working copies now exist.
type AdminStoryOrchestrationDraftIngestResponse struct {
	ID                string                                      `json:"id"`
	RunID             string                                      `json:"runId"`
	EditorialReviewID string                                      `json:"editorialReviewId"`
	StorySlug         string                                      `json:"storySlug"`
	CreatedAt         string                                      `json:"createdAt"`
	Outcome           AdminStoryOrchestrationDraftIngestOutcome   `json:"outcome"`
	Editions          []AdminStoryOrchestrationDraftIngestEdition `json:"editions"`
}
