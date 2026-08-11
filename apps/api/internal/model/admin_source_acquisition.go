package model

import "errors"

var ErrAdminSourceAcquisitionNotFound = errors.New("source acquisition was not found")

type AdminSourceAcquisitionOutcome string

const (
	AdminSourceAcquisitionOutcomeCreated AdminSourceAcquisitionOutcome = "created"
	AdminSourceAcquisitionOutcomeReused  AdminSourceAcquisitionOutcome = "reused"
)

type AdminSourceQualityStatus string

const (
	AdminSourceQualityPending  AdminSourceQualityStatus = "pending"
	AdminSourceQualityApproved AdminSourceQualityStatus = "approved"
	AdminSourceQualityRejected AdminSourceQualityStatus = "rejected"
)

type AdminSourceAcquisitionContributor struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

type AdminSourceAcquisitionRepresentation struct {
	Label       *string `json:"label,omitempty"`
	MediaType   string  `json:"mediaType"`
	ProviderURL string  `json:"providerUrl"`
	SizeBytes   *int64  `json:"sizeBytes,omitempty"`
}

type AdminSourceQualityReview struct {
	Status     AdminSourceQualityStatus `json:"status"`
	Note       *string                  `json:"note,omitempty"`
	ReviewedAt *string                  `json:"reviewedAt,omitempty"`
}

type AdminCopyrightEvidenceReference struct {
	Source     string  `json:"source"`
	Fact       string  `json:"fact"`
	Locator    *string `json:"locator,omitempty"`
	Identifier *string `json:"identifier,omitempty"`
	Digest     *string `json:"digest,omitempty"`
}

type AdminCopyrightFactEvidence struct {
	State      string                            `json:"state"`
	References []AdminCopyrightEvidenceReference `json:"references"`
}

// AdminSourceEligibilityHumanEvidence contains facts used by the policy, not
// a human legal conclusion or eligibility override.
type AdminSourceEligibilityHumanEvidence struct {
	WorkCategory           string                            `json:"workCategory"`
	WorkCategoryReferences []AdminCopyrightEvidenceReference `json:"workCategoryReferences"`
	AuthorDeathYear        *int                              `json:"authorDeathYear,omitempty"`
	AuthorDeathReferences  []AdminCopyrightEvidenceReference `json:"authorDeathReferences"`
	FirstPublicationYear   int                               `json:"firstPublicationYear"`
	FirstPublicationRefs   []AdminCopyrightEvidenceReference `json:"firstPublicationReferences"`
	Translation            AdminCopyrightFactEvidence        `json:"translation"`
	AdditionalTextual      AdminCopyrightFactEvidence        `json:"additionalTextualContribution"`
	SpecialCategory        AdminCopyrightFactEvidence        `json:"specialCategory"`
	UnpublishedAtEnd1988   AdminCopyrightFactEvidence        `json:"unpublishedAtEnd1988"`
}

// AdminSourceEligibilityEffectiveUKEvidence is the complete factual dossier
// actually evaluated after provider facts have been bound server-side. It is
// immutable assessment evidence, not browser input.
type AdminSourceEligibilityEffectiveUKEvidence struct {
	WorkCategory           string                            `json:"workCategory"`
	WorkCategoryReferences []AdminCopyrightEvidenceReference `json:"workCategoryReferences"`
	Authorship             string                            `json:"authorship"`
	AuthorshipReferences   []AdminCopyrightEvidenceReference `json:"authorshipReferences"`
	AuthorName             string                            `json:"authorName"`
	AuthorDeathYear        int                               `json:"authorDeathYear"`
	AuthorReferences       []AdminCopyrightEvidenceReference `json:"authorReferences"`
	FirstPublicationYear   int                               `json:"firstPublicationYear"`
	FirstPublicationRefs   []AdminCopyrightEvidenceReference `json:"firstPublicationReferences"`
	Translation            AdminCopyrightFactEvidence        `json:"translation"`
	AdditionalTextual      AdminCopyrightFactEvidence        `json:"additionalTextualContribution"`
	SpecialCategory        AdminCopyrightFactEvidence        `json:"specialCategory"`
	UnpublishedAtEnd1988   AdminCopyrightFactEvidence        `json:"unpublishedAtEnd1988"`
}

type AdminCopyrightContributorEvidence struct {
	Name      string `json:"name"`
	Role      string `json:"role"`
	BirthYear *int   `json:"birthYear,omitempty"`
	DeathYear *int   `json:"deathYear,omitempty"`
}

type AdminCopyrightJurisdiction struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// AdminSourceEligibility is immutable evidence retained for the exact saved
// acquisition. It deliberately does not expose raw RDF or source text.
type AdminSourceEligibility struct {
	PolicyVersion  string                                    `json:"policyVersion"`
	EvaluationDate string                                    `json:"evaluationDate"`
	EvaluatedAt    string                                    `json:"evaluatedAt"`
	US             AdminCopyrightJurisdiction                `json:"us"`
	UK             AdminCopyrightJurisdiction                `json:"uk"`
	Overall        string                                    `json:"overall"`
	OverallReason  string                                    `json:"overallReason"`
	OPDSRights     string                                    `json:"opdsRights"`
	RDFRights      string                                    `json:"rdfRights"`
	HeaderRights   string                                    `json:"headerRights"`
	ProviderTitle  string                                    `json:"providerTitle"`
	Contributors   []AdminCopyrightContributorEvidence       `json:"contributors"`
	RDFDigest      string                                    `json:"rdfDigest"`
	EffectiveUK    AdminSourceEligibilityEffectiveUKEvidence `json:"effectiveUkEvidence"`
	AssessmentHash *string                                   `json:"assessmentHash,omitempty"`
}

type AdminSourceAcquisitionSummary struct {
	ID                     string                               `json:"id"`
	Provider               string                               `json:"provider"`
	ExternalID             string                               `json:"externalId"`
	Title                  string                               `json:"title"`
	Contributors           []AdminSourceAcquisitionContributor  `json:"contributors"`
	Languages              []string                             `json:"languages"`
	LandingURL             string                               `json:"landingUrl"`
	ProviderRights         *string                              `json:"providerRights,omitempty"`
	SelectedRepresentation AdminSourceAcquisitionRepresentation `json:"selectedRepresentation"`
	NormalisationVersion   string                               `json:"normalisationVersion"`
	RetrievedContentHash   string                               `json:"retrievedContentHash"`
	NormalisedContentHash  string                               `json:"normalisedContentHash"`
	SnapshotHash           string                               `json:"snapshotHash"`
	CreatedAt              string                               `json:"createdAt"`
	Eligibility            *AdminSourceEligibility              `json:"eligibility,omitempty"`
	SourceQuality          AdminSourceQualityReview             `json:"sourceQuality"`
}

type AdminSourceAcquisitionDetail struct {
	AdminSourceAcquisitionSummary
	SourceText string `json:"sourceText"`
}

type AdminSourceAcquisitionsListResponse struct {
	Items []AdminSourceAcquisitionSummary `json:"items"`
}

type AdminSourceAcquisitionPersistResponse struct {
	Outcome     AdminSourceAcquisitionOutcome `json:"outcome"`
	Acquisition AdminSourceAcquisitionSummary `json:"acquisition"`
}

type AdminSourceQualityReviewUpdateRequest struct {
	Status AdminSourceQualityStatus `json:"status"`
	Note   string                   `json:"note"`
}
