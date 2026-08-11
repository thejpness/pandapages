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

// This test exercises synthetic cross-source reconciliation rules only. It is
// not a claim that a production adapter supplies Library of Congress evidence.
func TestResolveSyntheticCrossSourceFactsRemainFailClosedForSpecialCategory(t *testing.T) {
	death := 1898
	publication := 1865
	loc := record(SourceLibraryOfCongress, "loc:alice", "Lewis Carroll", &death, &publication)
	ol := record(SourceOpenLibrary, "ol:alice", "Carroll, Lewis", &death, &publication)
	for _, value := range []*BibliographicRecord{&loc, &ol} {
		value.EditionID = value.Identifier + ":edition"
		value.OriginalLanguages = []string{"en"}
		value.ContributorRolesObserved = true
	}
	resolver := newResolver(t,
		loc,
		ol,
	)
	resolution, err := resolver.Resolve(context.Background(), aliceContext())
	if err != nil {
		t.Fatal(err)
	}
	if resolution.WorkCategory.Status != ResolutionEstablished || resolution.Authorship.Value != copyrighteligibility.AuthorshipSingleKnown || resolution.Author.DeathYear != 1898 || resolution.FirstPublication.Year != 1865 || resolution.UnpublishedAtEnd1988.State != copyrighteligibility.FactNoneConfirmed {
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

func TestResolveNegativeFactsRequireExactObservableEvidence(t *testing.T) {
	death := 1898
	publication := 1865
	exactRecord := record(SourceOpenLibrary, "ol:alice", "Lewis Carroll", &death, &publication)
	exactRecord.EditionID = "OL123M"
	exactRecord.OriginalLanguages = []string{"en"}
	exactRecord.ContributorRolesObserved = true

	resolver := newResolver(t, exactRecord)
	resolution, err := resolver.Resolve(context.Background(), aliceContext())
	if err != nil || resolution.Translation.State != copyrighteligibility.FactNoneConfirmed || resolution.AdditionalTextual.State != copyrighteligibility.FactNoneConfirmed {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}

	missingEdition := exactRecord
	missingEdition.EditionID = ""
	resolver = newResolver(t, missingEdition)
	resolution, err = resolver.Resolve(context.Background(), aliceContext())
	if err != nil || resolution.Translation.Status != ResolutionInsufficient || resolution.AdditionalTextual.Status != ResolutionInsufficient {
		t.Fatalf("missing edition resolution=%#v err=%v", resolution, err)
	}

	wrongLanguage := exactRecord
	wrongLanguage.OriginalLanguages = []string{"fr"}
	resolver = newResolver(t, wrongLanguage)
	resolution, err = resolver.Resolve(context.Background(), aliceContext())
	if err != nil || resolution.Translation.Status != ResolutionInsufficient {
		t.Fatalf("language mismatch resolution=%#v err=%v", resolution, err)
	}

	frontMissing := aliceContext()
	frontMissing.SourceText = "provider wrapper without a marker"
	resolver = newResolver(t, exactRecord)
	resolution, err = resolver.Resolve(context.Background(), frontMissing)
	if err != nil || resolution.Translation.Status != ResolutionInsufficient || resolution.AdditionalTextual.Status != ResolutionInsufficient {
		t.Fatalf("front matter resolution=%#v err=%v", resolution, err)
	}
}

func TestResolveBibliographicPositiveContributorBlocksAbsence(t *testing.T) {
	death := 1898
	publication := 1865
	value := record(SourceOpenLibrary, "ol:alice", "Lewis Carroll", &death, &publication)
	value.EditionID = "OL123M"
	value.OriginalLanguages = []string{"en"}
	value.ContributorRolesObserved = true
	value.Contributors = append(value.Contributors, Contributor{Name: "Example Translator", Role: "translator"})
	resolver := newResolver(t, value)
	resolution, err := resolver.Resolve(context.Background(), aliceContext())
	if err != nil || resolution.Translation.Status != ResolutionEstablished || resolution.Translation.State != copyrighteligibility.FactPresent {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}

	value.Contributors = []Contributor{{Name: "Lewis Carroll", Role: "author"}, {Name: "Example Editor", Role: "editor"}}
	resolver = newResolver(t, value)
	resolution, err = resolver.Resolve(context.Background(), aliceContext())
	if err != nil || resolution.AdditionalTextual.Status != ResolutionEstablished || resolution.AdditionalTextual.State != copyrighteligibility.FactPresent {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
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

func TestResolveFirstPublicationRequiresAuthoritativeIndependentEvidence(t *testing.T) {
	death := 1898
	publication := 1865
	publicationConflict := 1866
	for _, test := range []struct {
		name     string
		records  []BibliographicRecord
		status   ResolutionStatus
		year     int
		evidence int
	}{
		{
			name:     "authoritative plus corroborator",
			records:  []BibliographicRecord{record(SourceLibraryOfCongress, "loc:alice", "Lewis Carroll", &death, &publication), record(SourceOpenLibrary, "ol:alice", "Lewis Carroll", &death, &publication)},
			status:   ResolutionEstablished,
			year:     publication,
			evidence: 2,
		},
		{
			name:     "two corroborators",
			records:  []BibliographicRecord{record(SourceOpenLibrary, "ol:alice", "Lewis Carroll", &death, &publication), record(SourceWikidata, "wd:alice", "Lewis Carroll", &death, &publication)},
			status:   ResolutionInsufficient,
			evidence: 2,
		},
		{
			name:     "authoritative alone",
			records:  []BibliographicRecord{record(SourceLibraryOfCongress, "loc:alice", "Lewis Carroll", &death, &publication)},
			status:   ResolutionInsufficient,
			evidence: 1,
		},
		{
			name:     "authoritative and corroborator conflict",
			records:  []BibliographicRecord{record(SourceLibraryOfCongress, "loc:alice", "Lewis Carroll", &death, &publication), record(SourceOpenLibrary, "ol:alice", "Lewis Carroll", &death, &publicationConflict)},
			status:   ResolutionConflicting,
			evidence: 2,
		},
		{
			name:     "duplicate authoritative class",
			records:  []BibliographicRecord{record(SourceLibraryOfCongress, "loc:alice:one", "Lewis Carroll", &death, &publication), record(SourceLibraryOfCongress, "loc:alice:two", "Lewis Carroll", &death, &publication)},
			status:   ResolutionInsufficient,
			evidence: 2,
		},
		{
			name:     "duplicate corroborating class",
			records:  []BibliographicRecord{record(SourceOpenLibrary, "ol:alice:one", "Lewis Carroll", &death, &publication), record(SourceOpenLibrary, "ol:alice:two", "Lewis Carroll", &death, &publication)},
			status:   ResolutionInsufficient,
			evidence: 2,
		},
		{
			name:   "no publication evidence",
			status: ResolutionInsufficient,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolution := resolveFirstPublication(test.records)
			if resolution.Status != test.status || resolution.Year != test.year || len(resolution.Evidence) != test.evidence {
				t.Fatalf("resolution=%#v", resolution)
			}
		})
	}
}

func TestPublicationAuthorityIsCentralAndFactSpecific(t *testing.T) {
	for _, test := range []struct {
		class SourceClass
		want  PublicationAuthority
	}{
		{SourceLibraryOfCongress, PublicationAuthorityAuthoritative},
		{SourceOpenLibrary, PublicationAuthorityCorroborating},
		{SourceWikidata, PublicationAuthorityCorroborating},
		{SourceProjectGutenberg, ""},
	} {
		if got := publicationAuthority(test.class); got != test.want {
			t.Fatalf("publicationAuthority(%q)=%q want=%q", test.class, got, test.want)
		}
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
	authorID := Identifier{Source: class, Value: identifier + ":author"}
	return BibliographicRecord{Source: class, SourceName: string(class), Identifier: identifier, Locator: "https://example.invalid/" + identifier, Digest: strings.Repeat("a", 64), Title: "Alice's Adventures in Wonderland", WorkID: identifier, Authors: []Person{{Name: author, Identifiers: []Identifier{authorID}, DeathYear: death}}, Contributors: []Contributor{{Name: author, Role: "author", Identifiers: []Identifier{authorID}}}, FirstPublicationYear: publication, Subjects: []string{"Fiction"}}
}

func aliceContext() ExactSourceContext {
	death := 1898
	return exactContext("11", "Alice's Adventures in Wonderland", []copyrighteligibility.ContributorEvidence{{Name: "Lewis Carroll", Role: "author", DeathYear: &death}})
}

func exactContext(id, title string, contributors []copyrighteligibility.ContributorEvidence) ExactSourceContext {
	return ExactSourceContext{ProviderEvidence: copyrighteligibility.ProviderEvidence{Provider: "project-gutenberg", ExternalID: id, Title: title, Contributors: contributors, Languages: []string{"en"}, EvidenceDigest: strings.Repeat("b", 64)}, SourceText: "Project Gutenberg metadata\n*** START OF THE PROJECT GUTENBERG EBOOK EXAMPLE ***\n"}
}
