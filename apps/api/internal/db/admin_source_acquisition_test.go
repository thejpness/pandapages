package db

import (
	"errors"
	"strings"
	"testing"
	"time"

	"pandapages/api/internal/model"
	"pandapages/api/internal/sourceprovider"
)

func TestAdminSourceAcquisitionInputHashesImmutableEvidence(t *testing.T) {
	candidate := testSourceAcquisitionCandidate()
	first, err := adminSourceAcquisitionInput(candidate)
	if err != nil {
		t.Fatalf("first input: %v", err)
	}
	second, err := adminSourceAcquisitionInput(candidate)
	if err != nil {
		t.Fatalf("second input: %v", err)
	}
	if first.SnapshotHash != second.SnapshotHash {
		t.Fatalf("same candidate snapshot hashes differ: %q / %q", first.SnapshotHash, second.SnapshotHash)
	}

	for name, mutate := range map[string]func(*sourceprovider.SourceCandidate){
		"provider rights": func(candidate *sourceprovider.SourceCandidate) {
			candidate.ProviderRights = "Different provider rights information"
		},
		"selected representation": func(candidate *sourceprovider.SourceCandidate) {
			candidate.SelectedRepresentation.URL = "https://www.gutenberg.org/files/11/11.txt"
		},
		"retrieved hash": func(candidate *sourceprovider.SourceCandidate) {
			candidate.RetrievedContentHash = strings.Repeat("b", 64)
		},
		"source text": func(candidate *sourceprovider.SourceCandidate) {
			candidate.SourceText = "Changed source text.\n"
			candidate.NormalisedContentHash = sourceAcquisitionSHA256(candidate.SourceText)
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := candidate
			mutate(&changed)
			input, err := adminSourceAcquisitionInput(changed)
			if err != nil {
				t.Fatalf("changed input: %v", err)
			}
			if input.SnapshotHash == first.SnapshotHash {
				t.Fatalf("%s did not change snapshot hash", name)
			}
		})
	}
}

func TestAdminSourceAcquisitionInputRejectsInconsistentSourceHash(t *testing.T) {
	candidate := testSourceAcquisitionCandidate()
	candidate.NormalisedContentHash = strings.Repeat("c", 64)
	_, err := adminSourceAcquisitionInput(candidate)
	var validation *model.AdminValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %v, want validation error", err)
	}
	if len(validation.Issues) != 1 || validation.Issues[0].Field != "sourceText" || validation.Issues[0].Code != "hash_mismatch" {
		t.Fatalf("issues = %#v", validation.Issues)
	}
}

func TestSourceEligibilityAssessmentHashUsesSemanticEvidenceNotTimestamp(t *testing.T) {
	candidate := testSourceAcquisitionCandidate()
	acquisition, err := adminSourceAcquisitionInput(candidate)
	if err != nil {
		t.Fatal(err)
	}
	first := testEligibleEvaluation(candidate)
	second := first
	second.EvaluatedAt = first.EvaluatedAt.Add(2 * time.Hour)
	firstInput, err := sourceEligibilityAssessmentInputFor("11111111-1111-4111-8111-111111111111", acquisition, first)
	if err != nil {
		t.Fatal(err)
	}
	secondInput, err := sourceEligibilityAssessmentInputFor("11111111-1111-4111-8111-111111111111", acquisition, second)
	if err != nil {
		t.Fatal(err)
	}
	if firstInput.Hash != secondInput.Hash {
		t.Fatalf("timestamp-only assessment hashes differ: %s / %s", firstInput.Hash, secondInput.Hash)
	}
	second.ProviderEvidence.EvidenceDigest = strings.Repeat("e", 64)
	changedInput, err := sourceEligibilityAssessmentInputFor("11111111-1111-4111-8111-111111111111", acquisition, second)
	if err != nil {
		t.Fatal(err)
	}
	if firstInput.Hash == changedInput.Hash {
		t.Fatal("provider evidence change did not affect assessment hash")
	}
}

func TestDecodeStoredSourceEligibilityJSONRejectsRemovedSpecialCategory(t *testing.T) {
	var effective model.AdminSourceEligibilityEffectiveUKEvidence
	err := decodeStoredSourceEligibilityJSON(`{"workTitle":"Alice's Adventures in Wonderland","specialCategory":{"state":"none_confirmed","references":[]}}`, &effective)
	if err == nil {
		t.Fatal("removed specialCategory was accepted in stored assessment evidence")
	}
}

func TestCanonicalSourceAcquisitionQualityReview(t *testing.T) {
	tests := []struct {
		name       string
		req        model.AdminSourceQualityReviewUpdateRequest
		wantStatus model.AdminSourceQualityStatus
		wantNote   *string
		wantError  bool
	}{
		{"pending", model.AdminSourceQualityReviewUpdateRequest{Status: model.AdminSourceQualityPending}, model.AdminSourceQualityPending, nil, false},
		{"approved", model.AdminSourceQualityReviewUpdateRequest{Status: model.AdminSourceQualityApproved, Note: "Reviewed for Panda Pages."}, model.AdminSourceQualityApproved, stringPointer("Reviewed for Panda Pages."), false},
		{"rejected", model.AdminSourceQualityReviewUpdateRequest{Status: model.AdminSourceQualityRejected, Note: "Needs a clearer source."}, model.AdminSourceQualityRejected, stringPointer("Needs a clearer source."), false},
		{"pending note", model.AdminSourceQualityReviewUpdateRequest{Status: model.AdminSourceQualityPending, Note: "Not allowed"}, "", nil, true},
		{"approved without note", model.AdminSourceQualityReviewUpdateRequest{Status: model.AdminSourceQualityApproved}, "", nil, true},
		{"unknown status", model.AdminSourceQualityReviewUpdateRequest{Status: "maybe", Note: "No"}, "", nil, true},
		{"oversized note", model.AdminSourceQualityReviewUpdateRequest{Status: model.AdminSourceQualityRejected, Note: strings.Repeat("a", 4001)}, "", nil, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, note, err := canonicalSourceAcquisitionQualityReview(test.req)
			if test.wantError {
				if err == nil {
					t.Fatal("expected validation error")
				}
				return
			}
			if err != nil || status != test.wantStatus || !sameStringPointer(note, test.wantNote) {
				t.Fatalf("status/note/error = %q/%#v/%v", status, note, err)
			}
		})
	}
}

func testSourceAcquisitionCandidate() sourceprovider.SourceCandidate {
	sourceText := "The source text.\n"
	return sourceprovider.SourceCandidate{
		Provider:       sourceprovider.ProjectGutenberg,
		ExternalID:     "11",
		Title:          "Alice's Adventures in Wonderland",
		Contributors:   []sourceprovider.Contributor{{Name: "Lewis Carroll", Role: "author"}},
		Languages:      []string{"en"},
		LandingURL:     "https://www.gutenberg.org/ebooks/11",
		ProviderRights: "Public domain in the USA.",
		SelectedRepresentation: sourceprovider.Representation{
			Label:     "Plain text UTF-8",
			MediaType: "text/plain; charset=utf-8",
			URL:       "https://www.gutenberg.org/files/11/11-0.txt",
			SizeBytes: 1024,
		},
		NormalisationVersion:  "project-gutenberg-plain-text-v1",
		RetrievedContentHash:  strings.Repeat("a", 64),
		NormalisedContentHash: sourceAcquisitionSHA256(sourceText),
		SourceText:            sourceText,
	}
}

func stringPointer(value string) *string { return &value }

func sameStringPointer(got, want *string) bool {
	if got == nil || want == nil {
		return got == want
	}
	return *got == *want
}
