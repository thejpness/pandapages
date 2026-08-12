package adaptationcontract

import (
	"strings"
	"testing"

	"pandapages/api/internal/model"
)

func validGeneratedEdition() GeneratedEditionInput {
	return GeneratedEditionInput{
		EditionKey: model.AdminStoryEditionStoryExplorers,
		Slug:       "alice-in-wonderland",
		Title:      "Alice in Wonderland",
		Author:     "Lewis Carroll",
		Markdown:   "# Alice in Wonderland\n\nAlice followed the White Rabbit.",
		Language:   "en-GB",
	}
}

func findingCodes(findings []Finding) []FindingCode {
	codes := make([]FindingCode, 0, len(findings))
	for _, finding := range findings {
		codes = append(codes, finding.Code)
	}
	return codes
}

func requireFindingCodes(t *testing.T, validation StructuralValidation, want ...FindingCode) {
	t.Helper()
	got := findingCodes(validation.Findings)
	if len(got) != len(want) {
		t.Fatalf("finding codes = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("finding code %d = %q, want %q; all = %v", index, got[index], want[index], got)
		}
		severity, ok := CanonicalSeverity(got[index])
		if !ok || validation.Findings[index].Severity != severity {
			t.Fatalf("finding %q severity = %q, want canonical %q", got[index], validation.Findings[index].Severity, severity)
		}
		if strings.TrimSpace(validation.Findings[index].Message) == "" {
			t.Fatalf("finding %q message must be non-empty", got[index])
		}
	}
}

func TestValidateGeneratedEditionPassesCleanStructureWithoutIssuingContractPass(t *testing.T) {
	input := validGeneratedEdition()
	validation := ValidateGeneratedEdition(input)

	if !validation.Passed() {
		t.Fatalf("Passed() = false; findings = %#v", validation.Findings)
	}
	if validation.ContractVersion != VersionV1 {
		t.Fatalf("contract version = %q, want %q", validation.ContractVersion, VersionV1)
	}
	if validation.EditionKey != input.EditionKey {
		t.Fatalf("edition key = %q, want %q", validation.EditionKey, input.EditionKey)
	}
	if len(validation.ContentSHA256) != 64 {
		t.Fatalf("content SHA-256 length = %d, want 64", len(validation.ContentSHA256))
	}
	if validation.ContentSHA256 != contentSHA256(input.Markdown) {
		t.Fatal("content SHA-256 must bind the exact Markdown")
	}
}

func TestValidateGeneratedEditionRejectsNonModernEditionKey(t *testing.T) {
	input := validGeneratedEdition()
	input.EditionKey = model.AdminStoryEditionClassic

	validation := ValidateGeneratedEdition(input)

	requireFindingCodes(t, validation, FindingInvalidEditionKey)
}

func TestValidateGeneratedEditionRejectsInvalidUTF8FailClosed(t *testing.T) {
	input := validGeneratedEdition()
	input.Markdown = string([]byte{'#', ' ', 'A', '\n', 0xff})

	validation := ValidateGeneratedEdition(input)

	requireFindingCodes(t, validation, FindingInvalidUTF8)
}

func TestValidateGeneratedEditionRejectsEmptyMarkdownWithoutSecondaryNoise(t *testing.T) {
	input := validGeneratedEdition()
	input.Markdown = " \n\t "

	validation := ValidateGeneratedEdition(input)

	requireFindingCodes(t, validation, FindingEmptyMarkdown)
}

func TestValidateGeneratedEditionRequiresH1AsFirstMarkdownBlock(t *testing.T) {
	input := validGeneratedEdition()
	input.Markdown = "A preface first.\n\n# Alice in Wonderland\n\nStory."

	validation := ValidateGeneratedEdition(input)

	requireFindingCodes(t, validation, FindingMissingH1Title)
}

func TestValidateGeneratedEditionRejectsRawHTMLButNotCode(t *testing.T) {
	t.Run("block html", func(t *testing.T) {
		input := validGeneratedEdition()
		input.Markdown = "# Alice in Wonderland\n\n<div>Hidden structure</div>\n\nStory."

		validation := ValidateGeneratedEdition(input)

		requireFindingCodes(t, validation, FindingRawHTMLPresent)
	})

	t.Run("inline html", func(t *testing.T) {
		input := validGeneratedEdition()
		input.Markdown = "# Alice in Wonderland\n\nAlice met <em>the rabbit</em>."

		validation := ValidateGeneratedEdition(input)

		requireFindingCodes(t, validation, FindingRawHTMLPresent)
	})

	t.Run("fenced code", func(t *testing.T) {
		input := validGeneratedEdition()
		input.Markdown = "# Alice in Wonderland\n\n```html\n<div>example</div>\n```"

		validation := ValidateGeneratedEdition(input)

		if !validation.Passed() {
			t.Fatalf("code fence must not count as raw HTML; findings = %#v", validation.Findings)
		}
	})

	t.Run("inline code", func(t *testing.T) {
		input := validGeneratedEdition()
		input.Markdown = "# Alice in Wonderland\n\nThe text says `<em>rabbit</em>`."

		validation := ValidateGeneratedEdition(input)

		if !validation.Passed() {
			t.Fatalf("inline code must not count as raw HTML; findings = %#v", validation.Findings)
		}
	})
}

func TestValidateGeneratedEditionUsesRealStoryIngestCompatibility(t *testing.T) {
	input := validGeneratedEdition()
	input.Slug = "Alice In Wonderland"

	validation := ValidateGeneratedEdition(input)

	requireFindingCodes(t, validation, FindingIngestIncompatible)
}

func TestValidateGeneratedEditionCanReportMultipleIndependentStructuralFailures(t *testing.T) {
	input := validGeneratedEdition()
	input.EditionKey = model.AdminStoryEditionClassic
	input.Slug = "Bad Slug"
	input.Markdown = "Preface.\n\n<div>raw</div>"

	validation := ValidateGeneratedEdition(input)

	requireFindingCodes(
		t,
		validation,
		FindingInvalidEditionKey,
		FindingMissingH1Title,
		FindingRawHTMLPresent,
		FindingIngestIncompatible,
	)
}

func TestStructuralFindingsCanFeedACombinedFailAssessment(t *testing.T) {
	input := validGeneratedEdition()
	input.Markdown = "# Alice in Wonderland\n\n<div>raw</div>"

	validation := ValidateGeneratedEdition(input)
	requireFindingCodes(t, validation, FindingRawHTMLPresent)

	assessment := Assessment{
		ContractVersion: VersionV1,
		AssessmentScope: AssessmentScopeEdition,
		EditionKey:      editionKey(input.EditionKey),
		Result:          ResultFail,
		Findings:        validation.Findings,
	}
	if err := assessment.Validate(); err != nil {
		t.Fatalf("combined assessment Validate() error = %v", err)
	}
	if err := assessment.ValidateSemantic(); err == nil {
		t.Fatal("semantic assessment must reject deterministic structural findings")
	}
}

func TestValidateClassicSourceIdentityIsExact(t *testing.T) {
	canonical := "# Alice\n\nDown the rabbit-hole.\n"

	t.Run("exact", func(t *testing.T) {
		validation := ValidateClassicSourceIdentity(canonical, canonical)
		if !validation.Passed() {
			t.Fatalf("exact Classic identity findings = %#v", validation.Findings)
		}
		if validation.EditionKey != model.AdminStoryEditionClassic {
			t.Fatalf("edition key = %q, want classic", validation.EditionKey)
		}
		if validation.ContentSHA256 != contentSHA256(canonical) {
			t.Fatal("Classic digest must bind the exact canonical content")
		}
	})

	t.Run("semantic edit", func(t *testing.T) {
		validation := ValidateClassicSourceIdentity(
			canonical,
			"# Alice\n\nShe went down the rabbit-hole.\n",
		)
		requireFindingCodes(t, validation, FindingClassicSourceChanged)
	})

	t.Run("line ending edit", func(t *testing.T) {
		validation := ValidateClassicSourceIdentity(
			canonical,
			strings.ReplaceAll(canonical, "\n", "\r\n"),
		)
		requireFindingCodes(t, validation, FindingClassicSourceChanged)
	})
}
