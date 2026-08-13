package storybenchmark

import (
	"strings"
	"testing"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
)

func TestLoadControlledFixtureSet(t *testing.T) {
	fixtures, err := LoadControlledFixtureSet("testdata/controlled")
	if err != nil {
		t.Fatalf("LoadControlledFixtureSet() error = %v", err)
	}

	if fixtures.BenchmarkVersion != VersionV1 {
		t.Fatalf("BenchmarkVersion = %q", fixtures.BenchmarkVersion)
	}
	if fixtures.FixtureKind != FixtureKindSyntheticControlled {
		t.Fatalf("FixtureKind = %q", fixtures.FixtureKind)
	}
	if fixtures.Story.Slug != "the-lantern-keeper" || fixtures.Story.Title != "The Lantern Keeper" {
		t.Fatalf("Story = %#v", fixtures.Story)
	}
	if strings.TrimSpace(fixtures.Story.CanonicalSource) == "" {
		t.Fatal("CanonicalSource is empty")
	}
	if err := fixtures.Story.Analysis.Validate(); err != nil {
		t.Fatalf("StoryAnalysis is invalid: %v", err)
	}
	publicationEligible, ok := fixtures.Story.Rights["publicationEligible"].(bool)
	if !ok || publicationEligible {
		t.Fatalf("publicationEligible = %#v, want false", fixtures.Story.Rights["publicationEligible"])
	}

	want := map[string]struct {
		scope    adaptationcontract.AssessmentScope
		result   adaptationcontract.Result
		required adaptationcontract.FindingCode
	}{
		"clean-control":                 {adaptationcontract.AssessmentScopeEdition, adaptationcontract.ResultPass, ""},
		"motivation-changed":            {adaptationcontract.AssessmentScopeEdition, adaptationcontract.ResultFail, adaptationcontract.FindingMotivationChanged},
		"invented-moralising":           {adaptationcontract.AssessmentScopeEdition, adaptationcontract.ResultFail, adaptationcontract.FindingInventedMoralising},
		"causal-chain-broken":           {adaptationcontract.AssessmentScopeEdition, adaptationcontract.ResultFail, adaptationcontract.FindingCausalChainBroken},
		"coercion-romanticised":         {adaptationcontract.AssessmentScopeEdition, adaptationcontract.ResultFail, adaptationcontract.FindingCoercionRomanticised},
		"substantial-material-invented": {adaptationcontract.AssessmentScopeEdition, adaptationcontract.ResultFail, adaptationcontract.FindingSubstantialMaterialInvented},
		"edition-identity-lost":         {adaptationcontract.AssessmentScopeEdition, adaptationcontract.ResultFail, adaptationcontract.FindingEditionIdentityLost},
		"scope-too-rich":                {adaptationcontract.AssessmentScopeEdition, adaptationcontract.ResultNeedsReview, adaptationcontract.FindingScopeTooRich},
		"scope-too-thin":                {adaptationcontract.AssessmentScopeEdition, adaptationcontract.ResultNeedsReview, adaptationcontract.FindingScopeTooThin},
		"progression-not-distinct":      {adaptationcontract.AssessmentScopeBundle, adaptationcontract.ResultFail, adaptationcontract.FindingEditionProgressionNotDistinct},
		"progression-inverted":          {adaptationcontract.AssessmentScopeBundle, adaptationcontract.ResultFail, adaptationcontract.FindingEditionProgressionInverted},
	}
	if len(fixtures.Cases) != len(want) {
		t.Fatalf("len(Cases) = %d, want %d", len(fixtures.Cases), len(want))
	}

	for _, fixtureCase := range fixtures.Cases {
		expected, exists := want[fixtureCase.ID]
		if !exists {
			t.Fatalf("unexpected fixture case %q", fixtureCase.ID)
		}
		delete(want, fixtureCase.ID)

		if fixtureCase.Expectation.AssessmentScope != expected.scope {
			t.Fatalf("%s scope = %q", fixtureCase.ID, fixtureCase.Expectation.AssessmentScope)
		}
		if fixtureCase.Expectation.ExpectedResult != expected.result {
			t.Fatalf("%s result = %q", fixtureCase.ID, fixtureCase.Expectation.ExpectedResult)
		}
		if expected.required == "" {
			if len(fixtureCase.Expectation.RequiredFindingCodes) != 0 {
				t.Fatalf("%s required findings = %#v", fixtureCase.ID, fixtureCase.Expectation.RequiredFindingCodes)
			}
		} else if len(fixtureCase.Expectation.RequiredFindingCodes) != 1 ||
			fixtureCase.Expectation.RequiredFindingCodes[0] != expected.required {
			t.Fatalf("%s required findings = %#v", fixtureCase.ID, fixtureCase.Expectation.RequiredFindingCodes)
		}
		if len(fixtureCase.Editions) == 0 {
			t.Fatalf("%s has no editions", fixtureCase.ID)
		}
		for _, edition := range fixtureCase.Editions {
			if !edition.StructuralValidation.Passed() {
				t.Fatalf("%s/%s structural validation failed", fixtureCase.ID, edition.EditionKey)
			}
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing fixture cases = %#v", want)
	}
}

func TestValidateControlledStoryRightsRejectsPublicationEligibleFixtures(t *testing.T) {
	tests := []struct {
		name      string
		rights    map[string]any
		wantError string
	}{
		{
			name:      "missing publication eligibility",
			rights:    map[string]any{"status": "synthetic-benchmark-fixture"},
			wantError: "must be a boolean",
		},
		{
			name:      "non boolean publication eligibility",
			rights:    map[string]any{"publicationEligible": "false"},
			wantError: "must be a boolean",
		},
		{
			name:      "publication eligible synthetic fixture",
			rights:    map[string]any{"publicationEligible": true},
			wantError: "must not be publication eligible",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateControlledStoryRights(test.rights)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("validateControlledStoryRights() error = %v, want substring %q", err, test.wantError)
			}
		})
	}

	if err := validateControlledStoryRights(map[string]any{"publicationEligible": false}); err != nil {
		t.Fatalf("validateControlledStoryRights(false) error = %v", err)
	}
}

func TestControlledBundleFixturesAreDeliberate(t *testing.T) {
	fixtures, err := LoadControlledFixtureSet("testdata/controlled")
	if err != nil {
		t.Fatalf("LoadControlledFixtureSet() error = %v", err)
	}

	notDistinct := controlledCaseByID(t, fixtures, "progression-not-distinct")
	if len(notDistinct.Editions) != len(adaptationcontract.ModernEditionKeys()) {
		t.Fatalf("progression-not-distinct editions = %d", len(notDistinct.Editions))
	}
	first := notDistinct.Editions[0].Markdown
	for _, edition := range notDistinct.Editions[1:] {
		if edition.Markdown != first {
			t.Fatalf("progression-not-distinct fixture is not actually identical across editions")
		}
	}

	inverted := controlledCaseByID(t, fixtures, "progression-inverted")
	if len(inverted.Editions) != 2 {
		t.Fatalf("progression-inverted editions = %d", len(inverted.Editions))
	}
	if inverted.Editions[0].EditionKey != model.AdminStoryEditionConfidentReaders ||
		inverted.Editions[1].EditionKey != model.AdminStoryEditionGrowingReaders {
		t.Fatalf("progression-inverted keys = %#v", inverted.Editions)
	}
	if len(strings.Fields(inverted.Editions[0].Markdown)) >= len(strings.Fields(inverted.Editions[1].Markdown)) {
		t.Fatalf(
			"progression-inverted fixture is not richer at growing-readers: confident=%d growing=%d",
			len(strings.Fields(inverted.Editions[0].Markdown)),
			len(strings.Fields(inverted.Editions[1].Markdown)),
		)
	}
}

func TestReadFixtureFileRejectsEscape(t *testing.T) {
	root := t.TempDir()
	_, err := readFixtureFile(root, "../outside.md")
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("readFixtureFile() error = %v", err)
	}
}

func TestDecodeStrictJSONRejectsDuplicateUnknownAndTrailingValues(t *testing.T) {
	tests := []struct {
		name      string
		data      string
		wantError string
	}{
		{
			name:      "duplicate key",
			data:      `{"benchmarkVersion":"a","benchmarkVersion":"b"}`,
			wantError: "duplicate key",
		},
		{
			name:      "unknown field",
			data:      `{"unexpected":true}`,
			wantError: "unknown field",
		},
		{
			name:      "trailing value",
			data:      `{} {}`,
			wantError: "trailing",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var manifest fixtureManifest
			err := decodeStrictJSON([]byte(test.data), &manifest)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("decodeStrictJSON() error = %v, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestDecodeStrictJSONAcceptsExactlyOneValue(t *testing.T) {
	var target struct {
		Value string `json:"value"`
	}
	if err := decodeStrictJSON([]byte(`{"value":"ok"}`), &target); err != nil {
		t.Fatalf("decodeStrictJSON() error = %v", err)
	}
	if target.Value != "ok" {
		t.Fatalf("Value = %q", target.Value)
	}
}

func controlledCaseByID(t *testing.T, fixtures ControlledFixtureSet, id string) ControlledCase {
	t.Helper()
	for _, fixtureCase := range fixtures.Cases {
		if fixtureCase.ID == id {
			return fixtureCase
		}
	}
	t.Fatalf("controlled fixture case %q not found", id)
	return ControlledCase{}
}
