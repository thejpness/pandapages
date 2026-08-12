package bnf

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"pandapages/api/internal/evidenceresolver"
)

const aliceFixture = `{
  "results": {"bindings": [{
    "work": {"type": "uri", "value": "http://data.bnf.fr/ark:/12148/cb12011248f#about"},
    "title": {"type": "literal", "value": "Alice's Adventures in Wonderland"},
    "firstYear": {"type": "typed-literal", "value": "1865"},
    "creator": {"type": "uri", "value": "http://data.bnf.fr/ark:/12148/cb118859183#about"},
    "creatorName": {"type": "literal", "value": "Lewis Carroll"},
    "language": {"type": "uri", "value": "http://id.loc.gov/vocabulary/iso639-2/eng"},
    "subject": {"type": "literal", "value": "Littératures"}
  }]}
}`

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
	target := *request.URL
	target.Scheme = t.target.Scheme
	target.Host = t.target.Host
	clone.URL = &target
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

func exactQuery() evidenceresolver.Query {
	return evidenceresolver.Query{
		Provider:   "project-gutenberg",
		ExternalID: "11",
		Title:      "Alice's Adventures in Wonderland",
		Authors:    []evidenceresolver.Person{{Name: "Carroll, Lewis"}},
	}
}

func TestLookupUsesOnlyFixedBoundedBNFSPARQLEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Accept") != "application/sparql-results+json" || request.Header.Get("User-Agent") != productUserAgent {
			t.Fatalf("headers=%#v", request.Header)
		}
		if request.URL.Path != endpointPath || request.URL.Query().Get("format") != "application/sparql-results+json" {
			t.Fatalf("request=%q", request.URL.String())
		}
		query := request.URL.Query().Get("query")
		if !strings.Contains(query, "bnf:firstYear") || strings.Contains(query, "dcterms:contributor") || !strings.Contains(query, `LCASE("Alice's Adventures in Wonderland")`) || !strings.Contains(query, "LIMIT 51") {
			t.Fatalf("query=%q", query)
		}
		w.Header().Set("Content-Type", "application/sparql-results+json; charset=utf-8")
		_, _ = io.WriteString(w, aliceFixture)
	}))
	defer server.Close()
	adapter, transport := adapterForServer(server)
	records, err := adapter.Lookup(context.Background(), exactQuery())
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%#v err=%v", records, err)
	}
	record := records[0]
	if record.Source != evidenceresolver.SourceBibliothequeNationaleDeFrance || record.Identifier != "ark:/12148/cb12011248f" || record.WorkID != record.Identifier || record.Locator != "https://data.bnf.fr/ark:/12148/cb12011248f" || record.FirstPublicationYear == nil || *record.FirstPublicationYear != 1865 || len(record.Authors) != 1 || record.Authors[0].Name != "Lewis Carroll" || len(record.OriginalLanguages) != 1 || record.OriginalLanguages[0] != "eng" || len(record.Subjects) != 1 || record.Subjects[0] != "Littératures" || len(record.Contributors) != 1 || record.Contributors[0].Role != "author" || record.Digest == "" {
		t.Fatalf("record=%#v", record)
	}
	requests := transport.requests()
	if len(requests) != 1 || requests[0].Scheme != "https" || requests[0].Host != host || requests[0].Path != endpointPath {
		t.Fatalf("requests=%+v", requests)
	}
}

func TestLookupDoesNotTreatWorkLevelGenericContributorAsRoleEvidence(t *testing.T) {
	fixture := strings.Replace(aliceFixture, `"language": {`, `"contributor": {"type": "uri", "value": "http://data.bnf.fr/ark:/12148/cb11926193t#about"},
    "contributorName": {"type": "literal", "value": "Example Editor"},
    "language": {`, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/sparql-results+json")
		_, _ = io.WriteString(w, fixture)
	}))
	defer server.Close()
	adapter, _ := adapterForServer(server)
	records, err := adapter.Lookup(context.Background(), exactQuery())
	if err != nil || len(records) != 1 || len(records[0].Contributors) != 1 || records[0].Contributors[0].Role != "author" {
		t.Fatalf("records=%#v err=%v", records, err)
	}
}

func TestLookupRejectsResultSentinel(t *testing.T) {
	var response selectResponse
	if err := json.Unmarshal([]byte(aliceFixture), &response); err != nil {
		t.Fatal(err)
	}
	for len(response.Results.Bindings) < resultSentinel {
		response.Results.Bindings = append(response.Results.Bindings, response.Results.Bindings[0])
	}
	body, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/sparql-results+json")
		_, _ = w.Write(body)
	}))
	defer server.Close()
	adapter, _ := adapterForServer(server)
	if _, err := adapter.Lookup(context.Background(), exactQuery()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("error=%v", err)
	}
}

func TestLookupFailsClosedForAmbiguousOrUnboundWork(t *testing.T) {
	for _, test := range []struct {
		name    string
		fixture string
	}{
		{"wrong author", strings.Replace(aliceFixture, "Lewis Carroll", "Other Author", 1)},
		{"multiple exact records", strings.Replace(aliceFixture, "\n  }]}\n}", `
  }, {
    "work": {"type": "uri", "value": "http://data.bnf.fr/ark:/12148/cb99999999z#about"},
    "title": {"type": "literal", "value": "Alice's Adventures in Wonderland"},
    "firstYear": {"type": "literal", "value": "1865"},
    "creator": {"type": "uri", "value": "http://data.bnf.fr/ark:/12148/cb118859183#about"},
    "creatorName": {"type": "literal", "value": "Lewis Carroll"}
  }]}
}`, 1)},
		{"later edition date is not a first publication observation", strings.Replace(aliceFixture, `"firstYear"`, `"date"`, 1)},
		{"untrusted record URI", strings.Replace(aliceFixture, "http://data.bnf.fr/ark:/12148/cb12011248f#about", "https://evil.example/record#about", 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/sparql-results+json")
				_, _ = io.WriteString(w, test.fixture)
			}))
			defer server.Close()
			adapter, _ := adapterForServer(server)
			records, err := adapter.Lookup(context.Background(), exactQuery())
			if err != nil || len(records) != 0 {
				t.Fatalf("records=%#v err=%v", records, err)
			}
		})
	}
}

func TestLookupRejectsInvalidQueryBeforeNetwork(t *testing.T) {
	adapter := New(Config{HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("request must not be sent")
	})}})
	if _, err := adapter.Lookup(context.Background(), evidenceresolver.Query{Title: "Alice"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("error=%v", err)
	}
}

func TestLookupFailsClosedAtNetworkAndContentBoundaries(t *testing.T) {
	tests := []struct {
		name string
		h    http.Handler
		want error
	}{
		{"redirect", http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			http.Redirect(w, request, "https://evil.example/", http.StatusFound)
		}), ErrUnavailable},
		{"wrong content type", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, "<html>")
		}), ErrInvalid},
		{"oversized", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/sparql-results+json")
			w.Header().Set("Content-Length", "999999")
		}), ErrInvalid},
		{"malformed", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/sparql-results+json")
			_, _ = io.WriteString(w, "{")
		}), ErrInvalid},
		{"non-2xx", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }), ErrUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(test.h)
			defer server.Close()
			adapter, _ := adapterForServer(server)
			if _, err := adapter.Lookup(context.Background(), exactQuery()); !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
}

func TestLookupHonoursCancellation(t *testing.T) {
	adapter := New(Config{HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := adapter.Lookup(ctx, exactQuery()); !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error=%v", err)
	}
}

func TestLiveAlice(t *testing.T) {
	if os.Getenv("PP_LIVE_BNF_SMOKE") != "1" {
		t.Skip("set PP_LIVE_BNF_SMOKE=1 to run the bounded live BnF Alice smoke test")
	}
	records, err := New(Config{}).Lookup(context.Background(), exactQuery())
	if err != nil || len(records) != 1 || records[0].FirstPublicationYear == nil || *records[0].FirstPublicationYear != 1865 {
		t.Fatalf("records=%#v err=%v", records, err)
	}
}
