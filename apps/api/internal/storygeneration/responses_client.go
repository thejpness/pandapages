package storygeneration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	openAIResponsesEndpoint = "https://api.openai.com/v1/responses"
	responsesRequestTimeout = 5 * time.Minute
	maxResponsesBodyBytes   = 4 << 20
	maxOutputTokensV2       = 128000
)

var (
	ErrOpenAIUnauthorized       = errors.New("OpenAI authentication failed")
	ErrOpenAIRateLimited        = errors.New("OpenAI rate limit exceeded")
	ErrOpenAIUnavailable        = errors.New("OpenAI Responses API unavailable")
	ErrOpenAIResponseInvalid    = errors.New("OpenAI response invalid")
	ErrOpenAIResponseIncomplete = errors.New("OpenAI response incomplete")
	ErrOpenAIResponseRefused    = errors.New("OpenAI response refused")
)

type ReasoningEffort string

const (
	ReasoningEffortNone   ReasoningEffort = "none"
	ReasoningEffortLow    ReasoningEffort = "low"
	ReasoningEffortMedium ReasoningEffort = "medium"
	ReasoningEffortHigh   ReasoningEffort = "high"
	ReasoningEffortXHigh  ReasoningEffort = "xhigh"
	ReasoningEffortMax    ReasoningEffort = "max"
)

type ResponsesClientConfig struct {
	APIKey     string
	HTTPClient *http.Client
}

type ResponsesClient struct {
	apiKey   string
	client   *http.Client
	endpoint *url.URL
}

type StructuredOutput struct {
	Name   string
	Schema json.RawMessage
}

type ResponsesCall struct {
	Model            string
	ReasoningEffort  ReasoningEffort
	MaxOutputTokens  int
	Prompt           Prompt
	StructuredOutput *StructuredOutput
}

type ResponsesUsage struct {
	InputTokens     int
	CachedTokens    int
	OutputTokens    int
	ReasoningTokens int
	TotalTokens     int
}

type ResponsesResult struct {
	ResponseID string
	Model      string
	OutputText string
	Usage      ResponsesUsage
}

func NewResponsesClient(cfg ResponsesClientConfig) (*ResponsesClient, error) {
	endpoint, _ := url.Parse(openAIResponsesEndpoint)
	return newResponsesClientWithEndpoint(cfg, endpoint)
}

func newResponsesClientWithEndpoint(cfg ResponsesClientConfig, endpoint *url.URL) (*ResponsesClient, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("OpenAI API key is required")
	}
	if endpoint == nil || endpoint.Scheme == "" || endpoint.Host == "" {
		return nil, fmt.Errorf("OpenAI Responses endpoint is invalid")
	}

	client := cfg.HTTPClient
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		client = &http.Client{Transport: transport}
	}
	copyClient := *client
	copyClient.Timeout = responsesRequestTimeout
	copyClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}

	copyEndpoint := *endpoint
	return &ResponsesClient{
		apiKey:   apiKey,
		client:   &copyClient,
		endpoint: &copyEndpoint,
	}, nil
}

func (client *ResponsesClient) Create(ctx context.Context, call ResponsesCall) (ResponsesResult, error) {
	if err := validateResponsesCall(call); err != nil {
		return ResponsesResult{}, err
	}

	body, err := buildResponsesRequestBody(call)
	if err != nil {
		return ResponsesResult{}, fmt.Errorf("build OpenAI Responses request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return ResponsesResult{}, fmt.Errorf("%w: build request", ErrOpenAIUnavailable)
	}
	request.Header.Set("Authorization", "Bearer "+client.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := client.client.Do(request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ResponsesResult{}, ctxErr
		}
		return ResponsesResult{}, fmt.Errorf("%w: request failed", ErrOpenAIUnavailable)
	}
	defer response.Body.Close()

	if err := classifyOpenAIHTTPStatus(response.StatusCode); err != nil {
		return ResponsesResult{}, err
	}
	if !validJSONContentType(response.Header.Get("Content-Type")) {
		return ResponsesResult{}, fmt.Errorf("%w: unexpected content type", ErrOpenAIResponseInvalid)
	}

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxResponsesBodyBytes+1))
	if err != nil {
		return ResponsesResult{}, fmt.Errorf("%w: read response", ErrOpenAIResponseInvalid)
	}
	if len(responseBody) > maxResponsesBodyBytes {
		return ResponsesResult{}, fmt.Errorf("%w: response exceeds %d bytes", ErrOpenAIResponseInvalid, maxResponsesBodyBytes)
	}

	return decodeResponsesResult(responseBody)
}

func validateResponsesCall(call ResponsesCall) error {
	if strings.TrimSpace(call.Model) == "" {
		return fmt.Errorf("OpenAI model is required")
	}
	if !validReasoningEffort(call.ReasoningEffort) {
		return fmt.Errorf("unsupported reasoning effort %q", call.ReasoningEffort)
	}
	if call.MaxOutputTokens < 1 || call.MaxOutputTokens > maxOutputTokensV2 {
		return fmt.Errorf("max output tokens must be between 1 and %d", maxOutputTokensV2)
	}
	if strings.TrimSpace(call.Prompt.DeveloperInstructions) == "" {
		return fmt.Errorf("developer instructions are required")
	}
	if !utf8.ValidString(call.Prompt.DeveloperInstructions) {
		return fmt.Errorf("developer instructions must be valid UTF-8")
	}
	if strings.TrimSpace(call.Prompt.UserInputJSON) == "" {
		return fmt.Errorf("user input is required")
	}
	if !utf8.ValidString(call.Prompt.UserInputJSON) {
		return fmt.Errorf("user input must be valid UTF-8")
	}
	trimmedInput := bytes.TrimSpace([]byte(call.Prompt.UserInputJSON))
	if len(trimmedInput) == 0 || trimmedInput[0] != '{' || !json.Valid(trimmedInput) {
		return fmt.Errorf("user input must be a JSON object")
	}
	if err := validateSingleJSONValueWithoutDuplicateKeys(trimmedInput); err != nil {
		return fmt.Errorf("user input JSON is invalid: %w", err)
	}
	if call.StructuredOutput != nil {
		if !validSchemaName(call.StructuredOutput.Name) {
			return fmt.Errorf("structured output name is invalid")
		}
		trimmedSchema := bytes.TrimSpace(call.StructuredOutput.Schema)
		if len(trimmedSchema) == 0 || trimmedSchema[0] != '{' || !json.Valid(trimmedSchema) {
			return fmt.Errorf("structured output schema must be a valid JSON object")
		}
		if err := validateSingleJSONValueWithoutDuplicateKeys(trimmedSchema); err != nil {
			return fmt.Errorf("structured output schema is invalid: %w", err)
		}
	}
	return nil
}

func validReasoningEffort(effort ReasoningEffort) bool {
	switch effort {
	case ReasoningEffortNone,
		ReasoningEffortLow,
		ReasoningEffortMedium,
		ReasoningEffortHigh,
		ReasoningEffortXHigh,
		ReasoningEffortMax:
		return true
	default:
		return false
	}
}

func validSchemaName(name string) bool {
	if len(name) < 1 || len(name) > 64 {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '_' ||
			r == '-' {
			continue
		}
		return false
	}
	return true
}

type responsesRequestBody struct {
	Model           string                  `json:"model"`
	Input           []responsesInputMessage `json:"input"`
	Reasoning       responsesReasoning      `json:"reasoning"`
	MaxOutputTokens int                     `json:"max_output_tokens"`
	Store           bool                    `json:"store"`
	Text            *responsesText          `json:"text,omitempty"`
}

type responsesInputMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responsesReasoning struct {
	Effort ReasoningEffort `json:"effort"`
}

type responsesText struct {
	Format responsesTextFormat `json:"format"`
}

type responsesTextFormat struct {
	Type   string          `json:"type"`
	Name   string          `json:"name"`
	Strict bool            `json:"strict"`
	Schema json.RawMessage `json:"schema"`
}

func buildResponsesRequestBody(call ResponsesCall) ([]byte, error) {
	body := responsesRequestBody{
		Model: call.Model,
		Input: []responsesInputMessage{
			{Role: "developer", Content: call.Prompt.DeveloperInstructions},
			{Role: "user", Content: call.Prompt.UserInputJSON},
		},
		Reasoning: responsesReasoning{
			Effort: call.ReasoningEffort,
		},
		MaxOutputTokens: call.MaxOutputTokens,
		Store:           false,
	}

	if call.StructuredOutput != nil {
		body.Text = &responsesText{
			Format: responsesTextFormat{
				Type:   "json_schema",
				Name:   call.StructuredOutput.Name,
				Strict: true,
				Schema: append(json.RawMessage(nil), call.StructuredOutput.Schema...),
			},
		}
	}

	return json.Marshal(body)
}

func classifyOpenAIHTTPStatus(status int) error {
	switch {
	case status >= http.StatusOK && status < http.StatusMultipleChoices:
		return nil
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return fmt.Errorf("%w: HTTP %d", ErrOpenAIUnauthorized, status)
	case status == http.StatusTooManyRequests:
		return fmt.Errorf("%w: HTTP %d", ErrOpenAIRateLimited, status)
	case status >= http.StatusInternalServerError:
		return fmt.Errorf("%w: HTTP %d", ErrOpenAIUnavailable, status)
	default:
		return fmt.Errorf("%w: HTTP %d", ErrOpenAIResponseInvalid, status)
	}
}

func validJSONContentType(raw string) bool {
	mediaType, _, err := mime.ParseMediaType(raw)
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

type responsesAPIResponse struct {
	ID                string                `json:"id"`
	Status            string                `json:"status"`
	Model             string                `json:"model"`
	IncompleteDetails *responsesIncomplete  `json:"incomplete_details"`
	Output            []responsesOutputItem `json:"output"`
	Usage             *responsesAPIUsage    `json:"usage"`
}

type responsesIncomplete struct {
	Reason string `json:"reason"`
}

type responsesOutputItem struct {
	Type    string                 `json:"type"`
	Content []responsesContentItem `json:"content"`
}

type responsesContentItem struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Refusal string `json:"refusal"`
}

type responsesAPIUsage struct {
	InputTokens        int                         `json:"input_tokens"`
	OutputTokens       int                         `json:"output_tokens"`
	TotalTokens        int                         `json:"total_tokens"`
	InputTokenDetails  responsesInputTokenDetails  `json:"input_tokens_details"`
	OutputTokenDetails responsesOutputTokenDetails `json:"output_tokens_details"`
}

type responsesInputTokenDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type responsesOutputTokenDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

func decodeResponsesResult(data []byte) (ResponsesResult, error) {
	if !utf8.Valid(data) {
		return ResponsesResult{}, fmt.Errorf("%w: response is not valid UTF-8", ErrOpenAIResponseInvalid)
	}
	if err := validateSingleJSONValueWithoutDuplicateKeys(data); err != nil {
		return ResponsesResult{}, fmt.Errorf("%w: invalid JSON boundary", ErrOpenAIResponseInvalid)
	}

	var response responsesAPIResponse
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&response); err != nil {
		return ResponsesResult{}, fmt.Errorf("%w: decode JSON", ErrOpenAIResponseInvalid)
	}

	if response.Status != "completed" {
		reason := ""
		if response.IncompleteDetails != nil {
			reason = strings.TrimSpace(response.IncompleteDetails.Reason)
		}
		if reason == "" {
			return ResponsesResult{}, fmt.Errorf("%w: status %q", ErrOpenAIResponseIncomplete, response.Status)
		}
		return ResponsesResult{}, fmt.Errorf("%w: %s", ErrOpenAIResponseIncomplete, reason)
	}
	if strings.TrimSpace(response.ID) == "" || strings.TrimSpace(response.Model) == "" {
		return ResponsesResult{}, fmt.Errorf("%w: missing response identity", ErrOpenAIResponseInvalid)
	}

	outputTexts := make([]string, 0, 1)
	for _, output := range response.Output {
		if output.Type != "message" {
			continue
		}
		for _, content := range output.Content {
			switch content.Type {
			case "refusal":
				return ResponsesResult{}, fmt.Errorf("%w", ErrOpenAIResponseRefused)
			case "output_text":
				if strings.TrimSpace(content.Text) == "" {
					return ResponsesResult{}, fmt.Errorf("%w: empty output text", ErrOpenAIResponseInvalid)
				}
				outputTexts = append(outputTexts, content.Text)
			}
		}
	}
	if len(outputTexts) != 1 {
		return ResponsesResult{}, fmt.Errorf("%w: expected exactly one output_text item, got %d", ErrOpenAIResponseInvalid, len(outputTexts))
	}

	if response.Usage == nil {
		return ResponsesResult{}, fmt.Errorf("%w: missing token usage", ErrOpenAIResponseInvalid)
	}
	if response.Usage.InputTokens < 0 ||
		response.Usage.OutputTokens < 0 ||
		response.Usage.TotalTokens < 0 ||
		response.Usage.InputTokenDetails.CachedTokens < 0 ||
		response.Usage.OutputTokenDetails.ReasoningTokens < 0 {
		return ResponsesResult{}, fmt.Errorf("%w: negative token usage", ErrOpenAIResponseInvalid)
	}

	return ResponsesResult{
		ResponseID: response.ID,
		Model:      response.Model,
		OutputText: outputTexts[0],
		Usage: ResponsesUsage{
			InputTokens:     response.Usage.InputTokens,
			CachedTokens:    response.Usage.InputTokenDetails.CachedTokens,
			OutputTokens:    response.Usage.OutputTokens,
			ReasoningTokens: response.Usage.OutputTokenDetails.ReasoningTokens,
			TotalTokens:     response.Usage.TotalTokens,
		},
	}, nil
}
