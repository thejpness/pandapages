package evidenceresolver

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"pandapages/api/internal/copyrighteligibility"
)

type sourceStub struct {
	class   SourceClass
	records []BibliographicRecord
	err     error
}

func (s sourceStub) SourceClass() SourceClass { return s.class }
func (s sourceStub) Lookup(context.Context, Query) ([]BibliographicRecord, error) {
	return s.records, s.err
}

func TestResolveAliceLikeFactsRemainFailClosedForSpecialCategory(t *testing.T) {
	death := 1898
	publication := 1865
	resolver := newResolver(t,
		record(SourceLibraryOfCongress, "loc:alice", "Lewis Carroll", &death, &publication),
		record(SourceOpenLibrary, "ol:alice", "Carroll, Lewis", &death, &publication),
	)
	resolution, err := resolver.Resolve(context.Background(), aliceContext())
	if err != nil {
		t.Fatal(err)
	}
	if resolution.WorkCategory.Status != ResolutionEstablished || resolution.Authorship.Value != copyrighteligibility.AuthorshipSingleKnown || resolution.Author.DeathYear != 1898 || resolution.FirstPublication.Year != 1865 || resolution.Translation.State != copyrighteligibility.FactNoneConfirmed || resolution.AdditionalTextual.State != copyrighteligibility.FactNoneConfirmed || resolution.UnpublishedAtEnd1988.State != copyrighteligibility.FactNoneConfirmed {
		t.Fatalf("resolution=%#v", resolution)
	}
	if resolution.SpecialCategory.Status != ResolutionInsufficient || resolution.SpecialCategory.Reason != ReasonSpecialCategoryNotAutoResolved {
		t.Fatalf("special=%#v", resolution.SpecialCategory)
	}
	assessment := copyrighteligibility.Evaluate(copyrighteligibility.Input{EvaluationDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), UK: ToUKEvidence(resolution)})
	if assessment.UK.Status != copyrighteligibility.JurisdictionIndeterminate || assessment.UK.Reason != copyrighteligibility.ReasonUKSpecialCategoryUnsupported {
		t.Fatalf("assessment=%#v", assessment)
	}
}

func TestResolveTranslatedWorkCannotCreatePassableDossier(t *testing.T) {
	death := 1902
	publication := 800
	translator := copyrighteligibility.ContributorEvidence{Name: "Samuel Butler", Role: "translator"}
	exact := exactContext("1727", "The Odyssey", []copyrighteligibility.ContributorEvidence{{Name: "Homer", Role: "author", DeathYear: &death}, translator})
	resolver := newResolver(t, record(SourceLibraryOfCongress, "loc:odyssey", "Homer", &death, &publication), record(SourceOpenLibrary, "ol:odyssey", "Homer", &death, &publication))
	resolution, err := resolver.Resolve(context.Background(), exact)
	if err != nil || resolution.Translation.Status != ResolutionEstablished || resolution.Translation.State != copyrighteligibility.FactPresent || resolution.Translation.Reason != ReasonProviderContributorPresent {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if evidence := ToUKEvidence(resolution); evidence.Translation.State != copyrighteligibility.FactPresent {
		t.Fatalf("UK evidence=%#v", evidence)
	}
}

func TestResolveConflictingAndMissingPublicationCannotPass(t *testing.T) {
	death := 1898
	year1865, year1866 := 1865, 1866
	resolver := newResolver(t, record(SourceLibraryOfCongress, "loc:alice", "Lewis Carroll", &death, &year1865), record(SourceOpenLibrary, "ol:alice", "Lewis Carroll", &death, &year1866))
	resolution, err := resolver.Resolve(context.Background(), aliceContext())
	if err != nil || resolution.FirstPublication.Status != ResolutionConflicting {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if evidence := ToUKEvidence(resolution); evidence.FirstPublication.Year != 0 {
		t.Fatalf("conflicting publication mapped=%#v", evidence.FirstPublication)
	}
	resolver, err = New(Config{Sources: []BibliographicSource{sourceStub{class: SourceOpenLibrary}}})
	if err != nil {
		t.Fatal(err)
	}
	resolution, err = resolver.Resolve(context.Background(), aliceContext())
	if err != nil || resolution.FirstPublication.Status != ResolutionInsufficient {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
}

func TestResolveUsesDeterministicAuthorityAndCorroborationRules(t *testing.T) {
	death := 1898
	publication := 1865
	resolver := newResolver(t, record(SourceOpenLibrary, "ol:alice", "Lewis Carroll", &death, &publication))
	resolution, err := resolver.Resolve(context.Background(), aliceContext())
	if err != nil || resolution.FirstPublication.Status != ResolutionInsufficient {
		t.Fatalf("open-library-only=%#v err=%v", resolution.FirstPublication, err)
	}
	resolver = newResolver(t, record(SourceLibraryOfCongress, "loc:alice", "Lewis Carroll", &death, &publication), record(SourceOpenLibrary, "ol:alice", "Lewis Carroll", &death, &publication))
	resolution, err = resolver.Resolve(context.Background(), aliceContext())
	if err != nil || resolution.FirstPublication.Status != ResolutionEstablished {
		t.Fatalf("corroborated=%#v err=%v", resolution.FirstPublication, err)
	}
	publicationConflict := 1866
	resolver = newResolver(t, record(SourceLibraryOfCongress, "loc:alice", "Lewis Carroll", &death, &publication), record(SourceWikidata, "wd:alice", "Lewis Carroll", &death, &publicationConflict))
	resolution, err = resolver.Resolve(context.Background(), aliceContext())
	if err != nil || resolution.FirstPublication.Status != ResolutionConflicting {
		t.Fatalf("strong-source-conflict=%#v err=%v", resolution.FirstPublication, err)
	}
}

func TestResolveSourceFailureIsInsufficientNotFatal(t *testing.T) {
	resolver, err := New(Config{Sources: []BibliographicSource{sourceStub{class: SourceOpenLibrary, err: errors.New("network diagnostic")}}})
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := resolver.Resolve(context.Background(), aliceContext())
	if err != nil || len(resolution.Diagnostics) != 1 || resolution.Diagnostics[0] != (Diagnostic{Source: SourceOpenLibrary, Reason: ReasonSourceUnavailable}) || resolution.WorkCategory.Status != ResolutionInsufficient {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
}

func TestResolveRejectsARecordThatIsNotTheExactProviderWork(t *testing.T) {
	death := 1898
	publication := 1865
	other := record(SourceOpenLibrary, "ol:other", "Lewis Carroll", &death, &publication)
	other.Title = "A different work"
	resolver, err := New(Config{Sources: []BibliographicSource{sourceStub{class: SourceOpenLibrary, records: []BibliographicRecord{other}}}})
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := resolver.Resolve(context.Background(), aliceContext())
	if err != nil || len(resolution.Diagnostics) != 1 || resolution.Diagnostics[0] != (Diagnostic{Source: SourceOpenLibrary, Reason: ReasonSourceInvalid}) || resolution.Authorship.Status != ResolutionInsufficient {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
}

func TestResolveDoesNotBindAuthorOnNameAlone(t *testing.T) {
	death := 1898
	publication := 1865
	weak := record(SourceOpenLibrary, "ol:alice", "Lewis Carroll", &death, &publication)
	weak.Authors[0].Identifiers = nil
	resolver, err := New(Config{Sources: []BibliographicSource{sourceStub{class: SourceOpenLibrary, records: []BibliographicRecord{weak}}}})
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := resolver.Resolve(context.Background(), aliceContext())
	if err != nil || resolution.Authorship.Status != ResolutionInsufficient || resolution.Author.Status != ResolutionInsufficient {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
}

func newResolver(t *testing.T, values ...BibliographicRecord) *Service {
	t.Helper()
	byClass := map[SourceClass][]BibliographicRecord{}
	for _, value := range values {
		byClass[value.Source] = append(byClass[value.Source], value)
	}
	sources := make([]BibliographicSource, 0, len(byClass))
	for _, class := range []SourceClass{SourceLibraryOfCongress, SourceOpenLibrary, SourceWikidata} {
		if records, ok := byClass[class]; ok {
			sources = append(sources, sourceStub{class: class, records: records})
		}
	}
	if len(sources) == 0 {
		sources = []BibliographicSource{sourceStub{class: SourceOpenLibrary}}
	}
	resolver, err := New(Config{Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

func record(class SourceClass, identifier, author string, death, publication *int) BibliographicRecord {
	return BibliographicRecord{Source: class, SourceName: string(class), Identifier: identifier, Locator: "https://example.invalid/" + identifier, Digest: strings.Repeat("a", 64), Title: "Alice's Adventures in Wonderland", Authors: []Person{{Name: author, Identifiers: []Identifier{{Source: class, Value: identifier + ":author"}}, DeathYear: death}}, FirstPublicationYear: publication, WorkCategory: copyrighteligibility.WorkCategoryOrdinaryLiterary, Translation: copyrighteligibility.FactNoneConfirmed, AdditionalTextual: copyrighteligibility.FactNoneConfirmed}
}

func aliceContext() ExactSourceContext {
	death := 1898
	return exactContext("11", "Alice's Adventures in Wonderland", []copyrighteligibility.ContributorEvidence{{Name: "Lewis Carroll", Role: "author", DeathYear: &death}})
}

func exactContext(id, title string, contributors []copyrighteligibility.ContributorEvidence) ExactSourceContext {
	return ExactSourceContext{ProviderEvidence: copyrighteligibility.ProviderEvidence{Provider: "project-gutenberg", ExternalID: id, Title: title, Contributors: contributors, EvidenceDigest: strings.Repeat("b", 64)}}
}
