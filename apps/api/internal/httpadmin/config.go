package httpadmin

import (
	"pandapages/api/internal/httpbearer"
	"pandapages/api/internal/sourceeligibility"
	"pandapages/api/internal/sourceprovider"
)

type Config struct {
	AdminKey                           string
	BearerAuthenticator                *httpbearer.Authenticator
	SourceDiscovery                    sourceprovider.Discovery
	SourceAcquisition                  sourceprovider.Acquisition
	SourceEligibility                  *sourceeligibility.Service
	StoryGeneration                    StoryGenerationService
	StoryOrchestrationRuns             StoryOrchestrationRunReader
	StoryOrchestrationRunHistory       StoryOrchestrationRunHistoryReader
	StoryOrchestrationEditorialReviews StoryOrchestrationEditorialReviewService
	StoryOrchestrationDraftIngests     StoryOrchestrationDraftIngestService
}
