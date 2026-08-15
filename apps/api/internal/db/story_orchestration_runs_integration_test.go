package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
	"pandapages/api/internal/storygeneration"
	"pandapages/api/internal/storyorchestration"
	"pandapages/api/internal/storyvalidation"
)

func TestStoryOrchestrationRunsIntegration(t *testing.T) {
	if os.Getenv(readerIntegrationGuardVar) != "1" {
		t.Skip("set PP_READER_STORE_TEST_DISPOSABLE=1 to run the disposable PostgreSQL integration test")
	}
	databaseURL := strings.TrimSpace(os.Getenv(readerIntegrationURLVar))
	if databaseURL == "" {
		t.Fatalf("%s is required", readerIntegrationURLVar)
	}
	adminDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	var databaseName string
	if err := adminDB.QueryRow(`SELECT current_database()`).Scan(&databaseName); err != nil || databaseName != readerIntegrationDBName {
		t.Fatalf("refusing orchestration persistence database %q: %v", databaseName, err)
	}

	const slug = "story-orchestration-runs-integration"
	t.Cleanup(func() {
		_, _ = adminDB.Exec(`
			DELETE FROM story_orchestration_runs
			WHERE source_version_id IN (
				SELECT version.id
				FROM story_source_versions AS version
				JOIN stories AS story ON story.id = version.story_id
				WHERE story.slug = $1
			)
		`, slug)
		_, _ = adminDB.Exec(`DELETE FROM stories WHERE slug = $1`, slug)
	})

	store := newReaderIntegrationStore(t, databaseURL)
	sourceText := "# The Lantern Tale\n\nA traveller follows a lantern home.\n"
	source, err := store.AdminSourceUpsert(slug, model.AdminSourceUpsertRequest{
		Title: "The Lantern Tale", Language: stringPointer("en-GB"), SourceText: sourceText,
	})
	if err != nil {
		t.Fatalf("create canonical source version: %v", err)
	}

	for _, semanticResult := range []adaptationcontract.Result{
		adaptationcontract.ResultPass,
		adaptationcontract.ResultNeedsReview,
		adaptationcontract.ResultFail,
	} {
		result := testCompletedOrchestrationResult(t, source.VersionID, sourceText, semanticResult)
		persisted, err := store.PersistCompletedStoryOrchestrationRun(source.VersionID, result)
		if err != nil {
			t.Fatalf("persist %q result: %v", semanticResult, err)
		}
		if persisted.ID == "" || persisted.SourceVersionID != source.VersionID || persisted.Result.SemanticResult != semanticResult || persisted.CreatedAt.IsZero() {
			t.Fatalf("persisted %q run = %#v", semanticResult, persisted)
		}
		loaded, err := store.GetCompletedStoryOrchestrationRun(persisted.ID)
		if err != nil {
			t.Fatalf("load %q run: %v", semanticResult, err)
		}
		if !reflect.DeepEqual(loaded.Result, result) {
			t.Fatalf("round-trip %q result differs:\n got %#v\nwant %#v", semanticResult, loaded.Result, result)
		}
		if got := editionKeysForTest(loaded.Result.Editions); !reflect.DeepEqual(got, storygeneration.DerivedEditionKeysV2()) {
			t.Fatalf("round-trip edition order = %v", got)
		}
		if got := assessmentKeysForTest(loaded.Result.EditionAssessments); !reflect.DeepEqual(got, storygeneration.DerivedEditionKeysV2()) {
			t.Fatalf("round-trip assessment order = %v", got)
		}
		if loaded.Result.AnalysisArtifact.Analysis.DevelopmentBeats == nil || loaded.Result.AnalysisArtifact.Analysis.EnrichmentMaterial == nil || len(loaded.Result.AnalysisArtifact.Analysis.DevelopmentBeats) != 0 || len(loaded.Result.AnalysisArtifact.Analysis.EnrichmentMaterial) != 0 {
			t.Fatalf("round-trip did not preserve non-nil empty arrays: %#v", loaded.Result.AnalysisArtifact.Analysis)
		}
	}

	assertRejectedWithoutNewRun := func(t *testing.T, name string, sourceVersionID string, result storyorchestration.Result) {
		t.Helper()
		before := orchestrationRunCount(t, adminDB, source.VersionID)
		if _, err := store.PersistCompletedStoryOrchestrationRun(sourceVersionID, result); err == nil {
			t.Fatalf("%s unexpectedly persisted", name)
		}
		if after := orchestrationRunCount(t, adminDB, source.VersionID); after != before {
			t.Fatalf("%s persisted partial evidence: before=%d after=%d", name, before, after)
		}
	}

	unknownID := "00000000-0000-4000-8000-000000000001"
	assertRejectedWithoutNewRun(t, "unknown source version", unknownID, testCompletedOrchestrationResult(t, unknownID, sourceText, adaptationcontract.ResultPass))

	for _, test := range []struct {
		name   string
		mutate func(*storyorchestration.Result)
	}{
		{"source identity mismatch", func(result *storyorchestration.Result) {
			result.SourceIdentity = "00000000-0000-4000-8000-000000000002"
		}},
		{"authoritative source mismatch", func(result *storyorchestration.Result) {
			result.AnalysisArtifact.SourceSHA256 = sha256HexForTest("other canonical source")
		}},
		{"invalid analysis", func(result *storyorchestration.Result) {
			result.AnalysisArtifact = storygeneration.StoryAnalysisArtifact{}
		}},
		{"missing edition", func(result *storyorchestration.Result) { result.Editions = result.Editions[:3] }},
		{"wrong edition order", func(result *storyorchestration.Result) {
			result.Editions[0], result.Editions[1] = result.Editions[1], result.Editions[0]
		}},
		{"invalid generated source binding", func(result *storyorchestration.Result) {
			result.Editions[0].SourceSHA256 = sha256HexForTest("wrong source")
		}},
		{"invalid generated analysis binding", func(result *storyorchestration.Result) {
			result.Editions[0].AnalysisSHA256 = sha256HexForTest("wrong analysis")
		}},
		{"invalid edition assessment", func(result *storyorchestration.Result) {
			result.EditionAssessments[0] = storyvalidation.AssessmentArtifact{}
		}},
		{"wrong edition assessment target", func(result *storyorchestration.Result) {
			key := result.Editions[1].EditionKey
			result.EditionAssessments[0].EditionKey = &key
		}},
		{"invalid bundle assessment", func(result *storyorchestration.Result) {
			result.BundleAssessment = storyvalidation.AssessmentArtifact{}
		}},
		{"semantic result mismatch", func(result *storyorchestration.Result) { result.SemanticResult = adaptationcontract.ResultFail }},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := testCompletedOrchestrationResult(t, source.VersionID, sourceText, adaptationcontract.ResultPass)
			test.mutate(&result)
			assertRejectedWithoutNewRun(t, test.name, source.VersionID, result)
		})
	}

	first, err := store.PersistCompletedStoryOrchestrationRun(source.VersionID, testCompletedOrchestrationResult(t, source.VersionID, sourceText, adaptationcontract.ResultPass))
	if err != nil {
		t.Fatalf("persist first duplicate content run: %v", err)
	}
	second, err := store.PersistCompletedStoryOrchestrationRun(source.VersionID, testCompletedOrchestrationResult(t, source.VersionID, sourceText, adaptationcontract.ResultPass))
	if err != nil {
		t.Fatalf("persist second duplicate content run: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("identical completed results reused run ID %q", first.ID)
	}

	if _, err := adminDB.Exec(`
		UPDATE story_orchestration_runs
		SET artifacts = jsonb_set(artifacts, ARRAY[$2]::text[], $3::jsonb, true)
		WHERE id = $1
	`, first.ID, "analysisArtifact", "{}"); err != nil {
		t.Fatalf("corrupt stored artifacts: %v", err)
	}
	if _, err := store.GetCompletedStoryOrchestrationRun(first.ID); err == nil {
		t.Fatal("malformed retained JSON unexpectedly loaded")
	}
}

func testCompletedOrchestrationResult(
	t *testing.T,
	sourceVersionID string,
	sourceText string,
	semanticResult adaptationcontract.Result,
) storyorchestration.Result {
	t.Helper()
	analysis := storygeneration.StoryAnalysis{
		CentralPlot: "A traveller follows a lantern home.",
		Characters: []storygeneration.Character{{
			Name:                "The traveller",
			Role:                "protagonist",
			ExplicitMotivations: []string{},
			FlawsOrAmbiguities:  []string{},
		}},
		Relationships:      []storygeneration.Relationship{},
		CoreStoryBeats:     []storygeneration.StoryBeat{{Summary: "The traveller follows the lantern."}},
		DevelopmentBeats:   []storygeneration.StoryBeat{},
		EnrichmentMaterial: []storygeneration.StoryBeat{},
		CausalDependencies: []storygeneration.CausalDependency{},
		IconicMaterial:     []storygeneration.IconicMaterial{},
		IntenseMaterial:    []storygeneration.IntenseMaterial{},
		AdaptationRisks:    []storygeneration.AdaptationRisk{},
	}
	analysisArtifact := storygeneration.StoryAnalysisArtifact{
		SpecificationVersion: storygeneration.SpecificationV2,
		PromptVersion:        storygeneration.SourceAnalysisPromptVersionV3,
		RequestedModel:       storygeneration.GenerationModelV2,
		ReturnedModel:        storygeneration.GenerationModelV2,
		ReasoningEffort:      storygeneration.ReasoningEffortMedium,
		SourceSHA256:         sha256HexForTest(sourceText),
		AnalysisSHA256:       jsonSHA256ForTest(t, analysis),
		Analysis:             analysis,
		ResponseID:           "analysis-response",
	}
	if err := analysisArtifact.Validate(); err != nil {
		t.Fatalf("test StoryAnalysis artifact: %v", err)
	}

	keys := storygeneration.DerivedEditionKeysV2()
	editions := make([]storygeneration.GeneratedEditionArtifact, 0, len(keys))
	for _, key := range keys {
		markdown := fmt.Sprintf("# The Lantern Tale\n\nThe traveller follows the lantern in the %s edition.\n", key)
		structural := adaptationcontract.ValidateGeneratedEdition(adaptationcontract.GeneratedEditionInput{
			EditionKey: key, Slug: "story-orchestration-runs-integration", Title: "The Lantern Tale", Author: "", Markdown: markdown, Language: "en-GB", Rights: map[string]any{},
		})
		if !structural.Passed() {
			t.Fatalf("test structural validation %q: %#v", key, structural.Findings)
		}
		edition := storygeneration.GeneratedEditionArtifact{
			SpecificationVersion: storygeneration.SpecificationV2,
			PromptVersion:        storygeneration.EditionPromptVersionV4,
			EditionKey:           key,
			RequestedModel:       storygeneration.GenerationModelV2,
			ReturnedModel:        storygeneration.GenerationModelV2,
			ReasoningEffort:      storygeneration.ReasoningEffortMedium,
			SourceSHA256:         analysisArtifact.SourceSHA256,
			AnalysisSHA256:       analysisArtifact.AnalysisSHA256,
			ContentSHA256:        structural.ContentSHA256,
			Markdown:             markdown,
			ResponseID:           "edition-" + string(key),
			StructuralValidation: structural,
		}
		if err := edition.Validate(); err != nil {
			t.Fatalf("test generated edition %q: %v", key, err)
		}
		editions = append(editions, edition)
	}

	editionAssessments := make([]storyvalidation.AssessmentArtifact, 0, len(editions))
	for index, edition := range editions {
		result := adaptationcontract.ResultPass
		if semanticResult == adaptationcontract.ResultNeedsReview && index == 0 {
			result = adaptationcontract.ResultNeedsReview
		}
		editionAssessments = append(editionAssessments, testEditionAssessmentArtifact(t, analysisArtifact, edition, result))
	}
	bundleResult := adaptationcontract.ResultPass
	if semanticResult == adaptationcontract.ResultFail {
		bundleResult = adaptationcontract.ResultFail
	}
	bundleAssessment := testBundleAssessmentArtifact(t, analysisArtifact, editions, bundleResult)
	return storyorchestration.Result{
		SourceIdentity:     sourceVersionID,
		SourceSHA256:       analysisArtifact.SourceSHA256,
		AnalysisArtifact:   analysisArtifact,
		Editions:           editions,
		EditionAssessments: editionAssessments,
		BundleAssessment:   bundleAssessment,
		SemanticResult:     semanticResult,
	}
}

func testEditionAssessmentArtifact(
	t *testing.T,
	analysis storygeneration.StoryAnalysisArtifact,
	edition storygeneration.GeneratedEditionArtifact,
	result adaptationcontract.Result,
) storyvalidation.AssessmentArtifact {
	t.Helper()
	key := edition.EditionKey
	assessment := storyvalidation.Assessment{
		ValidationVersion:    storyvalidation.ValidationV3,
		SpecificationVersion: storygeneration.SpecificationV2,
		AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
		EditionKey:           &key,
		Result:               result,
		Findings:             editionFindingsForTest(result, key),
	}
	return testAssessmentArtifact(t, analysis, []storygeneration.GeneratedEditionArtifact{edition}, assessment)
}

func testBundleAssessmentArtifact(
	t *testing.T,
	analysis storygeneration.StoryAnalysisArtifact,
	editions []storygeneration.GeneratedEditionArtifact,
	result adaptationcontract.Result,
) storyvalidation.AssessmentArtifact {
	t.Helper()
	keys := editionKeysForTest(editions)
	assessment := storyvalidation.Assessment{
		ValidationVersion:    storyvalidation.ValidationV3,
		SpecificationVersion: storygeneration.SpecificationV2,
		AssessmentScope:      adaptationcontract.AssessmentScopeBundle,
		EditionKeys:          keys,
		Result:               result,
		Findings:             bundleFindingsForTest(result, keys),
	}
	return testAssessmentArtifact(t, analysis, editions, assessment)
}

func testAssessmentArtifact(
	t *testing.T,
	analysis storygeneration.StoryAnalysisArtifact,
	editions []storygeneration.GeneratedEditionArtifact,
	assessment storyvalidation.Assessment,
) storyvalidation.AssessmentArtifact {
	t.Helper()
	bindings := make([]storyvalidation.EditionBinding, 0, len(editions))
	for _, edition := range editions {
		bindings = append(bindings, storyvalidation.EditionBinding{EditionKey: edition.EditionKey, ContentSHA256: edition.ContentSHA256})
	}
	promptVersion := storyvalidation.EditionJudgementPromptVersionV3
	if assessment.AssessmentScope == adaptationcontract.AssessmentScopeBundle {
		promptVersion = storyvalidation.BundleJudgementPromptVersionV3
	}
	artifact := storyvalidation.AssessmentArtifact{
		ValidationVersion:    storyvalidation.ValidationV3,
		SpecificationVersion: storygeneration.SpecificationV2,
		PromptVersion:        promptVersion,
		AssessmentScope:      assessment.AssessmentScope,
		EditionKey:           assessment.EditionKey,
		EditionKeys:          append([]model.AdminStoryEditionKey(nil), assessment.EditionKeys...),
		RequestedModel:       "semantic-validator-test",
		ReturnedModel:        "semantic-validator-test",
		ReasoningEffort:      storygeneration.ReasoningEffortMedium,
		SourceSHA256:         analysis.SourceSHA256,
		AnalysisSHA256:       analysis.AnalysisSHA256,
		EditionBindings:      bindings,
		AssessmentSHA256:     jsonSHA256ForTest(t, assessment),
		Assessment:           assessment,
		ResponseID:           "semantic-response",
	}
	if err := artifact.Validate(); err != nil {
		t.Fatalf("test assessment artifact: %v", err)
	}
	return artifact
}

func editionFindingsForTest(result adaptationcontract.Result, key model.AdminStoryEditionKey) []storyvalidation.Finding {
	switch result {
	case adaptationcontract.ResultPass:
		return []storyvalidation.Finding{}
	case adaptationcontract.ResultNeedsReview:
		return []storyvalidation.Finding{{
			Code: adaptationcontract.FindingScopeTooRich, Severity: adaptationcontract.FindingSeverityReview, Message: "Editorial review is required.",
			Evidence: []storyvalidation.Evidence{{Location: storyvalidation.EvidenceGeneratedEdition, EditionKey: &key, Excerpt: "traveller", Explanation: "The edition needs editorial review."}},
		}}
	default:
		return []storyvalidation.Finding{{
			Code: adaptationcontract.FindingMotivationChanged, Severity: adaptationcontract.FindingSeverityBlocking, Message: "The source-grounded motivation changed.",
			Evidence: []storyvalidation.Evidence{{Location: storyvalidation.EvidenceGeneratedEdition, EditionKey: &key, Excerpt: "traveller", Explanation: "The edition changes a source-grounded motivation."}},
		}}
	}
}

func bundleFindingsForTest(result adaptationcontract.Result, keys []model.AdminStoryEditionKey) []storyvalidation.Finding {
	switch result {
	case adaptationcontract.ResultPass:
		return []storyvalidation.Finding{}
	case adaptationcontract.ResultNeedsReview:
		return []storyvalidation.Finding{{
			Code: adaptationcontract.FindingEditionProgressionQuestionable, Severity: adaptationcontract.FindingSeverityReview, Message: "The progression needs editorial review.",
			Evidence: []storyvalidation.Evidence{{Location: storyvalidation.EvidenceGeneratedEdition, EditionKey: &keys[0], Excerpt: "traveller", Explanation: "The compared edition needs review."}},
		}}
	default:
		return []storyvalidation.Finding{{
			Code: adaptationcontract.FindingEditionProgressionNotDistinct, Severity: adaptationcontract.FindingSeverityBlocking, Message: "The progression is not distinct enough.",
			Evidence: []storyvalidation.Evidence{{Location: storyvalidation.EvidenceGeneratedEdition, EditionKey: &keys[0], Excerpt: "traveller", Explanation: "The compared edition establishes the finding."}},
		}}
	}
}

func editionKeysForTest(editions []storygeneration.GeneratedEditionArtifact) []model.AdminStoryEditionKey {
	keys := make([]model.AdminStoryEditionKey, 0, len(editions))
	for _, edition := range editions {
		keys = append(keys, edition.EditionKey)
	}
	return keys
}

func assessmentKeysForTest(assessments []storyvalidation.AssessmentArtifact) []model.AdminStoryEditionKey {
	keys := make([]model.AdminStoryEditionKey, 0, len(assessments))
	for _, assessment := range assessments {
		if assessment.EditionKey == nil {
			return nil
		}
		keys = append(keys, *assessment.EditionKey)
	}
	return keys
}

func orchestrationRunCount(t *testing.T, db *sql.DB, sourceVersionID string) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM story_orchestration_runs WHERE source_version_id = $1`, sourceVersionID).Scan(&count); err != nil {
		t.Fatalf("count orchestration runs: %v", err)
	}
	return count
}

func jsonSHA256ForTest(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal digest value: %v", err)
	}
	return sha256HexForTest(string(encoded))
}

func sha256HexForTest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
