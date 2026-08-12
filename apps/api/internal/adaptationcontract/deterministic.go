package adaptationcontract

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode/utf8"

	"pandapages/api/internal/model"
	"pandapages/api/internal/storyingest"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

type GeneratedEditionInput struct {
	EditionKey model.AdminStoryEditionKey
	Slug       string
	Title      string
	Author     string
	Markdown   string
	Language   string
	SourceURL  string
	Rights     map[string]any
}

type StructuralValidation struct {
	ContractVersion ContractVersion
	EditionKey      model.AdminStoryEditionKey
	ContentSHA256   string
	Findings        []Finding
}

func (validation StructuralValidation) Passed() bool {
	return len(validation.Findings) == 0
}

// ValidateGeneratedEdition performs deterministic checks only. A clean result
// is not an adaptation-contract pass: semantic edition assessment is still
// required before a progression ticket can exist.
func ValidateGeneratedEdition(input GeneratedEditionInput) StructuralValidation {
	validation := StructuralValidation{
		ContractVersion: VersionV1,
		EditionKey:      input.EditionKey,
		ContentSHA256:   contentSHA256(input.Markdown),
		Findings:        make([]Finding, 0, 6),
	}

	if !ValidModernEditionKey(input.EditionKey) {
		validation.Findings = append(validation.Findings, structuralFinding(
			FindingInvalidEditionKey,
			"Generated edition key is not a canonical modern Panda Pages edition key.",
		))
	}

	if !utf8.ValidString(input.Markdown) {
		validation.Findings = append(validation.Findings, structuralFinding(
			FindingInvalidUTF8,
			"Generated Markdown is not valid UTF-8.",
		))
		return validation
	}

	if strings.TrimSpace(input.Markdown) == "" {
		validation.Findings = append(validation.Findings, structuralFinding(
			FindingEmptyMarkdown,
			"Generated Markdown is empty.",
		))
		return validation
	}

	startsWithH1, rawHTML := inspectMarkdownStructure(input.Markdown)
	if !startsWithH1 {
		validation.Findings = append(validation.Findings, structuralFinding(
			FindingMissingH1Title,
			"Generated Markdown must begin with an H1 story title.",
		))
	}
	if rawHTML {
		validation.Findings = append(validation.Findings, structuralFinding(
			FindingRawHTMLPresent,
			"Generated Markdown contains raw HTML.",
		))
	}

	if _, err := storyingest.Ingest(storyingest.Input{
		Slug:      input.Slug,
		Title:     input.Title,
		Author:    input.Author,
		Markdown:  input.Markdown,
		Language:  input.Language,
		SourceURL: input.SourceURL,
		Rights:    input.Rights,
	}); err != nil {
		validation.Findings = append(validation.Findings, structuralFinding(
			FindingIngestIncompatible,
			"Generated edition is not compatible with Panda Pages story ingestion.",
		))
	}

	return validation
}

// ValidateClassicSourceIdentity enforces the source-preserving Classic rule.
// Once the canonical source has been established, Classic is an exact
// projection rather than a semantic generation target.
func ValidateClassicSourceIdentity(canonicalMarkdown, classicMarkdown string) StructuralValidation {
	validation := StructuralValidation{
		ContractVersion: VersionV1,
		EditionKey:      model.AdminStoryEditionClassic,
		ContentSHA256:   contentSHA256(classicMarkdown),
		Findings:        []Finding{},
	}
	if canonicalMarkdown != classicMarkdown {
		validation.Findings = append(validation.Findings, structuralFinding(
			FindingClassicSourceChanged,
			"Classic content does not exactly match the canonical source.",
		))
	}
	return validation
}

func structuralFinding(code FindingCode, message string) Finding {
	severity, ok := CanonicalSeverity(code)
	if !ok {
		panic("adaptationcontract: structural finding code is not registered")
	}
	kind, ok := FindingKindFor(code)
	if !ok || kind != FindingKindStructural {
		panic("adaptationcontract: finding code is not structural")
	}
	return Finding{
		Code:     code,
		Severity: severity,
		Message:  message,
	}
}

func contentSHA256(markdown string) string {
	sum := sha256.Sum256([]byte(markdown))
	return hex.EncodeToString(sum[:])
}

func inspectMarkdownStructure(markdown string) (startsWithH1 bool, rawHTML bool) {
	source := []byte(strings.TrimLeft(markdown, "\ufeff \t\r\n"))
	document := goldmark.DefaultParser().Parse(text.NewReader(source))

	if first := document.FirstChild(); first != nil {
		if heading, ok := first.(*ast.Heading); ok && heading.Level == 1 {
			startsWithH1 = true
		}
	}
	rawHTML = containsRawHTML(document)
	return startsWithH1, rawHTML
}

func containsRawHTML(parent ast.Node) bool {
	for node := parent.FirstChild(); node != nil; node = node.NextSibling() {
		switch node.Kind() {
		case ast.KindHTMLBlock, ast.KindRawHTML:
			return true
		}
		if containsRawHTML(node) {
			return true
		}
	}
	return false
}
