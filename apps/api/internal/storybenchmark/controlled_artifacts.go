package storybenchmark

import (
	"context"
	"encoding/json"
	"fmt"

	"pandapages/api/internal/storygeneration"
)

type controlledFixtureResponse struct {
	promptVersion storygeneration.PromptVersion
	result        storygeneration.ResponsesResult
}

type controlledFixtureGateway struct {
	responses []controlledFixtureResponse
	index     int
}

func (gateway *controlledFixtureGateway) Create(
	ctx context.Context,
	call storygeneration.ResponsesCall,
) (storygeneration.ResponsesResult, error) {
	if err := ctx.Err(); err != nil {
		return storygeneration.ResponsesResult{}, err
	}
	if gateway.index >= len(gateway.responses) {
		return storygeneration.ResponsesResult{}, fmt.Errorf("controlled fixture gateway received an unexpected extra call")
	}
	expected := gateway.responses[gateway.index]
	if call.Model != storygeneration.GenerationModelV2 {
		return storygeneration.ResponsesResult{}, fmt.Errorf("controlled fixture gateway expected generation model %q, got %q", storygeneration.GenerationModelV2, call.Model)
	}
	if call.Prompt.Version != expected.promptVersion {
		return storygeneration.ResponsesResult{}, fmt.Errorf(
			"controlled fixture gateway expected prompt version %q, got %q",
			expected.promptVersion,
			call.Prompt.Version,
		)
	}
	gateway.index++
	return expected.result, nil
}

func buildControlledCaseArtifacts(
	ctx context.Context,
	story ControlledStory,
	fixtureCase ControlledCase,
) (storygeneration.StoryAnalysisArtifact, []storygeneration.GeneratedEditionArtifact, error) {
	analysisJSON, err := json.Marshal(story.Analysis)
	if err != nil {
		return storygeneration.StoryAnalysisArtifact{}, nil, fmt.Errorf("encode controlled StoryAnalysis: %w", err)
	}

	responses := make([]controlledFixtureResponse, 0, 1+len(fixtureCase.Editions))
	responses = append(responses, controlledFixtureResponse{
		promptVersion: storygeneration.SourceAnalysisPromptVersionV2,
		result: storygeneration.ResponsesResult{
			ResponseID: "benchmark-fixture-analysis-" + fixtureCase.ID,
			Model:      storygeneration.GenerationModelV2,
			OutputText: string(analysisJSON),
		},
	})
	for _, edition := range fixtureCase.Editions {
		responses = append(responses, controlledFixtureResponse{
			promptVersion: storygeneration.EditionPromptVersionV2,
			result: storygeneration.ResponsesResult{
				ResponseID: "benchmark-fixture-edition-" + fixtureCase.ID + "-" + string(edition.EditionKey),
				Model:      storygeneration.GenerationModelV2,
				OutputText: edition.Markdown,
			},
		})
	}

	gateway := &controlledFixtureGateway{responses: responses}
	generationRunner, err := storygeneration.NewV2Runner(storygeneration.V2RunnerConfig{
		Gateway:                 gateway,
		AnalysisReasoningEffort: storygeneration.ReasoningEffortNone,
		AnalysisMaxOutputTokens: 1,
		EditionReasoningEffort:  storygeneration.ReasoningEffortNone,
		EditionMaxOutputTokens:  1,
	})
	if err != nil {
		return storygeneration.StoryAnalysisArtifact{}, nil, fmt.Errorf("create controlled fixture generation adapter: %w", err)
	}

	analysis, err := generationRunner.AnalyseSource(ctx, storygeneration.SourceAnalysisPromptInput{
		Title:           story.Title,
		Author:          story.Author,
		CanonicalSource: story.CanonicalSource,
	})
	if err != nil {
		return storygeneration.StoryAnalysisArtifact{}, nil, fmt.Errorf("construct controlled StoryAnalysis artifact: %w", err)
	}

	editions := make([]storygeneration.GeneratedEditionArtifact, 0, len(fixtureCase.Editions))
	for _, controlledEdition := range fixtureCase.Editions {
		edition, err := generationRunner.GenerateEdition(ctx, storygeneration.GenerateEditionInput{
			EditionKey:       controlledEdition.EditionKey,
			Title:            story.Title,
			Author:           story.Author,
			Slug:             story.Slug,
			Language:         story.Language,
			SourceURL:        story.SourceURL,
			Rights:           cloneStringAnyMap(story.Rights),
			CanonicalSource:  story.CanonicalSource,
			AnalysisArtifact: analysis,
		})
		if err != nil {
			return storygeneration.StoryAnalysisArtifact{}, nil, fmt.Errorf(
				"construct controlled generated-edition artifact %q: %w",
				controlledEdition.EditionKey,
				err,
			)
		}
		editions = append(editions, edition)
	}

	if gateway.index != len(gateway.responses) {
		return storygeneration.StoryAnalysisArtifact{}, nil, fmt.Errorf(
			"controlled fixture gateway consumed %d of %d scripted responses",
			gateway.index,
			len(gateway.responses),
		)
	}
	return analysis, editions, nil
}
