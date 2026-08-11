package gutenberg

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"pandapages/api/internal/sourceprovider"
)

const searchFixture = `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom" xmlns:dcterms="http://purl.org/dc/terms/"><entry><id>https://www.gutenberg.org/ebooks/authors/search.opds/?query=alice</id><title>Authors</title></entry><entry><id>https://www.gutenberg.org/ebooks/11.opds</id><title>Alice's Adventures in Wonderland</title><rights>Public domain in the USA.</rights><author><name>Carroll, Lewis</name></author><dcterms:language>en</dcterms:language><link rel="http://opds-spec.org/acquisition" type="text/plain" title="Plain Text UTF-8" length="123" href="https://www.gutenberg.org/files/11/11-0.txt"/></entry><entry><id>https://www.gutenberg.org/ebooks/12.opds</id><title>Through the Looking-Glass</title><author><name>Carroll, Lewis</name></author><dcterms:language>en</dcterms:language></entry></feed>`

const workFixture = `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom" xmlns:dcterms="http://purl.org/dc/terms/"><entry><id>urn:gutenberg:11:2</id><title>Alice's Adventures in Wonderland</title><rights>Public domain in the USA.</rights><author><name>Carroll, Lewis</name></author><dcterms:language>en</dcterms:language><link rel="http://opds-spec.org/acquisition" type="application/epub+zip" title="EPUB" length="456" href="https://www.gutenberg.org/ebooks/11.epub.images"/></entry><entry><id>urn:gutenberg:11:3</id><title>Alice's Adventures in Wonderland</title><rights>Public domain in the USA.</rights><author><name>Carroll, Lewis</name></author><dcterms:language>en</dcterms:language><link rel="http://opds-spec.org/acquisition" type="application/x-mobipocket-ebook" title="Kindle" length="789" href="https://www.gutenberg.org/ebooks/11.kf8.images"/></entry></feed>`

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

type fixedHostTransport struct {
	target *url.URL
	base   http.RoundTripper
	mu     sync.Mutex
	seen   []*url.URL
}

func (t *fixedHostTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.mu.Lock()
	seen := *request.URL
	t.seen = append(t.seen, &seen)
	t.mu.Unlock()
	clone := request.Clone(request.Context())
	endpoint := *request.URL
	endpoint.Scheme = t.target.Scheme
	endpoint.Host = t.target.Host
	clone.URL = &endpoint
	clone.Host = ""
	return t.base.RoundTrip(clone)
}
func (t *fixedHostTransport) requests() []*url.URL {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]*url.URL(nil), t.seen...)
}
func adapterForServer(server *httptest.Server) (*Adapter, *fixedHostTransport) {
	target, _ := url.Parse(server.URL)
	transport := &fixedHostTransport{target: target, base: http.DefaultTransport}
	return New(Config{HTTPClient: &http.Client{Transport: transport}}), transport
}

func TestParseSearchNormalisesOnlyGutenbergWorks(t *testing.T) {
	result, err := parseSearch([]byte(searchFixture))
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider != sourceprovider.ProjectGutenberg || len(result.Results) != 2 {
		t.Fatalf("result=%+v", result)
	}
	first := result.Results[0]
	if first.ExternalID != "11" || first.Title != "Alice's Adventures in Wonderland" || first.LandingURL != "https://www.gutenberg.org/ebooks/11" {
		t.Fatalf("first=%+v", first)
	}
	if len(first.Contributors) != 1 || first.Contributors[0] != (sourceprovider.Contributor{Name: "Carroll, Lewis", Role: "author"}) || len(first.Languages) != 1 || first.Languages[0] != "en" || first.ProviderRights != "Public domain in the USA." {
		t.Fatalf("metadata=%+v", first)
	}
	if len(first.Representations) != 1 || first.Representations[0].URL != "https://www.gutenberg.org/files/11/11-0.txt" || first.Representations[0].SizeBytes != 123 {
		t.Fatalf("representations=%+v", first.Representations)
	}
}
func TestParseWorkAggregatesSupportedMachineDetail(t *testing.T) {
	work, err := parseWork([]byte(workFixture), "11")
	if err != nil {
		t.Fatal(err)
	}
	if work.ExternalID != "11" || work.LandingURL != "https://www.gutenberg.org/ebooks/11" || len(work.Representations) != 2 {
		t.Fatalf("work=%+v", work)
	}
	if work.ProviderRights != "Public domain in the USA." || work.Representations[1].MediaType != "application/x-mobipocket-ebook" {
		t.Fatalf("work=%+v", work)
	}
}
func TestParserRejectsMalformedRequiredProviderData(t *testing.T) {
	for _, body := range []string{`<feed><entry><id>https://www.gutenberg.org/ebooks/not-a-number.opds</id><title>Broken</title></entry></feed>`, `<feed><entry><id>https://www.gutenberg.org/ebooks/11.opds</id><title></title></entry></feed>`, `<feed><entry>`} {
		if _, err := parseSearch([]byte(body)); !errors.Is(err, sourceprovider.ErrResponseInvalid) {
			t.Fatalf("error=%v body=%q", err, body)
		}
	}
}
func TestSearchUsesFixedMachineEndpointAndTreatsInputAsQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("User-Agent") != userAgent {
			t.Errorf("user agent=%q", request.Header.Get("User-Agent"))
		}
		response.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
		_, _ = io.WriteString(response, searchFixture)
	}))
	defer server.Close()
	adapter, transport := adapterForServer(server)
	query := "http://127.0.0.1/file:///etc/passwd//evil.example"
	result, err := adapter.Search(context.Background(), query, 1)
	if err != nil || len(result.Results) != 1 {
		t.Fatalf("result/error=%+v/%v", result, err)
	}
	requests := transport.requests()
	if len(requests) != 1 || requests[0].Scheme != "https" || requests[0].Host != endpointHost || requests[0].Path != "/ebooks/search.opds/" || requests[0].Query().Get("query") != query {
		t.Fatalf("requests=%+v", requests)
	}
}
func TestSearchValidationAndResponseBoundaries(t *testing.T) {
	if _, limit, err := validateSearch(" alice ", 0); err != nil || limit != defaultLimit {
		t.Fatalf("default limit/error=%d/%v", limit, err)
	}
	if _, limit, err := validateSearch("alice", maxLimit); err != nil || limit != maxLimit {
		t.Fatalf("maximum limit/error=%d/%v", limit, err)
	}
	adapter := New(Config{HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("request should not be sent") })}})
	for _, input := range []struct {
		query string
		limit int
	}{{"", 0}, {" ", 0}, {"a", 0}, {strings.Repeat("x", maxQueryBytes+1), 0}, {"alice", -1}, {"alice", maxLimit + 1}} {
		if _, err := adapter.Search(context.Background(), input.query, input.limit); !errors.Is(err, sourceprovider.ErrQueryInvalid) {
			t.Fatalf("input=%+v error=%v", input, err)
		}
	}
	if _, err := adapter.GetWork(context.Background(), "11/evil"); !errors.Is(err, sourceprovider.ErrWorkIDInvalid) {
		t.Fatalf("error=%v", err)
	}
}
func TestNetworkFailuresAreFiniteAndBounded(t *testing.T) {
	tests := []struct {
		name string
		h    http.Handler
		want error
	}{
		{"unexpected content type", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, "<html>")
		}), sourceprovider.ErrResponseInvalid},
		{"upstream status", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }), sourceprovider.ErrUnavailable},
		{"malformed xml", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/atom+xml")
			_, _ = io.WriteString(w, "<feed>")
		}), sourceprovider.ErrResponseInvalid},
		{"oversized", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/atom+xml")
			_, _ = io.WriteString(w, strings.Repeat("x", maxResponseBytes+1))
		}), sourceprovider.ErrResponseInvalid},
		{"redirect refused", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://evil.example/feed", http.StatusFound)
		}), sourceprovider.ErrUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(test.h)
			defer server.Close()
			adapter, _ := adapterForServer(server)
			if _, err := adapter.Search(context.Background(), "alice", 10); !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
}
func TestSearchRefusesRedirectsWithoutFollowingTheDestination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example/feed", http.StatusFound)
	}))
	defer server.Close()
	adapter, transport := adapterForServer(server)
	if _, err := adapter.Search(context.Background(), "alice", 10); !errors.Is(err, sourceprovider.ErrUnavailable) {
		t.Fatalf("error=%v", err)
	}
	if requests := transport.requests(); len(requests) != 1 || requests[0].Host != endpointHost {
		t.Fatalf("requests=%+v", requests)
	}
}

func TestSearchHonoursTimeoutAndCancellation(t *testing.T) {
	adapter := New(Config{HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}})
	deadline, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := adapter.Search(deadline, "alice", 10); !errors.Is(err, sourceprovider.ErrTimeout) {
		t.Fatalf("timeout error=%v", err)
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := adapter.Search(cancelled, "alice", 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error=%v", err)
	}
}
