package copyrighteligibility

import (
	"testing"
	"time"
)

func TestEvaluateUKOrdinaryLiteraryLifePlusSeventyBoundary(t *testing.T) {
	for _, test := range []struct {
		name   string
		date   time.Time
		death  int
		want   JurisdictionStatus
		reason ReasonCode
	}{
		{"last day of the term", time.Date(2025, time.December, 31, 23, 59, 59, 0, time.UTC), 1955, JurisdictionIneligible, ReasonUKOrdinaryLiteraryTermActive},
		{"first day after the term", time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), 1955, JurisdictionEligible, ReasonUKOrdinaryLiteraryTermExpired},
		{"death year 1956 is still active", time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), 1956, JurisdictionIneligible, ReasonUKOrdinaryLiteraryTermActive},
	} {
		t.Run(test.name, func(t *testing.T) {
			assessment := Evaluate(Input{EvaluationDate: test.date, UK: ordinaryLiteraryEvidence(test.death)})
			if assessment.UK.Status != test.want || assessment.UK.Reason != test.reason {
				t.Fatalf("UK=%+v", assessment.UK)
			}
		})
	}
}

func TestEvaluateUKRequiresEvidenceForEachConfirmedAbsence(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*UKEvidence)
		reason ReasonCode
	}{
		{"translation", func(e *UKEvidence) { e.Translation.References = nil }, ReasonUKTranslationEvidenceMissing},
		{"additional textual contribution", func(e *UKEvidence) { e.AdditionalTextualContribution.References = nil }, ReasonUKAdditionalContributionEvidenceMissing},
		{"special category", func(e *UKEvidence) { e.SpecialCategory.References = nil }, ReasonUKSpecialCategoryEvidenceMissing},
		{"unpublished history", func(e *UKEvidence) { e.UnpublishedAtEnd1988.References = nil }, ReasonUKUnpublishedHistoryEvidenceMissing},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence := ordinaryLiteraryEvidence(1898)
			test.modify(&evidence)
			assessment := Evaluate(Input{EvaluationDate: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), UK: evidence})
			if assessment.UK.Status != JurisdictionIndeterminate || assessment.UK.Reason != test.reason {
				t.Fatalf("UK=%+v", assessment.UK)
			}
		})
	}
}

func TestEvaluateUKConfirmedAbsencesWithEvidenceSupportEligiblePath(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*UKEvidence)
	}{
		{"translation", func(e *UKEvidence) {
			e.Translation = confirmedAbsence("No translation appears in the acquired source.")
		}},
		{"additional textual contribution", func(e *UKEvidence) {
			e.AdditionalTextualContribution = confirmedAbsence("No separately protected textual contribution appears in the acquired source.")
		}},
		{"special category", func(e *UKEvidence) {
			e.SpecialCategory = confirmedAbsence("No special statutory category applies to the work.")
		}},
		{"unpublished history", func(e *UKEvidence) {
			e.UnpublishedAtEnd1988 = confirmedAbsence("The work was not unpublished at the end of 1988.")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence := ordinaryLiteraryEvidence(1898)
			test.modify(&evidence)
			assessment := Evaluate(Input{EvaluationDate: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), UK: evidence})
			if assessment.UK.Status != JurisdictionEligible || assessment.UK.Reason != ReasonUKOrdinaryLiteraryTermExpired {
				t.Fatalf("UK=%+v", assessment.UK)
			}
		})
	}
}

func TestEvaluateUKRequiresEvidenceForHumanPassCriticalClassifications(t *testing.T) {
	base := ordinaryLiteraryEvidence(1898)
	base.WorkCategoryReferences = nil
	if got := Evaluate(Input{EvaluationDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), UK: base}).UK; got.Reason != ReasonUKWorkCategoryEvidenceMissing {
		t.Fatalf("work category evidence = %#v", got)
	}
	base = ordinaryLiteraryEvidence(1898)
	base.AuthorshipReferences = nil
	if got := Evaluate(Input{EvaluationDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), UK: base}).UK; got.Reason != ReasonUKAuthorshipEvidenceMissing {
		t.Fatalf("authorship evidence = %#v", got)
	}
}

func TestEvaluateUKFailsClosedOutsideOrdinarySupportedSubset(t *testing.T) {
	valid := ordinaryLiteraryEvidence(1898)
	tests := []struct {
		name   string
		modify func(*UKEvidence)
		want   JurisdictionStatus
		reason ReasonCode
	}{
		{"death year missing", func(e *UKEvidence) { e.Author.DeathYear = 0 }, JurisdictionIndeterminate, ReasonUKAuthorDeathUnknown},
		{"death year invalid", func(e *UKEvidence) { e.Author.DeathYear = -1 }, JurisdictionIndeterminate, ReasonUKAuthorDeathInvalid},
		{"death year future", func(e *UKEvidence) { e.Author.DeathYear = 2027 }, JurisdictionIndeterminate, ReasonUKAuthorDeathFuture},
		{"author evidence missing", func(e *UKEvidence) { e.Author.References = nil }, JurisdictionIndeterminate, ReasonUKAuthorEvidenceMissing},
		{"publication evidence missing", func(e *UKEvidence) { e.FirstPublication.References = nil }, JurisdictionIndeterminate, ReasonUKPublicationEvidenceMissing},
		{"publication year invalid", func(e *UKEvidence) { e.FirstPublication.Year = -1 }, JurisdictionIndeterminate, ReasonUKPublicationYearInvalid},
		{"publication year future", func(e *UKEvidence) { e.FirstPublication.Year = 2027 }, JurisdictionIndeterminate, ReasonUKPublicationYearFuture},
		{"posthumous publication", func(e *UKEvidence) { e.FirstPublication.Year = 1900 }, JurisdictionIndeterminate, ReasonUKPublicationPosthumousUnsupported},
		{"translation present", func(e *UKEvidence) { e.Translation = FactEvidence{State: FactPresent} }, JurisdictionIndeterminate, ReasonUKTranslationPresent},
		{"translation unknown", func(e *UKEvidence) { e.Translation = FactEvidence{State: FactUnknown} }, JurisdictionIndeterminate, ReasonUKTranslationUnknown},
		{"additional contribution present", func(e *UKEvidence) { e.AdditionalTextualContribution = FactEvidence{State: FactPresent} }, JurisdictionIndeterminate, ReasonUKAdditionalContributionPresent},
		{"additional contribution unknown", func(e *UKEvidence) { e.AdditionalTextualContribution = FactEvidence{State: FactUnknown} }, JurisdictionIndeterminate, ReasonUKAdditionalContributionUnknown},
		{"joint authorship", func(e *UKEvidence) { e.Authorship = AuthorshipJoint }, JurisdictionIndeterminate, ReasonUKJointAuthorshipUnsupported},
		{"anonymous authorship", func(e *UKEvidence) { e.Authorship = AuthorshipAnonymous }, JurisdictionIndeterminate, ReasonUKAnonymousAuthorshipUnsupported},
		{"pseudonymous authorship", func(e *UKEvidence) { e.Authorship = AuthorshipPseudonymous }, JurisdictionIndeterminate, ReasonUKPseudonymousAuthorshipUnsupported},
		{"special category", func(e *UKEvidence) { e.SpecialCategory = FactEvidence{State: FactPresent} }, JurisdictionIndeterminate, ReasonUKSpecialCategoryUnsupported},
		{"possible unpublished history", func(e *UKEvidence) { e.UnpublishedAtEnd1988 = FactEvidence{State: FactUnknown} }, JurisdictionIndeterminate, ReasonUKUnpublishedHistoryUnsupported},
		{"unsupported work category", func(e *UKEvidence) { e.WorkCategory = WorkCategoryUnknown }, JurisdictionIndeterminate, ReasonUKWorkCategoryUnsupported},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence := valid
			test.modify(&evidence)
			assessment := Evaluate(Input{EvaluationDate: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), UK: evidence})
			if assessment.UK.Status != test.want || assessment.UK.Reason != test.reason {
				t.Fatalf("UK=%+v", assessment.UK)
			}
		})
	}
}

func TestEvaluateUSRequiresCorroboratingProviderEvidence(t *testing.T) {
	public := USProviderEvidence{OPDSRights: ProviderRightsPublicDomain, RDFRights: ProviderRightsPublicDomain, HeaderRights: SourceHeaderRightsNoClassification}
	tests := []struct {
		name     string
		evidence USProviderEvidence
		want     JurisdictionStatus
		reason   ReasonCode
	}{
		{"opds and rdf public domain", public, JurisdictionEligible, ReasonUSProviderPublicDomainConfirmed},
		{"rdf restricted", USProviderEvidence{OPDSRights: ProviderRightsPublicDomain, RDFRights: ProviderRightsRestricted, HeaderRights: SourceHeaderRightsNoClassification}, JurisdictionIneligible, ReasonUSProviderRestricted},
		{"opds restricted", USProviderEvidence{OPDSRights: ProviderRightsRestricted, RDFRights: ProviderRightsPublicDomain, HeaderRights: SourceHeaderRightsNoClassification}, JurisdictionIneligible, ReasonUSProviderRestricted},
		{"header restricted", USProviderEvidence{OPDSRights: ProviderRightsPublicDomain, RDFRights: ProviderRightsPublicDomain, HeaderRights: SourceHeaderRightsRestricted}, JurisdictionIneligible, ReasonUSProviderRestricted},
		{"rdf unknown", USProviderEvidence{OPDSRights: ProviderRightsPublicDomain, RDFRights: ProviderRightsUnknown, HeaderRights: SourceHeaderRightsNoClassification}, JurisdictionIndeterminate, ReasonUSProviderRightsMissing},
		{"opds unknown", USProviderEvidence{OPDSRights: ProviderRightsUnknown, RDFRights: ProviderRightsPublicDomain, HeaderRights: SourceHeaderRightsNoClassification}, JurisdictionIndeterminate, ReasonUSProviderRightsMissing},
		{"header conflict", USProviderEvidence{OPDSRights: ProviderRightsPublicDomain, RDFRights: ProviderRightsPublicDomain, HeaderRights: SourceHeaderRightsConflicting}, JurisdictionIndeterminate, ReasonUSHeaderRightsConflict},
		{"malformed classification", USProviderEvidence{OPDSRights: "unexpected", RDFRights: ProviderRightsPublicDomain, HeaderRights: SourceHeaderRightsNoClassification}, JurisdictionIndeterminate, ReasonUSProviderRightsMissing},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assessment := Evaluate(Input{US: test.evidence})
			if assessment.US.Status != test.want || assessment.US.Reason != test.reason {
				t.Fatalf("US=%+v", assessment.US)
			}
		})
	}
}

func TestOverallRequiresBothJurisdictionsToBeEligible(t *testing.T) {
	statuses := []JurisdictionStatus{JurisdictionEligible, JurisdictionIneligible, JurisdictionIndeterminate}
	for _, us := range statuses {
		for _, uk := range statuses {
			t.Run(string(us)+"/"+string(uk), func(t *testing.T) {
				got, _ := overallStatus(us, uk)
				want := OverallBlocked
				if us == JurisdictionEligible && uk == JurisdictionEligible {
					want = OverallEligible
				}
				if got != want {
					t.Fatalf("overall=%q want=%q", got, want)
				}
			})
		}
	}
}

func ordinaryLiteraryEvidence(deathYear int) UKEvidence {
	return UKEvidence{
		WorkCategory:           WorkCategoryOrdinaryLiterary,
		Authorship:             AuthorshipSingleKnown,
		WorkCategoryReferences: []EvidenceReference{{Source: "Bibliographic record", Fact: "The acquired work is an ordinary literary work."}},
		AuthorshipReferences:   []EvidenceReference{{Source: "Author authority record", Fact: "The provider identifies one author."}},
		Author: PersonEvidence{
			Name:      "Lewis Carroll",
			DeathYear: deathYear,
			References: []EvidenceReference{{
				Source: "Author authority record",
				Fact:   "Lewis Carroll died in the stated year.",
			}},
		},
		FirstPublication: PublicationEvidence{
			Year: 1865,
			References: []EvidenceReference{{
				Source: "Bibliographic record",
				Fact:   "First publication was in 1865.",
			}},
		},
		Translation:                   confirmedAbsence("No translation appears in the acquired source."),
		AdditionalTextualContribution: confirmedAbsence("No separately protected textual contribution appears in the acquired source."),
		SpecialCategory:               confirmedAbsence("No special statutory category applies to the work."),
		UnpublishedAtEnd1988:          confirmedAbsence("The work was not unpublished at the end of 1988."),
	}
}

func confirmedAbsence(fact string) FactEvidence {
	return FactEvidence{State: FactNoneConfirmed, References: []EvidenceReference{{
		Source: "Reviewed bibliographic evidence",
		Fact:   fact,
	}}}
}
