package evidenceresolver

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"time"
	"unicode"

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
			diagnostics = append(diagnostics, Diagnostic{Source: source.SourceClass(), Reason: ReasonSourceUnavailable})
			s.log("source_failed", exact.ProviderEvidence, "source", string(source.SourceClass()), "failure_class", string(ReasonSourceUnavailable))
			continue
		}
		for _, record := range found {
			if record.Source != source.SourceClass() || !validRecord(record) || normalisedTitle(record.Title) != normalisedTitle(query.Title) {
				diagnostics = append(diagnostics, Diagnostic{Source: source.SourceClass(), Reason: ReasonSourceInvalid})
				s.log("source_invalid", exact.ProviderEvidence, "source", string(source.SourceClass()), "failure_class", string(ReasonSourceInvalid))
				continue
			}
			records = append(records, canonicalRecord(record))
		}
	}
	canonicalRecords(records)
	front := ExtractFrontMatter(exact.SourceText)
	resolution := Resolution{
		WorkCategory:      resolveWorkCategory(records),
		Authorship:        resolveAuthorship(exact.ProviderEvidence, records),
		FirstPublication:  resolveFirstPublication(records),
		Translation:       resolveTranslation(exact.ProviderEvidence, records, front),
		AdditionalTextual: resolveAdditionalTextual(exact.ProviderEvidence, records, front),
		SpecialCategory:   ResolvedFact{Status: ResolutionInsufficient, State: copyrighteligibility.FactUnknown, Reason: ReasonSpecialCategoryNotAutoResolved},
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
	case SourceLibraryOfCongress, SourceOpenLibrary, SourceWikidata:
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
		if record.WorkCategory != copyrighteligibility.WorkCategoryUnknown {
			values = append(values, record)
		}
	}
	if len(values) == 0 {
		return ResolvedWorkCategory{Status: ResolutionInsufficient, Value: copyrighteligibility.WorkCategoryUnknown, Reason: ReasonEvidenceInsufficient}
	}
	for _, record := range values[1:] {
		if record.WorkCategory != values[0].WorkCategory {
			return ResolvedWorkCategory{Status: ResolutionConflicting, Value: copyrighteligibility.WorkCategoryUnknown, Reason: ReasonEvidenceConflict, Evidence: recordReferences(values, "Bibliographic records disagree about material category.")}
		}
	}
	if len(distinctClasses(values)) < 2 {
		return ResolvedWorkCategory{Status: ResolutionInsufficient, Value: copyrighteligibility.WorkCategoryUnknown, Reason: ReasonEvidenceInsufficient, Evidence: recordReferences(values, "One bibliographic source class identifies the material category.")}
	}
	return ResolvedWorkCategory{Status: ResolutionEstablished, Value: values[0].WorkCategory, Reason: ReasonEstablished, Evidence: recordReferences(values, "Independent bibliographic records identify the material category.")}
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
		if normalisedName(author.Name) != normalisedName(provider.Name) {
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

// resolveFirstPublication applies the v1 source-priority rule: a publication
// year is established only when a Library of Congress record and at least one
// independent structured source agree. A corroborating-only record is never
// enough, and any observed disagreement remains conflicting.
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
	if !hasAuthorityAndCorroboration(values) {
		return ResolvedYear{Status: ResolutionInsufficient, Reason: ReasonEvidenceInsufficient, Evidence: recordReferences(values, "Publication year requires Library of Congress evidence and an independent corroborating record.")}
	}
	return ResolvedYear{Status: ResolutionEstablished, Year: *values[0].FirstPublicationYear, Reason: ReasonEstablished, Evidence: recordReferences(values, "Library of Congress and an independent bibliographic source agree on first publication year.")}
}

func resolveTranslation(provider copyrighteligibility.ProviderEvidence, records []BibliographicRecord, front FrontMatter) ResolvedFact {
	if hasProviderRole(provider.Contributors, "translator") {
		return ResolvedFact{Status: ResolutionEstablished, State: copyrighteligibility.FactPresent, Reason: ReasonProviderContributorPresent, Evidence: []EvidenceItem{providerReference(provider, "Project Gutenberg RDF identifies a translator.")}}
	}
	if len(front.Translators) > 0 {
		return ResolvedFact{Status: ResolutionEstablished, State: copyrighteligibility.FactPresent, Reason: ReasonFrontMatterContributorPresent, Evidence: []EvidenceItem{{Class: SourceProjectGutenberg, Source: "Project Gutenberg source front matter", Digest: front.Digest, Fact: "Provider front matter identifies a translator."}}}
	}
	return resolveBibliographicFact(records, func(record BibliographicRecord) copyrighteligibility.FactState { return record.Translation }, "translation")
}

func resolveAdditionalTextual(provider copyrighteligibility.ProviderEvidence, records []BibliographicRecord, front FrontMatter) ResolvedFact {
	if hasProviderTextualRole(provider.Contributors) {
		return ResolvedFact{Status: ResolutionEstablished, State: copyrighteligibility.FactPresent, Reason: ReasonProviderContributorPresent, Evidence: []EvidenceItem{providerReference(provider, "Project Gutenberg RDF identifies an additional textual contributor.")}}
	}
	if len(front.TextualContributors) > 0 {
		return ResolvedFact{Status: ResolutionEstablished, State: copyrighteligibility.FactPresent, Reason: ReasonFrontMatterContributorPresent, Evidence: []EvidenceItem{{Class: SourceProjectGutenberg, Source: "Project Gutenberg source front matter", Digest: front.Digest, Fact: "Provider front matter identifies an additional textual contributor."}}}
	}
	return resolveBibliographicFact(records, func(record BibliographicRecord) copyrighteligibility.FactState { return record.AdditionalTextual }, "additional textual contribution")
}

func resolveBibliographicFact(records []BibliographicRecord, value func(BibliographicRecord) copyrighteligibility.FactState, label string) ResolvedFact {
	known := make([]BibliographicRecord, 0, len(records))
	for _, record := range records {
		if state := value(record); state == copyrighteligibility.FactPresent || state == copyrighteligibility.FactNoneConfirmed {
			known = append(known, record)
		}
	}
	if len(known) == 0 {
		return ResolvedFact{Status: ResolutionInsufficient, State: copyrighteligibility.FactUnknown, Reason: ReasonEvidenceInsufficient}
	}
	for _, record := range known[1:] {
		if value(record) != value(known[0]) {
			return ResolvedFact{Status: ResolutionConflicting, State: copyrighteligibility.FactUnknown, Reason: ReasonEvidenceConflict, Evidence: recordReferences(known, "Bibliographic records disagree about "+label+".")}
		}
	}
	if len(distinctClasses(known)) < 2 {
		return ResolvedFact{Status: ResolutionInsufficient, State: copyrighteligibility.FactUnknown, Reason: ReasonEvidenceInsufficient, Evidence: recordReferences(known, "One bibliographic source class addresses "+label+".")}
	}
	return ResolvedFact{Status: ResolutionEstablished, State: value(known[0]), Reason: ReasonEstablished, Evidence: recordReferences(known, "Independent bibliographic records address "+label+".")}
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

func hasAuthorityAndCorroboration(records []BibliographicRecord) bool {
	classes := distinctClasses(records)
	if _, ok := classes[SourceLibraryOfCongress]; !ok {
		return false
	}
	return len(classes) >= 2
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

func normalisedName(value string) string {
	if parts := strings.Split(value, ","); len(parts) == 2 {
		value = strings.TrimSpace(parts[1]) + " " + strings.TrimSpace(parts[0])
	}
	var result strings.Builder
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			result.WriteRune(r)
		}
	}
	return result.String()
}

func normalisedTitle(value string) string {
	var result strings.Builder
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			result.WriteRune(r)
		}
	}
	return result.String()
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
	return record
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
	value.SpecialCategory.Evidence = canonicalEvidence(value.SpecialCategory.Evidence)
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
