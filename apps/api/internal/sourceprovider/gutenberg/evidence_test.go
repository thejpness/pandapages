package gutenberg

import (
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

const rdfAliceFixture = `<?xml version="1.0"?>
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:pgterms="http://www.gutenberg.org/2009/pgterms/" xmlns:dcterms="http://purl.org/dc/terms/">
  <pgterms:ebook rdf:about="ebooks/11">
    <dcterms:title>Alice Adventures</dcterms:title>
    <dcterms:rights>Public domain in the USA.</dcterms:rights>
    <dcterms:creator><pgterms:agent><pgterms:name>Carroll, Lewis</pgterms:name><pgterms:birthdate>1832</pgterms:birthdate><pgterms:deathdate>1898</pgterms:deathdate></pgterms:agent></dcterms:creator>
    <dcterms:language><rdf:Description><rdf:value>en</rdf:value></rdf:Description></dcterms:language>
  </pgterms:ebook>
</rdf:RDF>`

const rdfOdysseyFixture = `<?xml version="1.0"?>
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:pgterms="http://www.gutenberg.org/2009/pgterms/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:marcrel="http://id.loc.gov/vocabulary/relators/">
  <pgterms:ebook rdf:about="ebooks/1727">
    <dcterms:title>The Odyssey</dcterms:title>
    <dcterms:rights>Public domain in the USA.</dcterms:rights>
    <dcterms:creator><pgterms:agent><pgterms:name>Homer</pgterms:name><pgterms:birthdate>-750</pgterms:birthdate><pgterms:deathdate>-650</pgterms:deathdate></pgterms:agent></dcterms:creator>
    <marcrel:trl><pgterms:agent><pgterms:name>Butler, Samuel</pgterms:name><pgterms:birthdate>1835</pgterms:birthdate><pgterms:deathdate>1902</pgterms:deathdate></pgterms:agent></marcrel:trl>
  </pgterms:ebook>
</rdf:RDF>`

const rdfRestrictedFixture = `<?xml version="1.0"?>
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:pgterms="http://www.gutenberg.org/2009/pgterms/" xmlns:dcterms="http://purl.org/dc/terms/">
  <pgterms:ebook rdf:about="ebooks/31632">
    <dcterms:title>Project Gutenberg 1971-2009</dcterms:title>
    <dcterms:rights>Copyrighted. Read the copyright notice inside this book for details.</dcterms:rights>
    <dcterms:creator><pgterms:agent><pgterms:name>Lebert, Marie</pgterms:name></pgterms:agent></dcterms:creator>
  </pgterms:ebook>
</rdf:RDF>`

func TestParseRDFEvidenceExtractsOnlyNeededProviderFacts(t *testing.T) {
	evidence, err := parseRDFEvidence([]byte(rdfAliceFixture), "11")
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Provider != string(sourceprovider.ProjectGutenberg) || evidence.ExternalID != "11" || evidence.Title != "Alice Adventures" || evidence.Rights != copyrighteligibility.ProviderRightsPublicDomain || evidence.RightsStatement != "Public domain in the USA." || evidence.EvidenceDigest != sha256Hex([]byte(rdfAliceFixture)) {
		t.Fatalf("evidence=%+v", evidence)
	}
	if len(evidence.Languages) != 1 || evidence.Languages[0] != "en" || len(evidence.Contributors) != 1 {
		t.Fatalf("evidence=%+v", evidence)
	}
	contributor := evidence.Contributors[0]
	if contributor.Name != "Carroll, Lewis" || contributor.Role != "author" || dereference(contributor.BirthYear) != 1832 || dereference(contributor.DeathYear) != 1898 {
		t.Fatalf("contributor=%+v", contributor)
	}
}

func TestParseRDFEvidencePreservesTranslatorRoleAndMissingOptionalDates(t *testing.T) {
	odyssey, err := parseRDFEvidence([]byte(rdfOdysseyFixture), "1727")
	if err != nil {
		t.Fatal(err)
	}
	if len(odyssey.Contributors) != 2 || odyssey.Contributors[0].Role != "author" || odyssey.Contributors[1].Role != "translator" || odyssey.Contributors[1].Name != "Butler, Samuel" || dereference(odyssey.Contributors[1].DeathYear) != 1902 {
		t.Fatalf("contributors=%+v", odyssey.Contributors)
	}
	restricted, err := parseRDFEvidence([]byte(rdfRestrictedFixture), "31632")
	if err != nil {
		t.Fatal(err)
	}
	if restricted.Rights != copyrighteligibility.ProviderRightsRestricted || restricted.Contributors[0].DeathYear != nil {
		t.Fatalf("restricted=%+v", restricted)
	}
}

func TestParseRDFEvidenceFailsClosedForMalformedOrMismatchedIdentity(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		want error
	}{
		{"malformed", "<rdf:RDF><ebook>", sourceprovider.ErrEvidenceInvalid},
		{"missing ebook", "<rdf:RDF></rdf:RDF>", sourceprovider.ErrEvidenceInvalid},
		{"wrong id", strings.Replace(rdfAliceFixture, "ebooks/11", "ebooks/12", 1), sourceprovider.ErrEvidenceIdentityMismatch},
		{"invalid id", strings.Replace(rdfAliceFixture, "ebooks/11", "ebooks/not-a-number", 1), sourceprovider.ErrEvidenceIdentityMismatch},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseRDFEvidence([]byte(test.body), "11"); !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
}

func TestEvidenceDigestIsStable(t *testing.T) {
	first, err := parseRDFEvidence([]byte(rdfAliceFixture), "11")
	if err != nil {
		t.Fatal(err)
	}
	second, err := parseRDFEvidence([]byte(rdfAliceFixture), "11")
	if err != nil || first.EvidenceDigest != second.EvidenceDigest {
		t.Fatalf("digests=%q/%q error=%v", first.EvidenceDigest, second.EvidenceDigest, err)
	}
}

func TestRDFEvidenceUsesFixedTrustedEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cache/epub/11/pg11.rdf" {
			t.Errorf("path=%q", r.URL.Path)
		}
		if r.Header.Get("User-Agent") != userAgent || !strings.Contains(r.Header.Get("Accept"), "application/rdf+xml") {
			t.Errorf("headers=%+v", r.Header)
		}
		w.Header().Set("Content-Type", "application/rdf+xml; charset=utf-8")
		_, _ = io.WriteString(w, rdfAliceFixture)
	}))
	defer server.Close()
	adapter, transport := adapterForServer(server)
	if _, err := adapter.RDFEvidence(context.Background(), "11"); err != nil {
		t.Fatal(err)
	}
	requests := transport.requests()
	if len(requests) != 1 || requests[0].Scheme != "https" || requests[0].Host != endpointHost || requests[0].Path != "/cache/epub/11/pg11.rdf" {
		t.Fatalf("requests=%+v", requests)
	}
}

func TestRDFEvidenceRejectsInvalidIDBeforeNetwork(t *testing.T) {
	adapter := New(Config{HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("request should not be sent")
	})}})
	for _, id := range []string{"", "11/evil", "http://127.0.0.1", "file:///etc/passwd"} {
		if _, err := adapter.RDFEvidence(context.Background(), id); !errors.Is(err, sourceprovider.ErrWorkIDInvalid) {
			t.Fatalf("id=%q error=%v", id, err)
		}
	}
}

func TestRDFEvidenceFailsClosedAtNetworkBoundary(t *testing.T) {
	tests := []struct {
		name string
		h    http.Handler
		want error
	}{
		{"wrong content type", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, "<html>")
		}), sourceprovider.ErrEvidenceInvalid},
		{"upstream status", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }), sourceprovider.ErrEvidenceUnavailable},
		{"declared oversized", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/rdf+xml")
			w.Header().Set("Content-Length", strconv.Itoa(maxRDFBytes+1))
		}), sourceprovider.ErrEvidenceTooLarge},
		{"oversized", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/rdf+xml")
			_, _ = io.WriteString(w, strings.Repeat("x", maxRDFBytes+1))
		}), sourceprovider.ErrEvidenceTooLarge},
		{"redirect refused", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://evil.example/evidence.rdf", http.StatusFound)
		}), sourceprovider.ErrEvidenceUnavailable},
		{"malformed rdf", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/rdf+xml")
			_, _ = io.WriteString(w, "<rdf:RDF>")
		}), sourceprovider.ErrEvidenceInvalid},
		{"identity mismatch", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/rdf+xml")
			_, _ = io.WriteString(w, strings.Replace(rdfAliceFixture, "ebooks/11", "ebooks/12", 1))
		}), sourceprovider.ErrEvidenceIdentityMismatch},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(test.h)
			defer server.Close()
			adapter, _ := adapterForServer(server)
			if _, err := adapter.RDFEvidence(context.Background(), "11"); !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
}

func TestRDFEvidenceHonoursTimeoutAndCancellation(t *testing.T) {
	adapter := New(Config{HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}})
	deadline, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := adapter.RDFEvidence(deadline, "11"); !errors.Is(err, sourceprovider.ErrEvidenceTimeout) {
		t.Fatalf("timeout error=%v", err)
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := adapter.RDFEvidence(cancelled, "11"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error=%v", err)
	}
}

func TestClassifySourceHeaderRightsIsBoundedAndExact(t *testing.T) {
	start := "*** START OF THE PROJECT GUTENBERG EBOOK EXAMPLE ***"
	tests := []struct {
		name string
		text string
		want copyrighteligibility.SourceHeaderRightsClassification
	}{
		{"public assertion", "Public domain in the USA.\n" + start + "\nBody", copyrighteligibility.SourceHeaderRightsPublicDomain},
		{"restricted assertion", "Copyrighted. Read the copyright notice inside this book for details.\n" + start + "\nBody", copyrighteligibility.SourceHeaderRightsRestricted},
		{"contradictory assertions", "Public domain in the USA.\nCopyrighted. Read the copyright notice inside this book for details.\n" + start, copyrighteligibility.SourceHeaderRightsConflicting},
		{"body words ignored", start + "\nCopyrighted. Read the copyright notice inside this book for details.\nBody", copyrighteligibility.SourceHeaderRightsNoClassification},
		{"near match ignored", "Copyrighted maybe\n" + start, copyrighteligibility.SourceHeaderRightsNoClassification},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifySourceHeaderRights([]byte(test.text)); got != test.want {
				t.Fatalf("classification=%q want=%q", got, test.want)
			}
		})
	}
	lateNotice := strings.Repeat("x", sourceHeaderScanBytes) + "\nPublic domain in the USA.\n" + start
	if got := classifySourceHeaderRights([]byte(lateNotice)); got != copyrighteligibility.SourceHeaderRightsNoClassification {
		t.Fatalf("late classification=%q", got)
	}
}

func dereference(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func TestCanonicalContributorsAndLanguagesAreOrderIndependent(t *testing.T) {
	birth, death := 1832, 1898
	values := []copyrighteligibility.ContributorEvidence{
		{Name: "Butler, Samuel", Role: "translator"},
		{Name: "Carroll, Lewis", Role: "author", BirthYear: &birth, DeathYear: &death},
	}
	contributors := canonicalContributors(values)
	if contributors[0].Role != "author" || contributors[1].Role != "translator" {
		t.Fatalf("contributors=%+v", contributors)
	}
	languages := canonicalLanguages([]string{"fr", "en", "fr"})
	if len(languages) != 2 || languages[0] != "en" || languages[1] != "fr" {
		t.Fatalf("languages=%+v", languages)
	}
}
