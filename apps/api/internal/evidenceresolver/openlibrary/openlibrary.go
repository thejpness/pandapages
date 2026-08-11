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
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"pandapages/api/internal/copyrighteligibility"
	"pandapages/api/internal/evidenceresolver"
)

const (
	host             = "openlibrary.org"
	userAgent        = "PandaPages/1.0 (+https://www.panda-pages.com)"
	requestTimeout   = 8 * time.Second
	maxResponseBytes = 512 << 10
)

var (
	authorKeyPattern = regexp.MustCompile(`^OL[0-9]+A$`)
	workKeyPattern   = regexp.MustCompile(`^/works/OL[0-9]+W$`)
)

var (
	ErrUnavailable = errors.New("open library evidence unavailable")
	ErrInvalid     = errors.New("open library evidence invalid")
)

type Config struct {
	HTTPClient *http.Client
}

type Adapter struct {
	client *http.Client
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
	return &Adapter{client: &copyClient}
}

func (*Adapter) SourceClass() evidenceresolver.SourceClass { return evidenceresolver.SourceOpenLibrary }

// Lookup uses the bounded official search and author APIs. It makes at most
// two requests: one search and one exact author record for the selected,
// exact-title candidate.
func (a *Adapter) Lookup(ctx context.Context, query evidenceresolver.Query) ([]evidenceresolver.BibliographicRecord, error) {
	if !validQuery(query) {
		return nil, ErrInvalid
	}
	searchURL := searchURL(query)
	body, err := a.fetch(ctx, searchURL)
	if err != nil {
		return nil, err
	}
	var response searchResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, ErrInvalid
	}
	document, ok := exactDocument(response.Docs, query)
	if !ok {
		return nil, nil
	}
	if len(document.AuthorKeys) != 1 || !authorKeyPattern.MatchString(document.AuthorKeys[0]) || len(document.AuthorNames) != 1 {
		return nil, nil
	}
	authorKey := document.AuthorKeys[0]
	authorBody, err := a.fetch(ctx, authorURL(authorKey))
	if err != nil {
		return nil, err
	}
	var author authorResponse
	if err := json.Unmarshal(authorBody, &author); err != nil {
		return nil, ErrInvalid
	}
	deathYear, ok := parseYear(author.DeathDate)
	if !ok || canonicalName(author.Name) != canonicalName(document.AuthorNames[0]) {
		return nil, nil
	}
	firstPublication, _ := parseYear(document.FirstPublishYear)
	record := evidenceresolver.BibliographicRecord{
		Source:               evidenceresolver.SourceOpenLibrary,
		SourceName:           "Open Library",
		Identifier:           document.Key,
		Locator:              "https://openlibrary.org" + document.Key,
		Digest:               digest(append(body, authorBody...)),
		Title:                document.Title,
		Authors:              []evidenceresolver.Person{{Name: author.Name, Identifiers: []evidenceresolver.Identifier{{Source: evidenceresolver.SourceOpenLibrary, Value: authorKey}}, DeathYear: deathYear}},
		FirstPublicationYear: firstPublication,
		WorkCategory:         workCategory(document.Subjects),
		Translation:          copyrighteligibility.FactUnknown,
		AdditionalTextual:    copyrighteligibility.FactUnknown,
	}
	return []evidenceresolver.BibliographicRecord{record}, nil
}

type searchResponse struct {
	Docs []searchDocument `json:"docs"`
}

type searchDocument struct {
	Key              string   `json:"key"`
	Title            string   `json:"title"`
	AuthorNames      []string `json:"author_name"`
	AuthorKeys       []string `json:"author_key"`
	FirstPublishYear int      `json:"first_publish_year"`
	Subjects         []string `json:"subject"`
}

type authorResponse struct {
	Name      string `json:"name"`
	DeathDate string `json:"death_date"`
}

func exactDocument(documents []searchDocument, query evidenceresolver.Query) (searchDocument, bool) {
	for _, document := range documents {
		if workKeyPattern.MatchString(document.Key) && canonicalName(document.Title) == canonicalName(query.Title) && len(document.AuthorNames) == 1 && canonicalName(document.AuthorNames[0]) == canonicalName(query.Authors[0].Name) {
			return document, true
		}
	}
	return searchDocument{}, false
}

func validQuery(query evidenceresolver.Query) bool {
	return strings.TrimSpace(query.Title) != "" && len(query.Authors) == 1 && strings.TrimSpace(query.Authors[0].Name) != ""
}

func searchURL(query evidenceresolver.Query) string {
	endpoint := url.URL{Scheme: "https", Host: host, Path: "/search.json"}
	values := endpoint.Query()
	values.Set("title", query.Title)
	values.Set("author", query.Authors[0].Name)
	values.Set("fields", "key,title,author_name,author_key,first_publish_year,subject")
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
	request.Header.Set("User-Agent", userAgent)
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
	var raw string
	switch value := value.(type) {
	case int:
		raw = strconv.Itoa(value)
	case string:
		raw = strings.TrimSpace(value)
	default:
		return nil, false
	}
	if len(raw) < 4 || len(raw) > 10 {
		return nil, false
	}
	year, err := strconv.Atoi(raw[:4])
	if err != nil || year < 1 || year > 9999 {
		return nil, false
	}
	return &year, true
}

func workCategory(subjects []string) copyrighteligibility.WorkCategory {
	for _, subject := range subjects {
		value := strings.ToLower(subject)
		if strings.Contains(value, "fiction") || strings.Contains(value, "novel") || strings.Contains(value, "poetry") || strings.Contains(value, "short stories") {
			return copyrighteligibility.WorkCategoryOrdinaryLiterary
		}
	}
	return copyrighteligibility.WorkCategoryUnknown
}

func canonicalName(value string) string {
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

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

var _ evidenceresolver.BibliographicSource = (*Adapter)(nil)
