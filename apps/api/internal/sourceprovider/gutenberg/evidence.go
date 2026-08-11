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
	"unicode/utf8"

	"pandapages/api/internal/copyrighteligibility"
	"pandapages/api/internal/sourceprovider"
)

const (
	maxRDFBytes           = 512 << 10 // 512 KiB; per-eBook RDF is metadata, not source text.
	sourceHeaderScanBytes = 64 << 10  // Inspect only the bounded provider wrapper before normalisation.
)

// RDFEvidence retrieves and extracts bounded official Project Gutenberg RDF
// evidence for a validated work ID. It does not persist evidence or evaluate
// copyright eligibility.
func (a *Adapter) RDFEvidence(ctx context.Context, externalID string) (copyrighteligibility.ProviderEvidence, error) {
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
		if !ok || start.Name.Local != "ebook" {
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
	book.About = attribute(start, "about")
	for {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		switch token := token.(type) {
		case xml.StartElement:
			switch token.Name.Local {
			case "title":
				book.Title, err = elementText(decoder, token)
			case "rights":
				book.Rights, err = elementText(decoder, token)
			case "language":
				var language string
				language, err = elementText(decoder, token)
				if language != "" {
					book.Languages = append(book.Languages, language)
				}
			case "creator", "aut", "trl", "edt", "adp", "ann", "com", "aui", "ctb", "ill":
				var contributor copyrighteligibility.ContributorEvidence
				var present bool
				contributor, present, err = rdfContributor(decoder, token)
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

func rdfContributor(decoder *xml.Decoder, relation xml.StartElement) (copyrighteligibility.ContributorEvidence, bool, error) {
	role, ok := rdfContributorRole(relation.Name.Local)
	if !ok {
		if err := decoder.Skip(); err != nil {
			return copyrighteligibility.ContributorEvidence{}, false, err
		}
		return copyrighteligibility.ContributorEvidence{}, false, nil
	}
	for {
		token, err := decoder.Token()
		if err != nil {
			return copyrighteligibility.ContributorEvidence{}, false, err
		}
		switch token := token.(type) {
		case xml.StartElement:
			if token.Name.Local != "agent" {
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
			return contributor, true, nil
		case xml.EndElement:
			if token.Name == relation.Name {
				return copyrighteligibility.ContributorEvidence{}, false, nil
			}
		}
	}
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
			switch token.Name.Local {
			case "name", "birthdate", "deathdate":
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

func rdfContributorRole(raw string) (string, bool) {
	switch raw {
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

func attribute(start xml.StartElement, local string) string {
	for _, attribute := range start.Attr {
		if attribute.Name.Local == local {
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
