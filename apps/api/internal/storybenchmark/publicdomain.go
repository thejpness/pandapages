package storybenchmark

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"pandapages/api/internal/copyrighteligibility"
	"pandapages/api/internal/sourceprovider"
)

const FixtureKindEligiblePublicDomain FixtureKind = "eligible_public_domain"

type PublicDomainFixture struct {
	BenchmarkVersion      Version
	FixtureKind           FixtureKind
	Provider              sourceprovider.ID
	ExternalID            string
	CanonicalSourcePath   string
	CanonicalSourceSHA256 string
	Canonicalisation      string
	EligibilityPolicy     string
	EligibilityDate       time.Time
	EligibilityAssessment copyrighteligibility.Assessment
	Source                EndToEndSource
}

type publicDomainManifest struct {
	BenchmarkVersion Version                         `json:"benchmarkVersion"`
	FixtureKind      FixtureKind                     `json:"fixtureKind"`
	Source           publicDomainSourceManifest      `json:"source"`
	Eligibility      publicDomainEligibilityManifest `json:"eligibility"`
}

type publicDomainSourceManifest struct {
	ID                    string `json:"id"`
	Slug                  string `json:"slug"`
	Title                 string `json:"title"`
	Author                string `json:"author"`
	Language              string `json:"language"`
	SourceURL             string `json:"sourceUrl"`
	Provider              string `json:"provider"`
	ExternalID            string `json:"externalId"`
	CanonicalSourcePath   string `json:"canonicalSourcePath"`
	CanonicalSourceSHA256 string `json:"canonicalSourceSha256"`
	Canonicalisation      string `json:"canonicalisation"`
}

type publicDomainEligibilityManifest struct {
	PolicyVersion  string                 `json:"policyVersion"`
	EvaluationDate string                 `json:"evaluationDate"`
	US             publicDomainUSManifest `json:"us"`
	UK             publicDomainUKManifest `json:"uk"`
}

type publicDomainUSManifest struct {
	OPDSRights   copyrighteligibility.ProviderRightsClassification     `json:"opdsRights"`
	RDFRights    copyrighteligibility.ProviderRightsClassification     `json:"rdfRights"`
	HeaderRights copyrighteligibility.SourceHeaderRightsClassification `json:"headerRights"`
}

type publicDomainUKManifest struct {
	WorkTitle                     string                                  `json:"workTitle"`
	WorkCategory                  copyrighteligibility.WorkCategory       `json:"workCategory"`
	Authorship                    copyrighteligibility.AuthorshipCategory `json:"authorship"`
	WorkCategoryReferences        []publicDomainReferenceManifest         `json:"workCategoryReferences"`
	AuthorshipReferences          []publicDomainReferenceManifest         `json:"authorshipReferences"`
	Author                        publicDomainPersonManifest              `json:"author"`
	FirstPublication              publicDomainPublicationManifest         `json:"firstPublication"`
	Translation                   publicDomainFactManifest                `json:"translation"`
	AdditionalTextualContribution publicDomainFactManifest                `json:"additionalTextualContribution"`
	UnpublishedAtEnd1988          publicDomainFactManifest                `json:"unpublishedAtEnd1988"`
}

type publicDomainPersonManifest struct {
	Name       string                          `json:"name"`
	DeathYear  int                             `json:"deathYear"`
	References []publicDomainReferenceManifest `json:"references"`
}

type publicDomainPublicationManifest struct {
	Year       int                             `json:"year"`
	References []publicDomainReferenceManifest `json:"references"`
}

type publicDomainFactManifest struct {
	State      copyrighteligibility.FactState  `json:"state"`
	References []publicDomainReferenceManifest `json:"references"`
}

type publicDomainReferenceManifest struct {
	Source     string `json:"source"`
	Fact       string `json:"fact"`
	Locator    string `json:"locator,omitempty"`
	Identifier string `json:"identifier,omitempty"`
	Digest     string `json:"digest,omitempty"`
}

func LoadPublicDomainFixture(root string) (PublicDomainFixture, error) {
	rootPath, err := resolveFixtureRoot(root)
	if err != nil {
		return PublicDomainFixture{}, err
	}

	manifestData, err := readFixtureFile(rootPath, "manifest.json")
	if err != nil {
		return PublicDomainFixture{}, fmt.Errorf("read public-domain fixture manifest: %w", err)
	}
	var manifest publicDomainManifest
	if err := decodeStrictJSON(manifestData, &manifest); err != nil {
		return PublicDomainFixture{}, fmt.Errorf("decode public-domain fixture manifest: %w", err)
	}
	if manifest.BenchmarkVersion != VersionV1 {
		return PublicDomainFixture{}, fmt.Errorf("public-domain fixture benchmark version must equal %q", VersionV1)
	}
	if manifest.FixtureKind != FixtureKindEligiblePublicDomain {
		return PublicDomainFixture{}, fmt.Errorf("public-domain fixture kind must equal %q", FixtureKindEligiblePublicDomain)
	}

	provider, err := validatePublicDomainProvenance(manifest.Source)
	if err != nil {
		return PublicDomainFixture{}, err
	}

	sourceData, err := readFixtureFile(rootPath, manifest.Source.CanonicalSourcePath)
	if err != nil {
		return PublicDomainFixture{}, fmt.Errorf("read public-domain canonical source: %w", err)
	}
	canonicalSource := string(sourceData)
	if strings.TrimSpace(canonicalSource) == "" {
		return PublicDomainFixture{}, fmt.Errorf("public-domain canonical source is empty")
	}
	actualDigest := exactFixtureSHA256(canonicalSource)
	expectedDigest := strings.ToLower(strings.TrimSpace(manifest.Source.CanonicalSourceSHA256))
	if !validFixtureSHA256(expectedDigest) {
		return PublicDomainFixture{}, fmt.Errorf("public-domain canonical source SHA-256 is invalid")
	}
	if actualDigest != expectedDigest {
		return PublicDomainFixture{}, fmt.Errorf("public-domain canonical source SHA-256 does not match committed content")
	}

	assessment, evaluationDate, err := evaluatePublicDomainEligibility(manifest.Eligibility)
	if err != nil {
		return PublicDomainFixture{}, err
	}

	rights := map[string]any{
		"status":              "eligible-public-domain-benchmark-source",
		"publicationEligible": true,
		"policyVersion":       copyrighteligibility.PolicyVersion,
		"evaluationDate":      evaluationDate.Format("2006-01-02"),
		"provider":            string(provider),
		"externalId":          strings.TrimSpace(manifest.Source.ExternalID),
		"sourceSha256":        actualDigest,
	}
	source := EndToEndSource{
		ID:              strings.TrimSpace(manifest.Source.ID),
		Slug:            strings.TrimSpace(manifest.Source.Slug),
		Title:           strings.TrimSpace(manifest.Source.Title),
		Author:          strings.TrimSpace(manifest.Source.Author),
		Language:        strings.TrimSpace(manifest.Source.Language),
		SourceURL:       strings.TrimSpace(manifest.Source.SourceURL),
		Rights:          rights,
		CanonicalSource: canonicalSource,
	}
	if err := source.Validate(); err != nil {
		return PublicDomainFixture{}, fmt.Errorf("public-domain end-to-end source is invalid: %w", err)
	}

	return PublicDomainFixture{
		BenchmarkVersion:      manifest.BenchmarkVersion,
		FixtureKind:           manifest.FixtureKind,
		Provider:              provider,
		ExternalID:            strings.TrimSpace(manifest.Source.ExternalID),
		CanonicalSourcePath:   filepath.Clean(manifest.Source.CanonicalSourcePath),
		CanonicalSourceSHA256: actualDigest,
		Canonicalisation:      strings.TrimSpace(manifest.Source.Canonicalisation),
		EligibilityPolicy:     copyrighteligibility.PolicyVersion,
		EligibilityDate:       evaluationDate,
		EligibilityAssessment: assessment,
		Source:                source,
	}, nil
}

func validatePublicDomainProvenance(source publicDomainSourceManifest) (sourceprovider.ID, error) {
	if !fixtureIDPattern.MatchString(strings.TrimSpace(source.ID)) {
		return "", fmt.Errorf("public-domain fixture source ID is invalid")
	}
	if !fixtureIDPattern.MatchString(strings.TrimSpace(source.Slug)) {
		return "", fmt.Errorf("public-domain fixture source slug is invalid")
	}
	if strings.TrimSpace(source.Title) == "" || strings.TrimSpace(source.Author) == "" || strings.TrimSpace(source.Language) == "" {
		return "", fmt.Errorf("public-domain fixture source identity is incomplete")
	}
	if strings.TrimSpace(source.Canonicalisation) == "" {
		return "", fmt.Errorf("public-domain fixture canonicalisation description is required")
	}
	provider := sourceprovider.ID(strings.TrimSpace(source.Provider))
	if provider != sourceprovider.ProjectGutenberg {
		return "", fmt.Errorf("public-domain benchmark fixture provider must equal %q", sourceprovider.ProjectGutenberg)
	}
	externalID := strings.TrimSpace(source.ExternalID)
	if externalID == "" {
		return "", fmt.Errorf("public-domain benchmark fixture external ID is required")
	}
	expectedURL := fmt.Sprintf("https://www.gutenberg.org/ebooks/%s", externalID)
	if strings.TrimSpace(source.SourceURL) != expectedURL {
		return "", fmt.Errorf("public-domain benchmark fixture source URL must equal %q", expectedURL)
	}
	return provider, nil
}

func evaluatePublicDomainEligibility(manifest publicDomainEligibilityManifest) (copyrighteligibility.Assessment, time.Time, error) {
	if strings.TrimSpace(manifest.PolicyVersion) != copyrighteligibility.PolicyVersion {
		return copyrighteligibility.Assessment{}, time.Time{}, fmt.Errorf("public-domain fixture eligibility policy must equal %q", copyrighteligibility.PolicyVersion)
	}
	evaluationDate, err := time.Parse("2006-01-02", strings.TrimSpace(manifest.EvaluationDate))
	if err != nil {
		return copyrighteligibility.Assessment{}, time.Time{}, fmt.Errorf("public-domain fixture evaluation date is invalid: %w", err)
	}
	input := copyrighteligibility.Input{
		EvaluationDate: evaluationDate,
		US: copyrighteligibility.USProviderEvidence{
			OPDSRights:   manifest.US.OPDSRights,
			RDFRights:    manifest.US.RDFRights,
			HeaderRights: manifest.US.HeaderRights,
		},
		UK: copyrighteligibility.UKEvidence{
			WorkTitle:                     strings.TrimSpace(manifest.UK.WorkTitle),
			WorkCategory:                  manifest.UK.WorkCategory,
			Authorship:                    manifest.UK.Authorship,
			WorkCategoryReferences:        publicDomainReferences(manifest.UK.WorkCategoryReferences),
			AuthorshipReferences:          publicDomainReferences(manifest.UK.AuthorshipReferences),
			Author:                        publicDomainPerson(manifest.UK.Author),
			FirstPublication:              publicDomainPublication(manifest.UK.FirstPublication),
			Translation:                   publicDomainFact(manifest.UK.Translation),
			AdditionalTextualContribution: publicDomainFact(manifest.UK.AdditionalTextualContribution),
			UnpublishedAtEnd1988:          publicDomainFact(manifest.UK.UnpublishedAtEnd1988),
		},
	}
	assessment := copyrighteligibility.Evaluate(input)
	if assessment.PolicyVersion != copyrighteligibility.PolicyVersion {
		return copyrighteligibility.Assessment{}, time.Time{}, fmt.Errorf("public-domain fixture eligibility returned unexpected policy version %q", assessment.PolicyVersion)
	}
	if assessment.Overall != copyrighteligibility.OverallEligible ||
		assessment.US.Status != copyrighteligibility.JurisdictionEligible ||
		assessment.UK.Status != copyrighteligibility.JurisdictionEligible {
		return copyrighteligibility.Assessment{}, time.Time{}, fmt.Errorf(
			"public-domain fixture is blocked by copyright policy: overall=%s us=%s/%s uk=%s/%s",
			assessment.Overall,
			assessment.US.Status,
			assessment.US.Reason,
			assessment.UK.Status,
			assessment.UK.Reason,
		)
	}
	return assessment, evaluationDate.UTC(), nil
}

func publicDomainPerson(value publicDomainPersonManifest) copyrighteligibility.PersonEvidence {
	return copyrighteligibility.PersonEvidence{
		Name:       strings.TrimSpace(value.Name),
		DeathYear:  value.DeathYear,
		References: publicDomainReferences(value.References),
	}
}

func publicDomainPublication(value publicDomainPublicationManifest) copyrighteligibility.PublicationEvidence {
	return copyrighteligibility.PublicationEvidence{
		Year:       value.Year,
		References: publicDomainReferences(value.References),
	}
}

func publicDomainFact(value publicDomainFactManifest) copyrighteligibility.FactEvidence {
	return copyrighteligibility.FactEvidence{
		State:      value.State,
		References: publicDomainReferences(value.References),
	}
}

func publicDomainReferences(values []publicDomainReferenceManifest) []copyrighteligibility.EvidenceReference {
	result := make([]copyrighteligibility.EvidenceReference, 0, len(values))
	for _, value := range values {
		result = append(result, copyrighteligibility.EvidenceReference{
			Source:     strings.TrimSpace(value.Source),
			Fact:       strings.TrimSpace(value.Fact),
			Locator:    strings.TrimSpace(value.Locator),
			Identifier: strings.TrimSpace(value.Identifier),
			Digest:     strings.TrimSpace(value.Digest),
		})
	}
	return result
}

func exactFixtureSHA256(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func validFixtureSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
