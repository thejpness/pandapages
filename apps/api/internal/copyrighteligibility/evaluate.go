package copyrighteligibility

import (
	"strings"
	"time"
)

// Evaluate applies Panda Pages copyright policy v1 to supplied factual
// evidence. It is pure and deterministic: it performs no I/O and does not
// inspect the wall clock.
func Evaluate(input Input) Assessment {
	us := evaluateUS(input.US)
	uk := evaluateUK(input.EvaluationDate, input.UK)
	overall, overallReason := overallStatus(us.Status, uk.Status)
	return Assessment{
		PolicyVersion: PolicyVersion,
		US:            us,
		UK:            uk,
		Overall:       overall,
		OverallReason: overallReason,
	}
}

func overallStatus(us, uk JurisdictionStatus) (OverallStatus, ReasonCode) {
	if us == JurisdictionEligible && uk == JurisdictionEligible {
		return OverallEligible, ReasonOverallEligible
	}
	return OverallBlocked, ReasonOverallBlocked
}

func evaluateUS(evidence USProviderEvidence) JurisdictionAssessment {
	if evidence.OPDSRights == ProviderRightsRestricted || evidence.RDFRights == ProviderRightsRestricted || evidence.HeaderRights == SourceHeaderRightsRestricted {
		return JurisdictionAssessment{Status: JurisdictionIneligible, Reason: ReasonUSProviderRestricted}
	}
	if evidence.HeaderRights == SourceHeaderRightsConflicting {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUSHeaderRightsConflict}
	}
	if !validProviderRights(evidence.OPDSRights) || !validProviderRights(evidence.RDFRights) {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUSProviderRightsMissing}
	}
	if !validHeaderRights(evidence.HeaderRights) {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUSHeaderRightsUnknown}
	}
	if evidence.OPDSRights == ProviderRightsUnknown || evidence.RDFRights == ProviderRightsUnknown {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUSProviderRightsMissing}
	}
	if evidence.OPDSRights != evidence.RDFRights {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUSProviderRightsConflict}
	}
	if evidence.OPDSRights != ProviderRightsPublicDomain {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUSProviderRightsMissing}
	}
	return JurisdictionAssessment{Status: JurisdictionEligible, Reason: ReasonUSProviderPublicDomainConfirmed}
}

func evaluateUK(evaluationDate time.Time, evidence UKEvidence) JurisdictionAssessment {
	date := evaluationDate.UTC()
	if date.IsZero() {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKEvaluationDateInvalid}
	}
	if evidence.WorkCategory != WorkCategoryOrdinaryLiterary {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKWorkCategoryUnsupported}
	}
	switch evidence.Authorship {
	case AuthorshipSingleKnown:
	case AuthorshipJoint:
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKJointAuthorshipUnsupported}
	case AuthorshipAnonymous:
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKAnonymousAuthorshipUnsupported}
	case AuthorshipPseudonymous:
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKPseudonymousAuthorshipUnsupported}
	default:
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKAuthorshipUnsupported}
	}
	if strings.TrimSpace(evidence.Author.Name) == "" {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKAuthorIdentityMissing}
	}
	if evidence.Author.DeathYear == 0 {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKAuthorDeathUnknown}
	}
	if !hasEvidence(evidence.Author.References) {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKAuthorEvidenceMissing}
	}
	if evidence.Translation == FactPresent {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKTranslationPresent}
	}
	if evidence.Translation != FactNoneConfirmed {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKTranslationUnknown}
	}
	if evidence.AdditionalTextualContribution == FactPresent {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKAdditionalContributionPresent}
	}
	if evidence.AdditionalTextualContribution != FactNoneConfirmed {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKAdditionalContributionUnknown}
	}
	if evidence.SpecialCategory != FactNoneConfirmed {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKSpecialCategoryUnsupported}
	}
	if evidence.UnpublishedAtEnd1988 != FactNoneConfirmed {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKUnpublishedHistoryUnsupported}
	}
	if evidence.FirstPublication.Year == 0 || !hasEvidence(evidence.FirstPublication.References) {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKPublicationEvidenceMissing}
	}
	if evidence.FirstPublication.Year > evidence.Author.DeathYear {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKPublicationPosthumousUnsupported}
	}

	// For the supported ordinary-life policy, the term ends on 31 December of
	// the death year plus 70. A UTC calendar date in the following year is the
	// first date that can satisfy this condition.
	if date.Year() < evidence.Author.DeathYear+71 {
		return JurisdictionAssessment{Status: JurisdictionIneligible, Reason: ReasonUKOrdinaryLiteraryTermActive}
	}
	return JurisdictionAssessment{Status: JurisdictionEligible, Reason: ReasonUKOrdinaryLiteraryTermExpired}
}

func validProviderRights(value ProviderRightsClassification) bool {
	return value == ProviderRightsPublicDomain || value == ProviderRightsRestricted || value == ProviderRightsUnknown
}

func validHeaderRights(value SourceHeaderRightsClassification) bool {
	return value == SourceHeaderRightsPublicDomain || value == SourceHeaderRightsRestricted || value == SourceHeaderRightsNoClassification || value == SourceHeaderRightsConflicting
}

func hasEvidence(references []EvidenceReference) bool {
	for _, reference := range references {
		if strings.TrimSpace(reference.Source) != "" && strings.TrimSpace(reference.Fact) != "" {
			return true
		}
	}
	return false
}
