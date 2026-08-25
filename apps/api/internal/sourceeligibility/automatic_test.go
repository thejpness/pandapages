package sourceeligibility

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"pandapages/api/internal/copyrighteligibility"
	"pandapages/api/internal/evidenceresolver"
	"pandapages/api/internal/evidenceresolver/bnf"
	"pandapages/api/internal/evidenceresolver/openlibrary"
	"pandapages/api/internal/sourceprovider"
	"pandapages/api/internal/sourceprovider/gutenberg"
)

const automaticBNFAlice = `{"results":{"bindings":[{"work":{"type":"uri","value":"http://data.bnf.fr/ark:/12148/cb12011248f#about"},"title":{"type":"literal","value":"Alice's Adventures in Wonderland"},"firstYear":{"type":"literal","value":"1865"},"creator":{"type":"uri","value":"http://data.bnf.fr/ark:/12148/cb118859183#about"},"creatorName":{"type":"literal","value":"Lewis Carroll"},"language":{"type":"uri","value":"http://id.loc.gov/vocabulary/iso639-2/eng"},"subject":{"type":"literal","value":"Littératures"}}]}}`

const automaticOpenLibraryAlice = `{"docs":[{"key":"/works/OL138052W","title":"Alice's Adventures in Wonderland","author_name":["Lewis Carroll"],"author_key":["OL22098A"],"first_publish_year":1865,"language":["eng"],"subject":["Fiction","Children's literature"]}]}`
const automaticOpenLibraryAuthor = `{"name":"Lewis Carroll","death_date":"1898"}`

const automaticBNFWizardOfOz = `{"results":{"bindings":[{"work":{"type":"uri","value":"https://data.bnf.fr/ark:/12148/cb119312746#about"},"title":{"type":"literal","value":"The Wonderful Wizard of Oz"},"firstYear":{"type":"literal","value":"1900"},"creator":{"type":"uri","value":"https://data.bnf.fr/ark:/12148/cb11890567s#about"},"creatorName":{"type":"literal","value":"Lyman Frank Baum"},"death":{"type":"literal","value":"1919-05-06"},"language":{"type":"uri","value":"http://id.loc.gov/vocabulary/iso639-2/eng"},"subject":{"type":"literal","value":"Littératures"}}]}}`
const automaticOpenLibraryWizardOfOz = `{"docs":[{"key":"/works/OL1849133W","title":"The Wonderful Wizard of Oz","author_name":["L. Frank Baum"],"author_key":["OL111486A"],"first_publish_year":1900,"language":["eng"],"subject":["Fiction","Children's literature"]}]}`
const automaticOpenLibraryWizardAuthor = `{"name":"Lyman Frank Baum","death_date":"1919-05-06"}`

type automaticTransport struct {
	mu       sync.Mutex
	requests []string
	wizard   bool
}

func (t *automaticTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.requests = append(t.requests, request.URL.String())
	t.mu.Unlock()
	var body string
	if t.wizard {
		switch request.URL.Host {
		case "data.bnf.fr":
			if request.URL.Path != "/sparql" {
				return nil, io.ErrUnexpectedEOF
			}
			body = automaticBNFWizardOfOz
		case "openlibrary.org":
			switch request.URL.Path {
			case "/search.json":
				if request.URL.Query().Get("author") != "L. Frank Baum" {
					return nil, io.ErrUnexpectedEOF
				}
				body = automaticOpenLibraryWizardOfOz
			case "/authors/OL111486A.json":
				body = automaticOpenLibraryWizardAuthor
			default:
				return nil, io.ErrUnexpectedEOF
			}
		default:
			return nil, io.ErrUnexpectedEOF
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	}
	switch request.URL.Host {
	case "data.bnf.fr":
		if request.URL.Path != "/sparql" {
			return nil, io.ErrUnexpectedEOF
		}
		body = automaticBNFAlice
	case "openlibrary.org":
		switch request.URL.Path {
		case "/search.json":
			body = automaticOpenLibraryAlice
		case "/authors/OL22098A.json":
			body = automaticOpenLibraryAuthor
		default:
			return nil, io.ErrUnexpectedEOF
		}
	default:
		return nil, io.ErrUnexpectedEOF
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json; charset=utf-8"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}, nil
}

func (t *automaticTransport) requestCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.requests)
}

func TestEvaluateAliceWithRuntimeBibliographicAdaptersUsesContributorRiskScreening(t *testing.T) {
	transport := &automaticTransport{}
	client := &http.Client{Transport: transport}
	resolver, err := evidenceresolver.New(evidenceresolver.Config{Sources: []evidenceresolver.BibliographicSource{
		bnf.New(bnf.Config{HTTPClient: client}),
		openlibrary.New(openlibrary.Config{HTTPClient: client}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	death := 1898
	service, err := New(Config{
		Gateway: gatewayStub{
			acquired: sourceprovider.AcquisitionEvidence{
				Candidate: sourceprovider.SourceCandidate{
					Provider:   sourceprovider.ProjectGutenberg,
					ExternalID: "11",
					Title:      "Alice's Adventures in Wonderland",
					SourceText: "CHAPTER I\nDown the rabbit-hole.\n",
				},
				OPDSRights:        copyrighteligibility.ProviderRightsPublicDomain,
				HeaderRights:      copyrighteligibility.SourceHeaderRightsPublicDomain,
				SourceFrontMatter: "Project Gutenberg metadata\n*** START OF THE PROJECT GUTENBERG EBOOK ALICE ***\n",
			},
			evidence: copyrighteligibility.ProviderEvidence{
				Provider:       string(sourceprovider.ProjectGutenberg),
				ExternalID:     "11",
				Title:          "Alice's Adventures in Wonderland",
				Rights:         copyrighteligibility.ProviderRightsPublicDomain,
				Languages:      []string{"en"},
				EvidenceDigest: strings.Repeat("a", 64),
				Contributors:   []copyrighteligibility.ContributorEvidence{{Name: "Lewis Carroll", Role: "author", DeathYear: &death}},
			},
		},
		Resolver: resolver,
		Now:      func() time.Time { return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := service.Evaluate(context.Background(), sourceprovider.ProjectGutenberg, "11", HumanUKEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	if transport.requestCount() != 3 {
		t.Fatalf("request count=%d want=3", transport.requestCount())
	}
	if evaluation.EffectiveUKEvidence.WorkCategory != copyrighteligibility.WorkCategoryOrdinaryLiterary || evaluation.EffectiveUKEvidence.FirstPublication.Year != 1865 || evaluation.EffectiveUKEvidence.Translation.State != copyrighteligibility.FactNoneConfirmed || evaluation.EffectiveUKEvidence.AdditionalTextualContribution.State != copyrighteligibility.FactNoneConfirmed || evaluation.EffectiveUKEvidence.UnpublishedAtEnd1988.State != copyrighteligibility.FactNoneConfirmed {
		t.Fatalf("automatic UK evidence=%#v", evaluation.EffectiveUKEvidence)
	}
	if evaluation.Assessment.PolicyVersion != copyrighteligibility.PolicyVersion || evaluation.Assessment.US.Status != copyrighteligibility.JurisdictionEligible || evaluation.Assessment.UK.Status != copyrighteligibility.JurisdictionEligible || evaluation.Assessment.UK.Reason != copyrighteligibility.ReasonUKOrdinaryLiteraryTermExpired || evaluation.Assessment.Overall != copyrighteligibility.OverallEligible {
		t.Fatalf("assessment=%#v", evaluation.Assessment)
	}
	if _, err := service.Evaluate(context.Background(), sourceprovider.ProjectGutenberg, "11", HumanUKEvidence{FirstPublication: copyrighteligibility.PublicationEvidence{Year: 1866}}); !errors.Is(err, ErrHumanEvidenceConflict) {
		t.Fatalf("conflicting human publication error=%v", err)
	}
	if _, err := service.Evaluate(context.Background(), sourceprovider.ProjectGutenberg, "11", HumanUKEvidence{Translation: copyrighteligibility.FactEvidence{State: copyrighteligibility.FactPresent}}); !errors.Is(err, ErrHumanEvidenceConflict) {
		t.Fatalf("conflicting human translation error=%v", err)
	}
}

func TestEvaluateWizardOfOzWithRuntimeBibliographicAdaptersUsesProviderAuthorVariants(t *testing.T) {
	transport := &automaticTransport{wizard: true}
	client := &http.Client{Transport: transport}
	resolver, err := evidenceresolver.New(evidenceresolver.Config{Sources: []evidenceresolver.BibliographicSource{
		bnf.New(bnf.Config{HTTPClient: client}),
		openlibrary.New(openlibrary.Config{HTTPClient: client}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	death := 1919
	service, err := New(Config{
		Gateway: gatewayStub{
			acquired: sourceprovider.AcquisitionEvidence{
				Candidate: sourceprovider.SourceCandidate{
					Provider:   sourceprovider.ProjectGutenberg,
					ExternalID: "55",
					Title:      "The Wonderful Wizard of Oz",
					SourceText: "CHAPTER I\nThe cyclone.\n",
				},
				OPDSRights:        copyrighteligibility.ProviderRightsPublicDomain,
				HeaderRights:      copyrighteligibility.SourceHeaderRightsNoClassification,
				SourceFrontMatter: "Project Gutenberg metadata\n*** START OF THE PROJECT GUTENBERG EBOOK THE WONDERFUL WIZARD OF OZ ***\n",
			},
			evidence: copyrighteligibility.ProviderEvidence{
				Provider:       string(sourceprovider.ProjectGutenberg),
				ExternalID:     "55",
				Title:          "The Wonderful Wizard of Oz",
				Rights:         copyrighteligibility.ProviderRightsPublicDomain,
				Languages:      []string{"en"},
				EvidenceDigest: strings.Repeat("b", 64),
				Contributors: []copyrighteligibility.ContributorEvidence{{
					Name:         "Baum, L. Frank (Lyman Frank)",
					NameVariants: []string{"L. Frank Baum", "Lyman Frank Baum", "Baum, L. Frank"},
					Role:         "author",
					DeathYear:    &death,
				}},
			},
		},
		Resolver: resolver,
		Now:      func() time.Time { return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := service.Evaluate(context.Background(), sourceprovider.ProjectGutenberg, "55", HumanUKEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	if transport.requestCount() != 3 {
		t.Fatalf("request count=%d want=3", transport.requestCount())
	}
	if evaluation.Resolution.WorkCategory.Status != evidenceresolver.ResolutionEstablished || evaluation.Resolution.Authorship.Status != evidenceresolver.ResolutionEstablished || evaluation.Resolution.FirstPublication.Status != evidenceresolver.ResolutionEstablished || evaluation.Assessment.UK.Status != copyrighteligibility.JurisdictionEligible || evaluation.Assessment.UK.Reason != copyrighteligibility.ReasonUKOrdinaryLiteraryTermExpired || evaluation.Assessment.Overall != copyrighteligibility.OverallEligible {
		t.Fatalf("resolution=%#v assessment=%#v", evaluation.Resolution, evaluation.Assessment)
	}
}

func TestLiveAliceEmptyHumanEvidenceUsesContributorRiskScreening(t *testing.T) {
	if os.Getenv("PP_LIVE_EVIDENCE_SMOKE") != "1" {
		t.Skip("set PP_LIVE_EVIDENCE_SMOKE=1 to run the bounded live Project Gutenberg Alice evidence smoke test")
	}
	registry, err := sourceprovider.NewRegistry(gutenberg.New(gutenberg.Config{}))
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := evidenceresolver.New(evidenceresolver.Config{Sources: []evidenceresolver.BibliographicSource{
		bnf.New(bnf.Config{}),
		openlibrary.New(openlibrary.Config{}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(Config{Gateway: registry, Resolver: resolver})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	evaluation, err := service.Evaluate(ctx, sourceprovider.ProjectGutenberg, "11", HumanUKEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Assessment.PolicyVersion != copyrighteligibility.PolicyVersion || evaluation.Assessment.US.Status != copyrighteligibility.JurisdictionEligible || evaluation.Assessment.UK.Status != copyrighteligibility.JurisdictionEligible || evaluation.Assessment.UK.Reason != copyrighteligibility.ReasonUKOrdinaryLiteraryTermExpired || evaluation.Assessment.Overall != copyrighteligibility.OverallEligible || evaluation.EffectiveUKEvidence.Translation.State != copyrighteligibility.FactNoneConfirmed || evaluation.EffectiveUKEvidence.AdditionalTextualContribution.State != copyrighteligibility.FactNoneConfirmed {
		t.Fatalf("assessment=%#v evidence=%#v", evaluation.Assessment, evaluation.EffectiveUKEvidence)
	}
}
