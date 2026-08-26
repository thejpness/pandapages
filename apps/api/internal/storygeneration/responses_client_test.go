package storygeneration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestResponsesClient(t *testing.T, handler http.Handler) (*ResponsesClient, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	endpoint, err := url.Parse(server.URL + "/v1/responses")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	client, err := newResponsesClientWithEndpoint(ResponsesClientConfig{APIKey: "test-key"}, endpoint)
	if err != nil {
		t.Fatalf("newResponsesClientWithEndpoint() error = %v", err)
	}
	return client, server
}

func validResponsesCall() ResponsesCall {
	return ResponsesCall{
		Operation:       ResponsesOperationAnalyseSource,
		Model:           GenerationModelV2,
		ReasoningEffort: ReasoningEffortMedium,
		MaxOutputTokens: 4096,
		Prompt: Prompt{
			Version:               SourceAnalysisPromptVersionV2,
			DeveloperInstructions: "Analyse the source.",
			UserInputJSON:         `{"canonicalSource":"# Story"}`,
		},
	}
}

type recordingResponsesUsageRecorder struct {
	events []ResponsesUsageObservation
	err    error
}

func (recorder *recordingResponsesUsageRecorder) RecordResponsesUsage(_ context.Context, observation ResponsesUsageObservation) error {
	recorder.events = append(recorder.events, observation)
	return recorder.err
}

func completedResponseJSON(output string) string {
	encoded, _ := json.Marshal(output)
	return fmt.Sprintf(`{
		"id":"resp_test",
		"status":"completed",
		"model":"gpt-5.6-terra",
		"output":[{
			"type":"message",
			"content":[{"type":"output_text","text":%s}]
		}],
		"usage":{
			"input_tokens":120,
			"input_tokens_details":{"cached_tokens":20},
			"output_tokens":80,
			"output_tokens_details":{"reasoning_tokens":15},
			"total_tokens":200
		}
	}`, encoded)
}

func responseJSONWithUsage(status, output string) string {
	return fmt.Sprintf(`{
		"id":"resp_observed",
		"status":%q,
		"model":"gpt-5.6-terra",
		"output":%s,
		"usage":{
			"input_tokens":120,
			"output_tokens":40,
			"total_tokens":160,
			"input_tokens_details":{"cached_tokens":20},
			"output_tokens_details":{"reasoning_tokens":10}
		}
	}`, status, output)
}

func TestNewResponsesClientRequiresAPIKey(t *testing.T) {
	_, err := NewResponsesClient(ResponsesClientConfig{})
	if err == nil || !strings.Contains(err.Error(), "API key is required") {
		t.Fatalf("NewResponsesClient() error = %v", err)
	}
}

func TestResponsesClientCreateSendsStrictStructuredOutputsRequest(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"value":{"type":"string"}},"required":["value"]}`)
	call := validResponsesCall()
	call.StructuredOutput = &StructuredOutput{
		Name:   "story_analysis_v2",
		Schema: schema,
	}

	var captured map[string]any
	client, _ := newTestResponsesClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/responses" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("ReadAll() error = %v", err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Errorf("json.Unmarshal(request) error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, completedResponseJSON(`{"value":"ok"}`))
	}))

	result, err := client.Create(context.Background(), call)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.OutputText != `{"value":"ok"}` {
		t.Fatalf("OutputText = %q", result.OutputText)
	}
	if result.ResponseID != "resp_test" || result.Model != GenerationModelV2 {
		t.Fatalf("identity = %#v", result)
	}
	if result.Usage.InputTokens != 120 ||
		result.Usage.CachedTokens != 20 ||
		result.Usage.OutputTokens != 80 ||
		result.Usage.ReasoningTokens != 15 ||
		result.Usage.TotalTokens != 200 {
		t.Fatalf("usage = %#v", result.Usage)
	}

	if captured["model"] != GenerationModelV2 {
		t.Fatalf("model = %v", captured["model"])
	}
	if captured["store"] != false {
		t.Fatalf("store = %v, want false", captured["store"])
	}
	if captured["max_output_tokens"] != float64(4096) {
		t.Fatalf("max_output_tokens = %v", captured["max_output_tokens"])
	}

	reasoning := captured["reasoning"].(map[string]any)
	if reasoning["effort"] != "medium" {
		t.Fatalf("reasoning effort = %v", reasoning["effort"])
	}

	input := captured["input"].([]any)
	if len(input) != 2 {
		t.Fatalf("input length = %d", len(input))
	}
	developer := input[0].(map[string]any)
	user := input[1].(map[string]any)
	if developer["role"] != "developer" || developer["content"] != call.Prompt.DeveloperInstructions {
		t.Fatalf("developer input = %#v", developer)
	}
	if user["role"] != "user" || user["content"] != call.Prompt.UserInputJSON {
		t.Fatalf("user input = %#v", user)
	}

	text := captured["text"].(map[string]any)
	format := text["format"].(map[string]any)
	if format["type"] != "json_schema" ||
		format["name"] != "story_analysis_v2" ||
		format["strict"] != true {
		t.Fatalf("format = %#v", format)
	}
	if _, ok := format["schema"].(map[string]any); !ok {
		t.Fatalf("schema = %#v", format["schema"])
	}
}

func TestResponsesClientCreateOmitsTextFormatForMarkdownOutput(t *testing.T) {
	var captured map[string]any
	client, _ := newTestResponsesClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, completedResponseJSON("# Story\n\nGenerated story."))
	}))

	if _, err := client.Create(context.Background(), validResponsesCall()); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, exists := captured["text"]; exists {
		t.Fatal("plain Markdown request must omit text.format")
	}
}

func TestResponsesClientRecordsTrustedUsageBeforeStrictResponseValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
		want error
	}{
		{
			name: "incomplete response",
			body: responseJSONWithUsage("incomplete", `[]`),
			want: ErrOpenAIResponseIncomplete,
		},
		{
			name: "refusal",
			body: responseJSONWithUsage("completed", `[{"type":"message","content":[{"type":"refusal","refusal":"no"}]}]`),
			want: ErrOpenAIResponseRefused,
		},
		{
			name: "missing output text",
			body: responseJSONWithUsage("completed", `[]`),
			want: ErrOpenAIResponseInvalid,
		},
		{
			name: "multiple output text values",
			body: responseJSONWithUsage("completed", `[{"type":"message","content":[{"type":"output_text","text":"first"},{"type":"output_text","text":"second"}]}]`),
			want: ErrOpenAIResponseInvalid,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, _ := newTestResponsesClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, test.body)
			}))
			recorder := &recordingResponsesUsageRecorder{}
			_, err := client.Create(WithResponsesUsageRecorder(context.Background(), recorder), validResponsesCall())
			if !errors.Is(err, test.want) {
				t.Fatalf("Create() error = %v, want errors.Is(..., %v)", err, test.want)
			}
			if len(recorder.events) != 1 {
				t.Fatalf("usage events = %#v, want exactly one", recorder.events)
			}
			event := recorder.events[0]
			if event.Operation != ResponsesOperationAnalyseSource || event.ProviderResponseID != "resp_observed" ||
				event.RequestedModel != GenerationModelV2 || event.ReturnedModel != "gpt-5.6-terra" ||
				event.Usage != (ResponsesUsage{InputTokens: 120, CachedTokens: 20, OutputTokens: 40, ReasoningTokens: 10, TotalTokens: 160}) {
				t.Fatalf("usage event = %#v", event)
			}
		})
	}
}

func TestResponsesClientDoesNotRecordUntrustedUsage(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "missing usage", body: `{"id":"resp_missing_usage","status":"completed","model":"gpt-5.6-terra","output":[]}`},
		{name: "missing provider response ID", body: `{"status":"completed","model":"gpt-5.6-terra","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}}`},
		{name: "negative usage", body: `{"id":"resp_negative","status":"completed","model":"gpt-5.6-terra","output":[],"usage":{"input_tokens":-1,"output_tokens":1,"total_tokens":0,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, _ := newTestResponsesClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, test.body)
			}))
			recorder := &recordingResponsesUsageRecorder{}
			_, _ = client.Create(WithResponsesUsageRecorder(context.Background(), recorder), validResponsesCall())
			if len(recorder.events) != 0 {
				t.Fatalf("usage events = %#v, want none", recorder.events)
			}
		})
	}
}

func TestResponsesClientUsageRecorderFailureStopsResponseProcessing(t *testing.T) {
	client, _ := newTestResponsesClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, completedResponseJSON("# Story\n\nGenerated."))
	}))
	recorder := &recordingResponsesUsageRecorder{err: errors.New("usage storage unavailable")}
	_, err := client.Create(WithResponsesUsageRecorder(context.Background(), recorder), validResponsesCall())
	if !errors.Is(err, ErrOpenAIUnavailable) || len(recorder.events) != 1 {
		t.Fatalf("error/events = %v/%#v", err, recorder.events)
	}
}

func TestResponsesClientCreateRejectsInvalidCallBeforeNetwork(t *testing.T) {
	var hits atomic.Int32
	client, _ := newTestResponsesClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))

	tests := []struct {
		name   string
		mutate func(*ResponsesCall)
		want   string
	}{
		{"missing operation", func(c *ResponsesCall) { c.Operation = "" }, "operation is invalid"},
		{"missing model", func(c *ResponsesCall) { c.Model = "" }, "model is required"},
		{"bad reasoning", func(c *ResponsesCall) { c.ReasoningEffort = "extreme" }, "unsupported reasoning effort"},
		{"zero output", func(c *ResponsesCall) { c.MaxOutputTokens = 0 }, "max output tokens"},
		{"too much output", func(c *ResponsesCall) { c.MaxOutputTokens = maxOutputTokensV2 + 1 }, "max output tokens"},
		{"missing instructions", func(c *ResponsesCall) { c.Prompt.DeveloperInstructions = "" }, "developer instructions"},
		{"missing user input", func(c *ResponsesCall) { c.Prompt.UserInputJSON = "" }, "user input"},
		{"user input not object", func(c *ResponsesCall) { c.Prompt.UserInputJSON = `[]` }, "JSON object"},
		{"duplicate user input key", func(c *ResponsesCall) { c.Prompt.UserInputJSON = `{"x":1,"x":2}` }, "duplicate object key"},
		{"bad schema name", func(c *ResponsesCall) {
			c.StructuredOutput = &StructuredOutput{Name: "bad name", Schema: json.RawMessage(`{}`)}
		}, "name is invalid"},
		{"bad schema json", func(c *ResponsesCall) {
			c.StructuredOutput = &StructuredOutput{Name: "schema", Schema: json.RawMessage(`{`)}
		}, "valid JSON object"},
		{"duplicate schema key", func(c *ResponsesCall) {
			c.StructuredOutput = &StructuredOutput{Name: "schema", Schema: json.RawMessage(`{"type":"object","type":"array"}`)}
		}, "duplicate object key"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			call := validResponsesCall()
			test.mutate(&call)
			_, err := client.Create(context.Background(), call)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Create() error = %v, want substring %q", err, test.want)
			}
		})
	}
	if hits.Load() != 0 {
		t.Fatalf("network hits = %d, want 0", hits.Load())
	}
}

func TestResponsesClientCreateFailsClosedOnProviderOutcomes(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
		want        error
	}{
		{
			name:        "refusal",
			status:      http.StatusOK,
			contentType: "application/json",
			body: `{
				"id":"resp_refused",
				"status":"completed",
				"model":"gpt-5.6-terra",
				"output":[{"type":"message","content":[{"type":"refusal","refusal":"No."}]}],
				"usage":{}
			}`,
			want: ErrOpenAIResponseRefused,
		},
		{
			name:        "incomplete",
			status:      http.StatusOK,
			contentType: "application/json",
			body: `{
				"id":"resp_incomplete",
				"status":"incomplete",
				"model":"gpt-5.6-terra",
				"incomplete_details":{"reason":"max_output_tokens"},
				"output":[],
				"usage":{}
			}`,
			want: ErrOpenAIResponseIncomplete,
		},
		{
			name:        "unauthorized",
			status:      http.StatusUnauthorized,
			contentType: "application/json",
			body:        `{}`,
			want:        ErrOpenAIUnauthorized,
		},
		{
			name:        "rate limited",
			status:      http.StatusTooManyRequests,
			contentType: "application/json",
			body:        `{"error":{"type":"rate_limit_exceeded","code":"rate_limit_exceeded"}}`,
			want:        ErrOpenAIRateLimited,
		},
		{
			name:        "server failure",
			status:      http.StatusBadGateway,
			contentType: "application/json",
			body:        `{}`,
			want:        ErrOpenAIUnavailable,
		},
		{
			name:        "bad request",
			status:      http.StatusBadRequest,
			contentType: "application/json",
			body:        `{}`,
			want:        ErrOpenAIResponseInvalid,
		},
		{
			name:        "wrong content type",
			status:      http.StatusOK,
			contentType: "text/html",
			body:        `<html></html>`,
			want:        ErrOpenAIResponseInvalid,
		},
		{
			name:        "malformed json",
			status:      http.StatusOK,
			contentType: "application/json",
			body:        `{`,
			want:        ErrOpenAIResponseInvalid,
		},
		{
			name:        "missing output text",
			status:      http.StatusOK,
			contentType: "application/json",
			body: `{
				"id":"resp_empty",
				"status":"completed",
				"model":"gpt-5.6-terra",
				"output":[],
				"usage":{}
			}`,
			want: ErrOpenAIResponseInvalid,
		},
		{
			name:        "multiple output text items",
			status:      http.StatusOK,
			contentType: "application/json",
			body: `{
				"id":"resp_multi",
				"status":"completed",
				"model":"gpt-5.6-terra",
				"output":[{
					"type":"message",
					"content":[
						{"type":"output_text","text":"one"},
						{"type":"output_text","text":"two"}
					]
				}],
				"usage":{}
			}`,
			want: ErrOpenAIResponseInvalid,
		},
		{
			name:        "missing token usage",
			status:      http.StatusOK,
			contentType: "application/json",
			body: `{
				"id":"resp_usage_missing",
				"status":"completed",
				"model":"gpt-5.6-terra",
				"output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]
			}`,
			want: ErrOpenAIResponseInvalid,
		},
		{
			name:        "negative token usage",
			status:      http.StatusOK,
			contentType: "application/json",
			body: `{
				"id":"resp_usage",
				"status":"completed",
				"model":"gpt-5.6-terra",
				"output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}],
				"usage":{"input_tokens":-1}
			}`,
			want: ErrOpenAIResponseInvalid,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, _ := newTestResponsesClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", test.contentType)
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			}))
			_, err := client.Create(context.Background(), validResponsesCall())
			if !errors.Is(err, test.want) {
				t.Fatalf("Create() error = %v, want errors.Is(..., %v)", err, test.want)
			}
		})
	}
}

func TestResponsesClientCreateRetainsOnlyAllowlistedRateLimitMetadata(t *testing.T) {
	providerMessage := "provider-message-must-not-be-retained"
	promptSecret := "prompt-must-not-be-retained"
	client, _ := newTestResponsesClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.Header().Set("X-Request-ID", "req_safe")
		w.Header().Set("X-RateLimit-Limit-Requests", "60")
		w.Header().Set("X-RateLimit-Remaining-Requests", "0")
		w.Header().Set("X-RateLimit-Reset-Requests", "1s")
		w.Header().Set("X-RateLimit-Limit-Tokens", "150000")
		w.Header().Set("X-RateLimit-Remaining-Tokens", "42")
		w.Header().Set("X-RateLimit-Reset-Tokens", "6m0s")
		w.Header().Set("X-RateLimit-Limit-Project-Tokens", "60000")
		w.Header().Set("X-RateLimit-Remaining-Project-Tokens", "0")
		w.Header().Set("X-RateLimit-Reset-Project-Tokens", "3s")
		w.Header().Set("Authorization", "Bearer response-secret-must-not-be-retained")
		w.Header().Set("X-Unrelated-Provider-Header", "arbitrary-header-must-not-be-retained")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprintf(w, `{"error":{"message":%q,"type":"rate_limit_exceeded","code":"rate_limit_exceeded"}}`, providerMessage)
	}))

	call := validResponsesCall()
	call.Prompt.DeveloperInstructions = promptSecret
	_, err := client.Create(context.Background(), call)
	if !errors.Is(err, ErrOpenAIRateLimited) {
		t.Fatalf("Create() error = %v", err)
	}
	if delay, ok := openAIRetryAfter(err); !ok || delay != 7*time.Second {
		t.Fatalf("retry-after = %v / %v, want 7s / true", delay, ok)
	}
	metadata, ok := openAIRateLimitMetadataFor(err)
	if !ok || metadata.requestID != "req_safe" || !metadata.requests.hasLimit || metadata.requests.limit != 60 ||
		!metadata.requests.hasRemaining || metadata.requests.remaining != 0 || !metadata.requests.hasReset || metadata.requests.reset != time.Second ||
		!metadata.tokens.hasLimit || metadata.tokens.limit != 150000 || !metadata.tokens.hasRemaining || metadata.tokens.remaining != 42 || metadata.tokens.reset != 6*time.Minute ||
		!metadata.projectTokens.hasLimit || metadata.projectTokens.limit != 60000 || !metadata.projectTokens.hasRemaining || metadata.projectTokens.remaining != 0 || metadata.projectTokens.reset != 3*time.Second {
		t.Fatalf("rate limit metadata = %#v", metadata)
	}
	var rateLimitErr *openAIRateLimitedError
	if !errors.As(err, &rateLimitErr) {
		t.Fatalf("rate limit error = %T, want *openAIRateLimitedError", err)
	}
	retained := fmt.Sprintf("%#v", rateLimitErr)
	for _, forbidden := range []string{providerMessage, promptSecret, "response-secret-must-not-be-retained", "arbitrary-header-must-not-be-retained"} {
		if strings.Contains(err.Error(), forbidden) || strings.Contains(retained, forbidden) {
			t.Fatalf("rate limit error unexpectedly retained %q: %s", forbidden, retained)
		}
	}
}

func TestResponsesClientCreateDoesNotRetryOrFollowRedirects(t *testing.T) {
	t.Run("no retry", func(t *testing.T) {
		var hits atomic.Int32
		client, _ := newTestResponsesClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{}`)
		}))
		_, err := client.Create(context.Background(), validResponsesCall())
		if !errors.Is(err, ErrOpenAIUnavailable) {
			t.Fatalf("Create() error = %v", err)
		}
		if hits.Load() != 1 {
			t.Fatalf("request count = %d, want 1", hits.Load())
		}
	})

	t.Run("no redirect", func(t *testing.T) {
		var redirectedHits atomic.Int32
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			redirectedHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, completedResponseJSON("should not be reached"))
		}))
		defer target.Close()

		client, _ := newTestResponsesClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL, http.StatusFound)
		}))
		_, err := client.Create(context.Background(), validResponsesCall())
		if !errors.Is(err, ErrOpenAIResponseInvalid) {
			t.Fatalf("Create() error = %v", err)
		}
		if redirectedHits.Load() != 0 {
			t.Fatalf("redirect target hits = %d, want 0", redirectedHits.Load())
		}
	})
}

func TestResponsesClientCreateRejectsOversizedResponse(t *testing.T) {
	client, _ := newTestResponsesClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, strings.Repeat("x", maxResponsesBodyBytes+1))
	}))
	_, err := client.Create(context.Background(), validResponsesCall())
	if !errors.Is(err, ErrOpenAIResponseInvalid) {
		t.Fatalf("Create() error = %v", err)
	}
}

func TestResponsesClientCreatePreservesContextCancellation(t *testing.T) {
	client, _ := newTestResponsesClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Create(ctx, validResponsesCall())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Create() error = %v, want context.Canceled", err)
	}
}

func TestResponsesClientCreateClassifiesQuotaExhaustionAsNonRetryable(t *testing.T) {
	client, _ := newTestResponsesClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"message":"provider billing detail must not escape","type":"insufficient_quota","code":"insufficient_quota"}}`)
	}))

	_, err := client.Create(context.Background(), validResponsesCall())
	if !errors.Is(err, ErrOpenAIQuotaExceeded) {
		t.Fatalf("Create() error = %v, want errors.Is(..., ErrOpenAIQuotaExceeded)", err)
	}
	if errors.Is(err, ErrOpenAIRateLimited) {
		t.Fatalf("quota exhaustion unexpectedly classified as rate limited: %v", err)
	}
	if got, want := err.Error(), ErrOpenAIQuotaExceeded.Error()+": HTTP 429"; got != want {
		t.Fatalf("Create() error = %q, want %q", got, want)
	}
}

func TestResponsesClientCreateUnknown429FailsClosed(t *testing.T) {
	client, _ := newTestResponsesClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"type":"unknown_provider_limit","code":"unknown_provider_limit"}}`)
	}))

	_, err := client.Create(context.Background(), validResponsesCall())
	if !errors.Is(err, ErrOpenAIUnavailable) {
		t.Fatalf("Create() error = %v, want errors.Is(..., ErrOpenAIUnavailable)", err)
	}
	if errors.Is(err, ErrOpenAIRateLimited) || errors.Is(err, ErrOpenAIQuotaExceeded) {
		t.Fatalf("unknown 429 unexpectedly classified as retryable/quota: %v", err)
	}
}
