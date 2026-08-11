// Package sourceprovider defines the provider-neutral boundary for discovering
// externally hosted source works. Providers do not persist Panda Pages data.
package sourceprovider

import (
	"context"
	"errors"
	"fmt"

	"pandapages/api/internal/copyrighteligibility"
)

const ProjectGutenberg ID = "project-gutenberg"

var (
	ErrUnknownProvider           = errors.New("source provider is not supported")
	ErrQueryInvalid              = errors.New("source provider query is invalid")
	ErrWorkIDInvalid             = errors.New("source provider work identifier is invalid")
	ErrWorkNotFound              = errors.New("source provider work was not found")
	ErrTimeout                   = errors.New("source provider request timed out")
	ErrUnavailable               = errors.New("source provider is unavailable")
	ErrResponseInvalid           = errors.New("source provider response is invalid")
	ErrRepresentationUnavailable = errors.New("source provider has no supported representation")
	ErrContentTooLarge           = errors.New("source provider content is too large")
	ErrContentInvalid            = errors.New("source provider content is invalid")
	ErrNormalisationFailed       = errors.New("source provider content could not be normalised")
	ErrEvidenceUnavailable       = errors.New("source provider evidence is unavailable")
	ErrEvidenceInvalid           = errors.New("source provider evidence is invalid")
	ErrEvidenceTooLarge          = errors.New("source provider evidence is too large")
	ErrEvidenceIdentityMismatch  = errors.New("source provider evidence identity does not match the requested work")
	ErrEvidenceTimeout           = errors.New("source provider evidence request timed out")
)

type ID string

type Contributor struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

type Representation struct {
	Label     string `json:"label"`
	MediaType string `json:"mediaType"`
	URL       string `json:"url"`
	SizeBytes int64  `json:"sizeBytes,omitempty"`
}

type WorkSummary struct {
	Provider        ID               `json:"provider"`
	ExternalID      string           `json:"externalId"`
	Title           string           `json:"title"`
	Contributors    []Contributor    `json:"contributors"`
	Languages       []string         `json:"languages"`
	LandingURL      string           `json:"landingUrl"`
	ProviderRights  string           `json:"providerRights,omitempty"`
	Representations []Representation `json:"representations"`
}

type Work = WorkSummary

// SourceCandidate is reviewable provider material. It is not a canonical source
// and does not represent Panda Pages rights approval.
type SourceCandidate struct {
	Provider       ID            `json:"provider"`
	ExternalID     string        `json:"externalId"`
	Title          string        `json:"title"`
	Contributors   []Contributor `json:"contributors"`
	Languages      []string      `json:"languages"`
	LandingURL     string        `json:"landingUrl"`
	ProviderRights string        `json:"providerRights,omitempty"`
	// SelectedRepresentation is provenance metadata; acquisition never accepts it from callers.
	SelectedRepresentation Representation `json:"selectedRepresentation"`
	NormalisationVersion   string         `json:"normalisationVersion"`
	RetrievedContentHash   string         `json:"retrievedContentHash"`
	NormalisedContentHash  string         `json:"normalisedContentHash"`
	SourceText             string         `json:"sourceText"`
}

type SearchResponse struct {
	Provider ID            `json:"provider"`
	Results  []WorkSummary `json:"results"`
}

// Provider owns the provider-specific protocol and returns only neutral work
// metadata. It must not create stories or write canonical-source rows.
type Provider interface {
	ID() ID
	Search(context.Context, string, int) (SearchResponse, error)
	GetWork(context.Context, string) (Work, error)
}

// Acquirer extends a provider with server-owned source candidate acquisition.
type Acquirer interface {
	Acquire(context.Context, string) (SourceCandidate, error)
}

// AcquisitionEvidence pairs one server-owned candidate fetch with the bounded
// provider evidence extracted from that same source body.
type AcquisitionEvidence struct {
	Candidate    SourceCandidate
	OPDSRights   copyrighteligibility.ProviderRightsClassification
	HeaderRights copyrighteligibility.SourceHeaderRightsClassification
}

// EvidenceAcquirer prevents eligibility orchestration from downloading an
// ebook twice merely to inspect its provider header.
type EvidenceAcquirer interface {
	AcquireEvidence(context.Context, string) (AcquisitionEvidence, error)
}

// CopyrightEvidenceReader returns provider-native copyright evidence in a
// provider-neutral typed form. It never writes Panda Pages data.
type CopyrightEvidenceReader interface {
	CopyrightEvidence(context.Context, string) (copyrighteligibility.ProviderEvidence, error)
}

// Discovery is the narrow service used by the admin HTTP layer.
type Discovery interface {
	Search(context.Context, ID, string, int) (SearchResponse, error)
	GetWork(context.Context, ID, string) (Work, error)
}

// Acquisition is the narrow service used to turn a selected provider work into
// an in-memory review candidate.
type Acquisition interface {
	Acquire(context.Context, ID, string) (SourceCandidate, error)
}

type Registry struct {
	providers map[ID]Provider
}

func NewRegistry(providers ...Provider) (*Registry, error) {
	registry := &Registry{providers: make(map[ID]Provider, len(providers))}
	for _, provider := range providers {
		if provider == nil || provider.ID() == "" {
			return nil, fmt.Errorf("source provider is invalid")
		}
		if _, exists := registry.providers[provider.ID()]; exists {
			return nil, fmt.Errorf("source provider %q is configured more than once", provider.ID())
		}
		registry.providers[provider.ID()] = provider
	}
	return registry, nil
}

func (r *Registry) Search(ctx context.Context, providerID ID, query string, limit int) (SearchResponse, error) {
	provider, err := r.provider(providerID)
	if err != nil {
		return SearchResponse{}, err
	}
	return provider.Search(ctx, query, limit)
}

func (r *Registry) GetWork(ctx context.Context, providerID ID, externalID string) (Work, error) {
	provider, err := r.provider(providerID)
	if err != nil {
		return Work{}, err
	}
	return provider.GetWork(ctx, externalID)
}

func (r *Registry) Acquire(ctx context.Context, providerID ID, externalID string) (SourceCandidate, error) {
	provider, err := r.provider(providerID)
	if err != nil {
		return SourceCandidate{}, err
	}
	acquirer, ok := provider.(Acquirer)
	if !ok {
		return SourceCandidate{}, ErrRepresentationUnavailable
	}
	return acquirer.Acquire(ctx, externalID)
}

func (r *Registry) AcquireEvidence(ctx context.Context, providerID ID, externalID string) (AcquisitionEvidence, error) {
	provider, err := r.provider(providerID)
	if err != nil {
		return AcquisitionEvidence{}, err
	}
	acquirer, ok := provider.(EvidenceAcquirer)
	if !ok {
		return AcquisitionEvidence{}, ErrEvidenceUnavailable
	}
	return acquirer.AcquireEvidence(ctx, externalID)
}

func (r *Registry) CopyrightEvidence(ctx context.Context, providerID ID, externalID string) (copyrighteligibility.ProviderEvidence, error) {
	provider, err := r.provider(providerID)
	if err != nil {
		return copyrighteligibility.ProviderEvidence{}, err
	}
	reader, ok := provider.(CopyrightEvidenceReader)
	if !ok {
		return copyrighteligibility.ProviderEvidence{}, ErrEvidenceUnavailable
	}
	return reader.CopyrightEvidence(ctx, externalID)
}

func (r *Registry) provider(id ID) (Provider, error) {
	if r == nil {
		return nil, ErrUnknownProvider
	}
	provider, ok := r.providers[id]
	if !ok {
		return nil, ErrUnknownProvider
	}
	return provider, nil
}
