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
    "death": {"type": "literal", "value": "1898-01-14"},
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
		if !strings.Contains(query, "bnf:firstYear") || !strings.Contains(query, "bio:death") || strings.Contains(query, "dcterms:contributor") || !strings.Contains(query, `LCASE("Alice's Adventures in Wonderland")`) || !strings.Contains(query, "LIMIT 51") {
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
	if record.Source != evidenceresolver.SourceBibliothequeNationaleDeFrance || record.Identifier != "ark:/12148/cb12011248f" || record.WorkID != record.Identifier || record.Locator != "https://data.bnf.fr/ark:/12148/cb12011248f" || record.FirstPublicationYear == nil || *record.FirstPublicationYear != 1865 || len(record.Authors) != 1 || record.Authors[0].Name != "Lewis Carroll" || record.Authors[0].DeathYear == nil || *record.Authors[0].DeathYear != 1898 || len(record.OriginalLanguages) != 1 || record.OriginalLanguages[0] != "eng" || len(record.Subjects) != 1 || record.Subjects[0] != "Littératures" || len(record.Contributors) != 1 || record.Contributors[0].Role != "author" || record.Digest == "" {
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
	if _, err := adapter.Lookup(context.Background(), evidenceresolver.Query{Title: "Alice"}); !errors.Is(err, ErrInvalid) || !errors.Is(err, evidenceresolver.ErrUnsupportedQuery) {
		t.Fatalf("error=%v", err)
	}
}

func TestExactRecordMapsStructuredAuthorityDeathYears(t *testing.T) {
	for _, test := range []struct {
		name  string
		death string
		want  int
	}{
		{"Emily Brontë", "1848-12-19", 1848},
		{"Jane Austen", "1817-07-18", 1817},
		{"Robert Louis Stevenson", "1894-12-03", 1894},
		{"Bram Stoker", "1912-04-20", 1912},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := evidenceresolver.Query{Title: "Exact Work", Authors: []evidenceresolver.Person{{Name: test.name}}}
			record, ok := exactRecord([]binding{{
				Work:      term{Type: "uri", Value: "https://data.bnf.fr/ark:/12148/cb12011248f#about"},
				Title:     term{Type: "literal", Value: query.Title},
				FirstYear: term{Type: "literal", Value: "1865"},
				Creator:   term{Type: "uri", Value: "https://data.bnf.fr/ark:/12148/cb118859183#about"},
				Name:      term{Type: "literal", Value: test.name},
				Death:     term{Type: "literal", Value: test.death},
			}}, query, []byte("fixture"))
			if !ok || len(record.Authors) != 1 || record.Authors[0].DeathYear == nil || *record.Authors[0].DeathYear != test.want {
				t.Fatalf("record=%#v ok=%v", record, ok)
			}
		})
	}
}

func TestExactRecordRejectsMalformedOrConflictingAuthorityDeath(t *testing.T) {
	query := exactQuery()
	t.Run("malformed date", func(t *testing.T) {
		var response selectResponse
		if err := json.Unmarshal([]byte(aliceFixture), &response); err != nil {
			t.Fatal(err)
		}
		bindings := append([]binding(nil), response.Results.Bindings...)
		bindings[0].Death.Value = "unknown"
		if _, ok := exactRecord(bindings, query, []byte("fixture")); ok {
			t.Fatal("malformed authority death was accepted")
		}
	})
	t.Run("conflicting valid dates", func(t *testing.T) {
		var response selectResponse
		if err := json.Unmarshal([]byte(aliceFixture), &response); err != nil {
			t.Fatal(err)
		}
		bindings := append([]binding(nil), response.Results.Bindings...)
		second := bindings[0]
		second.Death.Value = "1899-01-14"
		bindings = append(bindings, second)
		if _, ok := exactRecord(bindings, query, []byte("fixture")); ok {
			t.Fatal("conflicting valid authority deaths were accepted")
		}
	})
}

func TestExactRecordAcceptsCanonicalEquivalentAuthorName(t *testing.T) {
	var response selectResponse
	if err := json.Unmarshal([]byte(aliceFixture), &response); err != nil {
		t.Fatal(err)
	}
	bindings := append([]binding(nil), response.Results.Bindings...)
	bindings[0].Name.Value = "Brontë, Emily"
	query := evidenceresolver.Query{
		Title:   "Alice's Adventures in Wonderland",
		Authors: []evidenceresolver.Person{{Name: "Emily Brontë"}},
	}
	record, ok := exactRecord(bindings, query, []byte("fixture"))
	if !ok || len(record.Authors) != 1 || record.Authors[0].Name != "Brontë, Emily" {
		t.Fatalf("record=%#v ok=%v", record, ok)
	}
}

func TestExactRecordAcceptsExplicitGutenbergAuthorVariant(t *testing.T) {
	death := 1919
	query := evidenceresolver.Query{
		Title: "The Wonderful Wizard of Oz",
		Authors: []evidenceresolver.Person{{
			Name:         "Baum, L. Frank (Lyman Frank)",
			NameVariants: []string{"L. Frank Baum", "Lyman Frank Baum", "Baum, L. Frank"},
			DeathYear:    &death,
		}},
	}
	bindings := []binding{{
		Work:      term{Type: "uri", Value: "https://data.bnf.fr/ark:/12148/cb119312746#about"},
		Title:     term{Type: "literal", Value: query.Title},
		FirstYear: term{Type: "literal", Value: "1900"},
		Creator:   term{Type: "uri", Value: "https://data.bnf.fr/ark:/12148/cb11890567s#about"},
		Name:      term{Type: "literal", Value: "Lyman Frank Baum"},
		Death:     term{Type: "literal", Value: "1919-05-06"},
		Subject:   term{Type: "literal", Value: "Littératures"},
	}}
	record, ok := exactRecord(bindings, query, []byte("fixture"))
	if !ok || len(record.Authors) != 1 || record.Authors[0].Name != "Lyman Frank Baum" || record.Authors[0].DeathYear == nil || *record.Authors[0].DeathYear != death {
		t.Fatalf("record=%#v ok=%v", record, ok)
	}

	bindings[0].Name.Value = "L. Frederick Baum"
	if _, ok := exactRecord(bindings, query, []byte("fixture")); ok {
		t.Fatal("surname and initial fragment was accepted as a provider identity variant")
	}
}

func TestLookupRejectsTwoAuthorQueryBeforeNetwork(t *testing.T) {
	calls := 0
	adapter := New(Config{HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("request must not be sent")
	})}})
	query := evidenceresolver.Query{Title: "Grimms' Fairy Tales", Authors: []evidenceresolver.Person{{Name: "Jacob Grimm"}, {Name: "Wilhelm Grimm"}}}
	if _, err := adapter.Lookup(context.Background(), query); !errors.Is(err, ErrInvalid) || !errors.Is(err, evidenceresolver.ErrUnsupportedQuery) || calls != 0 {
		t.Fatalf("error=%v calls=%d", err, calls)
	}
}

func TestLookupAcceptsBoundedMisterTitleVariantOnlyWithMatchingAuthor(t *testing.T) {
	fixture := strings.Replace(strings.Replace(strings.Replace(aliceFixture, "Alice's Adventures in Wonderland", "The strange case of Dr Jekyll and mister Hyde", 1), "Lewis Carroll", "Robert Louis Stevenson", 1), "1898-01-14", "1894-12-03", 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if query := request.URL.Query().Get("query"); !strings.Contains(query, `LCASE("The strange case of Dr Jekyll and mister Hyde")`) {
			t.Fatalf("query=%q", query)
		}
		w.Header().Set("Content-Type", "application/sparql-results+json")
		_, _ = io.WriteString(w, fixture)
	}))
	defer server.Close()
	adapter, _ := adapterForServer(server)
	query := evidenceresolver.Query{Title: "The strange case of Dr. Jekyll and Mr. Hyde", Authors: []evidenceresolver.Person{{Name: "Robert Louis Stevenson"}}}
	records, err := adapter.Lookup(context.Background(), query)
	if err != nil || len(records) != 1 || records[0].Title != "The strange case of Dr Jekyll and mister Hyde" {
		t.Fatalf("records=%#v err=%v", records, err)
	}

	wrongAuthor := strings.Replace(fixture, "Robert Louis Stevenson", "Other Author", 1)
	wrongAuthorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/sparql-results+json")
		_, _ = io.WriteString(w, wrongAuthor)
	}))
	defer wrongAuthorServer.Close()
	adapter, _ = adapterForServer(wrongAuthorServer)
	if records, err := adapter.Lookup(context.Background(), query); err != nil || len(records) != 0 {
		t.Fatalf("records=%#v err=%v", records, err)
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
