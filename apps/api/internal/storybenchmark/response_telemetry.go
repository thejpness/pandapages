package storybenchmark

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"pandapages/api/internal/storygeneration"
)

type ResponseTelemetryStatus string

const (
	ResponseTelemetrySucceeded ResponseTelemetryStatus = "succeeded"
	ResponseTelemetryFailed    ResponseTelemetryStatus = "failed"
)

type ResponseObservation struct {
	Sequence             int                             `json:"sequence"`
	Status               ResponseTelemetryStatus         `json:"status"`
	RequestedModel       string                          `json:"requestedModel"`
	ReturnedModel        string                          `json:"returnedModel,omitempty"`
	ReasoningEffort      storygeneration.ReasoningEffort `json:"reasoningEffort"`
	MaxOutputTokens      int                             `json:"maxOutputTokens"`
	StructuredOutputName string                          `json:"structuredOutputName,omitempty"`
	ResponseID           string                          `json:"responseId,omitempty"`
	Error                string                          `json:"error,omitempty"`
	Usage                TokenUsage                      `json:"usage"`
}

type ResponseModelUsage struct {
	RequestedModel      string     `json:"requestedModel"`
	AttemptedRequests   int        `json:"attemptedRequests"`
	SuccessfulResponses int        `json:"successfulResponses"`
	FailedRequests      int        `json:"failedRequests"`
	Usage               TokenUsage `json:"usage"`
}

type ResponseTelemetry struct {
	AttemptedRequests   int                   `json:"attemptedRequests"`
	SuccessfulResponses int                   `json:"successfulResponses"`
	FailedRequests      int                   `json:"failedRequests"`
	Usage               TokenUsage            `json:"usage"`
	ByRequestedModel    []ResponseModelUsage  `json:"byRequestedModel"`
	Observations        []ResponseObservation `json:"observations"`
}

type RecordingResponsesGateway struct {
	inner storygeneration.ResponsesGateway

	mu           sync.Mutex
	observations []ResponseObservation
}

func NewRecordingResponsesGateway(inner storygeneration.ResponsesGateway) (*RecordingResponsesGateway, error) {
	if inner == nil {
		return nil, fmt.Errorf("Responses gateway is required")
	}
	return &RecordingResponsesGateway{inner: inner}, nil
}

func (gateway *RecordingResponsesGateway) Create(ctx context.Context, call storygeneration.ResponsesCall) (storygeneration.ResponsesResult, error) {
	result, err := gateway.inner.Create(ctx, call)

	observation := ResponseObservation{
		Status:          ResponseTelemetrySucceeded,
		RequestedModel:  strings.TrimSpace(call.Model),
		ReasoningEffort: call.ReasoningEffort,
		MaxOutputTokens: call.MaxOutputTokens,
	}
	if call.StructuredOutput != nil {
		observation.StructuredOutputName = strings.TrimSpace(call.StructuredOutput.Name)
	}
	if err != nil {
		observation.Status = ResponseTelemetryFailed
		observation.Error = err.Error()
	} else {
		observation.ReturnedModel = strings.TrimSpace(result.Model)
		observation.ResponseID = strings.TrimSpace(result.ResponseID)
		observation.Usage = tokenUsageFromResponses(result.Usage)
	}

	gateway.mu.Lock()
	observation.Sequence = len(gateway.observations) + 1
	gateway.observations = append(gateway.observations, observation)
	gateway.mu.Unlock()

	return result, err
}

func (gateway *RecordingResponsesGateway) Snapshot() ResponseTelemetry {
	if gateway == nil {
		return ResponseTelemetry{}
	}
	gateway.mu.Lock()
	observations := append([]ResponseObservation(nil), gateway.observations...)
	gateway.mu.Unlock()
	return summarizeResponseObservations(observations)
}

func (telemetry ResponseTelemetry) Validate() error {
	if telemetry.AttemptedRequests != len(telemetry.Observations) {
		return fmt.Errorf("Responses telemetry attempted request count does not match observations")
	}
	if telemetry.SuccessfulResponses+telemetry.FailedRequests != telemetry.AttemptedRequests {
		return fmt.Errorf("Responses telemetry status counts do not match attempted requests")
	}

	expected := summarizeResponseObservations(telemetry.Observations)
	if telemetry.SuccessfulResponses != expected.SuccessfulResponses ||
		telemetry.FailedRequests != expected.FailedRequests ||
		telemetry.Usage != expected.Usage ||
		!sameResponseModelUsage(telemetry.ByRequestedModel, expected.ByRequestedModel) {
		return fmt.Errorf("Responses telemetry summary does not match observations")
	}

	for index, observation := range telemetry.Observations {
		if observation.Sequence != index+1 {
			return fmt.Errorf("Responses telemetry observation sequence is invalid")
		}
		switch observation.Status {
		case ResponseTelemetrySucceeded:
			if strings.TrimSpace(observation.RequestedModel) == "" {
				return fmt.Errorf("successful Responses telemetry observation %d requested model is required", index+1)
			}
			if !validBenchmarkReasoningEffort(observation.ReasoningEffort) {
				return fmt.Errorf("successful Responses telemetry observation %d reasoning effort is invalid", index+1)
			}
			if observation.MaxOutputTokens < 1 {
				return fmt.Errorf("successful Responses telemetry observation %d max output tokens must be positive", index+1)
			}
			if strings.TrimSpace(observation.Error) != "" {
				return fmt.Errorf("successful Responses telemetry observation %d must not contain an error", index+1)
			}
			if strings.TrimSpace(observation.ReturnedModel) == "" || strings.TrimSpace(observation.ResponseID) == "" {
				return fmt.Errorf("successful Responses telemetry observation %d response identity is required", index+1)
			}
		case ResponseTelemetryFailed:
			if strings.TrimSpace(observation.Error) == "" {
				return fmt.Errorf("failed Responses telemetry observation %d error is required", index+1)
			}
			if observation.Usage != (TokenUsage{}) {
				return fmt.Errorf("failed Responses telemetry observation %d must not claim token usage", index+1)
			}
		default:
			return fmt.Errorf("Responses telemetry observation %d status %q is invalid", index+1, observation.Status)
		}
	}
	return nil
}

func summarizeResponseObservations(observations []ResponseObservation) ResponseTelemetry {
	telemetry := ResponseTelemetry{
		AttemptedRequests: len(observations),
		Observations:      append([]ResponseObservation(nil), observations...),
	}
	byModel := make(map[string]*ResponseModelUsage)
	for _, observation := range observations {
		model := strings.TrimSpace(observation.RequestedModel)
		usage := byModel[model]
		if usage == nil {
			usage = &ResponseModelUsage{RequestedModel: model}
			byModel[model] = usage
		}
		usage.AttemptedRequests++

		switch observation.Status {
		case ResponseTelemetrySucceeded:
			telemetry.SuccessfulResponses++
			usage.SuccessfulResponses++
			addTokenUsage(&telemetry.Usage, observation.Usage)
			addTokenUsage(&usage.Usage, observation.Usage)
		case ResponseTelemetryFailed:
			telemetry.FailedRequests++
			usage.FailedRequests++
		}
	}

	models := make([]string, 0, len(byModel))
	for model := range byModel {
		models = append(models, model)
	}
	sort.Strings(models)
	telemetry.ByRequestedModel = make([]ResponseModelUsage, 0, len(models))
	for _, model := range models {
		telemetry.ByRequestedModel = append(telemetry.ByRequestedModel, *byModel[model])
	}
	return telemetry
}

func sameResponseModelUsage(left, right []ResponseModelUsage) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func optionalResponseTelemetry(values []ResponseTelemetry) (ResponseTelemetry, error) {
	if len(values) > 1 {
		return ResponseTelemetry{}, fmt.Errorf("at most one Responses telemetry snapshot may be supplied")
	}
	if len(values) == 0 {
		return ResponseTelemetry{}, nil
	}
	if err := values[0].Validate(); err != nil {
		return ResponseTelemetry{}, err
	}
	return summarizeResponseObservations(values[0].Observations), nil
}
