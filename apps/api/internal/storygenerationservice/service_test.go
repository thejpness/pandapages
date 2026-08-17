package storygenerationservice

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/storyorchestration"
)

const testSourceVersionID = "11111111-1111-4111-8111-111111111111"

type fakeSourceLoader struct {
	input storyorchestration.Input
	err   error
	calls []string
}

func (fake *fakeSourceLoader) LoadGenerationSourceVersion(sourceVersionID string) (storyorchestration.Input, error) {
	fake.calls = append(fake.calls, sourceVersionID)
	return fake.input, fake.err
}

type fakeOrchestrator struct {
	result storyorchestration.Result
	err    error
	calls  []storyorchestration.Input
}

func (fake *fakeOrchestrator) Run(_ context.Context, input storyorchestration.Input) (storyorchestration.Result, error) {
	fake.calls = append(fake.calls, input)
	return fake.result, fake.err
}

type fakeRunStore struct {
	persisted storyorchestration.PersistedRun
	err       error
	ids       []string
	results   []storyorchestration.Result
}

func (fake *fakeRunStore) PersistCompletedStoryOrchestrationRun(sourceVersionID string, result storyorchestration.Result) (storyorchestration.PersistedRun, error) {
	fake.ids = append(fake.ids, sourceVersionID)
	fake.results = append(fake.results, result)
	return fake.persisted, fake.err
}

func TestRunComposesTrustedSourceOrchestrationAndPersistence(t *testing.T) {
	input := storyorchestration.Input{SourceIdentity: testSourceVersionID, Title: "A source", CanonicalSource: "Canonical source."}
	result := storyorchestration.Result{SourceIdentity: testSourceVersionID, SemanticResult: adaptationcontract.ResultPass}
	persisted := storyorchestration.PersistedRun{ID: "22222222-2222-4222-8222-222222222222", SourceVersionID: testSourceVersionID, Result: result}
	loader := &fakeSourceLoader{input: input}
	orchestrator := &fakeOrchestrator{result: result}
	store := &fakeRunStore{persisted: persisted}
	service := newTestService(t, loader, orchestrator, store)

	got, err := service.Run(context.Background(), testSourceVersionID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, persisted) {
		t.Fatalf("persisted run = %#v, want %#v", got, persisted)
	}
	if !reflect.DeepEqual(loader.calls, []string{testSourceVersionID}) {
		t.Fatalf("loader calls = %v", loader.calls)
	}
	if !reflect.DeepEqual(orchestrator.calls, []storyorchestration.Input{input}) {
		t.Fatalf("orchestrator inputs = %#v", orchestrator.calls)
	}
	if !reflect.DeepEqual(store.ids, []string{testSourceVersionID}) || !reflect.DeepEqual(store.results, []storyorchestration.Result{result}) {
		t.Fatalf("persistence calls = ids:%v results:%#v", store.ids, store.results)
	}
}

func TestRunPersistsEveryValidSemanticOutcome(t *testing.T) {
	for _, semanticResult := range []adaptationcontract.Result{
		adaptationcontract.ResultPass,
		adaptationcontract.ResultNeedsReview,
		adaptationcontract.ResultFail,
	} {
		t.Run(string(semanticResult), func(t *testing.T) {
			input := storyorchestration.Input{SourceIdentity: testSourceVersionID}
			result := storyorchestration.Result{SourceIdentity: testSourceVersionID, SemanticResult: semanticResult}
			loader := &fakeSourceLoader{input: input}
			orchestrator := &fakeOrchestrator{result: result}
			store := &fakeRunStore{persisted: storyorchestration.PersistedRun{SourceVersionID: testSourceVersionID, Result: result}}

			if _, err := newTestService(t, loader, orchestrator, store).Run(context.Background(), testSourceVersionID); err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if len(store.ids) != 1 || store.results[0].SemanticResult != semanticResult {
				t.Fatalf("semantic result %q was not persisted: %#v", semanticResult, store.results)
			}
		})
	}
}

func TestRunFailsClosedAtDependencyBoundaries(t *testing.T) {
	input := storyorchestration.Input{SourceIdentity: testSourceVersionID}
	result := storyorchestration.Result{SourceIdentity: testSourceVersionID, SemanticResult: adaptationcontract.ResultPass}
	tests := []struct {
		name          string
		loader        fakeSourceLoader
		orchestrator  fakeOrchestrator
		store         fakeRunStore
		wantRunCalls  int
		wantStoreCall int
	}{
		{
			name:          "loader error",
			loader:        fakeSourceLoader{err: errors.New("load failed")},
			wantRunCalls:  0,
			wantStoreCall: 0,
		},
		{
			name:          "loader identity mismatch",
			loader:        fakeSourceLoader{input: storyorchestration.Input{SourceIdentity: "33333333-3333-4333-8333-333333333333"}},
			wantRunCalls:  0,
			wantStoreCall: 0,
		},
		{
			name:          "orchestration error",
			loader:        fakeSourceLoader{input: input},
			orchestrator:  fakeOrchestrator{err: errors.New("orchestration failed")},
			wantRunCalls:  1,
			wantStoreCall: 0,
		},
		{
			name:          "orchestration result identity mismatch",
			loader:        fakeSourceLoader{input: input},
			orchestrator:  fakeOrchestrator{result: storyorchestration.Result{SourceIdentity: "44444444-4444-4444-8444-444444444444"}},
			wantRunCalls:  1,
			wantStoreCall: 0,
		},
		{
			name:          "persistence error",
			loader:        fakeSourceLoader{input: input},
			orchestrator:  fakeOrchestrator{result: result},
			store:         fakeRunStore{err: errors.New("insert failed")},
			wantRunCalls:  1,
			wantStoreCall: 1,
		},
		{
			name:          "persisted run identity mismatch",
			loader:        fakeSourceLoader{input: input},
			orchestrator:  fakeOrchestrator{result: result},
			store:         fakeRunStore{persisted: storyorchestration.PersistedRun{SourceVersionID: "55555555-5555-4555-8555-555555555555", Result: result}},
			wantRunCalls:  1,
			wantStoreCall: 1,
		},
		{
			name:          "persisted result identity mismatch",
			loader:        fakeSourceLoader{input: input},
			orchestrator:  fakeOrchestrator{result: result},
			store:         fakeRunStore{persisted: storyorchestration.PersistedRun{SourceVersionID: testSourceVersionID, Result: storyorchestration.Result{SourceIdentity: "66666666-6666-4666-8666-666666666666"}}},
			wantRunCalls:  1,
			wantStoreCall: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loader := test.loader
			orchestrator := test.orchestrator
			store := test.store
			if _, err := newTestService(t, &loader, &orchestrator, &store).Run(context.Background(), testSourceVersionID); err == nil {
				t.Fatal("Run() unexpectedly succeeded")
			}
			if len(orchestrator.calls) != test.wantRunCalls || len(store.ids) != test.wantStoreCall {
				t.Fatalf("calls = orchestration:%d persistence:%d, want %d/%d", len(orchestrator.calls), len(store.ids), test.wantRunCalls, test.wantStoreCall)
			}
		})
	}
}

func TestRunAllowsIndependentRepeatedCalls(t *testing.T) {
	input := storyorchestration.Input{SourceIdentity: testSourceVersionID}
	result := storyorchestration.Result{SourceIdentity: testSourceVersionID, SemanticResult: adaptationcontract.ResultPass}
	loader := &fakeSourceLoader{input: input}
	orchestrator := &fakeOrchestrator{result: result}
	store := &fakeRunStore{persisted: storyorchestration.PersistedRun{SourceVersionID: testSourceVersionID, Result: result}}
	service := newTestService(t, loader, orchestrator, store)

	for range 2 {
		if _, err := service.Run(context.Background(), testSourceVersionID); err != nil {
			t.Fatal(err)
		}
	}
	if len(loader.calls) != 2 || len(orchestrator.calls) != 2 || len(store.ids) != 2 {
		t.Fatalf("repeated calls = loader:%d orchestration:%d persistence:%d", len(loader.calls), len(orchestrator.calls), len(store.ids))
	}
}

func newTestService(t *testing.T, loader SourceVersionLoader, orchestrator OrchestrationRunner, store CompletedRunStore) *Service {
	t.Helper()
	service, err := New(Config{SourceLoader: loader, Orchestrator: orchestrator, RunStore: store})
	if err != nil {
		t.Fatal(err)
	}
	return service
}
