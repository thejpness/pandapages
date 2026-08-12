// Package copyrighteligibility contains Panda Pages' deterministic,
// evidence-based copyright eligibility policy. It does not make legal
// determinations and deliberately treats incomplete evidence as blocked for
// product workflow purposes.
package copyrighteligibility

import "time"

// PolicyVersion identifies the deliberately narrow Panda Pages policy used to
// derive an assessment. It is stable application policy, not a source-control
// revision or a claim of legal certainty.
const PolicyVersion = "panda-pages-copyright-v3"

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

	ReasonUKOrdinaryLiteraryTermExpired           ReasonCode = "uk_ordinary_literary_term_expired"
	ReasonUKOrdinaryLiteraryTermActive            ReasonCode = "uk_ordinary_literary_term_active"
	ReasonUKEvaluationDateInvalid                 ReasonCode = "uk_evaluation_date_invalid"
	ReasonUKWorkCategoryUnsupported               ReasonCode = "uk_work_category_unsupported"
	ReasonUKWorkCategoryEvidenceMissing           ReasonCode = "uk_work_category_evidence_missing"
	ReasonUKJointAuthorshipUnsupported            ReasonCode = "uk_joint_authorship_unsupported"
	ReasonUKAnonymousAuthorshipUnsupported        ReasonCode = "uk_anonymous_authorship_unsupported"
	ReasonUKPseudonymousAuthorshipUnsupported     ReasonCode = "uk_pseudonymous_authorship_unsupported"
	ReasonUKAuthorshipUnsupported                 ReasonCode = "uk_authorship_unsupported"
	ReasonUKAuthorshipEvidenceMissing             ReasonCode = "uk_authorship_evidence_missing"
	ReasonUKAuthorIdentityMissing                 ReasonCode = "uk_author_identity_missing"
	ReasonUKAuthorDeathUnknown                    ReasonCode = "uk_author_death_unknown"
	ReasonUKAuthorEvidenceMissing                 ReasonCode = "uk_author_evidence_missing"
	ReasonUKPublicationEvidenceMissing            ReasonCode = "uk_publication_evidence_missing"
	ReasonUKPublicationPosthumousUnsupported      ReasonCode = "uk_publication_posthumous_unsupported"
	ReasonUKTranslationPresent                    ReasonCode = "uk_translation_present"
	ReasonUKTranslationUnknown                    ReasonCode = "uk_translation_unknown"
	ReasonUKTranslationEvidenceMissing            ReasonCode = "uk_translation_evidence_missing"
	ReasonUKAdditionalContributionPresent         ReasonCode = "uk_additional_contribution_present"
	ReasonUKAdditionalContributionUnknown         ReasonCode = "uk_additional_contribution_unknown"
	ReasonUKAdditionalContributionEvidenceMissing ReasonCode = "uk_additional_contribution_evidence_missing"
	ReasonUKKnownExceptionPeterPan                ReasonCode = "uk_known_exception_peter_pan"
	ReasonUKKnownExceptionKingJamesBible          ReasonCode = "uk_known_exception_king_james_bible"
	ReasonUKKnownExceptionBookOfCommonPrayer      ReasonCode = "uk_known_exception_book_of_common_prayer"
	ReasonUKUnpublishedHistoryUnsupported         ReasonCode = "uk_unpublished_history_unsupported"
	ReasonUKUnpublishedHistoryEvidenceMissing     ReasonCode = "uk_unpublished_history_evidence_missing"
	ReasonUKAuthorDeathInvalid                    ReasonCode = "uk_author_death_invalid"
	ReasonUKAuthorDeathFuture                     ReasonCode = "uk_author_death_future"
	ReasonUKPublicationYearInvalid                ReasonCode = "uk_publication_year_invalid"
	ReasonUKPublicationYearFuture                 ReasonCode = "uk_publication_year_future"

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
	// FactNoneConfirmed means the applicable Panda Pages screening evidence
	// indicates no material risk for the fact. It is not a universal legal
	// assertion that the relevant circumstance has never existed.
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

// FactEvidence records a fact state and the evidence supporting that state.
// It has no decision or override field: the evaluator derives eligibility.
type FactEvidence struct {
	State      FactState
	References []EvidenceReference
}

// UKEvidence deliberately represents only the ordinary published literary
// work subset supported by policy v3. Unsupported categories remain explicit
// rather than being approximated by zero values or prose.
type UKEvidence struct {
	WorkTitle                     string
	WorkCategory                  WorkCategory
	Authorship                    AuthorshipCategory
	WorkCategoryReferences        []EvidenceReference
	AuthorshipReferences          []EvidenceReference
	Author                        PersonEvidence
	FirstPublication              PublicationEvidence
	Translation                   FactEvidence
	AdditionalTextualContribution FactEvidence
	UnpublishedAtEnd1988          FactEvidence
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

// IsJurisdictionStatus reports whether value is one of the finite policy
// outcomes that may be retained as evidence.
func IsJurisdictionStatus(value JurisdictionStatus) bool {
	return value == JurisdictionEligible || value == JurisdictionIneligible || value == JurisdictionIndeterminate
}

// IsOverallStatus reports whether value is one of the finite overall policy
// outcomes.
func IsOverallStatus(value OverallStatus) bool {
	return value == OverallEligible || value == OverallBlocked
}

// IsReasonCode reports whether value is a reason emitted by policy v3.
func IsReasonCode(value ReasonCode) bool {
	switch value {
	case ReasonUSProviderPublicDomainConfirmed, ReasonUSProviderRestricted,
		ReasonUSProviderRightsMissing, ReasonUSProviderRightsConflict,
		ReasonUSHeaderRightsConflict, ReasonUSHeaderRightsUnknown,
		ReasonUKOrdinaryLiteraryTermExpired, ReasonUKOrdinaryLiteraryTermActive,
		ReasonUKEvaluationDateInvalid, ReasonUKWorkCategoryUnsupported,
		ReasonUKWorkCategoryEvidenceMissing, ReasonUKJointAuthorshipUnsupported,
		ReasonUKAnonymousAuthorshipUnsupported, ReasonUKPseudonymousAuthorshipUnsupported,
		ReasonUKAuthorshipUnsupported, ReasonUKAuthorshipEvidenceMissing,
		ReasonUKAuthorIdentityMissing, ReasonUKAuthorDeathUnknown,
		ReasonUKAuthorEvidenceMissing, ReasonUKPublicationEvidenceMissing,
		ReasonUKPublicationPosthumousUnsupported, ReasonUKTranslationPresent,
		ReasonUKTranslationUnknown, ReasonUKTranslationEvidenceMissing,
		ReasonUKAdditionalContributionPresent, ReasonUKAdditionalContributionUnknown,
		ReasonUKAdditionalContributionEvidenceMissing, ReasonUKKnownExceptionPeterPan,
		ReasonUKKnownExceptionKingJamesBible, ReasonUKKnownExceptionBookOfCommonPrayer,
		ReasonUKUnpublishedHistoryUnsupported,
		ReasonUKUnpublishedHistoryEvidenceMissing, ReasonUKAuthorDeathInvalid,
		ReasonUKAuthorDeathFuture, ReasonUKPublicationYearInvalid,
		ReasonUKPublicationYearFuture, ReasonOverallEligible, ReasonOverallBlocked:
		return true
	default:
		return false
	}
}
