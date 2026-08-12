package sourceeligibility

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"pandapages/api/internal/copyrighteligibility"
	"pandapages/api/internal/evidenceresolver"
	"pandapages/api/internal/sourceprovider"
)

type gatewayStub struct {
	acquired    sourceprovider.AcquisitionEvidence
	acquireErr  error
	evidence    copyrighteligibility.ProviderEvidence
	evidenceErr error
}

func (s gatewayStub) AcquireEvidence(context.Context, sourceprovider.ID, string) (sourceprovider.AcquisitionEvidence, error) {
	return s.acquired, s.acquireErr
}
func (s gatewayStub) CopyrightEvidence(context.Context, sourceprovider.ID, string) (copyrighteligibility.ProviderEvidence, error) {
	return s.evidence, s.evidenceErr
}

type resolverStub struct {
	resolution evidenceresolver.Resolution
}

func (s resolverStub) Resolve(context.Context, evidenceresolver.ExactSourceContext) (evidenceresolver.Resolution, error) {
	return s.resolution, nil
}

func TestEvaluateBindsProviderFactsAndHumanFacts(t *testing.T) {
	death := 1898
	service, err := New(Config{Gateway: gatewayStub{acquired: acquisitionEvidence("11"), evidence: providerEvidence("11", []copyrighteligibility.ContributorEvidence{{Name: "Lewis Carroll", Role: "author", DeathYear: &death}})}, Now: func() time.Time { return time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := service.Evaluate(context.Background(), sourceprovider.ProjectGutenberg, "11", eligibleHumanEvidence())
	if err != nil || evaluation.Assessment.Overall != copyrighteligibility.OverallEligible || evaluation.EffectiveUKEvidence.Author.Name != "Lewis Carroll" || evaluation.EffectiveUKEvidence.Author.DeathYear != death {
		t.Fatalf("evaluation=%#v err=%v", evaluation, err)
	}
	if len(evaluation.EffectiveUKEvidence.Author.References) != 1 || evaluation.EffectiveUKEvidence.Author.References[0].Source != "Project Gutenberg RDF" {
		t.Fatalf("provider death evidence was not bound: %#v", evaluation.EffectiveUKEvidence.Author.References)
	}
}

func TestEvaluateRejectsManualConflictAndProviderRoleOverrides(t *testing.T) {
	death := 1898
	translator := copyrighteligibility.ContributorEvidence{Name: "Translator", Role: "translator"}
	service, _ := New(Config{Gateway: gatewayStub{acquired: acquisitionEvidence("11"), evidence: providerEvidence("11", []copyrighteligibility.ContributorEvidence{{Name: "Lewis Carroll", Role: "author", DeathYear: &death}, translator})}})
	conflicting := eligibleHumanEvidence()
	otherDeath := 1900
	conflicting.AuthorDeathYear = &otherDeath
	if _, err := service.Evaluate(context.Background(), sourceprovider.ProjectGutenberg, "11", conflicting); !errors.Is(err, ErrHumanEvidenceConflict) {
		t.Fatalf("conflicting death = %v", err)
	}
	evaluation, err := service.Evaluate(context.Background(), sourceprovider.ProjectGutenberg, "11", eligibleHumanEvidence())
	if err != nil || evaluation.EffectiveUKEvidence.Translation.State != copyrighteligibility.FactPresent || evaluation.Assessment.UK.Reason != copyrighteligibility.ReasonUKTranslationPresent {
		t.Fatalf("translator was overridden: %#v / %v", evaluation, err)
	}
}

func TestEvaluateHumanFallbackCannotRepairAutomatedConflicts(t *testing.T) {
	death := 1898
	gateway := gatewayStub{
		acquired: acquisitionEvidence("11"),
		evidence: providerEvidence("11", []copyrighteligibility.ContributorEvidence{{Name: "Lewis Carroll", Role: "author", DeathYear: &death}}),
	}
	human := eligibleHumanEvidence()

	publicationConflict, err := New(Config{Gateway: gateway, Resolver: resolverStub{resolution: evidenceresolver.Resolution{
		WorkTitle:        "Alice",
		FirstPublication: evidenceresolver.ResolvedYear{Status: evidenceresolver.ResolutionConflicting, Reason: evidenceresolver.ReasonEvidenceConflict},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publicationConflict.Evaluate(context.Background(), sourceprovider.ProjectGutenberg, "11", human); !errors.Is(err, ErrHumanEvidenceConflict) {
		t.Fatalf("publication conflict repaired by human evidence: %v", err)
	}

	translationConflict, err := New(Config{Gateway: gateway, Resolver: resolverStub{resolution: evidenceresolver.Resolution{
		WorkTitle:   "Alice",
		Translation: evidenceresolver.ResolvedFact{Status: evidenceresolver.ResolutionConflicting, State: copyrighteligibility.FactUnknown, Reason: evidenceresolver.ReasonEvidenceConflict},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := translationConflict.Evaluate(context.Background(), sourceprovider.ProjectGutenberg, "11", human); !errors.Is(err, ErrHumanEvidenceConflict) {
		t.Fatalf("translation conflict repaired by human evidence: %v", err)
	}

	insufficient, err := New(Config{Gateway: gateway, Resolver: resolverStub{resolution: evidenceresolver.Resolution{
		WorkTitle:        "Alice",
		FirstPublication: evidenceresolver.ResolvedYear{Status: evidenceresolver.ResolutionInsufficient, Reason: evidenceresolver.ReasonEvidenceInsufficient},
		Translation:      evidenceresolver.ResolvedFact{Status: evidenceresolver.ResolutionInsufficient, State: copyrighteligibility.FactUnknown, Reason: evidenceresolver.ReasonEvidenceInsufficient},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := insufficient.Evaluate(context.Background(), sourceprovider.ProjectGutenberg, "11", human)
	if err != nil {
		t.Fatalf("insufficient evidence should retain factual fallback: %v", err)
	}
	if evaluation.EffectiveUKEvidence.FirstPublication.Year != 1865 || evaluation.EffectiveUKEvidence.Translation.State != copyrighteligibility.FactNoneConfirmed {
		t.Fatalf("human fallback was not retained for insufficient evidence: %#v", evaluation.EffectiveUKEvidence)
	}
}

func TestEvaluateRequiresProviderIdentityAndLeavesIllustratorsAlone(t *testing.T) {
	death := 1898
	provider := providerEvidence("12", []copyrighteligibility.ContributorEvidence{{Name: "Lewis Carroll", Role: "author", DeathYear: &death}})
	service, _ := New(Config{Gateway: gatewayStub{acquired: acquisitionEvidence("11"), evidence: provider}})
	if _, err := service.Evaluate(context.Background(), sourceprovider.ProjectGutenberg, "11", eligibleHumanEvidence()); !errors.Is(err, ErrProviderEvidenceInvalid) {
		t.Fatalf("identity mismatch = %v", err)
	}
	service, _ = New(Config{Gateway: gatewayStub{acquired: acquisitionEvidence("11"), evidence: providerEvidence("11", []copyrighteligibility.ContributorEvidence{{Name: "Lewis Carroll", Role: "author", DeathYear: &death}, {Name: "Tenniel", Role: "illustrator"}})}})
	evaluation, err := service.Evaluate(context.Background(), sourceprovider.ProjectGutenberg, "11", eligibleHumanEvidence())
	if err != nil || evaluation.EffectiveUKEvidence.AdditionalTextualContribution.State != copyrighteligibility.FactNoneConfirmed {
		t.Fatalf("illustrator became textual contribution: %#v / %v", evaluation, err)
	}
}

func TestEvaluateMultipleAuthorsCannotBecomeSingleKnown(t *testing.T) {
	death := 1898
	service, _ := New(Config{Gateway: gatewayStub{acquired: acquisitionEvidence("11"), evidence: providerEvidence("11", []copyrighteligibility.ContributorEvidence{{Name: "One", Role: "author", DeathYear: &death}, {Name: "Two", Role: "author", DeathYear: &death}})}})
	evaluation, err := service.Evaluate(context.Background(), sourceprovider.ProjectGutenberg, "11", eligibleHumanEvidence())
	if err != nil || evaluation.EffectiveUKEvidence.Authorship != copyrighteligibility.AuthorshipJoint || evaluation.Assessment.UK.Reason != copyrighteligibility.ReasonUKJointAuthorshipUnsupported {
		t.Fatalf("multiple authors = %#v / %v", evaluation, err)
	}
}

func acquisitionEvidence(id string) sourceprovider.AcquisitionEvidence {
	return sourceprovider.AcquisitionEvidence{Candidate: sourceprovider.SourceCandidate{Provider: sourceprovider.ProjectGutenberg, ExternalID: id, Title: "Alice", SourceText: "Source\n"}, OPDSRights: copyrighteligibility.ProviderRightsPublicDomain, HeaderRights: copyrighteligibility.SourceHeaderRightsPublicDomain}
}
func providerEvidence(id string, contributors []copyrighteligibility.ContributorEvidence) copyrighteligibility.ProviderEvidence {
	return copyrighteligibility.ProviderEvidence{Provider: string(sourceprovider.ProjectGutenberg), ExternalID: id, Title: "Alice", Rights: copyrighteligibility.ProviderRightsPublicDomain, Contributors: contributors, EvidenceDigest: strings.Repeat("a", 64)}
}
func eligibleHumanEvidence() HumanUKEvidence {
	ref := func(fact string) []copyrighteligibility.EvidenceReference {
		return []copyrighteligibility.EvidenceReference{{Source: "Catalogue", Fact: fact}}
	}
	return HumanUKEvidence{WorkCategory: copyrighteligibility.WorkCategoryOrdinaryLiterary, WorkCategoryReferences: ref("Ordinary literary work"), FirstPublication: copyrighteligibility.PublicationEvidence{Year: 1865, References: ref("Published in 1865")}, Translation: copyrighteligibility.FactEvidence{State: copyrighteligibility.FactNoneConfirmed, References: ref("No translation in acquired text")}, AdditionalTextual: copyrighteligibility.FactEvidence{State: copyrighteligibility.FactNoneConfirmed, References: ref("No additional textual contribution")}, UnpublishedAtEnd1988: copyrighteligibility.FactEvidence{State: copyrighteligibility.FactNoneConfirmed, References: ref("Published before 1988")}}
}
