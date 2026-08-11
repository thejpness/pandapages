package bnf

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
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"pandapages/api/internal/evidenceresolver"
)

const (
	host             = "data.bnf.fr"
	endpointPath     = "/sparql"
	productUserAgent = "PandaPages/1.0"
	requestTimeout   = 15 * time.Second
	maxResponseBytes = 512 << 10
	maxResultRows    = 50
)

var (
	arkPattern  = regexp.MustCompile(`^/ark:/12148/[A-Za-z0-9]+$`)
	yearPattern = regexp.MustCompile(`^[0-9]{1,4}$`)

	ErrUnavailable = errors.New("bibliotheque nationale de france evidence unavailable")
	ErrInvalid     = errors.New("bibliotheque nationale de france evidence invalid")
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

func (*Adapter) SourceClass() evidenceresolver.SourceClass {
	return evidenceresolver.SourceBibliothequeNationaleDeFrance
}

// Lookup makes one fixed-host request for an exact-title BnF work authority
// record. It uses only bnf-onto:firstYear, which BnF defines as a work's
// first-publication year; it deliberately does not use manifestation dates.
func (a *Adapter) Lookup(ctx context.Context, query evidenceresolver.Query) ([]evidenceresolver.BibliographicRecord, error) {
	if !validQuery(query) {
		return nil, ErrInvalid
	}
	body, err := a.fetch(ctx, lookupURL(query.Title))
	if err != nil {
		return nil, err
	}
	var response selectResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, ErrInvalid
	}
	if len(response.Results.Bindings) > maxResultRows {
		return nil, ErrInvalid
	}
	record, ok := exactRecord(response.Results.Bindings, query, body)
	if !ok {
		return nil, nil
	}
	return []evidenceresolver.BibliographicRecord{record}, nil
}

type selectResponse struct {
	Results struct {
		Bindings []binding `json:"bindings"`
	} `json:"results"`
}

type binding struct {
	Work            term `json:"work"`
	Title           term `json:"title"`
	FirstYear       term `json:"firstYear"`
	Creator         term `json:"creator"`
	Name            term `json:"creatorName"`
	Contributor     term `json:"contributor"`
	ContributorName term `json:"contributorName"`
	Language        term `json:"language"`
	Subject         term `json:"subject"`
}

type term struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type workCandidate struct {
	workURI      string
	title        string
	year         int
	creatorURI   string
	creator      string
	contributors map[string]evidenceresolver.Contributor
	languages    map[string]struct{}
	subjects     map[string]struct{}
	invalid      bool
}

func exactRecord(bindings []binding, query evidenceresolver.Query, body []byte) (evidenceresolver.BibliographicRecord, bool) {
	candidates := make(map[string]*workCandidate)
	for _, value := range bindings {
		if value.Work.Type != "uri" || value.Creator.Type != "uri" || strings.TrimSpace(value.Title.Value) == "" || strings.TrimSpace(value.Name.Value) == "" {
			continue
		}
		workURI := strings.TrimSpace(value.Work.Value)
		candidate, exists := candidates[workURI]
		if !exists {
			year, ok := parseYear(value.FirstYear)
			if !ok {
				continue
			}
			candidate = &workCandidate{
				workURI:      workURI,
				title:        strings.TrimSpace(value.Title.Value),
				year:         year,
				creatorURI:   strings.TrimSpace(value.Creator.Value),
				creator:      strings.TrimSpace(value.Name.Value),
				contributors: make(map[string]evidenceresolver.Contributor),
				languages:    make(map[string]struct{}),
				subjects:     make(map[string]struct{}),
			}
			candidates[workURI] = candidate
		} else if candidate.title != strings.TrimSpace(value.Title.Value) || candidate.creatorURI != strings.TrimSpace(value.Creator.Value) || candidate.creator != strings.TrimSpace(value.Name.Value) || candidate.yearString() != strings.TrimSpace(value.FirstYear.Value) {
			candidate.invalid = true
		}
		if language, ok := languageCode(value.Language); ok {
			candidate.languages[language] = struct{}{}
		}
		if value.Contributor.Value != "" || value.ContributorName.Value != "" {
			identifier, _, ok := authorityIdentifier(value.Contributor.Value)
			if value.Contributor.Type != "uri" || !ok || !validText(value.ContributorName.Value, 200) {
				candidate.invalid = true
			} else {
				candidate.contributors[identifier] = evidenceresolver.Contributor{
					Name:        strings.TrimSpace(value.ContributorName.Value),
					Role:        "contributor",
					Identifiers: []evidenceresolver.Identifier{{Source: evidenceresolver.SourceBibliothequeNationaleDeFrance, Value: identifier}},
				}
			}
		}
		if subject := strings.TrimSpace(value.Subject.Value); subject != "" && len([]rune(subject)) <= 300 {
			candidate.subjects[subject] = struct{}{}
		}
	}

	matching := make([]*workCandidate, 0, 1)
	for _, candidate := range candidates {
		if candidate.invalid || canonical(candidate.title) != canonical(query.Title) || canonical(candidate.creator) != canonical(query.Authors[0].Name) {
			continue
		}
		if _, _, ok := authorityIdentifier(candidate.workURI); !ok {
			continue
		}
		if _, _, ok := authorityIdentifier(candidate.creatorURI); !ok {
			continue
		}
		matching = append(matching, candidate)
	}
	if len(matching) != 1 {
		return evidenceresolver.BibliographicRecord{}, false
	}
	candidate := matching[0]
	workID, locator, _ := authorityIdentifier(candidate.workURI)
	creatorID, _, _ := authorityIdentifier(candidate.creatorURI)
	year := candidate.year
	author := evidenceresolver.Person{
		Name:        candidate.creator,
		Identifiers: []evidenceresolver.Identifier{{Source: evidenceresolver.SourceBibliothequeNationaleDeFrance, Value: creatorID}},
	}
	contributors := make([]evidenceresolver.Contributor, 0, len(candidate.contributors)+1)
	contributors = append(contributors, evidenceresolver.Contributor{Name: author.Name, Role: "author", Identifiers: author.Identifiers})
	for _, contributor := range candidate.contributors {
		contributors = append(contributors, contributor)
	}
	sort.Slice(contributors, func(i, j int) bool {
		if contributors[i].Role != contributors[j].Role {
			return contributors[i].Role < contributors[j].Role
		}
		return contributors[i].Name < contributors[j].Name
	})
	return evidenceresolver.BibliographicRecord{
		Source:                   evidenceresolver.SourceBibliothequeNationaleDeFrance,
		SourceName:               "Bibliothèque nationale de France data.bnf.fr",
		Identifier:               workID,
		Locator:                  locator,
		Digest:                   digest(body),
		Title:                    candidate.title,
		WorkID:                   workID,
		ContributorRecordID:      workID,
		Authors:                  []evidenceresolver.Person{author},
		Contributors:             contributors,
		FirstPublicationYear:     &year,
		Languages:                sortedKeys(candidate.languages),
		OriginalLanguages:        sortedKeys(candidate.languages),
		Subjects:                 sortedKeys(candidate.subjects),
		ContributorRolesObserved: true,
	}, true
}

func (c *workCandidate) yearString() string { return strconv.Itoa(c.year) }

func validQuery(query evidenceresolver.Query) bool {
	return validText(query.Title, 300) && len(query.Authors) == 1 && validText(query.Authors[0].Name, 200)
}

func validText(value string, maximum int) bool {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) || len([]rune(value)) > maximum {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func lookupURL(title string) string {
	endpoint := url.URL{Scheme: "https", Host: host, Path: endpointPath}
	values := endpoint.Query()
	values.Set("query", lookupQuery(title))
	values.Set("format", "application/sparql-results+json")
	endpoint.RawQuery = values.Encode()
	return endpoint.String()
}

func lookupQuery(title string) string {
	return "PREFIX bnf: <http://data.bnf.fr/ontology/bnf-onto/>\n" +
		"PREFIX dcterms: <http://purl.org/dc/terms/>\n" +
		"PREFIX foaf: <http://xmlns.com/foaf/0.1/>\n" +
		"SELECT ?work ?title ?firstYear ?creator ?creatorName ?contributor ?contributorName ?language ?subject WHERE {\n" +
		"  ?work bnf:firstYear ?firstYear ; dcterms:title ?title ; dcterms:creator ?creator .\n" +
		"  ?creator foaf:name ?creatorName .\n" +
		"  OPTIONAL { ?work dcterms:contributor ?contributor . ?contributor foaf:name ?contributorName . }\n" +
		"  OPTIONAL { ?work dcterms:language ?language . }\n" +
		"  OPTIONAL { ?work bnf:subject ?subject . }\n" +
		"  FILTER(LCASE(STR(?title)) = LCASE(" + sparqlLiteral(title) + "))\n" +
		"}\nLIMIT 50"
}

func sparqlLiteral(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	return "\"" + value + "\""
}

func (a *Adapter) fetch(ctx context.Context, rawURL string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, ErrUnavailable
	}
	request.Header.Set("Accept", "application/sparql-results+json")
	request.Header.Set("User-Agent", productUserAgent)
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
	if err != nil || (!strings.EqualFold(mediaType, "application/sparql-results+json") && !strings.EqualFold(mediaType, "application/json")) {
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

func parseYear(value term) (int, bool) {
	if (value.Type != "literal" && value.Type != "typed-literal") || !yearPattern.MatchString(strings.TrimSpace(value.Value)) {
		return 0, false
	}
	year, err := strconv.Atoi(value.Value)
	if err != nil || year < 1 || year > 9999 {
		return 0, false
	}
	return year, true
}

func languageCode(value term) (string, bool) {
	raw := strings.TrimSpace(value.Value)
	if raw == "" {
		return "", false
	}
	if value.Type == "uri" {
		const prefix = "http://id.loc.gov/vocabulary/iso639-2/"
		if !strings.HasPrefix(raw, prefix) {
			return "", false
		}
		raw = strings.TrimPrefix(raw, prefix)
	}
	raw = strings.ToLower(raw)
	if len(raw) != 3 {
		return "", false
	}
	for _, r := range raw {
		if r < 'a' || r > 'z' {
			return "", false
		}
	}
	return raw, true
}

func authorityIdentifier(raw string) (string, string, bool) {
	value, err := url.Parse(raw)
	if err != nil || value.User != nil || (value.Scheme != "http" && value.Scheme != "https") || !strings.EqualFold(value.Host, host) || value.RawQuery != "" || value.Fragment != "about" || !arkPattern.MatchString(value.Path) {
		return "", "", false
	}
	identifier := strings.TrimPrefix(value.Path, "/")
	return identifier, "https://" + host + value.Path, true
}

func canonical(value string) string {
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

func sortedKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

var _ evidenceresolver.BibliographicSource = (*Adapter)(nil)
