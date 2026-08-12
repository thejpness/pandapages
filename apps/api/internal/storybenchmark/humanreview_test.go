package storybenchmark

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pandapages/api/internal/adaptationcontract"
)

func TestHumanReviewTemplateBindsExactGeneratedContent(t *testing.T) {
	document := endToEndDocumentForHumanReviewTest(t)
	review, err := BuildHumanReviewTemplate(document)
	if err != nil {
		t.Fatalf("BuildHumanReviewTemplate() error = %v", err)
	}
	if review.SourceSHA256 != document.Source.SourceSHA256 || len(review.Targets) != 5 {
		t.Fatalf("review = %#v", review)
	}
	for _, target := range review.Targets {
		if target.AnalysisSHA256 != document.Run.Generations[0].AnalysisArtifact.AnalysisSHA256 {
			t.Fatalf("analysis binding = %q", target.AnalysisSHA256)
		}
		if len(target.ContentBindings) == 0 {
			t.Fatal("review target has no content binding")
		}
	}
}

func TestScoreHumanReviewMatchesPassValidator(t *testing.T) {
	document := endToEndDocumentForHumanReviewTest(t)
	review := completedPassHumanReviewForTest(t, document)
	score, err := ScoreHumanReview(document, review)
	if err != nil {
		t.Fatalf("ScoreHumanReview() error = %v", err)
	}
	if score.Summary.Trials != 5 || score.Summary.Agreements != 5 || score.Summary.ResultMatches != 5 || score.Summary.ExactFindingMatches != 5 {
		t.Fatalf("summary = %#v", score.Summary)
	}
	if len(score.ByValidator) != 1 || score.ByValidator[0].Summary.Agreements != 5 {
		t.Fatalf("by validator = %#v", score.ByValidator)
	}
}

func TestScoreHumanReviewRejectsStaleContentBinding(t *testing.T) {
	document := endToEndDocumentForHumanReviewTest(t)
	review := completedPassHumanReviewForTest(t, document)
	review.Targets[0].ContentBindings[0].ContentSHA256 = strings.Repeat("a", 64)
	if _, err := ScoreHumanReview(document, review); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("ScoreHumanReview() error = %v", err)
	}
}

func TestScoreHumanReviewSeparatesResultAndFindingAgreement(t *testing.T) {
	document := endToEndDocumentForHumanReviewTest(t)
	review := completedPassHumanReviewForTest(t, document)
	review.Targets[0].ExpectedResult = adaptationcontract.ResultFail
	review.Targets[0].ExpectedFindingCodes = []adaptationcontract.FindingCode{adaptationcontract.FindingMotivationChanged}
	score, err := ScoreHumanReview(document, review)
	if err != nil {
		t.Fatalf("ScoreHumanReview() error = %v", err)
	}
	if score.Summary.Trials != 5 || score.Summary.Agreements != 4 || score.Summary.ResultMatches != 4 || score.Summary.ExactFindingMatches != 4 {
		t.Fatalf("summary = %#v", score.Summary)
	}
	first := score.Trials[0]
	if first.ResultMatch || first.ExactFindingMatch || first.Agreement || len(first.MissingExpectedCodes) != 1 {
		t.Fatalf("first score = %#v", first)
	}
}

func TestLoadHumanReviewRejectsPendingTemplate(t *testing.T) {
	document := endToEndDocumentForHumanReviewTest(t)
	review, err := BuildHumanReviewTemplate(document)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := MarshalHumanReviewJSON(review)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "human-review.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadHumanReviewDocument(path); err == nil || !strings.Contains(err.Error(), "pending") {
		t.Fatalf("LoadHumanReviewDocument() error = %v", err)
	}
}

func TestWriteHumanReviewScoreArtifactsIsExclusive(t *testing.T) {
	document := endToEndDocumentForHumanReviewTest(t)
	review := completedPassHumanReviewForTest(t, document)
	score, err := ScoreHumanReview(document, review)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	written, err := WriteHumanReviewScoreArtifacts(directory, score)
	if err != nil {
		t.Fatalf("WriteHumanReviewScoreArtifacts() error = %v", err)
	}
	for _, path := range []string{written.ScoreJSON, written.ReportMD} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o, want 600", filepath.Base(path), info.Mode().Perm())
		}
	}
	if _, err := WriteHumanReviewScoreArtifacts(directory, score); err == nil {
		t.Fatal("second WriteHumanReviewScoreArtifacts() unexpectedly succeeded")
	}
}

func TestHumanReviewNonPassRequiresExpectedFindingCode(t *testing.T) {
	document := endToEndDocumentForHumanReviewTest(t)
	review := completedPassHumanReviewForTest(t, document)
	review.Targets[0].ExpectedResult = adaptationcontract.ResultFail
	review.Targets[0].ExpectedFindingCodes = []adaptationcontract.FindingCode{}
	if _, err := ScoreHumanReview(document, review); err == nil || !strings.Contains(err.Error(), "requires at least one expected finding code") {
		t.Fatalf("ScoreHumanReview() error = %v", err)
	}
}

func completedPassHumanReviewForTest(t *testing.T, document EndToEndResultDocument) HumanReviewDocument {
	t.Helper()
	review, err := BuildHumanReviewTemplate(document)
	if err != nil {
		t.Fatalf("BuildHumanReviewTemplate() error = %v", err)
	}
	for index := range review.Targets {
		review.Targets[index].ReviewStatus = HumanReviewComplete
		review.Targets[index].ExpectedResult = adaptationcontract.ResultPass
		review.Targets[index].ExpectedFindingCodes = []adaptationcontract.FindingCode{}
		review.Targets[index].Note = "reviewed against exact generated content"
	}
	if err := validateHumanReviewShape(review, true); err != nil {
		t.Fatalf("completed review is invalid: %v", err)
	}
	return review
}
