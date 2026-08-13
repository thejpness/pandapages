package storybenchmark

import (
	"strings"
	"testing"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
	"pandapages/api/internal/storygeneration"
	"pandapages/api/internal/storyvalidation"
)

func TestScoreAssessment(t *testing.T) {
	growing := model.AdminStoryEditionGrowingReaders
	expectation := AssessmentExpectation{
		AssessmentScope:       adaptationcontract.AssessmentScopeEdition,
		EditionKey:            &growing,
		ExpectedResult:        adaptationcontract.ResultFail,
		RequiredFindingCodes:  []adaptationcontract.FindingCode{adaptationcontract.FindingMotivationChanged},
		ForbiddenFindingCodes: []adaptationcontract.FindingCode{adaptationcontract.FindingInventedMoralising},
	}

	t.Run("matches expected result and finding behaviour", func(t *testing.T) {
		assessment := validEditionAssessment(t, growing, adaptationcontract.FindingMotivationChanged)
		score, err := ScoreAssessment(expectation, assessment)
		if err != nil {
			t.Fatalf("ScoreAssessment() error = %v", err)
		}
		if !score.ExpectationMet || !score.ResultMatched {
			t.Fatalf("score = %#v", score)
		}
		if score.RequiredFindingsExpected != 1 || score.RequiredFindingsDetected != 1 ||
			score.ForbiddenFindingsChecked != 1 || score.ForbiddenFindingsTriggered != 0 {
			t.Fatalf("score counts = %#v", score)
		}
	})

	t.Run("missing required finding is a quality mismatch", func(t *testing.T) {
		assessment := validEditionAssessment(t, growing, adaptationcontract.FindingCausalChainBroken)
		score, err := ScoreAssessment(expectation, assessment)
		if err != nil {
			t.Fatalf("ScoreAssessment() error = %v", err)
		}
		if !score.ResultMatched || score.ExpectationMet {
			t.Fatalf("score = %#v", score)
		}
		if len(score.MissingRequiredFindingCodes) != 1 ||
			score.MissingRequiredFindingCodes[0] != adaptationcontract.FindingMotivationChanged {
			t.Fatalf("missing findings = %#v", score.MissingRequiredFindingCodes)
		}
	})

	t.Run("triggered forbidden finding is a quality mismatch", func(t *testing.T) {
		assessment := validEditionAssessment(
			t,
			growing,
			adaptationcontract.FindingMotivationChanged,
			adaptationcontract.FindingInventedMoralising,
		)
		score, err := ScoreAssessment(expectation, assessment)
		if err != nil {
			t.Fatalf("ScoreAssessment() error = %v", err)
		}
		if score.ExpectationMet || score.ForbiddenFindingsTriggered != 1 {
			t.Fatalf("score = %#v", score)
		}
	})

	t.Run("wrong assessment target is not scored", func(t *testing.T) {
		assessment := validEditionAssessment(t, model.AdminStoryEditionStoryExplorers, adaptationcontract.FindingMotivationChanged)
		_, err := ScoreAssessment(expectation, assessment)
		if err == nil || !strings.Contains(err.Error(), "edition target does not match") {
			t.Fatalf("ScoreAssessment() error = %v", err)
		}
	})

	t.Run("invalid semantic assessment is not scored", func(t *testing.T) {
		assessment := validEditionAssessment(t, growing, adaptationcontract.FindingMotivationChanged)
		assessment.Result = adaptationcontract.ResultPass
		_, err := ScoreAssessment(expectation, assessment)
		if err == nil || !strings.Contains(err.Error(), "semantic assessment is invalid") {
			t.Fatalf("ScoreAssessment() error = %v", err)
		}
	})
}

func TestSummarize(t *testing.T) {
	summary := Summarize([]AssessmentScore{
		{
			ResultMatched:              true,
			RequiredFindingsExpected:   2,
			RequiredFindingsDetected:   2,
			ForbiddenFindingsChecked:   1,
			ForbiddenFindingsTriggered: 0,
			ExpectationMet:             true,
		},
		{
			ResultMatched:              true,
			RequiredFindingsExpected:   1,
			RequiredFindingsDetected:   0,
			ForbiddenFindingsChecked:   2,
			ForbiddenFindingsTriggered: 1,
			ExpectationMet:             false,
		},
	})

	if summary.Trials != 2 ||
		summary.ExpectationsMet != 1 ||
		summary.ResultMatches != 2 ||
		summary.RequiredFindingsExpected != 3 ||
		summary.RequiredFindingsDetected != 2 ||
		summary.ForbiddenFindingsChecked != 3 ||
		summary.ForbiddenFindingsTriggered != 1 {
		t.Fatalf("summary = %#v", summary)
	}
}

func validEditionAssessment(
	t *testing.T,
	key model.AdminStoryEditionKey,
	codes ...adaptationcontract.FindingCode,
) storyvalidation.Assessment {
	t.Helper()
	findings := make([]storyvalidation.Finding, 0, len(codes))
	for _, code := range codes {
		severity, ok := adaptationcontract.CanonicalSeverity(code)
		if !ok {
			t.Fatalf("CanonicalSeverity(%q) not found", code)
		}
		findings = append(findings, storyvalidation.Finding{
			Code:     code,
			Severity: severity,
			Message:  "controlled benchmark defect",
			Evidence: []storyvalidation.Evidence{
				{
					Location:    storyvalidation.EvidenceGeneratedEdition,
					EditionKey:  editionKeyForTest(key),
					Excerpt:     "controlled fixture excerpt",
					Explanation: "The controlled fixture contains the defect under test.",
				},
			},
		})
	}

	result := adaptationcontract.ResultPass
	if len(findings) != 0 {
		result = adaptationcontract.ResultNeedsReview
		for _, finding := range findings {
			if finding.Severity == adaptationcontract.FindingSeverityBlocking {
				result = adaptationcontract.ResultFail
				break
			}
		}
	}
	assessment := storyvalidation.Assessment{
		ValidationVersion:    storyvalidation.ValidationV2,
		SpecificationVersion: storygeneration.SpecificationV2,
		AssessmentScope:      adaptationcontract.AssessmentScopeEdition,
		EditionKey:           editionKeyForTest(key),
		Result:               result,
		Findings:             findings,
	}
	if err := assessment.Validate(); err != nil {
		t.Fatalf("fixture assessment is invalid: %v", err)
	}
	return assessment
}

func editionKeyForTest(key model.AdminStoryEditionKey) *model.AdminStoryEditionKey {
	copyKey := key
	return &copyKey
}
