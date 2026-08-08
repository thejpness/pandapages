package db

import (
	"errors"
	"reflect"
	"testing"

	"pandapages/api/internal/model"
)

func TestCanonicalAdminSourcePreservesBodyAndHashesCompleteSnapshot(t *testing.T) {
	author := "  Lewis Carroll  "
	language := " en "
	sourceURL := " https://example.invalid/alice "
	rights := map[string]any{
		"label": "Public domain",
		"year":  1865,
	}
	body := "ALICE\r\n\r\nCafé 世界\r\n"
	first, err := canonicalAdminSourceInput(model.AdminSourceUpsertRequest{
		Title:      "  Alice's Adventures in Wonderland  ",
		Author:     &author,
		Language:   &language,
		SourceURL:  &sourceURL,
		Rights:     rights,
		SourceText: body,
	})
	if err != nil {
		t.Fatalf("canonical source: %v", err)
	}
	if first.Title != "Alice's Adventures in Wonderland" ||
		first.Author == nil || *first.Author != "Lewis Carroll" ||
		first.Language != "en" ||
		first.SourceURL == nil || *first.SourceURL != "https://example.invalid/alice" ||
		first.SourceText != body ||
		len(first.SnapshotHash) != 64 {
		t.Fatalf("canonical source = %#v", first)
	}

	second, err := canonicalAdminSourceInput(model.AdminSourceUpsertRequest{
		Title:      "Alice's Adventures in Wonderland",
		Author:     first.Author,
		Language:   &first.Language,
		SourceURL:  first.SourceURL,
		Rights:     rights,
		SourceText: body,
	})
	if err != nil {
		t.Fatalf("repeat canonical source: %v", err)
	}
	if first.SnapshotHash != second.SnapshotHash ||
		!reflect.DeepEqual(first.Rights, second.Rights) {
		t.Fatalf("canonical source hash changed: %q / %q", first.SnapshotHash, second.SnapshotHash)
	}

	changedBody := body + "More text.\n"
	changed, err := canonicalAdminSourceInput(model.AdminSourceUpsertRequest{
		Title:      first.Title,
		Author:     first.Author,
		Language:   &first.Language,
		SourceURL:  first.SourceURL,
		Rights:     rights,
		SourceText: changedBody,
	})
	if err != nil {
		t.Fatalf("changed canonical source: %v", err)
	}
	if changed.SnapshotHash == first.SnapshotHash {
		t.Fatal("source-text change did not change snapshot identity")
	}

	changedRights := map[string]any{"label": "Public domain", "year": 1866}
	metadataOnly, err := canonicalAdminSourceInput(model.AdminSourceUpsertRequest{
		Title:      first.Title,
		Author:     first.Author,
		Language:   &first.Language,
		SourceURL:  first.SourceURL,
		Rights:     changedRights,
		SourceText: body,
	})
	if err != nil {
		t.Fatalf("metadata-only canonical source: %v", err)
	}
	if metadataOnly.SnapshotHash == first.SnapshotHash {
		t.Fatal("source metadata change did not change snapshot identity")
	}
}

func TestCanonicalAdminSourceReturnsFiniteValidationIssues(t *testing.T) {
	_, err := canonicalAdminSourceInput(model.AdminSourceUpsertRequest{})
	var validationErr *model.AdminValidationError
	if !errors.As(err, &validationErr) || len(validationErr.Issues) < 2 {
		t.Fatalf("validation error = %#v", err)
	}
	fields := map[string]bool{}
	for _, issue := range validationErr.Issues {
		fields[issue.Field] = true
		if issue.Code == "" || issue.Message == "" {
			t.Fatalf("incomplete source issue = %#v", issue)
		}
	}
	if !fields["title"] || !fields["sourceText"] {
		t.Fatalf("source validation fields = %#v", fields)
	}
}
