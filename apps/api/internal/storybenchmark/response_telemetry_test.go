package storybenchmark

import (
	"context"
	"errors"
	"testing"

	"pandapages/api/internal/storygeneration"
)

type scriptedTelemetryGateway struct {
	results []storygeneration.ResponsesResult
	errors  []error
	calls   int
}

func (gateway *scriptedTelemetryGateway) Create(_ context.Context, _ storygeneration.ResponsesCall) (storygeneration.ResponsesResult, error) {
	index := gateway.calls
	gateway.calls++
	if index >= len(gateway.results) || index >= len(gateway.errors) {
		return storygeneration.ResponsesResult{}, errors.New("unexpected telemetry test call")
	}
	return gateway.results[index], gateway.errors[index]
}

func TestRecordingResponsesGatewayRetainsUsageWhenDownstreamCouldRejectResponse(t *testing.T) {
	inner := &scriptedTelemetryGateway{
		results: []storygeneration.ResponsesResult{
			{
				ResponseID: "resp_valid_transport_invalid_evidence",
				Model:      "gpt-5.6-luna",
				OutputText: `{"result":"fail","findings":[]}`,
				Usage: storygeneration.ResponsesUsage{
					InputTokens:     100,
					CachedTokens:    80,
					OutputTokens:    20,
					ReasoningTokens: 5,
					TotalTokens:     120,
				},
			},
			{},
		},
		errors: []error{nil, errors.New("synthetic transport failure")},
	}
	recorder, err := NewRecordingResponsesGateway(inner)
	if err != nil {
		t.Fatalf("NewRecordingResponsesGateway() error = %v", err)
	}

	_, err = recorder.Create(context.Background(), storygeneration.ResponsesCall{
		Model:            "gpt-5.6-luna",
		ReasoningEffort:  storygeneration.ReasoningEffortMedium,
		MaxOutputTokens:  8192,
		StructuredOutput: &storygeneration.StructuredOutput{Name: "semantic_assessment"},
	})
	if err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	_, err = recorder.Create(context.Background(), storygeneration.ResponsesCall{
		Model:           "gpt-5.6-terra",
		ReasoningEffort: storygeneration.ReasoningEffortHigh,
		MaxOutputTokens: 4096,
	})
	if err == nil {
		t.Fatal("second Create() unexpectedly succeeded")
	}

	snapshot := recorder.Snapshot()
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("Snapshot().Validate() error = %v", err)
	}
	if snapshot.AttemptedRequests != 2 || snapshot.SuccessfulResponses != 1 || snapshot.FailedRequests != 1 {
		t.Fatalf("snapshot counts = %#v", snapshot)
	}
	if snapshot.Usage.TotalTokens != 120 || snapshot.Usage.CachedTokens != 80 {
		t.Fatalf("snapshot usage = %#v", snapshot.Usage)
	}
	if len(snapshot.Observations) != 2 {
		t.Fatalf("observations = %#v", snapshot.Observations)
	}
	first := snapshot.Observations[0]
	if first.ResponseID != "resp_valid_transport_invalid_evidence" || first.StructuredOutputName != "semantic_assessment" || first.Status != ResponseTelemetrySucceeded {
		t.Fatalf("first observation = %#v", first)
	}
	second := snapshot.Observations[1]
	if second.Status != ResponseTelemetryFailed || second.Error != "synthetic transport failure" || second.Usage != (TokenUsage{}) {
		t.Fatalf("second observation = %#v", second)
	}
	if len(snapshot.ByRequestedModel) != 2 || snapshot.ByRequestedModel[0].RequestedModel != "gpt-5.6-luna" || snapshot.ByRequestedModel[1].RequestedModel != "gpt-5.6-terra" {
		t.Fatalf("by model = %#v", snapshot.ByRequestedModel)
	}
}

func TestResponseTelemetryRejectsTamperedSummary(t *testing.T) {
	telemetry := ResponseTelemetry{
		AttemptedRequests:   1,
		SuccessfulResponses: 1,
		Usage:               TokenUsage{TotalTokens: 999},
		Observations: []ResponseObservation{
			{
				Sequence:        1,
				Status:          ResponseTelemetrySucceeded,
				RequestedModel:  "gpt-5.6-terra",
				ReturnedModel:   "gpt-5.6-terra",
				ReasoningEffort: storygeneration.ReasoningEffortMedium,
				MaxOutputTokens: 8192,
				ResponseID:      "resp_1",
				Usage:           TokenUsage{TotalTokens: 12},
			},
		},
	}
	if err := telemetry.Validate(); err == nil {
		t.Fatal("tampered telemetry unexpectedly validated")
	}
}
