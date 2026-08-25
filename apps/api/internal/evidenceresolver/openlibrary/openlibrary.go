// Package openlibrary retrieves bounded structured bibliographic records from
// Open Library. It is corroborating evidence only and never makes a copyright
// decision.
package openlibrary

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"pandapages/api/internal/evidenceresolver"
)

const (
	host             = "openlibrary.org"
	productUserAgent = "PandaPages/1.0"
	requestTimeout   = 8 * time.Second
	maxResponseBytes = 512 << 10
)

var (
	authorKeyPattern = regexp.MustCompile(`^OL[0-9]+A$`)
	workKeyPattern   = regexp.MustCompile(`^/works/OL[0-9]+W$`)
	yearPattern      = regexp.MustCompile(`^([1-2][0-9]{3})$`)
)

var (
	ErrUnavailable = errors.New("open library evidence unavailable")
	ErrInvalid     = errors.New("open library evidence invalid")
)

const (
	searchSourceName = "Open Library search"
	fullSourceName   = "Open Library"
	maxAuthorQueries = 3
)

type Config struct {
	HTTPClient          *http.Client
	ContactEmailOrPhone string
}

type Adapter struct {
	client    *http.Client
	userAgent string
}

func New(cfg Config) *Adapter {
	client := cfg.HTTPClient
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		client = &http.Client{Transport: transport}
	}
	copyClient := *client
	copyClient.Timeout = requestTimeout
	copyClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &Adapter{client: &copyClient, userAgent: configuredUserAgent(cfg.ContactEmailOrPhone)}
}

func (*Adapter) SourceClass() evidenceresolver.SourceClass { return evidenceresolver.SourceOpenLibrary }

// Lookup uses the bounded official search and author APIs. It makes at most
// four requests: up to three explicit provider-authenticated author-name
// searches and one exact author record for the selected exact-title candidate.
func (a *Adapter) Lookup(ctx context.Context, query evidenceresolver.Query) ([]evidenceresolver.BibliographicRecord, error) {
	if !validQuery(query) {
		return nil, errors.Join(ErrInvalid, evidenceresolver.ErrUnsupportedQuery)
	}
	var baseline evidenceresolver.BibliographicRecord
	var searchBody []byte
	for _, authorName := range evidenceresolver.QueryPersonNames(query.Authors[0], maxAuthorQueries) {
		body, err := a.fetch(ctx, searchURL(query, authorName))
		if err != nil {
			return nil, err
		}
		var response searchResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, ErrInvalid
		}
		document, ok := exactDocument(response.Docs, query)
		if !ok {
			continue
		}
		baseline, ok = searchRecord(document, body)
		if ok {
			searchBody = body
			break
		}
	}
	if baseline.Identifier == "" {
		return nil, nil
	}
	authorKey := baseline.Authors[0].Identifiers[0].Value
	authorBody, err := a.fetch(ctx, authorURL(authorKey))
	if err != nil {
		if errors.Is(err, ErrUnavailable) {
			return []evidenceresolver.BibliographicRecord{baseline}, nil
		}
		return nil, err
	}
	var author authorResponse
	if err := json.Unmarshal(authorBody, &author); err != nil {
		return nil, ErrInvalid
	}
	deathYear, _ := parseYear(author.DeathDate)
	if !evidenceresolver.MatchesPersonName(query.Authors[0], author.Name) {
		return nil, ErrInvalid
	}
	baseline.SourceName = fullSourceName
	baseline.Digest = digest(append(searchBody, authorBody...))
	baseline.Authors[0].Name = author.Name
	baseline.Authors[0].DeathYear = deathYear
	baseline.Contributors[0].Name = author.Name
	return []evidenceresolver.BibliographicRecord{baseline}, nil
}

type searchResponse struct {
	Docs []searchDocument `json:"docs"`
}

type searchDocument struct {
	Key              string   `json:"key"`
	Title            string   `json:"title"`
	AuthorNames      []string `json:"author_name"`
	AuthorKeys       []string `json:"author_key"`
	FirstPublishYear any      `json:"first_publish_year"`
	Languages        []string `json:"language"`
	Subjects         []string `json:"subject"`
}

type authorResponse struct {
	Name      string `json:"name"`
	DeathDate string `json:"death_date"`
}

func exactDocument(documents []searchDocument, query evidenceresolver.Query) (searchDocument, bool) {
	var match searchDocument
	found := false
	for _, document := range documents {
		if !workKeyPattern.MatchString(document.Key) || evidenceresolver.NormalisedTitle(document.Title) != evidenceresolver.NormalisedTitle(query.Title) || len(document.AuthorNames) != 1 || len(document.AuthorKeys) != 1 || !authorKeyPattern.MatchString(document.AuthorKeys[0]) || !evidenceresolver.MatchesPersonName(query.Authors[0], document.AuthorNames[0]) {
			continue
		}
		if found {
			return searchDocument{}, false
		}
		match = document
		found = true
	}
	return match, found
}

func searchRecord(document searchDocument, body []byte) (evidenceresolver.BibliographicRecord, bool) {
	if len(document.AuthorNames) != 1 || len(document.AuthorKeys) != 1 || !authorKeyPattern.MatchString(document.AuthorKeys[0]) {
		return evidenceresolver.BibliographicRecord{}, false
	}
	firstPublication, _ := parseYear(document.FirstPublishYear)
	authorIdentity := evidenceresolver.Identifier{Source: evidenceresolver.SourceOpenLibrary, Value: document.AuthorKeys[0]}
	author := evidenceresolver.Person{Name: document.AuthorNames[0], Identifiers: []evidenceresolver.Identifier{authorIdentity}}
	return evidenceresolver.BibliographicRecord{
		Source:               evidenceresolver.SourceOpenLibrary,
		SourceName:           searchSourceName,
		Identifier:           document.Key,
		Locator:              "https://openlibrary.org" + document.Key,
		Digest:               digest(body),
		Title:                document.Title,
		WorkID:               document.Key,
		Authors:              []evidenceresolver.Person{author},
		Contributors:         []evidenceresolver.Contributor{{Name: author.Name, Role: "author", Identifiers: author.Identifiers}},
		FirstPublicationYear: firstPublication,
		Languages:            append([]string(nil), document.Languages...),
		Subjects:             append([]string(nil), document.Subjects...),
	}, true
}

func validQuery(query evidenceresolver.Query) bool {
	return strings.TrimSpace(query.Title) != "" && len(query.Authors) == 1 && strings.TrimSpace(query.Authors[0].Name) != ""
}

func searchURL(query evidenceresolver.Query, authorName string) string {
	endpoint := url.URL{Scheme: "https", Host: host, Path: "/search.json"}
	values := endpoint.Query()
	values.Set("title", query.Title)
	values.Set("author", authorName)
	values.Set("fields", "key,title,author_name,author_key,first_publish_year,language,subject")
	values.Set("limit", "5")
	endpoint.RawQuery = values.Encode()
	return endpoint.String()
}

func authorURL(key string) string {
	return "https://" + host + "/authors/" + key + ".json"
}

func (a *Adapter) fetch(ctx context.Context, rawURL string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, ErrUnavailable
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", a.userAgent)
	response, err := a.client.Do(request)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, ctx.Err()
		}
		return nil, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, ErrUnavailable
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return nil, ErrInvalid
	}
	if response.ContentLength > maxResponseBytes {
		return nil, ErrInvalid
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(body) > maxResponseBytes || !utf8.Valid(body) {
		return nil, ErrInvalid
	}
	return body, nil
}

func parseYear(value interface{}) (*int, bool) {
	switch value := value.(type) {
	case int:
		return boundedYear(value)
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value || value < 1000 || value > 2099 {
			return nil, false
		}
		return boundedYear(int(value))
	case string:
		return parseStringYear(strings.TrimSpace(value))
	default:
		return nil, false
	}
}

func parseStringYear(raw string) (*int, bool) {
	if matches := yearPattern.FindStringSubmatch(raw); len(matches) == 2 {
		year, err := strconv.Atoi(matches[1])
		if err != nil {
			return nil, false
		}
		return boundedYear(year)
	}
	for _, layout := range []string{"2006-01-02", "2 January 2006", "January 2, 2006"} {
		parsed, err := time.Parse(layout, raw)
		if err == nil {
			return boundedYear(parsed.Year())
		}
	}
	return nil, false
}

func boundedYear(year int) (*int, bool) {
	if year < 1000 || year > 2099 {
		return nil, false
	}
	return &year, true
}

func configuredUserAgent(contact string) string {
	contact = strings.TrimSpace(contact)
	if contact == "" || len(contact) > 254 || strings.ContainsAny(contact, "\r\n") {
		return productUserAgent
	}
	return productUserAgent + " (" + contact + ")"
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

var _ evidenceresolver.BibliographicSource = (*Adapter)(nil)
