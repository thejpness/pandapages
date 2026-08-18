package evidenceresolver

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"time"

	"pandapages/api/internal/copyrighteligibility"
)

const maxSources = 3

type Config struct {
	Sources []BibliographicSource
	Logger  *slog.Logger
}

type Service struct {
	sources []BibliographicSource
	logger  *slog.Logger
}

func New(cfg Config) (*Service, error) {
	if len(cfg.Sources) == 0 || len(cfg.Sources) > maxSources {
		return nil, errors.New("evidence resolver requires one to three bibliographic sources")
	}
	sources := append([]BibliographicSource(nil), cfg.Sources...)
	seen := make(map[SourceClass]struct{}, len(sources))
	for _, source := range sources {
		if source == nil || !validSourceClass(source.SourceClass()) {
			return nil, errors.New("evidence resolver source is invalid")
		}
		if _, exists := seen[source.SourceClass()]; exists {
			return nil, errors.New("evidence resolver sources must be distinct")
		}
		seen[source.SourceClass()] = struct{}{}
	}
	return &Service{sources: sources, logger: cfg.Logger}, nil
}

// Resolve obtains only bounded source-owned bibliographic records, reconciles
// factual claims deterministically, and never invokes the copyright evaluator.
// A source outage is recorded as an insufficient diagnostic rather than being
// allowed to create an established claim.
func (s *Service) Resolve(ctx context.Context, exact ExactSourceContext) (Resolution, error) {
	if s == nil || len(s.sources) == 0 || !validExactContext(exact) {
		return Resolution{}, errors.New("exact source context is invalid")
	}
	started := time.Now()
	s.log("resolve_started", exact.ProviderEvidence, "source_count", len(s.sources))
	query := Query{
		Provider:   exact.ProviderEvidence.Provider,
		ExternalID: exact.ProviderEvidence.ExternalID,
		Title:      exact.ProviderEvidence.Title,
		Authors:    providerAuthors(exact.ProviderEvidence.Contributors),
		Languages:  append([]string(nil), exact.ProviderEvidence.Languages...),
	}
	records := make([]BibliographicRecord, 0, maxSources)
	diagnostics := make([]Diagnostic, 0, maxSources)
	for _, source := range s.sources {
		found, err := source.Lookup(ctx, query)
		if err != nil {
			reason := ReasonSourceUnavailable
			if errors.Is(err, ErrUnsupportedQuery) {
				reason = ReasonSourceInvalid
			}
			diagnostics = append(diagnostics, Diagnostic{Source: source.SourceClass(), Reason: reason})
			s.log("source_failed", exact.ProviderEvidence, "source", string(source.SourceClass()), "failure_class", string(reason))
			continue
		}
		for _, record := range found {
			if record.Source != source.SourceClass() || !validRecord(record) || NormalisedTitle(record.Title) != NormalisedTitle(query.Title) {
				diagnostics = append(diagnostics, Diagnostic{Source: source.SourceClass(), Reason: ReasonSourceInvalid})
				s.log("source_invalid", exact.ProviderEvidence, "source", string(source.SourceClass()), "failure_class", string(ReasonSourceInvalid))
				continue
			}
			records = append(records, canonicalRecord(record))
		}
	}
	canonicalRecords(records)
	frontSource := exact.SourceFrontMatter
	if frontSource == "" {
		frontSource = exact.SourceText
	}
	front := ExtractFrontMatter(frontSource)
	postMarker := ExtractPostMarkerSignals(frontSource)
	resolution := Resolution{
		WorkTitle:         strings.TrimSpace(exact.ProviderEvidence.Title),
		WorkCategory:      resolveWorkCategory(records),
		Authorship:        resolveAuthorship(exact.ProviderEvidence, records),
		FirstPublication:  resolveFirstPublication(records),
		Translation:       resolveTranslation(exact.ProviderEvidence, records, front, postMarker),
		AdditionalTextual: resolveAdditionalTextual(exact.ProviderEvidence, records, front, postMarker),
		Diagnostics:       diagnostics,
	}
	resolution.Author = resolveAuthor(exact.ProviderEvidence, records, resolution.Authorship)
	resolution.UnpublishedAtEnd1988 = resolveUnpublishedAtEnd1988(resolution.FirstPublication)
	resolution = canonicalResolution(resolution)
	s.log("resolve_completed", exact.ProviderEvidence, "duration", time.Since(started).String(), "record_count", len(records), "diagnostic_count", len(resolution.Diagnostics))
	return resolution, nil
}

func (s *Service) log(operation string, provider copyrighteligibility.ProviderEvidence, attributes ...any) {
	if s.logger == nil {
		return
	}
	fields := []any{"operation", operation, "provider", provider.Provider, "external_id", provider.ExternalID}
	fields = append(fields, attributes...)
	s.logger.Info("copyright evidence resolver", fields...)
}

func validExactContext(exact ExactSourceContext) bool {
	provider := exact.ProviderEvidence
	return strings.TrimSpace(provider.Provider) != "" && strings.TrimSpace(provider.ExternalID) != "" && strings.TrimSpace(provider.Title) != ""
}

func validSourceClass(value SourceClass) bool {
	switch value {
	case SourceBibliothequeNationaleDeFrance, SourceLibraryOfCongress, SourceOpenLibrary, SourceWikidata:
		return true
	default:
		return false
	}
}

func validRecord(record BibliographicRecord) bool {
	if !validSourceClass(record.Source) || strings.TrimSpace(record.SourceName) == "" || strings.TrimSpace(record.Identifier) == "" || strings.TrimSpace(record.Title) == "" {
		return false
	}
	if record.FirstPublicationYear != nil && (*record.FirstPublicationYear < 1 || *record.FirstPublicationYear > 9999) {
		return false
	}
	for _, author := range record.Authors {
		if strings.TrimSpace(author.Name) == "" {
			return false
		}
		if author.DeathYear != nil && (*author.DeathYear < 1 || *author.DeathYear > 9999) {
			return false
		}
	}
	for _, contributor := range record.Contributors {
		if strings.TrimSpace(contributor.Name) == "" || strings.TrimSpace(contributor.Role) == "" {
			return false
		}
	}
	return true
}

func providerAuthors(values []copyrighteligibility.ContributorEvidence) []Person {
	result := make([]Person, 0, len(values))
	for _, value := range values {
		if value.Role == "author" && strings.TrimSpace(value.Name) != "" {
			result = append(result, Person{Name: strings.TrimSpace(value.Name), DeathYear: value.DeathYear})
		}
	}
	return result
}

func providerReference(provider copyrighteligibility.ProviderEvidence, fact string) EvidenceItem {
	return EvidenceItem{Class: SourceProjectGutenberg, Source: "Project Gutenberg RDF", Identifier: provider.ExternalID, Fact: fact, Digest: provider.EvidenceDigest}
}

func recordReference(record BibliographicRecord, fact string) EvidenceItem {
	return EvidenceItem{Class: record.Source, Source: record.SourceName, Identifier: record.Identifier, Locator: record.Locator, Fact: fact, Digest: record.Digest}
}

func resolveWorkCategory(records []BibliographicRecord) ResolvedWorkCategory {
	values := make([]BibliographicRecord, 0, len(records))
	for _, record := range records {
		if hasOrdinaryLiteraryMaterial(record) {
			values = append(values, record)
		}
	}
	if len(values) == 0 {
		return ResolvedWorkCategory{Status: ResolutionInsufficient, Value: copyrighteligibility.WorkCategoryUnknown, Reason: ReasonEvidenceInsufficient}
	}
	if len(distinctClasses(values)) < 2 {
		return ResolvedWorkCategory{Status: ResolutionInsufficient, Value: copyrighteligibility.WorkCategoryUnknown, Reason: ReasonEvidenceInsufficient, Evidence: recordReferences(values, "One bibliographic source class supplies literary material-type evidence.")}
	}
	return ResolvedWorkCategory{Status: ResolutionEstablished, Value: copyrighteligibility.WorkCategoryOrdinaryLiterary, Reason: ReasonEstablished, Evidence: recordReferences(values, "Independent bibliographic records describe an ordinary literary material type.")}
}

func hasOrdinaryLiteraryMaterial(record BibliographicRecord) bool {
	values := append(append([]string(nil), record.Subjects...), record.MaterialTypes...)
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if strings.Contains(value, "fiction") || strings.Contains(value, "novel") || strings.Contains(value, "short stor") || strings.Contains(value, "poetry") || strings.Contains(value, "literature") {
			return true
		}
		// BnF's controlled French subject heading "Littératures" is the
		// direct bibliographic equivalent of the existing literature signal.
		if strings.Contains(value, "littératur") {
			return true
		}
	}
	return false
}

func resolveAuthorship(provider copyrighteligibility.ProviderEvidence, records []BibliographicRecord) ResolvedAuthorship {
	authors := providerAuthors(provider.Contributors)
	switch len(authors) {
	case 0:
		return ResolvedAuthorship{Status: ResolutionInsufficient, Value: copyrighteligibility.AuthorshipUnknown, Reason: ReasonEvidenceInsufficient}
	case 1:
		matches, conflicts := matchingExternalAuthors(authors[0], records)
		if conflicts {
			return ResolvedAuthorship{Status: ResolutionConflicting, Value: copyrighteligibility.AuthorshipUnknown, Reason: ReasonEvidenceConflict, Evidence: append([]EvidenceItem{providerReference(provider, "Project Gutenberg RDF identifies one author.")}, recordReferences(records, "Bibliographic author data conflicts with Project Gutenberg RDF.")...)}
		}
		if len(matches) == 0 {
			return ResolvedAuthorship{Status: ResolutionInsufficient, Value: copyrighteligibility.AuthorshipUnknown, Reason: ReasonEvidenceInsufficient, Evidence: []EvidenceItem{providerReference(provider, "Project Gutenberg RDF identifies one author.")}}
		}
		return ResolvedAuthorship{Status: ResolutionEstablished, Value: copyrighteligibility.AuthorshipSingleKnown, Reason: ReasonEstablished, Evidence: append([]EvidenceItem{providerReference(provider, "Project Gutenberg RDF identifies one author.")}, personReferences(matches, "An identified bibliographic authority record corroborates the provider author and life dates.")...)}
	default:
		return ResolvedAuthorship{Status: ResolutionEstablished, Value: copyrighteligibility.AuthorshipJoint, Reason: ReasonEstablished, Evidence: []EvidenceItem{providerReference(provider, "Project Gutenberg RDF identifies multiple authors.")}}
	}
}

func resolveAuthor(provider copyrighteligibility.ProviderEvidence, records []BibliographicRecord, authorship ResolvedAuthorship) ResolvedAuthor {
	if authorship.Status != ResolutionEstablished || authorship.Value != copyrighteligibility.AuthorshipSingleKnown {
		return ResolvedAuthor{Status: authorship.Status, Reason: authorship.Reason}
	}
	authors := providerAuthors(provider.Contributors)
	matches, conflicts := matchingExternalAuthors(authors[0], records)
	if conflicts || len(matches) == 0 || authors[0].DeathYear == nil {
		return ResolvedAuthor{Status: ResolutionInsufficient, Reason: ReasonEvidenceInsufficient}
	}
	return ResolvedAuthor{Status: ResolutionEstablished, Name: authors[0].Name, DeathYear: *authors[0].DeathYear, Reason: ReasonEstablished, Evidence: append([]EvidenceItem{providerReference(provider, "Project Gutenberg RDF supplies the recognised author death year.")}, personReferences(matches, "An identified bibliographic authority record corroborates the author death year.")...)}
}

func matchingExternalAuthors(provider Person, records []BibliographicRecord) ([]BibliographicRecord, bool) {
	if provider.DeathYear == nil {
		return nil, false
	}
	matches := make([]BibliographicRecord, 0, len(records))
	for _, record := range records {
		if len(record.Authors) > 1 {
			return nil, true
		}
		if len(record.Authors) == 0 {
			continue
		}
		author := record.Authors[0]
		if NormalisedPersonName(author.Name) != NormalisedPersonName(provider.Name) {
			return nil, true
		}
		if len(author.Identifiers) == 0 || author.DeathYear == nil {
			continue
		}
		if *author.DeathYear != *provider.DeathYear {
			return nil, true
		}
		matches = append(matches, record)
	}
	return matches, false
}

// resolveFirstPublication applies Panda Pages' publication-evidence policy. A
// year is established only when an authoritative bibliographic source class
// and an independent source class agree. The algorithm is provider-neutral;
// the publicationAuthority table centrally assigns the current source classes.
// Corroborating-only evidence is never enough, and disagreement remains
// conflicting regardless of source authority.
func resolveFirstPublication(records []BibliographicRecord) ResolvedYear {
	values := make([]BibliographicRecord, 0, len(records))
	for _, record := range records {
		if record.FirstPublicationYear != nil {
			values = append(values, record)
		}
	}
	if len(values) == 0 {
		return ResolvedYear{Status: ResolutionInsufficient, Reason: ReasonEvidenceInsufficient}
	}
	for _, record := range values[1:] {
		if *record.FirstPublicationYear != *values[0].FirstPublicationYear {
			return ResolvedYear{Status: ResolutionConflicting, Reason: ReasonEvidenceConflict, Evidence: recordReferences(values, "Bibliographic records disagree about first publication year.")}
		}
	}
	classes := distinctClasses(values)
	if !hasAuthoritativePublicationClass(classes) {
		return ResolvedYear{Status: ResolutionInsufficient, Reason: ReasonEvidenceInsufficient, Evidence: recordReferences(values, "Publication year requires authoritative bibliographic evidence and an independent corroborating source.")}
	}
	if len(classes) < 2 {
		return ResolvedYear{Status: ResolutionInsufficient, Reason: ReasonEvidenceInsufficient, Evidence: recordReferences(values, "Authoritative publication evidence requires an independent bibliographic source class to corroborate it.")}
	}
	return ResolvedYear{Status: ResolutionEstablished, Year: *values[0].FirstPublicationYear, Reason: ReasonEstablished, Evidence: recordReferences(values, "An authoritative bibliographic source and an independent source agree on first publication year.")}
}

func resolveTranslation(provider copyrighteligibility.ProviderEvidence, records []BibliographicRecord, front FrontMatter, postMarker PostMarkerSignals) ResolvedFact {
	if hasProviderRole(provider.Contributors, "translator") {
		return ResolvedFact{Status: ResolutionEstablished, State: copyrighteligibility.FactPresent, Reason: ReasonProviderContributorPresent, Evidence: []EvidenceItem{providerReference(provider, "Project Gutenberg RDF identifies a translator.")}}
	}
	if len(front.Translators) > 0 {
		return ResolvedFact{Status: ResolutionEstablished, State: copyrighteligibility.FactPresent, Reason: ReasonFrontMatterContributorPresent, Evidence: []EvidenceItem{{Class: SourceProjectGutenberg, Source: "Project Gutenberg source front matter", Digest: front.Digest, Fact: "Provider front matter identifies a translator."}}}
	}
	if len(postMarker.Translators) > 0 {
		return ResolvedFact{Status: ResolutionEstablished, State: copyrighteligibility.FactPresent, Reason: ReasonFrontMatterContributorPresent, Evidence: []EvidenceItem{{Class: SourceProjectGutenberg, Source: "Project Gutenberg title page", Digest: postMarker.Digest, Fact: "Bounded provider title-page material identifies a translator."}}}
	}
	if record, ok := firstBibliographicContributor(records, "translator"); ok {
		return ResolvedFact{Status: ResolutionEstablished, State: copyrighteligibility.FactPresent, Reason: ReasonProviderContributorPresent, Evidence: []EvidenceItem{recordReference(record, "Structured bibliographic contributor data identifies a translator.")}}
	}
	return resolveTranslationAbsence(provider, records, front)
}

func resolveAdditionalTextual(provider copyrighteligibility.ProviderEvidence, records []BibliographicRecord, front FrontMatter, postMarker PostMarkerSignals) ResolvedFact {
	if hasProviderTextualRole(provider.Contributors) {
		return ResolvedFact{Status: ResolutionEstablished, State: copyrighteligibility.FactPresent, Reason: ReasonProviderContributorPresent, Evidence: []EvidenceItem{providerReference(provider, "Project Gutenberg RDF identifies an additional textual contributor.")}}
	}
	if len(front.TextualContributors) > 0 {
		return ResolvedFact{Status: ResolutionEstablished, State: copyrighteligibility.FactPresent, Reason: ReasonFrontMatterContributorPresent, Evidence: []EvidenceItem{{Class: SourceProjectGutenberg, Source: "Project Gutenberg source front matter", Digest: front.Digest, Fact: "Provider front matter identifies an additional textual contributor."}}}
	}
	if len(postMarker.TextualContributors) > 0 {
		return ResolvedFact{Status: ResolutionEstablished, State: copyrighteligibility.FactPresent, Reason: ReasonFrontMatterContributorPresent, Evidence: []EvidenceItem{{Class: SourceProjectGutenberg, Source: "Project Gutenberg title page", Digest: postMarker.Digest, Fact: "Bounded provider title-page material identifies an additional textual contributor."}}}
	}
	if record, ok := firstBibliographicTextualContributor(records); ok {
		return ResolvedFact{Status: ResolutionEstablished, State: copyrighteligibility.FactPresent, Reason: ReasonProviderContributorPresent, Evidence: []EvidenceItem{recordReference(record, "Structured bibliographic contributor data identifies an additional textual contributor.")}}
	}
	return resolveAdditionalTextualAbsence(provider, front)
}

// resolveTranslationAbsence applies Panda Pages' v3 contributor-risk screen.
// It requires a clean, bounded exact Gutenberg wrapper and provider record,
// plus a single acquired language that agrees with all usable structured
// original-work language observations. This is screening evidence that no
// material translation risk is indicated by the acquired source; it is not
// proof that no translator has ever existed. Missing or conflicting language
// evidence remains unknown.
func resolveTranslationAbsence(provider copyrighteligibility.ProviderEvidence, records []BibliographicRecord, front FrontMatter) ResolvedFact {
	if !front.Inspected || !hasSingleLanguage(provider.Languages) {
		return ResolvedFact{Status: ResolutionInsufficient, State: copyrighteligibility.FactUnknown, Reason: ReasonEvidenceInsufficient}
	}
	acquiredLanguage := canonicalLanguage(provider.Languages[0])
	if acquiredLanguage == "" {
		return ResolvedFact{Status: ResolutionInsufficient, State: copyrighteligibility.FactUnknown, Reason: ReasonEvidenceInsufficient}
	}
	originalLanguage, values, status := resolveOriginalWorkLanguage(records)
	if status != ResolutionEstablished {
		reason := ReasonEvidenceInsufficient
		if status == ResolutionConflicting {
			reason = ReasonEvidenceConflict
		}
		return ResolvedFact{Status: status, State: copyrighteligibility.FactUnknown, Reason: reason, Evidence: recordReferences(values, "Structured original-work language evidence is unresolved.")}
	}
	if acquiredLanguage != originalLanguage {
		evidence := []EvidenceItem{providerReference(provider, "Project Gutenberg RDF supplies the acquired source language.")}
		evidence = append(evidence, recordReferences(values, "Structured original-work language evidence conflicts with the acquired source language.")...)
		return ResolvedFact{Status: ResolutionConflicting, State: copyrighteligibility.FactUnknown, Reason: ReasonEvidenceConflict, Evidence: evidence}
	}
	evidence := []EvidenceItem{
		providerReference(provider, "Project Gutenberg RDF contains no recognised translator role."),
		{Class: SourceProjectGutenberg, Source: "Project Gutenberg source front matter", Digest: front.Digest, Fact: "Bounded provider front matter contains no translator signal."},
	}
	evidence = append(evidence, recordReferences(values, "Structured original-work language evidence agrees with the acquired source language.")...)
	return ResolvedFact{Status: ResolutionEstablished, State: copyrighteligibility.FactNoneConfirmed, Reason: ReasonEstablished, Evidence: evidence}
}

// resolveAdditionalTextualAbsence applies Panda Pages' v3 contributor-risk
// screen. A clean exact Gutenberg wrapper and provider record are sufficient
// to establish that no material additional-text risk is indicated by the
// acquired source. This does not assert that no separate contribution has ever
// existed; any positive provider, front-matter, or structured observation wins.
func resolveAdditionalTextualAbsence(provider copyrighteligibility.ProviderEvidence, front FrontMatter) ResolvedFact {
	if !front.Inspected {
		return ResolvedFact{Status: ResolutionInsufficient, State: copyrighteligibility.FactUnknown, Reason: ReasonEvidenceInsufficient}
	}
	evidence := []EvidenceItem{
		providerReference(provider, "Project Gutenberg RDF contains no recognised additional textual contributor role."),
		{Class: SourceProjectGutenberg, Source: "Project Gutenberg source front matter", Digest: front.Digest, Fact: "Bounded provider front matter contains no additional textual-contributor signal."},
	}
	return ResolvedFact{Status: ResolutionEstablished, State: copyrighteligibility.FactNoneConfirmed, Reason: ReasonEstablished, Evidence: evidence}
}

// resolveOriginalWorkLanguage establishes one original-work language from
// structured bibliographic work observations. Publication authority is not
// reused for this separate fact: one usable observation is enough, but every
// usable observation must canonically agree. A record with multiple plausible
// original languages is itself unresolved and therefore blocks automated
// translation screening.
func resolveOriginalWorkLanguage(records []BibliographicRecord) (string, []BibliographicRecord, ResolutionStatus) {
	var language string
	values := make([]BibliographicRecord, 0, len(records))
	for _, record := range records {
		if len(record.OriginalLanguages) == 0 {
			continue
		}
		languages := make(map[string]struct{}, len(record.OriginalLanguages))
		for _, value := range record.OriginalLanguages {
			value = canonicalLanguage(value)
			if value != "" {
				languages[value] = struct{}{}
			}
		}
		if len(languages) == 0 {
			continue
		}
		values = append(values, record)
		if len(languages) != 1 {
			return "", values, ResolutionConflicting
		}
		var observed string
		for value := range languages {
			observed = value
		}
		if language != "" && language != observed {
			return "", values, ResolutionConflicting
		}
		language = observed
	}
	if language == "" {
		return "", values, ResolutionInsufficient
	}
	return language, values, ResolutionEstablished
}

func firstBibliographicContributor(records []BibliographicRecord, role string) (BibliographicRecord, bool) {
	for _, record := range records {
		if hasContributorRole(record, role) {
			return record, true
		}
	}
	return BibliographicRecord{}, false
}

func firstBibliographicTextualContributor(records []BibliographicRecord) (BibliographicRecord, bool) {
	for _, record := range records {
		if hasTextualContributor(record) {
			return record, true
		}
	}
	return BibliographicRecord{}, false
}

func hasContributorRole(record BibliographicRecord, role string) bool {
	for _, contributor := range record.Contributors {
		if strings.EqualFold(strings.TrimSpace(contributor.Role), role) {
			return true
		}
	}
	return false
}

func hasTextualContributor(record BibliographicRecord) bool {
	for _, contributor := range record.Contributors {
		switch strings.ToLower(strings.TrimSpace(contributor.Role)) {
		case "adapter", "annotator", "compiler", "introduction_author", "introduction", "editor", "preface_author", "preface", "contributor", "notes_author", "notes":
			return true
		}
	}
	return false
}

func canonicalLanguage(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "http://id.loc.gov/vocabulary/iso639-2/")
	value = strings.TrimPrefix(value, "https://id.loc.gov/vocabulary/iso639-2/")
	switch value {
	case "en", "eng":
		return "eng"
	case "fr", "fre", "fra":
		return "fra"
	case "de", "ger", "deu":
		return "deu"
	case "es", "spa":
		return "spa"
	default:
		if len(value) < 2 || len(value) > 3 {
			return ""
		}
		for _, r := range value {
			if r < 'a' || r > 'z' {
				return ""
			}
		}
		return value
	}
}

func hasSingleLanguage(values []string) bool {
	return len(values) == 1 && strings.TrimSpace(values[0]) != ""
}

func resolveUnpublishedAtEnd1988(publication ResolvedYear) ResolvedFact {
	if publication.Status == ResolutionEstablished && publication.Year <= 1988 {
		evidence := append([]EvidenceItem(nil), publication.Evidence...)
		evidence = append(evidence, EvidenceItem{Class: SourceProjectGutenberg, Source: "Panda Pages evidence resolver", Fact: "An established first publication year no later than 1988 means the work was not unpublished at the end of 1988."})
		return ResolvedFact{Status: ResolutionEstablished, State: copyrighteligibility.FactNoneConfirmed, Reason: ReasonPublicationDerivedBefore1989, Evidence: evidence}
	}
	return ResolvedFact{Status: ResolutionInsufficient, State: copyrighteligibility.FactUnknown, Reason: ReasonEvidenceInsufficient}
}

func hasProviderRole(values []copyrighteligibility.ContributorEvidence, role string) bool {
	for _, value := range values {
		if value.Role == role {
			return true
		}
	}
	return false
}

func hasProviderTextualRole(values []copyrighteligibility.ContributorEvidence) bool {
	for _, value := range values {
		switch value.Role {
		case "adapter", "annotator", "compiler", "introduction_author", "editor", "contributor":
			return true
		}
	}
	return false
}

func distinctClasses(records []BibliographicRecord) map[SourceClass]struct{} {
	result := make(map[SourceClass]struct{}, len(records))
	for _, record := range records {
		result[record.Source] = struct{}{}
	}
	return result
}

// publicationAuthority is the central Panda Pages policy for this one fact.
// BnF and Library of Congress are authoritative classes; Open Library and
// Wikidata are corroborating. Adding another authority requires updating this
// table alongside its source class and adapter, never this algorithm.
func publicationAuthority(class SourceClass) PublicationAuthority {
	switch class {
	case SourceBibliothequeNationaleDeFrance, SourceLibraryOfCongress:
		return PublicationAuthorityAuthoritative
	case SourceOpenLibrary, SourceWikidata:
		return PublicationAuthorityCorroborating
	default:
		return ""
	}
}

func hasAuthoritativePublicationClass(classes map[SourceClass]struct{}) bool {
	for class := range classes {
		if publicationAuthority(class) == PublicationAuthorityAuthoritative {
			return true
		}
	}
	return false
}

func recordReferences(records []BibliographicRecord, fact string) []EvidenceItem {
	result := make([]EvidenceItem, 0, len(records))
	for _, record := range records {
		result = append(result, recordReference(record, fact))
	}
	return result
}

func personReferences(records []BibliographicRecord, fact string) []EvidenceItem {
	return recordReferences(records, fact)
}

func canonicalRecord(record BibliographicRecord) BibliographicRecord {
	record.Authors = append([]Person(nil), record.Authors...)
	for i := range record.Authors {
		record.Authors[i].Identifiers = append([]Identifier(nil), record.Authors[i].Identifiers...)
		sort.Slice(record.Authors[i].Identifiers, func(a, b int) bool {
			if record.Authors[i].Identifiers[a].Source != record.Authors[i].Identifiers[b].Source {
				return record.Authors[i].Identifiers[a].Source < record.Authors[i].Identifiers[b].Source
			}
			return record.Authors[i].Identifiers[a].Value < record.Authors[i].Identifiers[b].Value
		})
	}
	record.Contributors = append([]Contributor(nil), record.Contributors...)
	for i := range record.Contributors {
		record.Contributors[i].Identifiers = append([]Identifier(nil), record.Contributors[i].Identifiers...)
		sort.Slice(record.Contributors[i].Identifiers, func(a, b int) bool {
			if record.Contributors[i].Identifiers[a].Source != record.Contributors[i].Identifiers[b].Source {
				return record.Contributors[i].Identifiers[a].Source < record.Contributors[i].Identifiers[b].Source
			}
			return record.Contributors[i].Identifiers[a].Value < record.Contributors[i].Identifiers[b].Value
		})
	}
	sort.Slice(record.Contributors, func(i, j int) bool {
		if record.Contributors[i].Role != record.Contributors[j].Role {
			return record.Contributors[i].Role < record.Contributors[j].Role
		}
		return record.Contributors[i].Name < record.Contributors[j].Name
	})
	record.Languages = canonicalStrings(record.Languages)
	record.OriginalLanguages = canonicalStrings(record.OriginalLanguages)
	record.Subjects = canonicalStrings(record.Subjects)
	record.MaterialTypes = canonicalStrings(record.MaterialTypes)
	return record
}

func canonicalStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func canonicalRecords(records []BibliographicRecord) {
	sort.Slice(records, func(i, j int) bool {
		if records[i].Source != records[j].Source {
			return records[i].Source < records[j].Source
		}
		return records[i].Identifier < records[j].Identifier
	})
}

func canonicalResolution(value Resolution) Resolution {
	value.WorkCategory.Evidence = canonicalEvidence(value.WorkCategory.Evidence)
	value.Authorship.Evidence = canonicalEvidence(value.Authorship.Evidence)
	value.Author.Evidence = canonicalEvidence(value.Author.Evidence)
	value.FirstPublication.Evidence = canonicalEvidence(value.FirstPublication.Evidence)
	value.Translation.Evidence = canonicalEvidence(value.Translation.Evidence)
	value.AdditionalTextual.Evidence = canonicalEvidence(value.AdditionalTextual.Evidence)
	value.UnpublishedAtEnd1988.Evidence = canonicalEvidence(value.UnpublishedAtEnd1988.Evidence)
	sort.Slice(value.Diagnostics, func(i, j int) bool {
		if value.Diagnostics[i].Source != value.Diagnostics[j].Source {
			return value.Diagnostics[i].Source < value.Diagnostics[j].Source
		}
		return value.Diagnostics[i].Reason < value.Diagnostics[j].Reason
	})
	return value
}

func canonicalEvidence(values []EvidenceItem) []EvidenceItem {
	result := append([]EvidenceItem(nil), values...)
	sort.Slice(result, func(i, j int) bool {
		left, right := result[i], result[j]
		if left.Class != right.Class {
			return left.Class < right.Class
		}
		if left.Source != right.Source {
			return left.Source < right.Source
		}
		if left.Identifier != right.Identifier {
			return left.Identifier < right.Identifier
		}
		return left.Fact < right.Fact
	})
	return result
}
