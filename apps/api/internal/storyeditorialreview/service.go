// Package storyeditorialreview composes immutable completed-run validation and
// append-only human editorial-review persistence. It deliberately owns no
// HTTP, authorization policy, database transaction spanning both operations,
// or generated-content mutation.
package storyeditorialreview

import (
	"fmt"

	"pandapages/api/internal/model"
	"pandapages/api/internal/storyorchestration"
)

// ValidatedRunReader returns complete retained evidence only after its exact
// immutable source binding and artifacts validate.
type ValidatedRunReader interface {
	GetCompletedStoryOrchestrationRun(string) (storyorchestration.PersistedRun, error)
}

type EditorialReviewWriter interface {
	CreateStoryOrchestrationEditorialReview(model.AdminStoryOrchestrationEditorialReviewCreateInput) (model.AdminStoryOrchestrationEditorialReview, error)
}

type EditorialReviewReader interface {
	ListStoryOrchestrationEditorialReviews(string, int) (model.AdminStoryOrchestrationEditorialReviewsListResponse, error)
}

type Config struct {
	ValidatedRunReader ValidatedRunReader
	Writer             EditorialReviewWriter
	Reader             EditorialReviewReader
}

// Service is the small application seam that prevents a human decision from
// being persisted unless the exact completed run revalidates first.
type Service struct {
	validatedRunReader ValidatedRunReader
	writer             EditorialReviewWriter
	reader             EditorialReviewReader
}

func New(cfg Config) (*Service, error) {
	if cfg.ValidatedRunReader == nil {
		return nil, fmt.Errorf("validated orchestration run reader is required")
	}
	if cfg.Writer == nil {
		return nil, fmt.Errorf("editorial review writer is required")
	}
	if cfg.Reader == nil {
		return nil, fmt.Errorf("editorial review reader is required")
	}
	return &Service{
		validatedRunReader: cfg.ValidatedRunReader,
		writer:             cfg.Writer,
		reader:             cfg.Reader,
	}, nil
}

func (service *Service) Create(
	input model.AdminStoryOrchestrationEditorialReviewCreateInput,
) (model.AdminStoryOrchestrationEditorialReview, error) {
	if err := input.Validate(); err != nil {
		return model.AdminStoryOrchestrationEditorialReview{}, err
	}
	if _, err := service.validatedRunReader.GetCompletedStoryOrchestrationRun(input.RunID); err != nil {
		return model.AdminStoryOrchestrationEditorialReview{}, err
	}
	review, err := service.writer.CreateStoryOrchestrationEditorialReview(input)
	if err != nil {
		return model.AdminStoryOrchestrationEditorialReview{}, err
	}
	if err := review.Validate(); err != nil || review.RunID != input.RunID || review.Decision != input.Decision ||
		review.ReviewerPrincipalID != input.ReviewerPrincipalID || review.ReviewerAccountID != input.ReviewerAccountID {
		return model.AdminStoryOrchestrationEditorialReview{}, fmt.Errorf("persisted story orchestration editorial review does not match its request")
	}
	return review, nil
}

func (service *Service) List(
	runID string,
	limit int,
) (model.AdminStoryOrchestrationEditorialReviewsListResponse, error) {
	return service.reader.ListStoryOrchestrationEditorialReviews(runID, limit)
}
