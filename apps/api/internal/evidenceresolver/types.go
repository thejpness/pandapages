package evidenceresolver

import (
	"context"

	"pandapages/api/internal/copyrighteligibility"
)

type ResolutionStatus string

const (
	ResolutionEstablished  ResolutionStatus = "established"
	ResolutionConflicting  ResolutionStatus = "conflicting"
	ResolutionInsufficient ResolutionStatus = "insufficient"
)

type ReasonCode string

const (
	ReasonEstablished                   ReasonCode = "established"
	ReasonSourceUnavailable             ReasonCode = "source_unavailable"
	ReasonSourceInvalid                 ReasonCode = "source_invalid"
	ReasonEvidenceConflict              ReasonCode = "evidence_conflict"
	ReasonEvidenceInsufficient          ReasonCode = "evidence_insufficient"
	ReasonProviderContributorPresent    ReasonCode = "provider_contributor_present"
	ReasonFrontMatterContributorPresent ReasonCode = "front_matter_contributor_present"
	ReasonPublicationDerivedBefore1989  ReasonCode = "publication_before_1989"
)

type SourceClass string

const (
	SourceProjectGutenberg              SourceClass = "project_gutenberg"
	SourceBibliothequeNationaleDeFrance SourceClass = "bibliotheque_nationale_de_france"
	SourceLibraryOfCongress             SourceClass = "library_of_congress"
	SourceOpenLibrary                   SourceClass = "open_library"
	SourceWikidata                      SourceClass = "wikidata"
)

// PublicationAuthority is Panda Pages' fact-specific trust policy for
// original-work first-publication evidence. It is not a universal statement
// about a source's authority for author identity, edition identity, or another
// fact type.
type PublicationAuthority string

const (
	PublicationAuthorityAuthoritative PublicationAuthority = "authoritative"
	PublicationAuthorityCorroborating PublicationAuthority = "corroborating"
)

// EvidenceItem is bounded factual provenance. Locator is descriptive only; no
// resolver path ever fetches a locator from this value.
type EvidenceItem struct {
	Class      SourceClass
	Source     string
	Identifier string
	Locator    string
	Fact       string
	Digest     string
}

// Identifier binds an externally observed person or work to its source.
type Identifier struct {
	Source SourceClass
	Value  string
}

type Person struct {
	Name        string
	Identifiers []Identifier
	DeathYear   *int
}

// Contributor is a bibliographic observation about a person and their role in
// a work or edition. Roles are source data, never a policy conclusion.
type Contributor struct {
	Name        string
	Role        string
	Identifiers []Identifier
}

// BibliographicRecord is an extracted, bounded record rather than a retained
// upstream payload. It contains observable bibliographic facts only: adapters
// must not classify a work under Panda Pages policy or assert that a legally
// relevant contribution is absent. Sources must not represent an edition date
// as a first-work publication year.
type BibliographicRecord struct {
	Source               SourceClass
	SourceName           string
	Identifier           string
	Locator              string
	Digest               string
	Title                string
	WorkID               string
	EditionID            string
	Authors              []Person
	Contributors         []Contributor
	FirstPublicationYear *int
	Languages            []string
	OriginalLanguages    []string
	Subjects             []string
	MaterialTypes        []string
	// ContributorRolesObserved means the source supplied a structured
	// contributor-role list for the identified exact edition. An empty list is
	// an observable result, not an adapter assertion that a contribution cannot
	// exist outside the record.
	ContributorRolesObserved bool
}

// Query contains only server-owned exact-work metadata. It does not contain a
// caller-controlled URL or user-provided legal conclusion.
type Query struct {
	Provider   string
	ExternalID string
	Title      string
	Authors    []Person
	Languages  []string
}

// BibliographicSource retrieves bounded structured records from a fixed,
// source-owned endpoint.
type BibliographicSource interface {
	SourceClass() SourceClass
	Lookup(context.Context, Query) ([]BibliographicRecord, error)
}

// ExactSourceContext is assembled from the exact provider work and source
// material already acquired by Panda Pages. SourceText is inspected only by
// the bounded front-matter extractor.
type ExactSourceContext struct {
	ProviderEvidence copyrighteligibility.ProviderEvidence
	SourceText       string
	// SourceFrontMatter is a bounded, server-owned prefix of the acquired raw
	// provider text. It is never placed on SourceCandidate or returned to a
	// browser; it only supports positive contributor inspection before source
	// normalisation removes the provider wrapper.
	SourceFrontMatter string
}

type Diagnostic struct {
	Source SourceClass
	Reason ReasonCode
}

type ResolvedWorkCategory struct {
	Status   ResolutionStatus
	Value    copyrighteligibility.WorkCategory
	Reason   ReasonCode
	Evidence []EvidenceItem
}

type ResolvedAuthorship struct {
	Status   ResolutionStatus
	Value    copyrighteligibility.AuthorshipCategory
	Reason   ReasonCode
	Evidence []EvidenceItem
}

type ResolvedAuthor struct {
	Status    ResolutionStatus
	Name      string
	DeathYear int
	Reason    ReasonCode
	Evidence  []EvidenceItem
}

type ResolvedYear struct {
	Status   ResolutionStatus
	Year     int
	Reason   ReasonCode
	Evidence []EvidenceItem
}

type ResolvedFact struct {
	Status   ResolutionStatus
	State    copyrighteligibility.FactState
	Reason   ReasonCode
	Evidence []EvidenceItem
}

// Resolution separates factual status from the existing legal policy result.
// Callers may map it to UKEvidence and invoke copyrighteligibility.Evaluate;
// this package never emits an eligibility decision itself.
type Resolution struct {
	WorkTitle            string
	WorkCategory         ResolvedWorkCategory
	Authorship           ResolvedAuthorship
	Author               ResolvedAuthor
	FirstPublication     ResolvedYear
	Translation          ResolvedFact
	AdditionalTextual    ResolvedFact
	UnpublishedAtEnd1988 ResolvedFact
	Diagnostics          []Diagnostic
}

func IsResolutionStatus(value ResolutionStatus) bool {
	return value == ResolutionEstablished || value == ResolutionConflicting || value == ResolutionInsufficient
}
