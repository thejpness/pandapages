package gutenberg

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"mime"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"pandapages/api/internal/copyrighteligibility"
	"pandapages/api/internal/sourceprovider"
)

const (
	rdfNamespace          = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	pgtermsNamespace      = "http://www.gutenberg.org/2009/pgterms/"
	dctermsNamespace      = "http://purl.org/dc/terms/"
	marcrelNamespace      = "http://id.loc.gov/vocabulary/relators/"
	maxRDFBytes           = 512 << 10 // 512 KiB; per-eBook RDF is metadata, not source text.
	sourceHeaderScanBytes = 64 << 10  // Inspect only the bounded provider wrapper before normalisation.
)

// RDFEvidence retrieves and extracts bounded official Project Gutenberg RDF
// evidence for a validated work ID. It does not persist evidence or evaluate
// copyright eligibility.
func (a *Adapter) CopyrightEvidence(ctx context.Context, externalID string) (copyrighteligibility.ProviderEvidence, error) {
	if !validExternalID(externalID) {
		return copyrighteligibility.ProviderEvidence{}, sourceprovider.ErrWorkIDInvalid
	}
	body, err := a.fetchRDF(ctx, externalID)
	if err != nil {
		return copyrighteligibility.ProviderEvidence{}, err
	}
	return parseRDFEvidence(body, externalID)
}

func (a *Adapter) fetchRDF(ctx context.Context, externalID string) ([]byte, error) {
	endpoint := a.url("/cache/epub/" + externalID + "/pg" + externalID + ".rdf")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, sourceprovider.ErrEvidenceUnavailable
	}
	request.Header.Set("Accept", "application/rdf+xml, application/xml;q=0.9, text/xml;q=0.8")
	request.Header.Set("User-Agent", userAgent)

	response, err := a.client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, sourceprovider.ErrEvidenceTimeout
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, ctx.Err()
		}
		return nil, sourceprovider.ErrEvidenceUnavailable
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, sourceprovider.ErrEvidenceUnavailable
	}
	if !validRDFContentType(response.Header.Get("Content-Type")) {
		return nil, sourceprovider.ErrEvidenceInvalid
	}
	if response.ContentLength > maxRDFBytes {
		return nil, sourceprovider.ErrEvidenceTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxRDFBytes+1))
	if err != nil {
		return nil, sourceprovider.ErrEvidenceInvalid
	}
	if len(body) > maxRDFBytes {
		return nil, sourceprovider.ErrEvidenceTooLarge
	}
	return body, nil
}

func validRDFContentType(raw string) bool {
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(mediaType) {
	case "application/rdf+xml", "application/xml", "text/xml":
		return true
	default:
		return false
	}
}

func parseRDFEvidence(body []byte, expectedID string) (copyrighteligibility.ProviderEvidence, error) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return copyrighteligibility.ProviderEvidence{}, sourceprovider.ErrEvidenceInvalid
		}
		if err != nil {
			return copyrighteligibility.ProviderEvidence{}, sourceprovider.ErrEvidenceInvalid
		}
		start, ok := token.(xml.StartElement)
		if !ok || !isElement(start.Name, pgtermsNamespace, "ebook") {
			continue
		}
		var book rdfBook
		if err := decoder.DecodeElement(&book, &start); err != nil {
			return copyrighteligibility.ProviderEvidence{}, sourceprovider.ErrEvidenceInvalid
		}
		identity, ok := rdfExternalID(book.About)
		if !ok || identity != expectedID {
			return copyrighteligibility.ProviderEvidence{}, sourceprovider.ErrEvidenceIdentityMismatch
		}
		return copyrighteligibility.ProviderEvidence{
			Provider:        string(sourceprovider.ProjectGutenberg),
			ExternalID:      identity,
			Title:           boundedText(book.Title, 1000),
			Rights:          classifyProviderRights(book.Rights),
			RightsStatement: boundedText(book.Rights, 1000),
			Contributors:    canonicalContributors(book.Contributors),
			Languages:       canonicalLanguages(book.Languages),
			EvidenceDigest:  sha256Hex(body),
		}, nil
	}
}

type rdfBook struct {
	About        string
	Title        string
	Rights       string
	Languages    []string
	Contributors []copyrighteligibility.ContributorEvidence
}

func (book *rdfBook) UnmarshalXML(decoder *xml.Decoder, start xml.StartElement) error {
	book.About = rdfAbout(start)
	for {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		switch token := token.(type) {
		case xml.StartElement:
			switch {
			case isElement(token.Name, dctermsNamespace, "title"):
				book.Title, err = elementText(decoder, token)
			case isElement(token.Name, dctermsNamespace, "rights"):
				book.Rights, err = elementText(decoder, token)
			case isElement(token.Name, dctermsNamespace, "language"):
				var language string
				language, err = elementText(decoder, token)
				if language != "" {
					book.Languages = append(book.Languages, language)
				}
			case isElement(token.Name, dctermsNamespace, "creator"):
				var contributor copyrighteligibility.ContributorEvidence
				var present bool
				contributor, present, err = rdfContributor(decoder, token, "author")
				if present {
					book.Contributors = append(book.Contributors, contributor)
				}
			case token.Name.Space == marcrelNamespace:
				role, recognised := rdfContributorRole(token.Name)
				if !recognised {
					err = decoder.Skip()
					break
				}
				var contributor copyrighteligibility.ContributorEvidence
				var present bool
				contributor, present, err = rdfContributor(decoder, token, role)
				if present {
					book.Contributors = append(book.Contributors, contributor)
				}
			default:
				err = decoder.Skip()
			}
			if err != nil {
				return err
			}
		case xml.EndElement:
			if token.Name == start.Name {
				return nil
			}
		}
	}
}

func rdfContributor(decoder *xml.Decoder, relation xml.StartElement, role string) (copyrighteligibility.ContributorEvidence, bool, error) {
	for {
		token, err := decoder.Token()
		if err != nil {
			return copyrighteligibility.ContributorEvidence{}, false, err
		}
		switch token := token.(type) {
		case xml.StartElement:
			if !isElement(token.Name, pgtermsNamespace, "agent") {
				if err := decoder.Skip(); err != nil {
					return copyrighteligibility.ContributorEvidence{}, false, err
				}
				continue
			}
			contributor, err := rdfAgent(decoder, token)
			if err != nil {
				return copyrighteligibility.ContributorEvidence{}, false, err
			}
			contributor.Role = role
			if contributor.Name == "" {
				return copyrighteligibility.ContributorEvidence{}, false, nil
			}
			if role == "author" {
				contributor.NameVariants = authorNameVariants(contributor.Name)
			}
			return contributor, true, nil
		case xml.EndElement:
			if token.Name == relation.Name {
				return copyrighteligibility.ContributorEvidence{}, false, nil
			}
		}
	}
}

// authorNameVariants derives only the explicit Gutenberg form
// "Last, First (Expanded First)". The parenthetical component is accepted
// only where it is a compatible expansion of the displayed given name; it is
// not treated as a free-form alias. The raw provider name remains the
// contributor name for provenance.
func authorNameVariants(raw string) []string {
	raw = strings.TrimSpace(raw)
	if strings.Count(raw, ",") != 1 {
		return nil
	}
	parts := strings.SplitN(raw, ",", 2)
	family := strings.TrimSpace(parts[0])
	givenAndExpanded := strings.TrimSpace(parts[1])
	open := strings.Index(givenAndExpanded, "(")
	if open <= 0 || !strings.HasSuffix(givenAndExpanded, ")") || strings.Count(givenAndExpanded, "(") != 1 || strings.Count(givenAndExpanded, ")") != 1 {
		return nil
	}
	given := strings.TrimSpace(givenAndExpanded[:open])
	expanded := strings.TrimSpace(givenAndExpanded[open+1 : len(givenAndExpanded)-1])
	if !validNamePart(family) || !validNamePart(given) || !validNamePart(expanded) || !compatibleGivenNames(given, expanded) {
		return nil
	}
	return distinctNameVariants(
		given+" "+family,
		expanded+" "+family,
		family+", "+given,
	)
}

func validNamePart(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) || len([]rune(value)) > 200 {
		return false
	}
	hasLetter := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsMark(r), unicode.IsSpace(r), r == '.', r == '-', r == '\'', r == '’':
		default:
			return false
		}
	}
	return hasLetter
}

func compatibleGivenNames(abbreviated, expanded string) bool {
	abbreviatedTokens := nameTokens(abbreviated)
	expandedTokens := nameTokens(expanded)
	if len(abbreviatedTokens) == 0 || len(expandedTokens) == 0 || !sameInitial(abbreviatedTokens[0], expandedTokens[0]) {
		return false
	}
	next := 0
	for _, token := range abbreviatedTokens {
		if len([]rune(token)) == 1 {
			continue
		}
		for next < len(expandedTokens) && !strings.EqualFold(token, expandedTokens[next]) {
			next++
		}
		if next == len(expandedTokens) {
			return false
		}
		next++
	}
	return true
}

func nameTokens(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsMark(r)
	})
}

func sameInitial(left, right string) bool {
	leftRunes := []rune(strings.ToLower(left))
	rightRunes := []rune(strings.ToLower(right))
	return len(leftRunes) > 0 && len(rightRunes) > 0 && leftRunes[0] == rightRunes[0]
}

func distinctNameVariants(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		duplicate := false
		for _, existing := range result {
			if strings.EqualFold(existing, value) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			result = append(result, value)
		}
	}
	return result
}

func rdfAgent(decoder *xml.Decoder, start xml.StartElement) (copyrighteligibility.ContributorEvidence, error) {
	var contributor copyrighteligibility.ContributorEvidence
	for {
		token, err := decoder.Token()
		if err != nil {
			return copyrighteligibility.ContributorEvidence{}, err
		}
		switch token := token.(type) {
		case xml.StartElement:
			var value string
			switch {
			case isElement(token.Name, pgtermsNamespace, "name"), isElement(token.Name, pgtermsNamespace, "birthdate"), isElement(token.Name, pgtermsNamespace, "deathdate"):
				value, err = elementText(decoder, token)
				if err != nil {
					return copyrighteligibility.ContributorEvidence{}, err
				}
				switch token.Name.Local {
				case "name":
					contributor.Name = boundedText(value, 500)
				case "birthdate":
					contributor.BirthYear = rdfYear(value)
				case "deathdate":
					contributor.DeathYear = rdfYear(value)
				}
			default:
				if err := decoder.Skip(); err != nil {
					return copyrighteligibility.ContributorEvidence{}, err
				}
			}
		case xml.EndElement:
			if token.Name == start.Name {
				return contributor, nil
			}
		}
	}
}

func elementText(decoder *xml.Decoder, start xml.StartElement) (string, error) {
	var text strings.Builder
	for {
		token, err := decoder.Token()
		if err != nil {
			return "", err
		}
		switch token := token.(type) {
		case xml.CharData:
			text.Write([]byte(token))
		case xml.EndElement:
			if token.Name == start.Name {
				return strings.TrimSpace(text.String()), nil
			}
		}
	}
}

func rdfContributorRole(name xml.Name) (string, bool) {
	if name.Space != marcrelNamespace {
		return "", false
	}
	switch name.Local {
	case "creator", "aut":
		return "author", true
	case "trl":
		return "translator", true
	case "edt":
		return "editor", true
	case "adp":
		return "adapter", true
	case "ann":
		return "annotator", true
	case "com":
		return "compiler", true
	case "aui":
		return "introduction_author", true
	case "ctb":
		return "contributor", true
	case "ill":
		return "illustrator", true
	default:
		return "", false
	}
}

func rdfExternalID(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if !strings.HasPrefix(value, "ebooks/") {
		return "", false
	}
	id := strings.TrimPrefix(value, "ebooks/")
	return id, validExternalID(id)
}

func isElement(name xml.Name, namespace, local string) bool {
	return name.Space == namespace && name.Local == local
}

func rdfAbout(start xml.StartElement) string {
	for _, attribute := range start.Attr {
		if isElement(attribute.Name, rdfNamespace, "about") {
			return attribute.Value
		}
	}
	return ""
}

func rdfYear(raw string) *int {
	value := strings.TrimSpace(raw)
	year, err := strconv.Atoi(value)
	if err != nil || year == 0 || len(value) > 7 {
		return nil
	}
	return &year
}

func canonicalContributors(values []copyrighteligibility.ContributorEvidence) []copyrighteligibility.ContributorEvidence {
	result := append([]copyrighteligibility.ContributorEvidence(nil), values...)
	sort.Slice(result, func(i, j int) bool {
		left, right := result[i], result[j]
		if left.Role != right.Role {
			return left.Role < right.Role
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		if contributorYear(left.DeathYear) != contributorYear(right.DeathYear) {
			return contributorYear(left.DeathYear) < contributorYear(right.DeathYear)
		}
		return contributorYear(left.BirthYear) < contributorYear(right.BirthYear)
	})
	return result
}

func canonicalLanguages(values []string) []string {
	result := languages(values)
	sort.Strings(result)
	return result
}

func contributorYear(year *int) int {
	if year == nil {
		return 0
	}
	return *year
}

func classifyProviderRights(raw string) copyrighteligibility.ProviderRightsClassification {
	switch strings.TrimSpace(raw) {
	case "Public domain in the USA.":
		return copyrighteligibility.ProviderRightsPublicDomain
	case "Copyrighted. Read the copyright notice inside this book for details.":
		return copyrighteligibility.ProviderRightsRestricted
	default:
		return copyrighteligibility.ProviderRightsUnknown
	}
}

// classifySourceHeaderRights inspects only the bounded text before a standard
// Gutenberg start marker. It recognises a tiny set of exact provider notices;
// it never performs keyword or prose analysis of the literary body.
func classifySourceHeaderRights(content []byte) copyrighteligibility.SourceHeaderRightsClassification {
	if !utf8.Valid(content) {
		return copyrighteligibility.SourceHeaderRightsNoClassification
	}
	if len(content) > sourceHeaderScanBytes {
		content = content[:sourceHeaderScanBytes]
	}
	lines := strings.Split(normaliseLineEndings(content), "\n")
	public, restricted := false, false
	for _, line := range lines {
		if isGutenbergStartMarker(line) {
			break
		}
		switch strings.TrimSpace(line) {
		case "Public domain in the USA.":
			public = true
		case "Copyrighted. Read the copyright notice inside this book for details.":
			restricted = true
		}
	}
	switch {
	case public && restricted:
		return copyrighteligibility.SourceHeaderRightsConflicting
	case restricted:
		return copyrighteligibility.SourceHeaderRightsRestricted
	case public:
		return copyrighteligibility.SourceHeaderRightsPublicDomain
	default:
		return copyrighteligibility.SourceHeaderRightsNoClassification
	}
}

var _ sourceprovider.CopyrightEvidenceReader = (*Adapter)(nil)
