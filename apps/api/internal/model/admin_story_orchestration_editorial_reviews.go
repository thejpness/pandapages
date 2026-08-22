package model

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var (
	ErrAdminStoryOrchestrationRunRepairRequired      = errors.New("stored story orchestration run requires repair")
	ErrAdminStoryOrchestrationEditorialReviewInvalid = errors.New("story orchestration editorial review is invalid")
	adminStoryOrchestrationEditorialReviewUUID       = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

const adminStoryOrchestrationEditorialReviewZeroUUID = "00000000-0000-0000-0000-000000000000"

type AdminStoryOrchestrationEditorialDecision string

const (
	AdminStoryOrchestrationEditorialDecisionApproved AdminStoryOrchestrationEditorialDecision = "approved"
	AdminStoryOrchestrationEditorialDecisionRejected AdminStoryOrchestrationEditorialDecision = "rejected"
)

// ValidAdminStoryOrchestrationEditorialDecision reports whether a value is a
// human editorial decision. It deliberately does not encode a machine result.
func ValidAdminStoryOrchestrationEditorialDecision(value AdminStoryOrchestrationEditorialDecision) bool {
	return value == AdminStoryOrchestrationEditorialDecisionApproved ||
		value == AdminStoryOrchestrationEditorialDecisionRejected
}

// AdminStoryOrchestrationEditorialReview is one immutable human decision
// bound to an exact persisted orchestration run. Reviewer identifiers are
// durable audit provenance and intentionally never serialised to browser API
// responses.
type AdminStoryOrchestrationEditorialReview struct {
	ID                  string                                   `json:"id"`
	RunID               string                                   `json:"runId"`
	Decision            AdminStoryOrchestrationEditorialDecision `json:"decision"`
	ReviewerPrincipalID string                                   `json:"-"`
	ReviewerAccountID   string                                   `json:"-"`
	CreatedAt           string                                   `json:"createdAt"`
}

type AdminStoryOrchestrationEditorialReviewsListResponse struct {
	Items []AdminStoryOrchestrationEditorialReview `json:"items"`
}

// AdminStoryOrchestrationEditorialReviewCreateRequest is the strictly narrow
// browser request. The authenticated reviewer identity is never accepted from
// this body.
type AdminStoryOrchestrationEditorialReviewCreateRequest struct {
	Decision AdminStoryOrchestrationEditorialDecision `json:"decision"`
}

// AdminStoryOrchestrationEditorialReviewCreateInput is server-owned input to
// persistence after the admin boundary has established reviewer provenance.
type AdminStoryOrchestrationEditorialReviewCreateInput struct {
	RunID               string
	Decision            AdminStoryOrchestrationEditorialDecision
	ReviewerPrincipalID string
	ReviewerAccountID   string
}

func (request AdminStoryOrchestrationEditorialReviewCreateRequest) Validate() error {
	if !ValidAdminStoryOrchestrationEditorialDecision(request.Decision) {
		return ErrAdminStoryOrchestrationEditorialReviewInvalid
	}
	return nil
}

func (input AdminStoryOrchestrationEditorialReviewCreateInput) Validate() error {
	if !validAdminStoryOrchestrationEditorialReviewUUID(input.RunID) ||
		!ValidAdminStoryOrchestrationEditorialDecision(input.Decision) ||
		!validAdminStoryOrchestrationEditorialReviewUUID(input.ReviewerPrincipalID) ||
		!validAdminStoryOrchestrationEditorialReviewUUID(input.ReviewerAccountID) {
		return ErrAdminStoryOrchestrationEditorialReviewInvalid
	}
	return nil
}

func (review AdminStoryOrchestrationEditorialReview) Validate() error {
	if !validAdminStoryOrchestrationEditorialReviewUUID(review.ID) ||
		!validAdminStoryOrchestrationEditorialReviewUUID(review.RunID) ||
		!ValidAdminStoryOrchestrationEditorialDecision(review.Decision) ||
		!validAdminStoryOrchestrationEditorialReviewUUID(review.ReviewerPrincipalID) ||
		!validAdminStoryOrchestrationEditorialReviewUUID(review.ReviewerAccountID) {
		return ErrAdminStoryOrchestrationEditorialReviewInvalid
	}
	if createdAt, err := time.Parse(time.RFC3339Nano, review.CreatedAt); err != nil || createdAt.IsZero() {
		return ErrAdminStoryOrchestrationEditorialReviewInvalid
	}
	return nil
}

func validAdminStoryOrchestrationEditorialReviewUUID(value string) bool {
	return value != adminStoryOrchestrationEditorialReviewZeroUUID &&
		value == strings.ToLower(value) &&
		adminStoryOrchestrationEditorialReviewUUID.MatchString(value)
}
