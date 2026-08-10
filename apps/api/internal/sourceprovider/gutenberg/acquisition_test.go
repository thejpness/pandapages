package gutenberg

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"pandapages/api/internal/sourceprovider"
)

const candidateTextFixture = "Provider header\n*** START OF THE PROJECT GUTENBERG EBOOK ALICE ***\n\nCHAPTER I\n\nDown the rabbit-hole.\n\n*** END OF THE PROJECT GUTENBERG EBOOK ALICE ***\nProvider footer\n"

func TestAcquireFetchesOneServerSelectedPlainTextCandidate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != userAgent {
			t.Errorf("user agent=%q", r.Header.Get("User-Agent"))
		}
		switch r.URL.Path {
		case "/ebooks/11.opds":
			w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
			_, _ = io.WriteString(w, workFixture)
		case "/files/11/11-0.txt":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = io.WriteString(w, candidateTextFixture)
		default:
			t.Errorf("unexpected path=%q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	adapter, transport := adapterForServer(server)
	candidate, err := adapter.Acquire(context.Background(), "11")
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Provider != sourceprovider.ProjectGutenberg || candidate.ExternalID != "11" || candidate.SelectedRepresentation.URL != "https://www.gutenberg.org/files/11/11-0.txt" || candidate.NormalisationVersion != normalisationVersion {
		t.Fatalf("candidate=%+v", candidate)
	}
	if candidate.SourceText != "CHAPTER I\n\nDown the rabbit-hole.\n" || candidate.RetrievedContentHash != sha256Hex([]byte(candidateTextFixture)) || candidate.NormalisedContentHash != sha256HexString(candidate.SourceText) {
		t.Fatalf("candidate text/hashes=%+v", candidate)
	}
	requests := transport.requests()
	if len(requests) != 2 || requests[0].String() != "https://www.gutenberg.org/ebooks/11.opds" || requests[1].String() != "https://www.gutenberg.org/files/11/11-0.txt" {
		t.Fatalf("requests=%+v", requests)
	}
}

func TestPlainTextSelectionIsDeterministicAndTrusted(t *testing.T) {
	utf8 := sourceprovider.Representation{Label: "UTF-8", MediaType: "text/plain; charset=utf-8", URL: "https://www.gutenberg.org/files/11/11-0.txt"}
	plain := sourceprovider.Representation{Label: "Plain", MediaType: "text/plain", URL: "https://www.gutenberg.org/files/11/11.txt"}
	epub := sourceprovider.Representation{Label: "EPUB", MediaType: "application/epub+zip", URL: "https://www.gutenberg.org/ebooks/11.epub.images"}
	for _, representations := range [][]sourceprovider.Representation{{epub, plain, utf8}, {utf8, epub, plain}} {
		selected, err := selectPlainTextRepresentation(representations)
		if err != nil || selected != utf8 {
			t.Fatalf("selected/error=%+v/%v", selected, err)
		}
	}
	first := sourceprovider.Representation{Label: "Plain", MediaType: "text/plain", URL: "https://www.gutenberg.org/files/11/a.txt"}
	second := sourceprovider.Representation{Label: "Plain", MediaType: "text/plain", URL: "https://www.gutenberg.org/files/11/b.txt"}
	selected, err := selectPlainTextRepresentation([]sourceprovider.Representation{second, first})
	if err != nil || selected != first {
		t.Fatalf("selected/error=%+v/%v", selected, err)
	}
	html := sourceprovider.Representation{Label: "HTML", MediaType: "text/html", URL: "https://www.gutenberg.org/files/11/11-h.htm"}
	zip := sourceprovider.Representation{Label: "ZIP", MediaType: "application/zip", URL: "https://www.gutenberg.org/files/11/11.zip"}
	if _, err := selectPlainTextRepresentation([]sourceprovider.Representation{epub, html, zip}); !errors.Is(err, sourceprovider.ErrRepresentationUnavailable) {
		t.Fatalf("error=%v", err)
	}
}

func TestAcquireRejectsUntrustedProviderRepresentationURLsWithoutFetchingThem(t *testing.T) {
	for _, maliciousURL := range []string{
		"http://127.0.0.1/secret",
		"https://127.0.0.1/secret",
		"file:///etc/passwd",
		"https://evil.example/book.txt",
		"https://www.gutenberg.org.evil.example/book.txt",
		"https://user@www.gutenberg.org/book.txt",
	} {
		t.Run(maliciousURL, func(t *testing.T) {
			fixture := strings.Replace(workFixture, "https://www.gutenberg.org/files/11/11-0.txt", maliciousURL, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/ebooks/11.opds" {
					t.Fatalf("untrusted representation was fetched: %q", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/atom+xml")
				_, _ = io.WriteString(w, fixture)
			}))
			defer server.Close()
			adapter, transport := adapterForServer(server)
			if _, err := adapter.Acquire(context.Background(), "11"); !errors.Is(err, sourceprovider.ErrRepresentationUnavailable) {
				t.Fatalf("error=%v", err)
			}
			if requests := transport.requests(); len(requests) != 1 || requests[0].Path != "/ebooks/11.opds" {
				t.Fatalf("requests=%+v", requests)
			}
		})
	}
}

func TestFetchPlainTextFailsClosedAtContentBoundary(t *testing.T) {
	tests := []struct {
		name string
		h    http.Handler
		want error
	}{
		{"wrong content type", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, "<html>")
		}), sourceprovider.ErrContentInvalid},
		{"unsupported charset", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=iso-8859-1")
			_, _ = io.WriteString(w, "text")
		}), sourceprovider.ErrContentInvalid},
		{"invalid utf8 normalisation", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte{0xff})
		}), sourceprovider.ErrContentInvalid},
		{"declared too large", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Length", strconv.Itoa(maxSourceBytes+1))
		}), sourceprovider.ErrContentTooLarge},
		{"stream too large", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, strings.Repeat("x", maxSourceBytes+1))
		}), sourceprovider.ErrContentTooLarge},
		{"upstream status", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }), sourceprovider.ErrUnavailable},
		{"redirect refused", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://evil.example/book.txt", http.StatusFound)
		}), sourceprovider.ErrUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(test.h)
			defer server.Close()
			adapter, _ := adapterForServer(server)
			if _, err := adapter.fetchPlainText(context.Background(), "https://www.gutenberg.org/files/11/11-0.txt"); !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
}

func TestFetchPlainTextLimitAppliesAfterGzipTransferDecoding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Encoding", "gzip")
		gzipWriter := gzip.NewWriter(w)
		_, _ = io.WriteString(gzipWriter, strings.Repeat("x", maxSourceBytes+1))
		_ = gzipWriter.Close()
	}))
	defer server.Close()
	adapter, _ := adapterForServer(server)
	if _, err := adapter.fetchPlainText(context.Background(), "https://www.gutenberg.org/files/11/11-0.txt"); !errors.Is(err, sourceprovider.ErrContentTooLarge) {
		t.Fatalf("error=%v", err)
	}
}

func TestFetchPlainTextHonoursTimeoutAndCancellation(t *testing.T) {
	adapter := New(Config{HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}})
	deadline, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := adapter.fetchPlainText(deadline, "https://www.gutenberg.org/files/11/11-0.txt"); !errors.Is(err, sourceprovider.ErrTimeout) {
		t.Fatalf("timeout error=%v", err)
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := adapter.fetchPlainText(cancelled, "https://www.gutenberg.org/files/11/11-0.txt"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error=%v", err)
	}
}

func TestAcquireRejectsInvalidWorkIDBeforeNetwork(t *testing.T) {
	adapter := New(Config{HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("request should not be sent")
	})}})
	if _, err := adapter.Acquire(context.Background(), "11/evil"); !errors.Is(err, sourceprovider.ErrWorkIDInvalid) {
		t.Fatalf("error=%v", err)
	}
}

func TestFetchPlainTextMapsNetworkFailureSafely(t *testing.T) {
	adapter := New(Config{HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial tcp internal-network.example: private diagnostic")
	})}})
	if _, err := adapter.fetchPlainText(context.Background(), "https://www.gutenberg.org/files/11/11-0.txt"); !errors.Is(err, sourceprovider.ErrUnavailable) {
		t.Fatalf("error=%v", err)
	}
}
