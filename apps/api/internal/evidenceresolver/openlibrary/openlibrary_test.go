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

	"pandapages/api/internal/evidenceresolver"
)

const searchFixture = `{"docs":[{"key":"/works/OL138052W","title":"Alice's Adventures in Wonderland","author_name":["Lewis Carroll"],"author_key":["OL22098A"],"first_publish_year":1865,"subject":["Fiction","Children's literature"]}]}`
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
		if r.Header.Get("User-Agent") != userAgent {
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
	if err != nil || len(records) != 1 || records[0].Source != evidenceresolver.SourceOpenLibrary || records[0].FirstPublicationYear == nil || *records[0].FirstPublicationYear != 1865 || len(records[0].Authors) != 1 || records[0].Authors[0].DeathYear == nil || *records[0].Authors[0].DeathYear != 1898 || records[0].Digest == "" {
		t.Fatalf("records=%#v err=%v", records, err)
	}
	requests := transport.requests()
	if len(requests) != 2 || requests[0].Scheme != "https" || requests[0].Host != host || requests[0].Path != "/search.json" || requests[1].String() != "https://openlibrary.org/authors/OL22098A.json" {
		t.Fatalf("requests=%+v", requests)
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
