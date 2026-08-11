package db

import (
	"testing"
)

func TestCanonicalSourceSnapshotSeparatesManualAndAcquisitionOrigins(t *testing.T) {
	manual := adminCanonicalSource{
		Title: "Alice", Language: "en", Rights: map[string]any{}, SourceText: "Exact source text.\n",
	}
	provider := manual
	provider.Provenance = &canonicalSourceProvenance{
		AcquisitionID:           "11111111-1111-4111-8111-111111111111",
		AcquisitionSnapshotHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		AssessmentID:            "22222222-2222-4222-8222-222222222222",
		Provider:                "project-gutenberg",
		ExternalID:              "11",
		AssessmentHash:          "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
	manualHash, err := canonicalSourceSnapshotHash(manual)
	if err != nil {
		t.Fatal(err)
	}
	providerHash, err := canonicalSourceSnapshotHash(provider)
	if err != nil {
		t.Fatal(err)
	}
	if manualHash == providerHash {
		t.Fatal("manual and reviewed-provider source origins collapsed")
	}
	repeat, err := canonicalSourceSnapshotHash(provider)
	if err != nil || repeat != providerHash {
		t.Fatalf("provider provenance hash not deterministic: %q / %v", repeat, err)
	}
}
