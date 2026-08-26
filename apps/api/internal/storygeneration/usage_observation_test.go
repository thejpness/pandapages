package storygeneration

import "testing"

func TestResponsesUsageObservationValidationUsesOnlyAllowlistedOperations(t *testing.T) {
	valid := ResponsesUsageObservation{
		Operation:          ResponsesOperationValidateBundle,
		ProviderResponseID: "resp-safe",
		RequestedModel:     "requested-model",
		ReturnedModel:      "returned-model",
		Usage:              ResponsesUsage{InputTokens: 1, TotalTokens: 1},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid observation: %v", err)
	}

	for _, mutate := range []func(*ResponsesUsageObservation){
		func(value *ResponsesUsageObservation) { value.Operation = "prompt-derived-operation" },
		func(value *ResponsesUsageObservation) { value.ProviderResponseID = " " },
		func(value *ResponsesUsageObservation) { value.RequestedModel = " requested-model" },
		func(value *ResponsesUsageObservation) { value.ReturnedModel = " " },
		func(value *ResponsesUsageObservation) { value.Usage.TotalTokens = -1 },
	} {
		candidate := valid
		mutate(&candidate)
		if err := candidate.Validate(); err == nil {
			t.Fatalf("invalid observation unexpectedly accepted: %#v", candidate)
		}
	}
}
