package gutenberg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"

	"pandapages/api/internal/sourceprovider"
)

const maxSourceBytes = 10 << 20 // 10 MiB decoded plain-text source body.

// Acquire obtains one provider-constructed, trusted plain-text representation and
// returns an in-memory review candidate. It never writes Panda Pages data.
func (a *Adapter) Acquire(ctx context.Context, externalID string) (sourceprovider.SourceCandidate, error) {
	acquired, err := a.AcquireEvidence(ctx, externalID)
	if err != nil {
		return sourceprovider.SourceCandidate{}, err
	}
	return acquired.Candidate, nil
}

// AcquireEvidence obtains one trusted plain-text body and derives both the
// existing review candidate and bounded header rights evidence from it.
func (a *Adapter) AcquireEvidence(ctx context.Context, externalID string) (sourceprovider.AcquisitionEvidence, error) {
	work, err := a.GetWork(ctx, externalID)
	if err != nil {
		return sourceprovider.AcquisitionEvidence{}, err
	}
	representation, err := plainTextRepresentation(work.ExternalID)
	if err != nil {
		return sourceprovider.AcquisitionEvidence{}, err
	}
	content, err := a.fetchPlainText(ctx, representation.URL)
	if err != nil {
		return sourceprovider.AcquisitionEvidence{}, err
	}
	headerRights := classifySourceHeaderRights(content)
	normalised, err := normalisePlainText(content)
	if err != nil {
		return sourceprovider.AcquisitionEvidence{}, err
	}

	return sourceprovider.AcquisitionEvidence{
		OPDSRights:        classifyProviderRights(work.ProviderRights),
		HeaderRights:      headerRights,
		SourceFrontMatter: boundedSourceFrontMatter(content),
		Candidate: sourceprovider.SourceCandidate{
			Provider:               work.Provider,
			ExternalID:             work.ExternalID,
			Title:                  work.Title,
			Contributors:           work.Contributors,
			Languages:              work.Languages,
			LandingURL:             work.LandingURL,
			ProviderRights:         work.ProviderRights,
			SelectedRepresentation: representation,
			NormalisationVersion:   normalisationVersion,
			RetrievedContentHash:   sha256Hex(content),
			NormalisedContentHash:  sha256HexString(normalised),
			SourceText:             normalised,
		},
	}, nil
}

func boundedSourceFrontMatter(content []byte) string {
	if len(content) > sourceHeaderScanBytes {
		content = content[:sourceHeaderScanBytes]
	}
	return string(content)
}

func plainTextRepresentation(externalID string) (sourceprovider.Representation, error) {
	if !validExternalID(externalID) {
		return sourceprovider.Representation{}, sourceprovider.ErrWorkIDInvalid
	}
	return sourceprovider.Representation{
		Label:     "Plain Text UTF-8",
		MediaType: "text/plain; charset=utf-8",
		URL:       "https://" + endpointHost + "/cache/epub/" + externalID + "/pg" + externalID + ".txt",
	}, nil
}

func plainTextCharsetRank(raw string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "utf-8", "utf8":
		return 0, true
	case "":
		return 1, true
	case "us-ascii":
		return 2, true
	default:
		return 0, false
	}
}

func (a *Adapter) fetchPlainText(ctx context.Context, rawURL string) ([]byte, error) {
	if !trustedRepresentationURL(rawURL) {
		return nil, sourceprovider.ErrRepresentationUnavailable
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, sourceprovider.ErrUnavailable
	}
	request.Header.Set("Accept", "text/plain")
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

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, sourceprovider.ErrUnavailable
	}
	if !validPlainTextContentType(response.Header.Get("Content-Type")) {
		return nil, sourceprovider.ErrContentInvalid
	}
	if response.ContentLength > maxSourceBytes {
		return nil, sourceprovider.ErrContentTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxSourceBytes+1))
	if err != nil {
		return nil, sourceprovider.ErrContentInvalid
	}
	if len(body) > maxSourceBytes {
		return nil, sourceprovider.ErrContentTooLarge
	}
	if !utf8.Valid(body) {
		return nil, sourceprovider.ErrContentInvalid
	}
	return body, nil
}

func validPlainTextContentType(raw string) bool {
	mediaType, parameters, err := mime.ParseMediaType(raw)
	if err != nil || !strings.EqualFold(mediaType, "text/plain") {
		return false
	}
	_, ok := plainTextCharsetRank(parameters["charset"])
	return ok
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func sha256HexString(value string) string {
	hash := sha256.New()
	_, _ = io.WriteString(hash, value)
	return hex.EncodeToString(hash.Sum(nil))
}

var _ sourceprovider.Acquirer = (*Adapter)(nil)
var _ sourceprovider.EvidenceAcquirer = (*Adapter)(nil)
