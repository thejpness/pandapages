// Package gutenberg adapts Project Gutenberg's supported OPDS catalogue to
// Panda Pages' provider-neutral source-discovery model.
package gutenberg

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"pandapages/api/internal/sourceprovider"
)

const (
	userAgent        = "PandaPages/1.0 (+https://www.panda-pages.com)"
	endpointHost     = "www.gutenberg.org"
	defaultLimit     = 10
	maxLimit         = 20
	minQueryRunes    = 2
	maxQueryBytes    = 160
	maxResponseBytes = 2 << 20
	requestTimeout   = 8 * time.Second
)

var externalIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,11}$`)

type Config struct {
	HTTPClient *http.Client
}

type Adapter struct {
	client   *http.Client
	endpoint *url.URL
}

func New(cfg Config) *Adapter {
	endpoint, _ := url.Parse("https://" + endpointHost)
	return newWithEndpoint(cfg, endpoint)
}

func newWithEndpoint(cfg Config, endpoint *url.URL) *Adapter {
	client := cfg.HTTPClient
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		client = &http.Client{Transport: transport}
	}
	copyClient := *client
	copyClient.Timeout = requestTimeout
	copyClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Adapter{client: &copyClient, endpoint: endpoint}
}

func (*Adapter) ID() sourceprovider.ID { return sourceprovider.ProjectGutenberg }

func (a *Adapter) Search(ctx context.Context, query string, limit int) (sourceprovider.SearchResponse, error) {
	query, limit, err := validateSearch(query, limit)
	if err != nil {
		return sourceprovider.SearchResponse{}, err
	}
	endpoint := a.url("/ebooks/search.opds/")
	values := endpoint.Query()
	values.Set("query", query)
	endpoint.RawQuery = values.Encode()
	body, err := a.fetch(ctx, endpoint, false)
	if err != nil {
		return sourceprovider.SearchResponse{}, err
	}
	result, err := parseSearch(body)
	if err != nil {
		return sourceprovider.SearchResponse{}, err
	}
	if len(result.Results) > limit {
		result.Results = result.Results[:limit]
	}
	return result, nil
}

func (a *Adapter) GetWork(ctx context.Context, externalID string) (sourceprovider.Work, error) {
	if !validExternalID(externalID) {
		return sourceprovider.Work{}, sourceprovider.ErrWorkIDInvalid
	}
	body, err := a.fetch(ctx, a.url("/ebooks/"+externalID+".opds"), true)
	if err != nil {
		return sourceprovider.Work{}, err
	}
	return parseWork(body, externalID)
}

func (a *Adapter) url(path string) *url.URL {
	endpoint := *a.endpoint
	endpoint.Path = path
	endpoint.RawPath = ""
	endpoint.RawQuery = ""
	endpoint.Fragment = ""
	return &endpoint
}

func (a *Adapter) fetch(ctx context.Context, endpoint *url.URL, work bool) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, sourceprovider.ErrUnavailable
	}
	request.Header.Set("Accept", "application/atom+xml;profile=opds-catalog, application/atom+xml, application/xml;q=0.9, text/xml;q=0.8")
	request.Header.Set("User-Agent", userAgent)

	response, err := a.client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, sourceprovider.ErrTimeout
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, ctx.Err()
		}
		return nil, sourceprovider.ErrUnavailable
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotFound && work {
		return nil, sourceprovider.ErrWorkNotFound
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, sourceprovider.ErrUnavailable
	}
	if !validContentType(response.Header.Get("Content-Type")) {
		return nil, sourceprovider.ErrResponseInvalid
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(body) > maxResponseBytes {
		return nil, sourceprovider.ErrResponseInvalid
	}
	return body, nil
}

func validateSearch(raw string, limit int) (string, int, error) {
	query := strings.TrimSpace(raw)
	if !utf8.ValidString(query) || utf8.RuneCountInString(query) < minQueryRunes || len(query) > maxQueryBytes {
		return "", 0, sourceprovider.ErrQueryInvalid
	}
	if limit == 0 {
		limit = defaultLimit
	}
	if limit < 1 || limit > maxLimit {
		return "", 0, sourceprovider.ErrQueryInvalid
	}
	return query, limit, nil
}

func validExternalID(value string) bool { return externalIDPattern.MatchString(value) }

func validContentType(raw string) bool {
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(mediaType) {
	case "application/atom+xml", "application/xml", "text/xml":
		return true
	default:
		return false
	}
}

type feed struct {
	Entries []entry `xml:"entry"`
}

type entry struct {
	ID        string   `xml:"id"`
	Title     string   `xml:"title"`
	Rights    string   `xml:"rights"`
	Authors   []author `xml:"author"`
	Languages []string `xml:"language"`
	Links     []link   `xml:"link"`
}

type author struct {
	Name string `xml:"name"`
}

type link struct {
	Rel    string `xml:"rel,attr"`
	Type   string `xml:"type,attr"`
	Title  string `xml:"title,attr"`
	Href   string `xml:"href,attr"`
	Length string `xml:"length,attr"`
}

func parseSearch(body []byte) (sourceprovider.SearchResponse, error) {
	feed, err := parseFeed(body)
	if err != nil {
		return sourceprovider.SearchResponse{}, err
	}
	results := make([]sourceprovider.WorkSummary, 0, len(feed.Entries))
	for _, item := range feed.Entries {
		externalID, isBook, err := externalIDFromSearchEntry(item.ID)
		if err != nil {
			return sourceprovider.SearchResponse{}, err
		}
		if !isBook {
			continue
		}
		work, err := workFromEntry(item, externalID)
		if err != nil {
			return sourceprovider.SearchResponse{}, err
		}
		results = append(results, work)
	}
	return sourceprovider.SearchResponse{Provider: sourceprovider.ProjectGutenberg, Results: results}, nil
}

func parseWork(body []byte, externalID string) (sourceprovider.Work, error) {
	feed, err := parseFeed(body)
	if err != nil || len(feed.Entries) == 0 {
		return sourceprovider.Work{}, sourceprovider.ErrResponseInvalid
	}
	work, err := workFromEntry(feed.Entries[0], externalID)
	if err != nil {
		return sourceprovider.Work{}, err
	}
	work.Representations = make([]sourceprovider.Representation, 0)
	seen := make(map[string]struct{})
	for _, item := range feed.Entries {
		if work.ProviderRights == "" {
			work.ProviderRights = boundedText(item.Rights, 1000)
		}
		for _, representation := range representations(item.Links) {
			if _, exists := seen[representation.URL]; exists {
				continue
			}
			seen[representation.URL] = struct{}{}
			work.Representations = append(work.Representations, representation)
		}
	}
	return work, nil
}

func parseFeed(body []byte) (feed, error) {
	decoder := xml.NewDecoder(strings.NewReader(string(body)))
	var parsed feed
	if err := decoder.Decode(&parsed); err != nil {
		return feed{}, sourceprovider.ErrResponseInvalid
	}
	return parsed, nil
}

func externalIDFromSearchEntry(raw string) (string, bool, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, endpointHost) {
		return "", false, nil
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "ebooks" || !strings.HasSuffix(parts[1], ".opds") {
		return "", false, nil
	}
	externalID := strings.TrimSuffix(parts[1], ".opds")
	if !validExternalID(externalID) {
		return "", false, sourceprovider.ErrResponseInvalid
	}
	return externalID, true, nil
}

func workFromEntry(item entry, externalID string) (sourceprovider.Work, error) {
	title := boundedText(item.Title, 1000)
	if title == "" || !validExternalID(externalID) {
		return sourceprovider.Work{}, sourceprovider.ErrResponseInvalid
	}
	return sourceprovider.Work{
		Provider:        sourceprovider.ProjectGutenberg,
		ExternalID:      externalID,
		Title:           title,
		Contributors:    contributors(item.Authors),
		Languages:       languages(item.Languages),
		LandingURL:      "https://" + endpointHost + "/ebooks/" + externalID,
		ProviderRights:  boundedText(item.Rights, 1000),
		Representations: representations(item.Links),
	}, nil
}

func contributors(authors []author) []sourceprovider.Contributor {
	result := make([]sourceprovider.Contributor, 0, len(authors))
	for _, author := range authors {
		if name := boundedText(author.Name, 500); name != "" {
			result = append(result, sourceprovider.Contributor{Name: name, Role: "author"})
		}
	}
	return result
}

func languages(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{})
	for _, value := range values {
		language := strings.ToLower(boundedText(value, 64))
		if language == "" {
			continue
		}
		if _, exists := seen[language]; !exists {
			seen[language] = struct{}{}
			result = append(result, language)
		}
	}
	return result
}

func representations(links []link) []sourceprovider.Representation {
	result := make([]sourceprovider.Representation, 0, len(links))
	for _, item := range links {
		if item.Rel != "http://opds-spec.org/acquisition" || !trustedRepresentationURL(item.Href) {
			continue
		}
		mediaType := boundedText(item.Type, 200)
		if mediaType == "" {
			continue
		}
		representation := sourceprovider.Representation{
			Label:     boundedText(item.Title, 500),
			MediaType: mediaType,
			URL:       item.Href,
		}
		if bytes, err := strconv.ParseInt(item.Length, 10, 64); err == nil && bytes > 0 {
			representation.SizeBytes = bytes
		}
		result = append(result, representation)
	}
	return result
}

func trustedRepresentationURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && parsed.Scheme == "https" && strings.EqualFold(parsed.Host, endpointHost) && parsed.User == nil
}

func boundedText(raw string, maximum int) string {
	value := strings.TrimSpace(raw)
	if !utf8.ValidString(value) || len(value) > maximum {
		return ""
	}
	return value
}

var _ sourceprovider.Provider = (*Adapter)(nil)
