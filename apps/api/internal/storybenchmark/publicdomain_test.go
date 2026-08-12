package storybenchmark

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pandapages/api/internal/copyrighteligibility"
	"pandapages/api/internal/sourceprovider"
)

const publicDomainFixtureTestRoot = "testdata/publicdomain/benjamin-bunny"

func TestLoadPublicDomainFixture(t *testing.T) {
	fixture, err := LoadPublicDomainFixture(publicDomainFixtureTestRoot)
	if err != nil {
		t.Fatalf("LoadPublicDomainFixture() error = %v", err)
	}

	if fixture.BenchmarkVersion != VersionV1 {
		t.Fatalf("BenchmarkVersion = %q", fixture.BenchmarkVersion)
	}
	if fixture.FixtureKind != FixtureKindEligiblePublicDomain {
		t.Fatalf("FixtureKind = %q", fixture.FixtureKind)
	}
	if fixture.Provider != sourceprovider.ProjectGutenberg || fixture.ExternalID != "14407" {
		t.Fatalf("provider identity = %q/%q", fixture.Provider, fixture.ExternalID)
	}
	if fixture.CanonicalSourceSHA256 != exactFixtureSHA256(fixture.Source.CanonicalSource) {
		t.Fatalf("canonical source digest does not bind loaded content")
	}
	if fixture.CanonicalSourceSHA256 != "5c3a4a4312e3bf8a89e7fc13c8da287625c5f20d1b9f1570aac25a2903abf6c5" {
		t.Fatalf("CanonicalSourceSHA256 = %q", fixture.CanonicalSourceSHA256)
	}
	if fixture.EligibilityPolicy != copyrighteligibility.PolicyVersion {
		t.Fatalf("EligibilityPolicy = %q", fixture.EligibilityPolicy)
	}
	if fixture.EligibilityDate.Format("2006-01-02") != "2026-08-12" {
		t.Fatalf("EligibilityDate = %s", fixture.EligibilityDate)
	}
	if fixture.EligibilityAssessment.Overall != copyrighteligibility.OverallEligible ||
		fixture.EligibilityAssessment.US.Status != copyrighteligibility.JurisdictionEligible ||
		fixture.EligibilityAssessment.UK.Status != copyrighteligibility.JurisdictionEligible {
		t.Fatalf("eligibility assessment = %#v", fixture.EligibilityAssessment)
	}
	if fixture.EligibilityAssessment.US.Reason != copyrighteligibility.ReasonUSProviderPublicDomainConfirmed {
		t.Fatalf("US reason = %q", fixture.EligibilityAssessment.US.Reason)
	}
	if fixture.EligibilityAssessment.UK.Reason != copyrighteligibility.ReasonUKOrdinaryLiteraryTermExpired {
		t.Fatalf("UK reason = %q", fixture.EligibilityAssessment.UK.Reason)
	}
	publicationEligible, ok := fixture.Source.Rights["publicationEligible"].(bool)
	if !ok || !publicationEligible {
		t.Fatalf("publicationEligible rights value = %#v", fixture.Source.Rights["publicationEligible"])
	}
	if err := fixture.Source.Validate(); err != nil {
		t.Fatalf("fixture EndToEndSource.Validate() error = %v", err)
	}

	for _, excerpt := range []string{
		"Peter replied, \"The scarecrow in Mr. McGregor's garden,\"",
		"He took a tremendous jump off the top of the wall on to the top of the cat",
		"took out his son Benjamin by the ears, and whipped him with the little switch",
	} {
		if !strings.Contains(fixture.Source.CanonicalSource, excerpt) {
			t.Fatalf("canonical source is missing reviewed excerpt %q", excerpt)
		}
	}
}

func TestLoadPublicDomainFixtureFailsClosed(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(t *testing.T, root string)
		wantError string
	}{
		{
			name: "changed canonical source",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				appendFixtureFile(t, filepath.Join(root, "source.md"), "\nTampered.\n")
			},
			wantError: "SHA-256 does not match committed content",
		},
		{
			name: "wrong policy version",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				replaceFixtureText(t, filepath.Join(root, "manifest.json"), "panda-pages-copyright-v3", "panda-pages-copyright-v999")
			},
			wantError: "eligibility policy must equal",
		},
		{
			name: "active UK term",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				replaceFixtureText(t, filepath.Join(root, "manifest.json"), "\"deathYear\": 1943", "\"deathYear\": 1960")
			},
			wantError: "blocked by copyright policy",
		},
		{
			name: "wrong provider",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				replaceFixtureText(t, filepath.Join(root, "manifest.json"), "\"provider\": \"project-gutenberg\"", "\"provider\": \"other-provider\"")
			},
			wantError: "provider must equal",
		},
		{
			name: "provider URL mismatch",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				replaceFixtureText(t, filepath.Join(root, "manifest.json"), `"sourceUrl": "https://www.gutenberg.org/ebooks/14407"`, `"sourceUrl": "https://www.gutenberg.org/ebooks/99999"`)
			},
			wantError: "source URL must equal",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := copyFixtureDirectory(t, publicDomainFixtureTestRoot)
			test.mutate(t, root)
			_, err := LoadPublicDomainFixture(root)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("LoadPublicDomainFixture() error = %v, want substring %q", err, test.wantError)
			}
		})
	}
}

func copyFixtureDirectory(t *testing.T, source string) string {
	t.Helper()
	destination := t.TempDir()
	entries, err := os.ReadDir(source)
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", source, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("unexpected nested fixture directory %q", entry.Name())
		}
		data, err := os.ReadFile(filepath.Join(source, entry.Name()))
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", entry.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(destination, entry.Name()), data, 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", entry.Name(), err)
		}
	}
	return destination
}

func appendFixtureFile(t *testing.T, path, suffix string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatalf("OpenFile(%q) error = %v", path, err)
	}
	if _, err := file.WriteString(suffix); err != nil {
		_ = file.Close()
		t.Fatalf("WriteString(%q) error = %v", path, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close(%q) error = %v", path, err)
	}
}

func replaceFixtureText(t *testing.T, path, old, replacement string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	text := string(data)
	if strings.Count(text, old) != 1 {
		t.Fatalf("fixture replacement %q count = %d, want 1", old, strings.Count(text, old))
	}
	text = strings.Replace(text, old, replacement, 1)
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}
