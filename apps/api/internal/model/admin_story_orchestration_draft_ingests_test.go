package model

import "testing"

func TestAdminStoryOrchestrationDraftIngestInputValidation(t *testing.T) {
	valid := AdminStoryOrchestrationDraftIngestInput{
		RunID:             "11111111-1111-4111-8111-111111111111",
		EditorialReviewID: "22222222-2222-4222-8222-222222222222",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	for _, input := range []AdminStoryOrchestrationDraftIngestInput{
		{RunID: "not-a-uuid", EditorialReviewID: valid.EditorialReviewID},
		{RunID: adminStoryOrchestrationEditorialReviewZeroUUID, EditorialReviewID: valid.EditorialReviewID},
		{RunID: valid.RunID, EditorialReviewID: "not-a-uuid"},
		{RunID: valid.RunID, EditorialReviewID: adminStoryOrchestrationEditorialReviewZeroUUID},
	} {
		if err := input.Validate(); err == nil {
			t.Fatalf("invalid input accepted: %#v", input)
		}
	}
}
