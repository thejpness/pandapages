package model

import "testing"

func TestValidAdminStoryOrchestrationEditorialReviewUUID(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		want  bool
	}{
		{name: "canonical valid", value: "11111111-1111-4111-8111-111111111111", want: true},
		{name: "malformed", value: "not-a-uuid", want: false},
		{name: "zero", value: adminStoryOrchestrationEditorialReviewZeroUUID, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := validAdminStoryOrchestrationEditorialReviewUUID(test.value); got != test.want {
				t.Fatalf("validAdminStoryOrchestrationEditorialReviewUUID(%q) = %v, want %v", test.value, got, test.want)
			}
		})
	}
}
