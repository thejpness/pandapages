// Package copyrighteligibility contains Panda Pages' deterministic,
// evidence-based copyright eligibility policy. It does not make legal
// determinations and deliberately treats incomplete evidence as ineligible for
// product workflow purposes.
package copyrighteligibility

import "time"

// PolicyVersion identifies the deliberately narrow Panda Pages policy used to
// derive an assessment. It is stable application policy, not a source-control
// revision or a claim of legal certainty.
const PolicyVersion = "panda-pages-copyright-v1"

type JurisdictionStatus string

const (
	JurisdictionEligible      JurisdictionStatus = "eligible"
	JurisdictionIneligible    JurisdictionStatus = "ineligible"
	JurisdictionIndeterminate JurisdictionStatus = "indeterminate"
)

type OverallStatus string

const (
	OverallEligible OverallStatus = "eligible"
	OverallBlocked  OverallStatus = "blocked"
)

type ReasonCode string

const (
	ReasonUSProviderPublicDomainConfirmed ReasonCode = "us_provider_public_domain_confirmed"
	ReasonUSProviderRestricted            ReasonCode = "us_provider_restricted"
	ReasonUSProviderRightsMissing         ReasonCode = "us_provider_rights_missing"
	ReasonUSProviderRightsConflict        ReasonCode = "us_provider_rights_conflict"
	ReasonUSHeaderRightsConflict          ReasonCode = "us_header_rights_conflict"
	ReasonUSHeaderRightsUnknown           ReasonCode = "us_header_rights_unknown"

	ReasonUKOrdinaryLiteraryTermExpired       ReasonCode = "uk_ordinary_literary_term_expired"
	ReasonUKOrdinaryLiteraryTermActive        ReasonCode = "uk_ordinary_literary_term_active"
	ReasonUKEvaluationDateInvalid             ReasonCode = "uk_evaluation_date_invalid"
	ReasonUKWorkCategoryUnsupported           ReasonCode = "uk_work_category_unsupported"
	ReasonUKJointAuthorshipUnsupported        ReasonCode = "uk_joint_authorship_unsupported"
	ReasonUKAnonymousAuthorshipUnsupported    ReasonCode = "uk_anonymous_authorship_unsupported"
	ReasonUKPseudonymousAuthorshipUnsupported ReasonCode = "uk_pseudonymous_authorship_unsupported"
	ReasonUKAuthorshipUnsupported             ReasonCode = "uk_authorship_unsupported"
	ReasonUKAuthorIdentityMissing             ReasonCode = "uk_author_identity_missing"
	ReasonUKAuthorDeathUnknown                ReasonCode = "uk_author_death_unknown"
	ReasonUKAuthorEvidenceMissing             ReasonCode = "uk_author_evidence_missing"
	ReasonUKPublicationEvidenceMissing        ReasonCode = "uk_publication_evidence_missing"
	ReasonUKPublicationPosthumousUnsupported  ReasonCode = "uk_publication_posthumous_unsupported"
	ReasonUKTranslationPresent                ReasonCode = "uk_translation_present"
	ReasonUKTranslationUnknown                ReasonCode = "uk_translation_unknown"
	ReasonUKAdditionalContributionPresent     ReasonCode = "uk_additional_contribution_present"
	ReasonUKAdditionalContributionUnknown     ReasonCode = "uk_additional_contribution_unknown"
	ReasonUKSpecialCategoryUnsupported        ReasonCode = "uk_special_category_unsupported"
	ReasonUKUnpublishedHistoryUnsupported     ReasonCode = "uk_unpublished_history_unsupported"

	ReasonOverallEligible ReasonCode = "overall_eligible"
	ReasonOverallBlocked  ReasonCode = "overall_blocked"
)

type ProviderRightsClassification string

const (
	ProviderRightsPublicDomain ProviderRightsClassification = "public_domain"
	ProviderRightsRestricted   ProviderRightsClassification = "restricted"
	ProviderRightsUnknown      ProviderRightsClassification = "unknown"
)

type SourceHeaderRightsClassification string

const (
	SourceHeaderRightsPublicDomain     SourceHeaderRightsClassification = "public_domain"
	SourceHeaderRightsRestricted       SourceHeaderRightsClassification = "restricted"
	SourceHeaderRightsNoClassification SourceHeaderRightsClassification = "no_classification"
	SourceHeaderRightsConflicting      SourceHeaderRightsClassification = "conflicting"
)

// USProviderEvidence is the extracted, provider-neutral evidence required by
// the current US policy. The evaluator never consumes provider XML directly.
type USProviderEvidence struct {
	OPDSRights   ProviderRightsClassification
	RDFRights    ProviderRightsClassification
	HeaderRights SourceHeaderRightsClassification
}

// ContributorEvidence is extracted provider metadata. Unknown roles and dates are
// represented explicitly rather than being rewritten as authorship facts.
type ContributorEvidence struct {
	Name      string
	Role      string
	BirthYear *int
	DeathYear *int
}

// ProviderEvidence is a provider-neutral, bounded extraction result. The
// evaluator consumes its classifications, not provider XML structures.
type ProviderEvidence struct {
	Provider        string
	ExternalID      string
	Title           string
	Rights          ProviderRightsClassification
	RightsStatement string
	Contributors    []ContributorEvidence
	Languages       []string
	EvidenceDigest  string
}

type WorkCategory string

const (
	WorkCategoryOrdinaryLiterary WorkCategory = "ordinary_literary"
	WorkCategoryUnknown          WorkCategory = "unknown"
)

type AuthorshipCategory string

const (
	AuthorshipSingleKnown  AuthorshipCategory = "single_known"
	AuthorshipJoint        AuthorshipCategory = "joint"
	AuthorshipAnonymous    AuthorshipCategory = "anonymous"
	AuthorshipPseudonymous AuthorshipCategory = "pseudonymous"
	AuthorshipUnknown      AuthorshipCategory = "unknown"
)

// FactState distinguishes a positively established absence from uncertainty.
// Its zero value is unknown and therefore never makes a work eligible.
type FactState string

const (
	FactNoneConfirmed FactState = "none_confirmed"
	FactPresent       FactState = "present"
	FactUnknown       FactState = "unknown"
)

// EvidenceReference records a factual source. It intentionally has no
// eligibility or override field: people may provide facts, while the policy
// derives the decision.
type EvidenceReference struct {
	Source     string
	Fact       string
	Locator    string
	Identifier string
	Digest     string
}

type PersonEvidence struct {
	Name       string
	DeathYear  int
	References []EvidenceReference
}

type PublicationEvidence struct {
	Year       int
	References []EvidenceReference
}

// UKEvidence deliberately represents only the ordinary published literary
// work subset supported by policy v1. Unsupported categories remain explicit
// rather than being approximated by zero values or prose.
type UKEvidence struct {
	WorkCategory                  WorkCategory
	Authorship                    AuthorshipCategory
	Author                        PersonEvidence
	FirstPublication              PublicationEvidence
	Translation                   FactState
	AdditionalTextualContribution FactState
	SpecialCategory               FactState
	UnpublishedAtEnd1988          FactState
}

type JurisdictionAssessment struct {
	Status JurisdictionStatus
	Reason ReasonCode
}

type Assessment struct {
	PolicyVersion string
	US            JurisdictionAssessment
	UK            JurisdictionAssessment
	Overall       OverallStatus
	OverallReason ReasonCode
}

type Input struct {
	// EvaluationDate is interpreted as a UTC calendar date. Callers must
	// provide it explicitly; the policy does not read the system clock.
	EvaluationDate time.Time
	US             USProviderEvidence
	UK             UKEvidence
}
