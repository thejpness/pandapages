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

	"pandapages/api/internal/copyrighteligibility"
	"pandapages/api/internal/sourceprovider"
)

const candidateTextFixture = "Public domain in the USA.\nProvider header\n*** START OF THE PROJECT GUTENBERG EBOOK ALICE ***\n\nCHAPTER I\n\nDown the rabbit-hole.\n\n*** END OF THE PROJECT GUTENBERG EBOOK ALICE ***\nProvider footer\n"

func TestAcquireFetchesOneServerSelectedPlainTextCandidate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != userAgent {
			t.Errorf("user agent=%q", r.Header.Get("User-Agent"))
		}
		switch r.URL.Path {
		case "/ebooks/11.opds":
			w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
			_, _ = io.WriteString(w, workFixture)
		case "/cache/epub/11/pg11.txt":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = io.WriteString(w, candidateTextFixture)
		default:
			t.Errorf("unexpected path=%q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	adapter, transport := adapterForServer(server)
	acquired, err := adapter.AcquireEvidence(context.Background(), "11")
	if err != nil {
		t.Fatal(err)
	}
	candidate := acquired.Candidate
	if acquired.HeaderRights != copyrighteligibility.SourceHeaderRightsPublicDomain {
		t.Fatalf("header rights=%q", acquired.HeaderRights)
	}
	if acquired.SourceFrontMatter != candidateTextFixture {
		t.Fatalf("front matter=%q", acquired.SourceFrontMatter)
	}
	if candidate.Provider != sourceprovider.ProjectGutenberg || candidate.ExternalID != "11" || candidate.SelectedRepresentation != (sourceprovider.Representation{Label: "Plain Text UTF-8", MediaType: "text/plain; charset=utf-8", URL: "https://www.gutenberg.org/cache/epub/11/pg11.txt"}) || candidate.NormalisationVersion != normalisationVersion {
		t.Fatalf("candidate=%+v", candidate)
	}
	if candidate.SourceText != "CHAPTER I\n\nDown the rabbit-hole.\n" || candidate.RetrievedContentHash != sha256Hex([]byte(candidateTextFixture)) || candidate.NormalisedContentHash != sha256HexString(candidate.SourceText) {
		t.Fatalf("candidate text/hashes=%+v", candidate)
	}
	requests := transport.requests()
	if len(requests) != 2 || requests[0].String() != "https://www.gutenberg.org/ebooks/11.opds" || requests[1].String() != "https://www.gutenberg.org/cache/epub/11/pg11.txt" {
		t.Fatalf("requests=%+v", requests)
	}
}

func TestConstructedPlainTextRepresentationIsExactAndValidated(t *testing.T) {
	representation, err := plainTextRepresentation("11")
	if err != nil || representation != (sourceprovider.Representation{Label: "Plain Text UTF-8", MediaType: "text/plain; charset=utf-8", URL: "https://www.gutenberg.org/cache/epub/11/pg11.txt"}) {
		t.Fatalf("representation/error=%+v/%v", representation, err)
	}
	for _, externalID := range []string{"", "0", "11/evil", " 11", "9999999999999"} {
		if _, err := plainTextRepresentation(externalID); !errors.Is(err, sourceprovider.ErrWorkIDInvalid) {
			t.Fatalf("external ID %q error=%v", externalID, err)
		}
	}
}

func TestAcquireIgnoresUntrustedOPDSRepresentationsAndFetchesConstructedText(t *testing.T) {
	for _, maliciousURL := range []string{
		"http://127.0.0.1/secret",
		"https://127.0.0.1/secret",
		"file:///etc/passwd",
		"https://evil.example/book.txt",
		"https://www.gutenberg.org.evil.example/book.txt",
		"https://user@www.gutenberg.org/book.txt",
	} {
		t.Run(maliciousURL, func(t *testing.T) {
			fixture := strings.Replace(workFixture, "</entry>", `<link rel="http://opds-spec.org/acquisition" type="text/plain" href="`+maliciousURL+`"/></entry>`, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/ebooks/11.opds":
					w.Header().Set("Content-Type", "application/atom+xml")
					_, _ = io.WriteString(w, fixture)
				case "/cache/epub/11/pg11.txt":
					w.Header().Set("Content-Type", "text/plain; charset=utf-8")
					_, _ = io.WriteString(w, candidateTextFixture)
				default:
					t.Fatalf("unexpected request=%q", r.URL.Path)
				}
			}))
			defer server.Close()
			adapter, transport := adapterForServer(server)
			if _, err := adapter.Acquire(context.Background(), "11"); err != nil {
				t.Fatalf("error=%v", err)
			}
			requests := transport.requests()
			if len(requests) != 2 || requests[0].Path != "/ebooks/11.opds" || requests[1].String() != "https://www.gutenberg.org/cache/epub/11/pg11.txt" {
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
			if _, err := adapter.fetchPlainText(context.Background(), "https://www.gutenberg.org/cache/epub/11/pg11.txt"); !errors.Is(err, test.want) {
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
	if _, err := adapter.fetchPlainText(context.Background(), "https://www.gutenberg.org/cache/epub/11/pg11.txt"); !errors.Is(err, sourceprovider.ErrContentTooLarge) {
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
	if _, err := adapter.fetchPlainText(deadline, "https://www.gutenberg.org/cache/epub/11/pg11.txt"); !errors.Is(err, sourceprovider.ErrTimeout) {
		t.Fatalf("timeout error=%v", err)
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := adapter.fetchPlainText(cancelled, "https://www.gutenberg.org/cache/epub/11/pg11.txt"); !errors.Is(err, context.Canceled) {
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
	if _, err := adapter.fetchPlainText(context.Background(), "https://www.gutenberg.org/cache/epub/11/pg11.txt"); !errors.Is(err, sourceprovider.ErrUnavailable) {
		t.Fatalf("error=%v", err)
	}
}
