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
	if err != nil || len(records) != 1 || records[0].Source != evidenceresolver.SourceOpenLibrary || records[0].WorkID != "/works/OL138052W" || records[0].EditionID != "" || records[0].FirstPublicationYear == nil || *records[0].FirstPublicationYear != 1865 || len(records[0].Authors) != 1 || records[0].Authors[0].DeathYear == nil || *records[0].Authors[0].DeathYear != 1898 || len(records[0].Languages) != 1 || records[0].Languages[0] != "eng" || len(records[0].Subjects) != 2 || len(records[0].Contributors) != 1 || records[0].ContributorRolesObserved || records[0].Digest == "" {
		t.Fatalf("records=%#v err=%v", records, err)
	}
	requests := transport.requests()
	if len(requests) != 2 || requests[0].Scheme != "https" || requests[0].Host != host || requests[0].Path != "/search.json" || requests[1].String() != "https://openlibrary.org/authors/OL22098A.json" {
		t.Fatalf("requests=%+v", requests)
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
// contributor list: those are not supplied by the current runtime adapter.
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
	if resolution.Authorship.Status != evidenceresolver.ResolutionEstablished || resolution.Author.Status != evidenceresolver.ResolutionEstablished || resolution.WorkCategory.Status != evidenceresolver.ResolutionInsufficient || resolution.FirstPublication.Status != evidenceresolver.ResolutionInsufficient || resolution.Translation.Status != evidenceresolver.ResolutionInsufficient || resolution.AdditionalTextual.Status != evidenceresolver.ResolutionInsufficient || resolution.SpecialCategory.Status != evidenceresolver.ResolutionInsufficient || resolution.UnpublishedAtEnd1988.Status != evidenceresolver.ResolutionInsufficient {
		t.Fatalf("resolution=%#v", resolution)
	}
	assessment := copyrighteligibility.Evaluate(copyrighteligibility.Input{EvaluationDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), UK: evidenceresolver.ToUKEvidence(resolution)})
	if assessment.UK.Status != copyrighteligibility.JurisdictionIndeterminate || assessment.UK.Reason != copyrighteligibility.ReasonUKWorkCategoryUnsupported {
		t.Fatalf("assessment=%#v", assessment)
	}
}
