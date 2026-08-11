package copyrighteligibility

import (
	"strings"
	"time"
)

// Evaluate applies Panda Pages copyright policy v2 to supplied factual
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
	if date.IsZero() || date.Year() < 1 {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKEvaluationDateInvalid}
	}
	if exception := knownUKException(evidence.WorkTitle, evidence.Author.Name); exception != UKKnownExceptionNone {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: knownExceptionReason(exception)}
	}
	evaluationYear := date.Year()
	if evidence.WorkCategory != WorkCategoryOrdinaryLiterary {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKWorkCategoryUnsupported}
	}
	if !hasEvidence(evidence.WorkCategoryReferences) {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKWorkCategoryEvidenceMissing}
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
	if !hasEvidence(evidence.AuthorshipReferences) {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKAuthorshipEvidenceMissing}
	}
	if strings.TrimSpace(evidence.Author.Name) == "" {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKAuthorIdentityMissing}
	}
	if evidence.Author.DeathYear == 0 {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKAuthorDeathUnknown}
	}
	if evidence.Author.DeathYear < 1 {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKAuthorDeathInvalid}
	}
	if evidence.Author.DeathYear > evaluationYear {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKAuthorDeathFuture}
	}
	if !hasEvidence(evidence.Author.References) {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKAuthorEvidenceMissing}
	}
	if result, ok := evaluateAbsenceFact(evidence.Translation, ReasonUKTranslationPresent, ReasonUKTranslationUnknown, ReasonUKTranslationEvidenceMissing); ok {
		return result
	}
	if result, ok := evaluateAbsenceFact(evidence.AdditionalTextualContribution, ReasonUKAdditionalContributionPresent, ReasonUKAdditionalContributionUnknown, ReasonUKAdditionalContributionEvidenceMissing); ok {
		return result
	}
	if result, ok := evaluateAbsenceFact(evidence.UnpublishedAtEnd1988, ReasonUKUnpublishedHistoryUnsupported, ReasonUKUnpublishedHistoryUnsupported, ReasonUKUnpublishedHistoryEvidenceMissing); ok {
		return result
	}
	if evidence.FirstPublication.Year == 0 || !hasEvidence(evidence.FirstPublication.References) {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKPublicationEvidenceMissing}
	}
	if evidence.FirstPublication.Year < 1 {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKPublicationYearInvalid}
	}
	if evidence.FirstPublication.Year > evaluationYear {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKPublicationYearFuture}
	}
	if evidence.FirstPublication.Year > evidence.Author.DeathYear {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: ReasonUKPublicationPosthumousUnsupported}
	}

	// For the supported ordinary-life policy, the term ends on 31 December of
	// the death year plus 70. A UTC calendar date in the following year is the
	// first date that can satisfy this condition.
	if evaluationYear-evidence.Author.DeathYear < 71 {
		return JurisdictionAssessment{Status: JurisdictionIneligible, Reason: ReasonUKOrdinaryLiteraryTermActive}
	}
	return JurisdictionAssessment{Status: JurisdictionEligible, Reason: ReasonUKOrdinaryLiteraryTermExpired}
}

func evaluateAbsenceFact(fact FactEvidence, present, unknown, missingEvidence ReasonCode) (JurisdictionAssessment, bool) {
	if fact.State == FactPresent {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: present}, true
	}
	if fact.State != FactNoneConfirmed {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: unknown}, true
	}
	if !hasEvidence(fact.References) {
		return JurisdictionAssessment{Status: JurisdictionIndeterminate, Reason: missingEvidence}, true
	}
	return JurisdictionAssessment{}, false
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
