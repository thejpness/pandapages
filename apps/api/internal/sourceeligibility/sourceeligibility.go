package sourceeligibility

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"pandapages/api/internal/copyrighteligibility"
	"pandapages/api/internal/evidenceresolver"
	"pandapages/api/internal/sourceprovider"
)

var (
	// ErrHumanEvidenceConflict means a browser-supplied factual claim conflicts
	// with current provider evidence for the exact selected work.
	ErrHumanEvidenceConflict = errors.New("human copyright evidence conflicts with provider evidence")
	// ErrProviderEvidenceInvalid means trusted provider responses could not be
	// bound consistently to the requested provider work.
	ErrProviderEvidenceInvalid = errors.New("provider copyright evidence is inconsistent")
)

// ProviderGateway is deliberately narrow: it exposes only server-owned,
// provider-selected material and evidence. Callers never provide a fetch URL.
type ProviderGateway interface {
	AcquireEvidence(context.Context, sourceprovider.ID, string) (sourceprovider.AcquisitionEvidence, error)
	CopyrightEvidence(context.Context, sourceprovider.ID, string) (copyrighteligibility.ProviderEvidence, error)
}

// EvidenceResolver produces only server-owned factual observations from the
// exact provider work and source text. Eligibility remains the responsibility
// of copyrighteligibility.Evaluate.
type EvidenceResolver interface {
	Resolve(context.Context, evidenceresolver.ExactSourceContext) (evidenceresolver.Resolution, error)
}

// HumanUKEvidence is the factual dossier supplied by an administrator. It
// contains no jurisdiction result, policy version, provider evidence, or
// override mechanism; the evaluator derives those server-side.
type HumanUKEvidence struct {
	WorkCategory           copyrighteligibility.WorkCategory
	WorkCategoryReferences []copyrighteligibility.EvidenceReference
	AuthorDeathYear        *int
	AuthorDeathReferences  []copyrighteligibility.EvidenceReference
	FirstPublication       copyrighteligibility.PublicationEvidence
	Translation            copyrighteligibility.FactEvidence
	AdditionalTextual      copyrighteligibility.FactEvidence
	UnpublishedAtEnd1988   copyrighteligibility.FactEvidence
}

// Evaluation is the complete server-owned evidence bundle for one current
// Project Gutenberg work. Candidate is intentionally omitted from API
// responses but is used atomically by the persistence gate.
type Evaluation struct {
	Candidate           sourceprovider.SourceCandidate
	ProviderEvidence    copyrighteligibility.ProviderEvidence
	OPDSRights          copyrighteligibility.ProviderRightsClassification
	HeaderRights        copyrighteligibility.SourceHeaderRightsClassification
	EffectiveUKEvidence copyrighteligibility.UKEvidence
	Assessment          copyrighteligibility.Assessment
	EvaluationDate      time.Time
	EvaluatedAt         time.Time
}

type Config struct {
	Gateway  ProviderGateway
	Resolver EvidenceResolver
	Now      func() time.Time
}

type Service struct {
	gateway  ProviderGateway
	resolver EvidenceResolver
	now      func() time.Time
}

func New(cfg Config) (*Service, error) {
	if cfg.Gateway == nil {
		return nil, errors.New("source eligibility gateway is required")
	}
	if cfg.Resolver == nil {
		cfg.Resolver = emptyEvidenceResolver{}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Service{gateway: cfg.Gateway, resolver: cfg.Resolver, now: cfg.Now}, nil
}

type emptyEvidenceResolver struct{}

func (emptyEvidenceResolver) Resolve(context.Context, evidenceresolver.ExactSourceContext) (evidenceresolver.Resolution, error) {
	return evidenceresolver.Resolution{}, nil
}

// Evaluate reacquires current server-owned source material and RDF evidence
// for the exact provider ID before evaluating the pure policy. It performs no
// database work; callers must re-run it at final persistence time.
func (s *Service) Evaluate(ctx context.Context, provider sourceprovider.ID, externalID string, human HumanUKEvidence) (Evaluation, error) {
	if s == nil || s.gateway == nil || s.resolver == nil {
		return Evaluation{}, ErrProviderEvidenceInvalid
	}
	acquired, err := s.gateway.AcquireEvidence(ctx, provider, externalID)
	if err != nil {
		return Evaluation{}, err
	}
	providerEvidence, err := s.gateway.CopyrightEvidence(ctx, provider, externalID)
	if err != nil {
		return Evaluation{}, err
	}
	if acquired.Candidate.Provider != provider || acquired.Candidate.ExternalID != externalID ||
		providerEvidence.Provider != string(provider) || providerEvidence.ExternalID != externalID {
		return Evaluation{}, ErrProviderEvidenceInvalid
	}
	resolution, err := s.resolver.Resolve(ctx, evidenceresolver.ExactSourceContext{
		ProviderEvidence:  providerEvidence,
		SourceText:        acquired.Candidate.SourceText,
		SourceFrontMatter: acquired.SourceFrontMatter,
	})
	if err != nil {
		return Evaluation{}, ErrProviderEvidenceInvalid
	}
	effective, err := bindGutenbergUKEvidence(providerEvidence, evidenceresolver.ToUKEvidence(resolution), human)
	if err != nil {
		return Evaluation{}, err
	}
	now := s.now().UTC()
	evaluationDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	assessment := copyrighteligibility.Evaluate(copyrighteligibility.Input{
		EvaluationDate: evaluationDate,
		US: copyrighteligibility.USProviderEvidence{
			OPDSRights:   acquired.OPDSRights,
			RDFRights:    providerEvidence.Rights,
			HeaderRights: acquired.HeaderRights,
		},
		UK: effective,
	})
	return Evaluation{
		Candidate:           acquired.Candidate,
		ProviderEvidence:    canonicalProviderEvidence(providerEvidence),
		OPDSRights:          acquired.OPDSRights,
		HeaderRights:        acquired.HeaderRights,
		EffectiveUKEvidence: canonicalUKEvidence(effective),
		Assessment:          assessment,
		EvaluationDate:      evaluationDate,
		EvaluatedAt:         now,
	}, nil
}

func bindGutenbergUKEvidence(provider copyrighteligibility.ProviderEvidence, automated copyrighteligibility.UKEvidence, human HumanUKEvidence) (copyrighteligibility.UKEvidence, error) {
	if provider.Provider != string(sourceprovider.ProjectGutenberg) || strings.TrimSpace(provider.ExternalID) == "" {
		return copyrighteligibility.UKEvidence{}, ErrProviderEvidenceInvalid
	}
	if automated.WorkTitle != "" && strings.TrimSpace(automated.WorkTitle) != strings.TrimSpace(provider.Title) {
		return copyrighteligibility.UKEvidence{}, ErrProviderEvidenceInvalid
	}
	workCategory, workCategoryReferences, err := mergeWorkCategory(automated.WorkCategory, automated.WorkCategoryReferences, human.WorkCategory, human.WorkCategoryReferences)
	if err != nil {
		return copyrighteligibility.UKEvidence{}, err
	}
	firstPublication, err := mergePublication(automated.FirstPublication, human.FirstPublication)
	if err != nil {
		return copyrighteligibility.UKEvidence{}, err
	}
	translation, err := mergeFact(automated.Translation, human.Translation)
	if err != nil {
		return copyrighteligibility.UKEvidence{}, err
	}
	additionalTextual, err := mergeFact(automated.AdditionalTextualContribution, human.AdditionalTextual)
	if err != nil {
		return copyrighteligibility.UKEvidence{}, err
	}
	unpublished, err := mergeFact(automated.UnpublishedAtEnd1988, human.UnpublishedAtEnd1988)
	if err != nil {
		return copyrighteligibility.UKEvidence{}, err
	}
	result := copyrighteligibility.UKEvidence{
		WorkTitle:                     strings.TrimSpace(provider.Title),
		WorkCategory:                  workCategory,
		WorkCategoryReferences:        workCategoryReferences,
		FirstPublication:              firstPublication,
		Translation:                   translation,
		AdditionalTextualContribution: additionalTextual,
		UnpublishedAtEnd1988:          unpublished,
	}

	authors := contributorsWithRole(provider.Contributors, "author")
	switch len(authors) {
	case 1:
		author := authors[0]
		result.Authorship = copyrighteligibility.AuthorshipSingleKnown
		result.AuthorshipReferences = []copyrighteligibility.EvidenceReference{providerReference(provider, "Project Gutenberg RDF identifies one recognised author.")}
		result.Author.Name = author.Name
		if author.DeathYear != nil {
			if human.AuthorDeathYear != nil && *human.AuthorDeathYear != *author.DeathYear {
				return copyrighteligibility.UKEvidence{}, ErrHumanEvidenceConflict
			}
			result.Author.DeathYear = *author.DeathYear
			result.Author.References = []copyrighteligibility.EvidenceReference{providerReference(provider, "Project Gutenberg RDF supplies the recognised author death year.")}
		} else if human.AuthorDeathYear != nil {
			result.Author.DeathYear = *human.AuthorDeathYear
			result.Author.References = canonicalReferences(human.AuthorDeathReferences)
		}
	case 0:
		result.Authorship = copyrighteligibility.AuthorshipUnknown
	default:
		result.Authorship = copyrighteligibility.AuthorshipJoint
		result.AuthorshipReferences = []copyrighteligibility.EvidenceReference{providerReference(provider, "Project Gutenberg RDF identifies multiple recognised authors.")}
	}

	if hasContributorRole(provider.Contributors, "translator") {
		result.Translation = providerFactEvidence(provider, copyrighteligibility.FactPresent, "Project Gutenberg RDF identifies a translator.")
	}
	if hasTextualContributor(provider.Contributors) {
		result.AdditionalTextualContribution = providerFactEvidence(provider, copyrighteligibility.FactPresent, "Project Gutenberg RDF identifies a possible additional textual contributor.")
	}
	return canonicalUKEvidence(result), nil
}

func mergeWorkCategory(automated copyrighteligibility.WorkCategory, automatedReferences []copyrighteligibility.EvidenceReference, human copyrighteligibility.WorkCategory, humanReferences []copyrighteligibility.EvidenceReference) (copyrighteligibility.WorkCategory, []copyrighteligibility.EvidenceReference, error) {
	if !unknownWorkCategory(automated) {
		if !unknownWorkCategory(human) && human != automated {
			return copyrighteligibility.WorkCategoryUnknown, nil, ErrHumanEvidenceConflict
		}
		return automated, canonicalReferences(automatedReferences), nil
	}
	if unknownWorkCategory(human) {
		return copyrighteligibility.WorkCategoryUnknown, nil, nil
	}
	return human, canonicalReferences(humanReferences), nil
}

func mergePublication(automated, human copyrighteligibility.PublicationEvidence) (copyrighteligibility.PublicationEvidence, error) {
	if automated.Year != 0 {
		if human.Year != 0 && human.Year != automated.Year {
			return copyrighteligibility.PublicationEvidence{}, ErrHumanEvidenceConflict
		}
		return copyrighteligibility.PublicationEvidence{Year: automated.Year, References: canonicalReferences(automated.References)}, nil
	}
	return copyrighteligibility.PublicationEvidence{Year: human.Year, References: canonicalReferences(human.References)}, nil
}

func mergeFact(automated, human copyrighteligibility.FactEvidence) (copyrighteligibility.FactEvidence, error) {
	if !unknownFactState(automated.State) {
		if !unknownFactState(human.State) && human.State != automated.State {
			return copyrighteligibility.FactEvidence{}, ErrHumanEvidenceConflict
		}
		return canonicalFactEvidence(automated), nil
	}
	if unknownFactState(human.State) {
		return copyrighteligibility.FactEvidence{State: copyrighteligibility.FactUnknown}, nil
	}
	return canonicalFactEvidence(human), nil
}

func unknownWorkCategory(value copyrighteligibility.WorkCategory) bool {
	return value == "" || value == copyrighteligibility.WorkCategoryUnknown
}

func unknownFactState(value copyrighteligibility.FactState) bool {
	return value == "" || value == copyrighteligibility.FactUnknown
}

func providerReference(provider copyrighteligibility.ProviderEvidence, fact string) copyrighteligibility.EvidenceReference {
	return copyrighteligibility.EvidenceReference{
		Source:     "Project Gutenberg RDF",
		Identifier: provider.ExternalID,
		Digest:     provider.EvidenceDigest,
		Fact:       fact,
	}
}

func providerFactEvidence(provider copyrighteligibility.ProviderEvidence, state copyrighteligibility.FactState, fact string) copyrighteligibility.FactEvidence {
	return copyrighteligibility.FactEvidence{State: state, References: []copyrighteligibility.EvidenceReference{providerReference(provider, fact)}}
}

func contributorsWithRole(values []copyrighteligibility.ContributorEvidence, role string) []copyrighteligibility.ContributorEvidence {
	result := make([]copyrighteligibility.ContributorEvidence, 0, len(values))
	for _, contributor := range values {
		if contributor.Role == role && strings.TrimSpace(contributor.Name) != "" {
			result = append(result, contributor)
		}
	}
	return result
}

func hasContributorRole(values []copyrighteligibility.ContributorEvidence, role string) bool {
	return len(contributorsWithRole(values, role)) > 0
}

func hasTextualContributor(values []copyrighteligibility.ContributorEvidence) bool {
	for _, contributor := range values {
		switch contributor.Role {
		case "adapter", "annotator", "compiler", "introduction_author", "editor", "contributor":
			return true
		}
	}
	return false
}

func canonicalProviderEvidence(value copyrighteligibility.ProviderEvidence) copyrighteligibility.ProviderEvidence {
	value.Contributors = append([]copyrighteligibility.ContributorEvidence(nil), value.Contributors...)
	sort.Slice(value.Contributors, func(i, j int) bool {
		left, right := value.Contributors[i], value.Contributors[j]
		if left.Role != right.Role {
			return left.Role < right.Role
		}
		return left.Name < right.Name
	})
	value.Languages = append([]string(nil), value.Languages...)
	sort.Strings(value.Languages)
	return value
}

func canonicalUKEvidence(value copyrighteligibility.UKEvidence) copyrighteligibility.UKEvidence {
	value.WorkTitle = strings.TrimSpace(value.WorkTitle)
	value.WorkCategoryReferences = canonicalReferences(value.WorkCategoryReferences)
	value.AuthorshipReferences = canonicalReferences(value.AuthorshipReferences)
	value.Author.References = canonicalReferences(value.Author.References)
	value.FirstPublication.References = canonicalReferences(value.FirstPublication.References)
	value.Translation = canonicalFactEvidence(value.Translation)
	value.AdditionalTextualContribution = canonicalFactEvidence(value.AdditionalTextualContribution)
	value.UnpublishedAtEnd1988 = canonicalFactEvidence(value.UnpublishedAtEnd1988)
	return value
}

func canonicalFactEvidence(value copyrighteligibility.FactEvidence) copyrighteligibility.FactEvidence {
	value.References = canonicalReferences(value.References)
	return value
}

func canonicalReferences(values []copyrighteligibility.EvidenceReference) []copyrighteligibility.EvidenceReference {
	result := append([]copyrighteligibility.EvidenceReference(nil), values...)
	for i := range result {
		result[i].Source = strings.TrimSpace(result[i].Source)
		result[i].Fact = strings.TrimSpace(result[i].Fact)
		result[i].Locator = strings.TrimSpace(result[i].Locator)
		result[i].Identifier = strings.TrimSpace(result[i].Identifier)
		result[i].Digest = strings.TrimSpace(result[i].Digest)
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := result[i], result[j]
		return fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s", left.Source, left.Fact, left.Locator, left.Identifier, left.Digest) <
			fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s", right.Source, right.Fact, right.Locator, right.Identifier, right.Digest)
	})
	return result
}
