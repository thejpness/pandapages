package storybenchmark

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
)

const HumanReviewVersionV1 = "panda-pages-human-review-v1"

type HumanReviewStatus string

const (
	HumanReviewPending  HumanReviewStatus = "pending"
	HumanReviewComplete HumanReviewStatus = "complete"
)

type HumanReviewContentBinding struct {
	EditionKey    model.AdminStoryEditionKey `json:"editionKey"`
	ContentSHA256 string                     `json:"contentSha256"`
}

type HumanReviewTarget struct {
	GenerationRepetition int                                `json:"generationRepetition"`
	AnalysisSHA256       string                             `json:"analysisSha256"`
	AssessmentScope      adaptationcontract.AssessmentScope `json:"assessmentScope"`
	EditionKey           *model.AdminStoryEditionKey        `json:"editionKey,omitempty"`
	EditionKeys          []model.AdminStoryEditionKey       `json:"editionKeys,omitempty"`
	ContentBindings      []HumanReviewContentBinding        `json:"contentBindings"`
	ReviewStatus         HumanReviewStatus                  `json:"reviewStatus"`
	ExpectedResult       adaptationcontract.Result          `json:"expectedResult,omitempty"`
	ExpectedFindingCodes []adaptationcontract.FindingCode   `json:"expectedFindingCodes"`
	Note                 string                             `json:"note"`
}

type HumanReviewDocument struct {
	ReviewVersion    string              `json:"reviewVersion"`
	BenchmarkVersion Version             `json:"benchmarkVersion"`
	Suite            string              `json:"suite"`
	SourceID         string              `json:"sourceId"`
	SourceSHA256     string              `json:"sourceSha256"`
	Targets          []HumanReviewTarget `json:"targets"`
}

type HumanReviewTrialScore struct {
	GenerationRepetition int                                `json:"generationRepetition"`
	ValidationRepetition int                                `json:"validationRepetition"`
	ValidatorConfigID    string                             `json:"validatorConfigId"`
	AssessmentScope      adaptationcontract.AssessmentScope `json:"assessmentScope"`
	EditionKey           *model.AdminStoryEditionKey        `json:"editionKey,omitempty"`
	EditionKeys          []model.AdminStoryEditionKey       `json:"editionKeys,omitempty"`
	ExpectedResult       adaptationcontract.Result          `json:"expectedResult"`
	ActualResult         adaptationcontract.Result          `json:"actualResult"`
	ResultMatch          bool                               `json:"resultMatch"`
	ExpectedFindingCodes []adaptationcontract.FindingCode   `json:"expectedFindingCodes"`
	ActualFindingCodes   []adaptationcontract.FindingCode   `json:"actualFindingCodes"`
	MissingExpectedCodes []adaptationcontract.FindingCode   `json:"missingExpectedCodes"`
	UnexpectedCodes      []adaptationcontract.FindingCode   `json:"unexpectedCodes"`
	ExactFindingMatch    bool                               `json:"exactFindingMatch"`
	Agreement            bool                               `json:"agreement"`
}

type HumanReviewSummary struct {
	Trials              int `json:"trials"`
	Agreements          int `json:"agreements"`
	ResultMatches       int `json:"resultMatches"`
	ExactFindingMatches int `json:"exactFindingMatches"`
}

type HumanReviewValidatorSummary struct {
	ValidatorConfigID string             `json:"validatorConfigId"`
	Summary           HumanReviewSummary `json:"summary"`
}

type HumanReviewScoreDocument struct {
	ReviewVersion    string                        `json:"reviewVersion"`
	BenchmarkVersion Version                       `json:"benchmarkVersion"`
	Suite            string                        `json:"suite"`
	SourceID         string                        `json:"sourceId"`
	SourceSHA256     string                        `json:"sourceSha256"`
	Summary          HumanReviewSummary            `json:"summary"`
	ByValidator      []HumanReviewValidatorSummary `json:"byValidator"`
	Trials           []HumanReviewTrialScore       `json:"trials"`
}

func BuildHumanReviewTemplate(document EndToEndResultDocument) (HumanReviewDocument, error) {
	if err := validateEndToEndResultDocument(document); err != nil {
		return HumanReviewDocument{}, err
	}

	review := HumanReviewDocument{
		ReviewVersion:    HumanReviewVersionV1,
		BenchmarkVersion: VersionV1,
		Suite:            EndToEndSuite,
		SourceID:         document.Source.ID,
		SourceSHA256:     document.Source.SourceSHA256,
		Targets:          make([]HumanReviewTarget, 0, len(document.Run.Generations)*5),
	}
	for _, generation := range document.Run.Generations {
		if generation.GenerationStatus != TrialStatusComplete || generation.AnalysisArtifact == nil {
			continue
		}
		for _, edition := range generation.Editions {
			key := edition.EditionKey
			review.Targets = append(review.Targets, HumanReviewTarget{
				GenerationRepetition: generation.Repetition,
				AnalysisSHA256:       generation.AnalysisArtifact.AnalysisSHA256,
				AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
				EditionKey:           &key,
				ContentBindings: []HumanReviewContentBinding{{
					EditionKey:    edition.EditionKey,
					ContentSHA256: edition.ContentSHA256,
				}},
				ReviewStatus:         HumanReviewPending,
				ExpectedFindingCodes: []adaptationcontract.FindingCode{},
				Note:                 "",
			})
		}
		keys := make([]model.AdminStoryEditionKey, 0, len(generation.Editions))
		bindings := make([]HumanReviewContentBinding, 0, len(generation.Editions))
		for _, edition := range generation.Editions {
			keys = append(keys, edition.EditionKey)
			bindings = append(bindings, HumanReviewContentBinding{EditionKey: edition.EditionKey, ContentSHA256: edition.ContentSHA256})
		}
		review.Targets = append(review.Targets, HumanReviewTarget{
			GenerationRepetition: generation.Repetition,
			AnalysisSHA256:       generation.AnalysisArtifact.AnalysisSHA256,
			AssessmentScope:      adaptationcontract.AssessmentScopeBundle,
			EditionKeys:          keys,
			ContentBindings:      bindings,
			ReviewStatus:         HumanReviewPending,
			ExpectedFindingCodes: []adaptationcontract.FindingCode{},
			Note:                 "",
		})
	}
	if err := validateHumanReviewShape(review, false); err != nil {
		return HumanReviewDocument{}, err
	}
	return review, nil
}

func MarshalHumanReviewJSON(review HumanReviewDocument) ([]byte, error) {
	if err := validateHumanReviewShape(review, false); err != nil {
		return nil, err
	}
	encoded, err := json.MarshalIndent(review, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode human review: %w", err)
	}
	return append(encoded, '\n'), nil
}

func LoadEndToEndResultDocument(path string) (EndToEndResultDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return EndToEndResultDocument{}, fmt.Errorf("read end-to-end result: %w", err)
	}
	var document EndToEndResultDocument
	if err := decodeStrictJSON(data, &document); err != nil {
		return EndToEndResultDocument{}, fmt.Errorf("decode end-to-end result: %w", err)
	}
	if err := validateEndToEndResultDocument(document); err != nil {
		return EndToEndResultDocument{}, err
	}
	return document, nil
}

func LoadHumanReviewDocument(path string) (HumanReviewDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return HumanReviewDocument{}, fmt.Errorf("read human review: %w", err)
	}
	var review HumanReviewDocument
	if err := decodeStrictJSON(data, &review); err != nil {
		return HumanReviewDocument{}, fmt.Errorf("decode human review: %w", err)
	}
	if err := validateHumanReviewShape(review, true); err != nil {
		return HumanReviewDocument{}, err
	}
	return review, nil
}

func ScoreHumanReview(document EndToEndResultDocument, review HumanReviewDocument) (HumanReviewScoreDocument, error) {
	if err := validateEndToEndResultDocument(document); err != nil {
		return HumanReviewScoreDocument{}, err
	}
	if err := validateHumanReviewShape(review, true); err != nil {
		return HumanReviewScoreDocument{}, err
	}
	if review.SourceID != document.Source.ID || review.SourceSHA256 != document.Source.SourceSHA256 {
		return HumanReviewScoreDocument{}, fmt.Errorf("human review source binding does not match end-to-end result")
	}

	expectedTargets, err := bindHumanReviewToResult(document, review)
	if err != nil {
		return HumanReviewScoreDocument{}, err
	}

	scores := make([]HumanReviewTrialScore, 0)
	byValidator := make(map[string]*HumanReviewSummary, len(document.Run.Validators))
	for _, config := range document.Run.Validators {
		byValidator[config.ID] = &HumanReviewSummary{}
	}
	total := HumanReviewSummary{}
	for _, generation := range document.Run.Generations {
		for _, trial := range generation.ValidationTrials {
			if trial.Status != TrialStatusComplete {
				continue
			}
			if trial.AssessmentArtifact == nil {
				return HumanReviewScoreDocument{}, fmt.Errorf("complete validation trial has no assessment artifact")
			}
			key := reviewTargetKey(trial.GenerationRepetition, trial.AssessmentScope, trial.EditionKey, trial.EditionKeys)
			expectation, exists := expectedTargets[key]
			if !exists {
				return HumanReviewScoreDocument{}, fmt.Errorf("human review has no expectation for validation target %q", key)
			}
			score := scoreHumanReviewTrial(trial, expectation)
			scores = append(scores, score)
			addHumanReviewSummary(&total, score)
			validatorSummary, exists := byValidator[trial.ValidatorConfigID]
			if !exists {
				return HumanReviewScoreDocument{}, fmt.Errorf("validation trial references unknown validator config %q", trial.ValidatorConfigID)
			}
			addHumanReviewSummary(validatorSummary, score)
		}
	}
	if len(scores) == 0 {
		return HumanReviewScoreDocument{}, fmt.Errorf("end-to-end result contains no complete validation trials to compare with human review")
	}

	ids := make([]string, 0, len(byValidator))
	for id := range byValidator {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	byValidatorResult := make([]HumanReviewValidatorSummary, 0, len(ids))
	for _, id := range ids {
		byValidatorResult = append(byValidatorResult, HumanReviewValidatorSummary{ValidatorConfigID: id, Summary: *byValidator[id]})
	}

	return HumanReviewScoreDocument{
		ReviewVersion:    HumanReviewVersionV1,
		BenchmarkVersion: VersionV1,
		Suite:            EndToEndSuite,
		SourceID:         document.Source.ID,
		SourceSHA256:     document.Source.SourceSHA256,
		Summary:          total,
		ByValidator:      byValidatorResult,
		Trials:           scores,
	}, nil
}

func MarshalHumanReviewScoreJSON(document HumanReviewScoreDocument) ([]byte, error) {
	if document.ReviewVersion != HumanReviewVersionV1 || document.BenchmarkVersion != VersionV1 || document.Suite != EndToEndSuite {
		return nil, fmt.Errorf("human-review score document is invalid")
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode human-review score: %w", err)
	}
	return append(encoded, '\n'), nil
}

func RenderHumanReviewScoreMarkdown(document HumanReviewScoreDocument) (string, error) {
	if document.ReviewVersion != HumanReviewVersionV1 || document.BenchmarkVersion != VersionV1 || document.Suite != EndToEndSuite {
		return "", fmt.Errorf("human-review score document is invalid")
	}
	var builder strings.Builder
	builder.WriteString("# Panda Pages human-review comparison\n\n")
	fmt.Fprintf(&builder, "- Review contract: `%s`\n", document.ReviewVersion)
	fmt.Fprintf(&builder, "- Source: `%s` / `%s`\n", document.SourceID, document.SourceSHA256)
	fmt.Fprintf(&builder, "- Full agreement: %d / %d (%s)\n", document.Summary.Agreements, document.Summary.Trials, percentage(document.Summary.Agreements, document.Summary.Trials))
	fmt.Fprintf(&builder, "- Result agreement: %d / %d (%s)\n", document.Summary.ResultMatches, document.Summary.Trials, percentage(document.Summary.ResultMatches, document.Summary.Trials))
	fmt.Fprintf(&builder, "- Exact finding-set agreement: %d / %d (%s)\n\n", document.Summary.ExactFindingMatches, document.Summary.Trials, percentage(document.Summary.ExactFindingMatches, document.Summary.Trials))
	builder.WriteString("## By validator\n\n")
	builder.WriteString("| Config | Full agreement | Result agreement | Exact findings | Trials |\n")
	builder.WriteString("| --- | ---: | ---: | ---: | ---: |\n")
	for _, item := range document.ByValidator {
		fmt.Fprintf(&builder, "| `%s` | %d | %d | %d | %d |\n", item.ValidatorConfigID, item.Summary.Agreements, item.Summary.ResultMatches, item.Summary.ExactFindingMatches, item.Summary.Trials)
	}
	builder.WriteString("\nThis comparison is editorial benchmark evidence only. It is not publication approval and does not bypass source eligibility or human review.\n")
	return builder.String(), nil
}

func validateHumanReviewShape(review HumanReviewDocument, requireComplete bool) error {
	if review.ReviewVersion != HumanReviewVersionV1 {
		return fmt.Errorf("human review version must equal %q", HumanReviewVersionV1)
	}
	if review.BenchmarkVersion != VersionV1 || review.Suite != EndToEndSuite {
		return fmt.Errorf("human review benchmark binding is invalid")
	}
	if strings.TrimSpace(review.SourceID) == "" || !validFixtureSHA256(strings.TrimSpace(review.SourceSHA256)) {
		return fmt.Errorf("human review source binding is invalid")
	}
	if len(review.Targets) == 0 && requireComplete {
		return fmt.Errorf("human review requires at least one completed target")
	}
	seen := make(map[string]struct{}, len(review.Targets))
	for index, target := range review.Targets {
		if target.GenerationRepetition < 1 || !validFixtureSHA256(strings.TrimSpace(target.AnalysisSHA256)) {
			return fmt.Errorf("human review target %d generation binding is invalid", index+1)
		}
		if target.ExpectedFindingCodes == nil {
			return fmt.Errorf("human review target %d expectedFindingCodes must be an array", index+1)
		}
		if err := validateHumanReviewTargetShape(target); err != nil {
			return fmt.Errorf("human review target %d: %w", index+1, err)
		}
		key := reviewTargetKey(target.GenerationRepetition, target.AssessmentScope, target.EditionKey, target.EditionKeys)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("human review target %d duplicates %q", index+1, key)
		}
		seen[key] = struct{}{}
		switch target.ReviewStatus {
		case HumanReviewPending:
			if requireComplete {
				return fmt.Errorf("human review target %d is still pending", index+1)
			}
			if target.ExpectedResult != "" || len(target.ExpectedFindingCodes) != 0 {
				return fmt.Errorf("pending human review target %d must not contain an expectation", index+1)
			}
		case HumanReviewComplete:
			expectation := AssessmentExpectation{
				AssessmentScope:       target.AssessmentScope,
				EditionKey:            copyReviewEditionKey(target.EditionKey),
				EditionKeys:           append([]model.AdminStoryEditionKey(nil), target.EditionKeys...),
				ExpectedResult:        target.ExpectedResult,
				RequiredFindingCodes:  append([]adaptationcontract.FindingCode(nil), target.ExpectedFindingCodes...),
				ForbiddenFindingCodes: []adaptationcontract.FindingCode{},
			}
			if err := expectation.Validate(); err != nil {
				return fmt.Errorf("completed human review target %d expectation is invalid: %w", index+1, err)
			}
			if target.ExpectedResult != adaptationcontract.ResultPass && len(target.ExpectedFindingCodes) == 0 {
				return fmt.Errorf("completed human review target %d with result %q requires at least one expected finding code", index+1, target.ExpectedResult)
			}
		default:
			return fmt.Errorf("human review target %d status %q is invalid", index+1, target.ReviewStatus)
		}
	}
	return nil
}

func validateHumanReviewTargetShape(target HumanReviewTarget) error {
	switch target.AssessmentScope {
	case adaptationcontract.AssessmentScopeEdition:
		if target.EditionKey == nil || !adaptationcontract.ValidModernEditionKey(*target.EditionKey) {
			return fmt.Errorf("edition review target requires one modern edition key")
		}
		if len(target.EditionKeys) != 0 || len(target.ContentBindings) != 1 {
			return fmt.Errorf("edition review target must contain exactly one content binding and no bundle keys")
		}
		if target.ContentBindings[0].EditionKey != *target.EditionKey {
			return fmt.Errorf("edition review content binding does not match edition key")
		}
	case adaptationcontract.AssessmentScopeBundle:
		if target.EditionKey != nil || len(target.EditionKeys) < 2 || len(target.ContentBindings) != len(target.EditionKeys) {
			return fmt.Errorf("bundle review target requires ordered edition keys and matching content bindings")
		}
		for index, key := range target.EditionKeys {
			if !adaptationcontract.ValidModernEditionKey(key) || target.ContentBindings[index].EditionKey != key {
				return fmt.Errorf("bundle review target binding %d does not match a modern edition key", index+1)
			}
		}
	default:
		return fmt.Errorf("assessment scope %q is unsupported", target.AssessmentScope)
	}
	for index, binding := range target.ContentBindings {
		if !validFixtureSHA256(strings.TrimSpace(binding.ContentSHA256)) {
			return fmt.Errorf("content binding %d SHA-256 is invalid", index+1)
		}
	}
	return nil
}

func bindHumanReviewToResult(document EndToEndResultDocument, review HumanReviewDocument) (map[string]HumanReviewTarget, error) {
	expectedTemplate, err := BuildHumanReviewTemplate(document)
	if err != nil {
		return nil, err
	}
	if len(review.Targets) != len(expectedTemplate.Targets) {
		return nil, fmt.Errorf("human review target count does not match current generated content")
	}
	expected := make(map[string]HumanReviewTarget, len(review.Targets))
	actualByKey := make(map[string]HumanReviewTarget, len(review.Targets))
	for _, target := range review.Targets {
		actualByKey[reviewTargetKey(target.GenerationRepetition, target.AssessmentScope, target.EditionKey, target.EditionKeys)] = target
	}
	for _, templateTarget := range expectedTemplate.Targets {
		key := reviewTargetKey(templateTarget.GenerationRepetition, templateTarget.AssessmentScope, templateTarget.EditionKey, templateTarget.EditionKeys)
		actual, exists := actualByKey[key]
		if !exists {
			return nil, fmt.Errorf("human review is missing generated target %q", key)
		}
		if actual.AnalysisSHA256 != templateTarget.AnalysisSHA256 || !sameHumanReviewBindings(actual.ContentBindings, templateTarget.ContentBindings) {
			return nil, fmt.Errorf("human review target %q is stale: generated content SHA-256 binding changed", key)
		}
		expected[key] = actual
	}
	return expected, nil
}

func scoreHumanReviewTrial(trial ValidationTrial, expectation HumanReviewTarget) HumanReviewTrialScore {
	actualResult := trial.AssessmentArtifact.Assessment.Result
	actualCodes := make([]adaptationcontract.FindingCode, 0, len(trial.AssessmentArtifact.Assessment.Findings))
	for _, finding := range trial.AssessmentArtifact.Assessment.Findings {
		actualCodes = append(actualCodes, finding.Code)
	}
	expectedCodes := append([]adaptationcontract.FindingCode(nil), expectation.ExpectedFindingCodes...)
	missing, unexpected := findingSetDifference(expectedCodes, actualCodes)
	resultMatch := actualResult == expectation.ExpectedResult
	exactFindingMatch := len(missing) == 0 && len(unexpected) == 0
	return HumanReviewTrialScore{
		GenerationRepetition: trial.GenerationRepetition,
		ValidationRepetition: trial.ValidationRepetition,
		ValidatorConfigID:    trial.ValidatorConfigID,
		AssessmentScope:      trial.AssessmentScope,
		EditionKey:           copyReviewEditionKey(trial.EditionKey),
		EditionKeys:          append([]model.AdminStoryEditionKey(nil), trial.EditionKeys...),
		ExpectedResult:       expectation.ExpectedResult,
		ActualResult:         actualResult,
		ResultMatch:          resultMatch,
		ExpectedFindingCodes: expectedCodes,
		ActualFindingCodes:   actualCodes,
		MissingExpectedCodes: missing,
		UnexpectedCodes:      unexpected,
		ExactFindingMatch:    exactFindingMatch,
		Agreement:            resultMatch && exactFindingMatch,
	}
}

func findingSetDifference(expected, actual []adaptationcontract.FindingCode) ([]adaptationcontract.FindingCode, []adaptationcontract.FindingCode) {
	expectedSet := make(map[adaptationcontract.FindingCode]struct{}, len(expected))
	actualSet := make(map[adaptationcontract.FindingCode]struct{}, len(actual))
	for _, code := range expected {
		expectedSet[code] = struct{}{}
	}
	for _, code := range actual {
		actualSet[code] = struct{}{}
	}
	missing := make([]adaptationcontract.FindingCode, 0)
	unexpected := make([]adaptationcontract.FindingCode, 0)
	for code := range expectedSet {
		if _, exists := actualSet[code]; !exists {
			missing = append(missing, code)
		}
	}
	for code := range actualSet {
		if _, exists := expectedSet[code]; !exists {
			unexpected = append(unexpected, code)
		}
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i] < missing[j] })
	sort.Slice(unexpected, func(i, j int) bool { return unexpected[i] < unexpected[j] })
	return missing, unexpected
}

func addHumanReviewSummary(summary *HumanReviewSummary, score HumanReviewTrialScore) {
	summary.Trials++
	if score.Agreement {
		summary.Agreements++
	}
	if score.ResultMatch {
		summary.ResultMatches++
	}
	if score.ExactFindingMatch {
		summary.ExactFindingMatches++
	}
}

func reviewTargetKey(generation int, scope adaptationcontract.AssessmentScope, editionKey *model.AdminStoryEditionKey, editionKeys []model.AdminStoryEditionKey) string {
	if scope == adaptationcontract.AssessmentScopeEdition && editionKey != nil {
		return fmt.Sprintf("%d|edition|%s", generation, *editionKey)
	}
	parts := make([]string, 0, len(editionKeys))
	for _, key := range editionKeys {
		parts = append(parts, string(key))
	}
	return fmt.Sprintf("%d|bundle|%s", generation, strings.Join(parts, ","))
}

func sameHumanReviewBindings(left, right []HumanReviewContentBinding) bool {
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

func copyReviewEditionKey(value *model.AdminStoryEditionKey) *model.AdminStoryEditionKey {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}
