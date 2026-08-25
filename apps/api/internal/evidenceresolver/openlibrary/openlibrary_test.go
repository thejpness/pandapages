package openlibrary

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

	"pandapages/api/internal/copyrighteligibility"
	"pandapages/api/internal/evidenceresolver"
)

const searchFixture = `{"docs":[{"key":"/works/OL138052W","title":"Alice's Adventures in Wonderland","author_name":["Lewis Carroll"],"author_key":["OL22098A"],"first_publish_year":1865,"language":["eng"],"subject":["Fiction","Children's literature"]}]}`
const authorFixture = `{"name":"Lewis Carroll","death_date":"1898"}`
const wutheringSearchFixture = `{"docs":[{"key":"/works/OL21177W","title":"Wuthering Heights","author_name":["Emily Brontë"],"author_key":["OL24529A"],"first_publish_year":1846,"language":["eng"],"subject":["Fiction","Gothic fiction"]}]}`
const wizardSearchFixture = `{"docs":[{"key":"/works/OL1849133W","title":"The Wonderful Wizard of Oz","author_name":["L. Frank Baum"],"author_key":["OL111486A"],"first_publish_year":1900,"language":["eng"],"subject":["Fiction","Children's literature"]}]}`
const wizardAuthorFixture = `{"name":"Lyman Frank Baum","death_date":"1919-05-06"}`

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
	return evidenceresolver.Query{Provider: "project-gutenberg", ExternalID: "11", Title: "Alice's Adventures in Wonderland", Authors: []evidenceresolver.Person{{Name: "Carroll, Lewis"}}}
}

func wutheringQuery() evidenceresolver.Query {
	return evidenceresolver.Query{Provider: "project-gutenberg", ExternalID: "768", Title: "Wuthering Heights", Authors: []evidenceresolver.Person{{Name: "Brontë, Emily"}}}
}

func wizardQuery() evidenceresolver.Query {
	return evidenceresolver.Query{
		Provider:   "project-gutenberg",
		ExternalID: "55",
		Title:      "The Wonderful Wizard of Oz",
		Authors: []evidenceresolver.Person{{
			Name:         "Baum, L. Frank (Lyman Frank)",
			NameVariants: []string{"L. Frank Baum", "Lyman Frank Baum", "Baum, L. Frank"},
		}},
	}
}

func TestLookupUsesOnlyFixedStructuredOpenLibraryEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != productUserAgent {
			t.Errorf("user agent=%q", r.Header.Get("User-Agent"))
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/search.json":
			if r.URL.Query().Get("title") != "Alice's Adventures in Wonderland" || r.URL.Query().Get("author") != "Carroll, Lewis" || r.URL.Query().Get("limit") != "5" {
				t.Fatalf("query=%q", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, searchFixture)
		case "/authors/OL22098A.json":
			_, _ = io.WriteString(w, authorFixture)
		default:
			t.Fatalf("path=%q", r.URL.Path)
		}
	}))
	defer server.Close()
	adapter, transport := adapterForServer(server)
	records, err := adapter.Lookup(context.Background(), exactQuery())
	if err != nil || len(records) != 1 || records[0].Source != evidenceresolver.SourceOpenLibrary || records[0].SourceName != fullSourceName || records[0].WorkID != "/works/OL138052W" || records[0].FirstPublicationYear == nil || *records[0].FirstPublicationYear != 1865 || len(records[0].Authors) != 1 || records[0].Authors[0].Name != "Lewis Carroll" || records[0].Authors[0].DeathYear == nil || *records[0].Authors[0].DeathYear != 1898 || len(records[0].Languages) != 1 || records[0].Languages[0] != "eng" || len(records[0].Subjects) != 2 || len(records[0].Contributors) != 1 || records[0].Digest != digest(append([]byte(searchFixture), []byte(authorFixture)...)) {
		t.Fatalf("records=%#v err=%v", records, err)
	}
	requests := transport.requests()
	if len(requests) != 2 || requests[0].Scheme != "https" || requests[0].Host != host || requests[0].Path != "/search.json" || requests[1].String() != "https://openlibrary.org/authors/OL22098A.json" {
		t.Fatalf("requests=%+v", requests)
	}
}

func TestLookupUsesProviderDerivedVariantForWizardOfOz(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search.json":
			if author := r.URL.Query().Get("author"); author != "L. Frank Baum" {
				t.Fatalf("author query=%q", author)
			}
			_, _ = io.WriteString(w, wizardSearchFixture)
		case "/authors/OL111486A.json":
			_, _ = io.WriteString(w, wizardAuthorFixture)
		default:
			t.Fatalf("path=%q", r.URL.Path)
		}
	}))
	defer server.Close()
	adapter, transport := adapterForServer(server)
	records, err := adapter.Lookup(context.Background(), wizardQuery())
	if err != nil || len(records) != 1 || records[0].Authors[0].Name != "Lyman Frank Baum" || records[0].Authors[0].DeathYear == nil || *records[0].Authors[0].DeathYear != 1919 {
		t.Fatalf("records=%#v err=%v", records, err)
	}
	if requests := transport.requests(); len(requests) != 2 || requests[0].Path != "/search.json" || requests[1].Path != "/authors/OL111486A.json" {
		t.Fatalf("requests=%+v", requests)
	}
}

func TestLookupBoundsAndOrdersProviderVariantSearches(t *testing.T) {
	var authors []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search.json" {
			t.Fatalf("unexpected path=%q", r.URL.Path)
		}
		authors = append(authors, r.URL.Query().Get("author"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"docs":[]}`)
	}))
	defer server.Close()
	adapter, _ := adapterForServer(server)
	if records, err := adapter.Lookup(context.Background(), wizardQuery()); err != nil || len(records) != 0 {
		t.Fatalf("records=%#v err=%v", records, err)
	}
	want := []string{"L. Frank Baum", "Lyman Frank Baum"}
	if len(authors) != len(want) {
		t.Fatalf("author queries=%q want=%q", authors, want)
	}
	for i := range authors {
		if authors[i] != want[i] {
			t.Fatalf("author queries=%q want=%q", authors, want)
		}
	}
}

func TestLookupRetainsSearchEvidenceWhenAuthorEndpointUnavailable(t *testing.T) {
	for _, test := range []struct {
		name    string
		adapter func(*testing.T) (*Adapter, *fixedHostTransport)
	}{
		{
			name: "upstream 503",
			adapter: func(t *testing.T) (*Adapter, *fixedHostTransport) {
				t.Helper()
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/search.json":
						w.Header().Set("Content-Type", "application/json")
						_, _ = io.WriteString(w, searchFixture)
					case "/authors/OL22098A.json":
						w.WriteHeader(http.StatusServiceUnavailable)
					default:
						t.Fatalf("path=%q", r.URL.Path)
					}
				}))
				t.Cleanup(server.Close)
				return adapterForServer(server)
			},
		},
		{
			name: "transport failure",
			adapter: func(t *testing.T) (*Adapter, *fixedHostTransport) {
				t.Helper()
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/search.json" {
						t.Fatalf("path=%q", r.URL.Path)
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, searchFixture)
				}))
				t.Cleanup(server.Close)
				target, _ := url.Parse(server.URL)
				transport := &fixedHostTransport{target: target, base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					if request.URL.Path == "/authors/OL22098A.json" {
						return nil, errors.New("author endpoint transport failure")
					}
					return http.DefaultTransport.RoundTrip(request)
				})}
				return New(Config{HTTPClient: &http.Client{Transport: transport}}), transport
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			adapter, transport := test.adapter(t)
			records, err := adapter.Lookup(context.Background(), exactQuery())
			if err != nil || len(records) != 1 {
				t.Fatalf("records=%#v err=%v", records, err)
			}
			assertSearchOnlyRecord(t, records[0], searchFixture, "Lewis Carroll", "OL22098A", 1865)
			if requests := transport.requests(); len(requests) != 2 || requests[1].Path != "/authors/OL22098A.json" {
				t.Fatalf("requests=%+v", requests)
			}
		})
	}
}

func TestLookupRejectsInvalidSearchCandidatesBeforeAuthorEnrichment(t *testing.T) {
	for _, test := range []struct {
		name    string
		fixture string
	}{
		{
			name:    "exact title with wrong author",
			fixture: strings.Replace(searchFixture, "Lewis Carroll", "Other Author", 1),
		},
		{
			name:    "invalid work key",
			fixture: strings.Replace(searchFixture, "/works/OL138052W", "/books/OL138052W", 1),
		},
		{
			name:    "invalid author key",
			fixture: strings.Replace(searchFixture, "OL22098A", "not-an-open-library-author", 1),
		},
		{
			name: "ambiguous exact records",
			fixture: `{"docs":[
				{"key":"/works/OL138052W","title":"Alice's Adventures in Wonderland","author_name":["Lewis Carroll"],"author_key":["OL22098A"]},
				{"key":"/works/OL999999W","title":"Alice's Adventures in Wonderland","author_name":["Lewis Carroll"],"author_key":["OL22098A"]}
			]}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.URL.Path != "/search.json" {
					t.Fatalf("unexpected author enrichment request: %q", request.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, test.fixture)
			}))
			defer server.Close()
			adapter, transport := adapterForServer(server)
			records, err := adapter.Lookup(context.Background(), exactQuery())
			if err != nil || len(records) != 0 {
				t.Fatalf("records=%#v err=%v", records, err)
			}
			if requests := transport.requests(); len(requests) != 1 || requests[0].Path != "/search.json" {
				t.Fatalf("requests=%+v", requests)
			}
		})
	}
}

func TestLookupRetainsSearchOnlyRecordWithoutSubjects(t *testing.T) {
	fixture := strings.Replace(searchFixture, `,"subject":["Fiction","Children's literature"]`, "", 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/search.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, fixture)
		case "/authors/OL22098A.json":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			t.Fatalf("path=%q", request.URL.Path)
		}
	}))
	defer server.Close()
	adapter, transport := adapterForServer(server)
	records, err := adapter.Lookup(context.Background(), exactQuery())
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%#v err=%v", records, err)
	}
	assertSearchOnlyRecord(t, records[0], fixture, "Lewis Carroll", "OL22098A", 1865)
	if len(records[0].Subjects) != 0 {
		t.Fatalf("subjects=%#v", records[0].Subjects)
	}
	if requests := transport.requests(); len(requests) != 2 || requests[1].Path != "/authors/OL22098A.json" {
		t.Fatalf("requests=%+v", requests)
	}
}

func TestLookupRejectsInvalidOrConflictingAuthorEnrichment(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "malformed author payload", body: "{"},
		{name: "conflicting author identity", body: `{"name":"Other Author","death_date":"1900"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/search.json":
					_, _ = io.WriteString(w, searchFixture)
				case "/authors/OL22098A.json":
					_, _ = io.WriteString(w, test.body)
				default:
					t.Fatalf("path=%q", r.URL.Path)
				}
			}))
			defer server.Close()
			adapter, _ := adapterForServer(server)
			records, err := adapter.Lookup(context.Background(), exactQuery())
			if !errors.Is(err, ErrInvalid) || len(records) != 0 {
				t.Fatalf("records=%#v err=%v", records, err)
			}
		})
	}
}

func TestLookupAcceptsCanonicalEquivalentSearchNameWhenAuthorUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, wutheringSearchFixture)
		case "/authors/OL24529A.json":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			t.Fatalf("path=%q", r.URL.Path)
		}
	}))
	defer server.Close()
	adapter, _ := adapterForServer(server)
	records, err := adapter.Lookup(context.Background(), wutheringQuery())
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%#v err=%v", records, err)
	}
	assertSearchOnlyRecord(t, records[0], wutheringSearchFixture, "Emily Brontë", "OL24529A", 1846)
}

func assertSearchOnlyRecord(t *testing.T, record evidenceresolver.BibliographicRecord, searchBody, name, authorID string, publication int) {
	t.Helper()
	if record.Source != evidenceresolver.SourceOpenLibrary || record.SourceName != searchSourceName || record.Digest != digest([]byte(searchBody)) || record.WorkID != record.Identifier || record.FirstPublicationYear == nil || *record.FirstPublicationYear != publication || len(record.Authors) != 1 || record.Authors[0].Name != name || len(record.Authors[0].Identifiers) != 1 || record.Authors[0].Identifiers[0] != (evidenceresolver.Identifier{Source: evidenceresolver.SourceOpenLibrary, Value: authorID}) || record.Authors[0].DeathYear != nil || len(record.Contributors) != 1 || record.Contributors[0].Name != name || record.Contributors[0].Role != "author" || len(record.OriginalLanguages) != 0 || len(record.MaterialTypes) != 0 {
		t.Fatalf("record=%#v", record)
	}
}

func TestLookupUsesConfiguredContactIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("User-Agent"), productUserAgent+" (contact@example.test)"; got != want {
			t.Errorf("user agent=%q want=%q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search.json":
			_, _ = io.WriteString(w, searchFixture)
		case "/authors/OL22098A.json":
			_, _ = io.WriteString(w, authorFixture)
		}
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	transport := &fixedHostTransport{target: target, base: http.DefaultTransport}
	adapter := New(Config{HTTPClient: &http.Client{Transport: transport}, ContactEmailOrPhone: "contact@example.test"})
	if _, err := adapter.Lookup(context.Background(), exactQuery()); err != nil {
		t.Fatal(err)
	}
}

func TestParseYearSupportsBoundedOpenLibraryDateForms(t *testing.T) {
	for _, test := range []struct {
		value any
		want  int
		ok    bool
	}{
		{"31 July 1965", 1965, true},
		{"January 14, 1898", 1898, true},
		{"1898", 1898, true},
		{"1898-01-14", 1898, true},
		{float64(1865), 1865, true},
		{"1898/1899", 0, false},
		{"circa 1898", 0, false},
		{"circa 1898 or 1899", 0, false},
		{"999", 0, false},
		{"2200", 0, false},
		{float64(1865.5), 0, false},
	} {
		got, ok := parseYear(test.value)
		if ok != test.ok || (ok && *got != test.want) {
			t.Fatalf("parseYear(%#v) = %v, %v; want %d, %v", test.value, got, ok, test.want, test.ok)
		}
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

func TestLookupFailsClosedAtNetworkAndContentBoundaries(t *testing.T) {
	tests := []struct {
		name string
		h    http.Handler
		want error
	}{
		{"redirect", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://evil.example/", http.StatusFound)
		}), ErrUnavailable},
		{"wrong content type", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, "<html>")
		}), ErrInvalid},
		{"oversized", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Length", "999999")
		}), ErrInvalid},
		{"malformed", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
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

func TestLookupIgnoresNonExactOrUnboundResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, strings.Replace(searchFixture, "Alice's Adventures in Wonderland", "Different Work", 1))
	}))
	defer server.Close()
	adapter, _ := adapterForServer(server)
	if records, err := adapter.Lookup(context.Background(), exactQuery()); err != nil || len(records) != 0 {
		t.Fatalf("records=%#v err=%v", records, err)
	}
}

// This composes the real Open Library adapter output with the resolver. It
// deliberately does not invent a Library of Congress record or an exact-edition
// original-work language: that is not supplied by the current runtime adapter.
func TestRuntimeOpenLibraryOutputLeavesAliceLikeDossierBlocked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search.json":
			_, _ = io.WriteString(w, searchFixture)
		case "/authors/OL22098A.json":
			_, _ = io.WriteString(w, authorFixture)
		default:
			t.Fatalf("path=%q", r.URL.Path)
		}
	}))
	defer server.Close()
	adapter, _ := adapterForServer(server)
	resolver, err := evidenceresolver.New(evidenceresolver.Config{Sources: []evidenceresolver.BibliographicSource{adapter}})
	if err != nil {
		t.Fatal(err)
	}
	death := 1898
	exact := evidenceresolver.ExactSourceContext{
		ProviderEvidence: copyrighteligibility.ProviderEvidence{
			Provider:       "project-gutenberg",
			ExternalID:     "11",
			Title:          "Alice's Adventures in Wonderland",
			Languages:      []string{"eng"},
			EvidenceDigest: strings.Repeat("a", 64),
			Contributors:   []copyrighteligibility.ContributorEvidence{{Name: "Lewis Carroll", Role: "author", DeathYear: &death}},
		},
		SourceText: "Project Gutenberg metadata\n*** START OF THE PROJECT GUTENBERG EBOOK EXAMPLE ***\n",
	}
	resolution, err := resolver.Resolve(context.Background(), exact)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Authorship.Status != evidenceresolver.ResolutionEstablished || resolution.Author.Status != evidenceresolver.ResolutionEstablished || resolution.WorkCategory.Status != evidenceresolver.ResolutionInsufficient || resolution.FirstPublication.Status != evidenceresolver.ResolutionInsufficient || resolution.Translation.Status != evidenceresolver.ResolutionInsufficient || resolution.AdditionalTextual.Status != evidenceresolver.ResolutionEstablished || resolution.AdditionalTextual.State != copyrighteligibility.FactNoneConfirmed || resolution.UnpublishedAtEnd1988.Status != evidenceresolver.ResolutionInsufficient {
		t.Fatalf("resolution=%#v", resolution)
	}
	assessment := copyrighteligibility.Evaluate(copyrighteligibility.Input{EvaluationDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), UK: evidenceresolver.ToUKEvidence(resolution)})
	if assessment.UK.Status != copyrighteligibility.JurisdictionIndeterminate || assessment.UK.Reason != copyrighteligibility.ReasonUKWorkCategoryUnsupported {
		t.Fatalf("assessment=%#v", assessment)
	}
}
