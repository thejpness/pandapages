// Package sourceprovider defines the provider-neutral boundary for discovering
// externally hosted source works. Providers do not persist Panda Pages data.
package sourceprovider

import (
	"context"
	"errors"
	"fmt"
)

const ProjectGutenberg ID = "project-gutenberg"

var (
	ErrUnknownProvider = errors.New("source provider is not supported")
	ErrQueryInvalid    = errors.New("source provider query is invalid")
	ErrWorkIDInvalid   = errors.New("source provider work identifier is invalid")
	ErrWorkNotFound    = errors.New("source provider work was not found")
	ErrTimeout         = errors.New("source provider request timed out")
	ErrUnavailable     = errors.New("source provider is unavailable")
	ErrResponseInvalid = errors.New("source provider response is invalid")
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

// Discovery is the narrow service used by the admin HTTP layer.
type Discovery interface {
	Search(context.Context, ID, string, int) (SearchResponse, error)
	GetWork(context.Context, ID, string) (Work, error)
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
