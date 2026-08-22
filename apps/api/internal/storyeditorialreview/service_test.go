package storyeditorialreview

import (
	"errors"
	"testing"

	"pandapages/api/internal/model"
	"pandapages/api/internal/storyorchestration"
)

const (
	testRunID       = "11111111-1111-4111-8111-111111111111"
	testPrincipalID = "22222222-2222-4222-8222-222222222222"
	testAccountID   = "33333333-3333-4333-8333-333333333333"
)

type validatedRunReaderStub struct {
	calls []string
	err   error
}

func (stub *validatedRunReaderStub) GetCompletedStoryOrchestrationRun(runID string) (storyorchestration.PersistedRun, error) {
	stub.calls = append(stub.calls, runID)
	return storyorchestration.PersistedRun{ID: runID}, stub.err
}

type editorialReviewWriterStub struct {
	inputs []model.AdminStoryOrchestrationEditorialReviewCreateInput
	err    error
}

func (stub *editorialReviewWriterStub) CreateStoryOrchestrationEditorialReview(
	input model.AdminStoryOrchestrationEditorialReviewCreateInput,
) (model.AdminStoryOrchestrationEditorialReview, error) {
	stub.inputs = append(stub.inputs, input)
	if stub.err != nil {
		return model.AdminStoryOrchestrationEditorialReview{}, stub.err
	}
	return model.AdminStoryOrchestrationEditorialReview{
		ID:                  "44444444-4444-4444-8444-444444444444",
		RunID:               input.RunID,
		Decision:            input.Decision,
		ReviewerPrincipalID: input.ReviewerPrincipalID,
		ReviewerAccountID:   input.ReviewerAccountID,
		CreatedAt:           "2026-08-22T12:00:00Z",
	}, nil
}

type editorialReviewHistoryReaderStub struct {
	response model.AdminStoryOrchestrationEditorialReviewsListResponse
	err      error
	calls    [][2]int
	runIDs   []string
}

func (stub *editorialReviewHistoryReaderStub) ListStoryOrchestrationEditorialReviews(
	runID string,
	limit int,
) (model.AdminStoryOrchestrationEditorialReviewsListResponse, error) {
	stub.runIDs = append(stub.runIDs, runID)
	stub.calls = append(stub.calls, [2]int{len(stub.runIDs), limit})
	return stub.response, stub.err
}

func newTestService(t *testing.T, reader *validatedRunReaderStub, writer *editorialReviewWriterStub, history *editorialReviewHistoryReaderStub) *Service {
	t.Helper()
	service, err := New(Config{ValidatedRunReader: reader, Writer: writer, Reader: history})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func testCreateInput(decision model.AdminStoryOrchestrationEditorialDecision) model.AdminStoryOrchestrationEditorialReviewCreateInput {
	return model.AdminStoryOrchestrationEditorialReviewCreateInput{
		RunID:               testRunID,
		Decision:            decision,
		ReviewerPrincipalID: testPrincipalID,
		ReviewerAccountID:   testAccountID,
	}
}

func TestCreateRevalidatesExactCompletedRunBeforeAppendingEveryHumanDecision(t *testing.T) {
	for _, semanticResult := range []string{"pass", "needs_review", "fail"} {
		for _, decision := range []model.AdminStoryOrchestrationEditorialDecision{
			model.AdminStoryOrchestrationEditorialDecisionApproved,
			model.AdminStoryOrchestrationEditorialDecisionRejected,
		} {
			t.Run(semanticResult+"/"+string(decision), func(t *testing.T) {
				reader := &validatedRunReaderStub{}
				writer := &editorialReviewWriterStub{}
				history := &editorialReviewHistoryReaderStub{}
				service := newTestService(t, reader, writer, history)

				review, err := service.Create(testCreateInput(decision))
				if err != nil {
					t.Fatalf("Create() error = %v", err)
				}
				if len(reader.calls) != 1 || reader.calls[0] != testRunID || len(writer.inputs) != 1 || review.Decision != decision {
					t.Fatalf("calls/review = reader=%#v writer=%#v review=%#v", reader.calls, writer.inputs, review)
				}
			})
		}
	}
}

func TestCreateRefusesUnreviewableRunBeforeWriting(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "unknown", err: errors.New("run not found")},
		{name: "corrupt retained artifact", err: model.ErrAdminStoryOrchestrationRunRepairRequired},
		{name: "source binding invalid", err: model.ErrAdminStoryOrchestrationRunRepairRequired},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := &validatedRunReaderStub{err: test.err}
			writer := &editorialReviewWriterStub{}
			service := newTestService(t, reader, writer, &editorialReviewHistoryReaderStub{})
			if _, err := service.Create(testCreateInput(model.AdminStoryOrchestrationEditorialDecisionApproved)); !errors.Is(err, test.err) {
				t.Fatalf("Create() error = %v, want %v", err, test.err)
			}
			if len(writer.inputs) != 0 {
				t.Fatalf("writer was called for an unreviewable run: %#v", writer.inputs)
			}
		})
	}
}

func TestCreatePreservesAppendOnlyEventsAndRejectsInvalidInput(t *testing.T) {
	reader := &validatedRunReaderStub{}
	writer := &editorialReviewWriterStub{}
	service := newTestService(t, reader, writer, &editorialReviewHistoryReaderStub{})

	for _, decision := range []model.AdminStoryOrchestrationEditorialDecision{
		model.AdminStoryOrchestrationEditorialDecisionApproved,
		model.AdminStoryOrchestrationEditorialDecisionApproved,
		model.AdminStoryOrchestrationEditorialDecisionRejected,
	} {
		if _, err := service.Create(testCreateInput(decision)); err != nil {
			t.Fatalf("append %q: %v", decision, err)
		}
	}
	if len(writer.inputs) != 3 {
		t.Fatalf("append-only writer calls = %#v", writer.inputs)
	}

	invalid := testCreateInput("needs_review")
	if _, err := service.Create(invalid); !errors.Is(err, model.ErrAdminStoryOrchestrationEditorialReviewInvalid) {
		t.Fatalf("invalid input error = %v", err)
	}
	if len(reader.calls) != 3 || len(writer.inputs) != 3 {
		t.Fatalf("invalid input touched dependencies: reader=%#v writer=%#v", reader.calls, writer.inputs)
	}
}

func TestNewRequiresAllEditorialReviewDependencies(t *testing.T) {
	reader := &validatedRunReaderStub{}
	writer := &editorialReviewWriterStub{}
	history := &editorialReviewHistoryReaderStub{}
	for _, config := range []Config{
		{Writer: writer, Reader: history},
		{ValidatedRunReader: reader, Reader: history},
		{ValidatedRunReader: reader, Writer: writer},
	} {
		if _, err := New(config); err == nil {
			t.Fatal("New() unexpectedly accepted an incomplete configuration")
		}
	}
}
