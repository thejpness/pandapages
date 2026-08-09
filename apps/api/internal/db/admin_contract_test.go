package db

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"pandapages/api/internal/model"
	"pandapages/api/internal/storyingest"
)

func TestAdminPreviewUsesCanonicalDraftInputWithoutStoreAccess(t *testing.T) {
	author := "  Panda Author  "
	language := "cy"
	sourceURL := "  https://example.invalid/source  "
	rights := map[string]any{"label": "Public domain"}
	response, err := (&Store{}).AdminPreview(model.AdminPreviewRequest{
		Slug:      "contract-story",
		Title:     "  Contract Story  ",
		Author:    &author,
		Markdown:  "# Contract Story\n\nOpening words.\n\n## Chapter One\n\nMore words.\n",
		Language:  &language,
		SourceURL: &sourceURL,
		Rights:    rights,
	})
	if err != nil {
		t.Fatalf("AdminPreview: %v", err)
	}
	if response.Slug != "contract-story" || response.Title != "Contract Story" ||
		response.Author == nil || *response.Author != "Panda Author" ||
		response.Language != "cy" || response.SourceURL == nil ||
		*response.SourceURL != "https://example.invalid/source" {
		t.Fatalf("normalized preview metadata = %#v", response)
	}
	if response.SegmentCount != 4 || response.ChapterCount != 1 || response.WordCount <= 0 {
		t.Fatalf("preview counts = segments %d, words %d, chapters %d", response.SegmentCount, response.WordCount, response.ChapterCount)
	}
	if response.Warnings == nil || !reflect.DeepEqual(response.Rights, rights) {
		t.Fatalf("preview warnings/rights = %#v / %#v", response.Warnings, response.Rights)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal preview response: %v", err)
	}
	for _, forbidden := range []string{"storyId", "segments", "contentKey", "locator"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("preview response leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestCanonicalAdminStoryInputReturnsFiniteIssues(t *testing.T) {
	tests := []struct {
		name      string
		request   model.AdminStoryInput
		wantField string
		wantCode  string
	}{
		{
			name:      "required fields",
			request:   model.AdminStoryInput{},
			wantField: "slug",
			wantCode:  "required",
		},
		{
			name: "invalid slug",
			request: model.AdminStoryInput{
				Slug: "Not Canonical", Title: "Story", Markdown: "# Story",
			},
			wantField: "slug",
			wantCode:  "invalid",
		},
		{
			name: "unreadable content",
			request: model.AdminStoryInput{
				Slug: "story", Title: "Story", Markdown: "<!-- no readable text -->",
			},
			wantField: "markdown",
			wantCode:  "invalid",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := canonicalAdminStoryInput(test.request)
			var validationErr *model.AdminValidationError
			if !errors.As(err, &validationErr) || len(validationErr.Issues) == 0 {
				t.Fatalf("validation error = %v", err)
			}
			issue := validationErr.Issues[0]
			if issue.Field != test.wantField || issue.Code != test.wantCode || strings.TrimSpace(issue.Message) == "" {
				t.Fatalf("first issue = %#v", issue)
			}
			if strings.Contains(validationErr.Error(), "goldmark") || strings.Contains(validationErr.Error(), "yaml") {
				t.Fatalf("validation error exposed parser detail: %v", validationErr)
			}
		})
	}
}

func TestAdminDraftEditionKeyDefaultsAndRejectsUnknown(t *testing.T) {
	key, err := adminDraftEditionKey(model.AdminDraftUpsertRequest{})
	if err != nil || key != model.AdminStoryEditionClassic {
		t.Fatalf("default edition = %q / %v", key, err)
	}

	growing := model.AdminStoryEditionGrowingReaders
	key, err = adminDraftEditionKey(model.AdminDraftUpsertRequest{EditionKey: &growing})
	if err != nil || key != growing {
		t.Fatalf("explicit edition = %q / %v", key, err)
	}

	unknown := model.AdminStoryEditionKey("bedtime-ultra")
	_, err = adminDraftEditionKey(model.AdminDraftUpsertRequest{EditionKey: &unknown})
	var validationErr *model.AdminValidationError
	if !errors.As(err, &validationErr) || len(validationErr.Issues) != 1 {
		t.Fatalf("unknown edition error = %#v", err)
	}
	if issue := validationErr.Issues[0]; issue.Field != "editionKey" || issue.Code != "invalid" {
		t.Fatalf("unknown edition issue = %#v", issue)
	}

	want := []model.AdminStoryEditionKey{
		model.AdminStoryEditionClassic,
		model.AdminStoryEditionConfidentReaders,
		model.AdminStoryEditionGrowingReaders,
		model.AdminStoryEditionStoryExplorers,
		model.AdminStoryEditionLittleListeners,
	}
	if got := model.AdminStoryEditionKeys(); !reflect.DeepEqual(got, want) {
		t.Fatalf("canonical edition order = %#v, want %#v", got, want)
	}
}

func TestImmutableAdminMetadataIncludesRightsAndRejectsMalformedUTF8(t *testing.T) {
	rights := map[string]any{"label": "Public domain", "year": 1908}
	output, err := storyingest.Ingest(storyingest.Input{
		Slug:      "metadata-story",
		Title:     "Metadata Story",
		Author:    "Panda Author",
		Markdown:  "# Metadata Story\n\nReadable.\n",
		Language:  "en",
		SourceURL: "https://example.invalid/source",
		Rights:    rights,
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	frontmatterJSON, err := json.Marshal(output.Frontmatter)
	if err != nil {
		t.Fatalf("marshal frontmatter: %v", err)
	}
	normalized, err := normalizeStoredFrontmatter(frontmatterJSON)
	if err != nil {
		t.Fatalf("normalize stored frontmatter: %v", err)
	}
	if normalized.SourceURL == nil || *normalized.SourceURL != "https://example.invalid/source" ||
		!jsonDocumentsEqual(mustJSON(t, normalized.Rights), mustJSON(t, rights)) {
		t.Fatalf("normalized source/rights = %#v / %#v", normalized.SourceURL, normalized.Rights)
	}
	if _, err := normalizeStoredFrontmatter([]byte{0xff, 0xfe}); err == nil {
		t.Fatal("malformed UTF-8 frontmatter was accepted")
	}
}

func TestAdminEditionStatusPolicyIsFinite(t *testing.T) {
	draft := "11111111-1111-4111-8111-111111111111"
	published := "22222222-2222-4222-8222-222222222222"
	tests := []struct {
		row              *adminEditionRow
		versionCount     int
		repair           bool
		publishedVersion *string
		want             model.AdminEditionStatus
	}{
		{row: nil, versionCount: 0, want: model.AdminEditionStatusEmpty},
		{row: &adminEditionRow{DraftVersionID: &draft}, versionCount: 1, want: model.AdminEditionStatusDraftOnly},
		{row: &adminEditionRow{DraftVersionID: &published}, versionCount: 1, publishedVersion: &published, want: model.AdminEditionStatusPublished},
		{row: &adminEditionRow{DraftVersionID: &draft}, versionCount: 2, publishedVersion: &published, want: model.AdminEditionStatusPublishedWithDraft},
		{row: &adminEditionRow{}, versionCount: 1, want: model.AdminEditionStatusUnpublished},
		{row: &adminEditionRow{}, versionCount: 1, repair: true, want: model.AdminEditionStatusRepairRequired},
	}
	for _, test := range tests {
		if got := adminEditionStatus(test.row, test.versionCount, test.repair, test.publishedVersion); got != test.want {
			t.Errorf(
				"adminEditionStatus(%#v, %d, %v, %#v) = %q, want %q",
				test.row,
				test.versionCount,
				test.repair,
				test.publishedVersion,
				got,
				test.want,
			)
		}
	}
}

func TestAdminPointerRejectsMissingOrUnavailableTargets(t *testing.T) {
	missingID := "11111111-1111-4111-8111-111111111111"
	if pointer, valid := adminPointer(&missingID, map[string]inspectedAdminVersion{}); pointer != nil || valid {
		t.Fatalf("missing pointer = %#v/%v, want nil/false", pointer, valid)
	}
	unavailable := map[string]inspectedAdminVersion{
		missingID: {
			Summary: model.AdminVersionSummary{
				VersionID: missingID,
				Version:   1,
				Health:    model.AdminVersionHealthUnavailable,
			},
		},
	}
	pointer, valid := adminPointer(&missingID, unavailable)
	if pointer == nil || pointer.VersionID != missingID || valid {
		t.Fatalf("unavailable pointer = %#v/%v, want safe summary/false", pointer, valid)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return encoded
}
