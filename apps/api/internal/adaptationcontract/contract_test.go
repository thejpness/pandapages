package adaptationcontract

import (
	"strings"
	"testing"

	"pandapages/api/internal/model"
)

func editionKey(key model.AdminStoryEditionKey) *model.AdminStoryEditionKey {
	return &key
}

func TestContractVocabulary(t *testing.T) {
	if VersionV1 != "panda-pages-adaptation-v1" {
		t.Fatalf("unexpected contract version %q", VersionV1)
	}

	want := []model.AdminStoryEditionKey{
		model.AdminStoryEditionConfidentReaders,
		model.AdminStoryEditionGrowingReaders,
		model.AdminStoryEditionStoryExplorers,
		model.AdminStoryEditionLittleListeners,
	}
	got := ModernEditionKeys()
	if len(got) != len(want) {
		t.Fatalf("modern edition key count = %d, want %d", len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("modern edition key %d = %q, want %q", index, got[index], want[index])
		}
	}
	if ValidModernEditionKey(model.AdminStoryEditionClassic) {
		t.Fatal("classic must not be a modern adaptation target")
	}

	for _, test := range []struct {
		code     FindingCode
		severity FindingSeverity
		kind     FindingKind
	}{
		{FindingCorePlotChanged, FindingSeverityBlocking, FindingKindSemantic},
		{FindingVocabularyMismatch, FindingSeverityReview, FindingKindSemantic},
		{FindingEditionProgressionNotDistinct, FindingSeverityBlocking, FindingKindSemantic},
		{FindingEditionProgressionQuestionable, FindingSeverityReview, FindingKindSemantic},
		{FindingInvalidUTF8, FindingSeverityBlocking, FindingKindStructural},
		{FindingClassicSourceChanged, FindingSeverityBlocking, FindingKindStructural},
	} {
		if !ValidFindingCode(test.code) {
			t.Fatalf("finding %q should be valid", test.code)
		}
		severity, ok := CanonicalSeverity(test.code)
		if !ok || severity != test.severity {
			t.Fatalf("severity for %q = %q, %v; want %q, true", test.code, severity, ok, test.severity)
		}
		kind, ok := FindingKindFor(test.code)
		if !ok || kind != test.kind {
			t.Fatalf("kind for %q = %q, %v; want %q, true", test.code, kind, ok, test.kind)
		}
	}

	if ValidFindingCode("made_up") {
		t.Fatal("unknown finding code must be rejected")
	}
}

func TestValidEditionAssessments(t *testing.T) {
	tests := []Assessment{
		{
			ContractVersion: VersionV1,
			AssessmentScope: AssessmentScopeEdition,
			EditionKey:      editionKey(model.AdminStoryEditionConfidentReaders),
			Result:          ResultPass,
			Findings:        []Finding{},
		},
		{
			ContractVersion: VersionV1,
			AssessmentScope: AssessmentScopeEdition,
			EditionKey:      editionKey(model.AdminStoryEditionGrowingReaders),
			Result:          ResultNeedsReview,
			Findings: []Finding{{
				Code:     FindingVocabularyMismatch,
				Severity: FindingSeverityReview,
				Message:  "Vocabulary may be too advanced for this edition.",
			}},
		},
		{
			ContractVersion: VersionV1,
			AssessmentScope: AssessmentScopeEdition,
			EditionKey:      editionKey(model.AdminStoryEditionStoryExplorers),
			Result:          ResultFail,
			Findings: []Finding{
				{
					Code:     FindingCausalChainBroken,
					Severity: FindingSeverityBlocking,
					Message:  "The retained ending no longer follows from the preceding event.",
				},
				{
					Code:     FindingIconicLanguageRemoved,
					Severity: FindingSeverityReview,
					Message:  "A defining refrain was removed.",
				},
			},
		},
	}

	for index, assessment := range tests {
		if err := assessment.Validate(); err != nil {
			t.Fatalf("assessment %d Validate() error = %v", index, err)
		}
		if err := assessment.ValidateSemantic(); err != nil {
			t.Fatalf("assessment %d ValidateSemantic() error = %v", index, err)
		}
	}
}

func TestValidBundleAssessments(t *testing.T) {
	tests := []Assessment{
		{
			ContractVersion: VersionV1,
			AssessmentScope: AssessmentScopeBundle,
			EditionKeys: []model.AdminStoryEditionKey{
				model.AdminStoryEditionConfidentReaders,
				model.AdminStoryEditionGrowingReaders,
				model.AdminStoryEditionStoryExplorers,
				model.AdminStoryEditionLittleListeners,
			},
			Result:   ResultPass,
			Findings: []Finding{},
		},
		{
			ContractVersion: VersionV1,
			AssessmentScope: AssessmentScopeBundle,
			EditionKeys: []model.AdminStoryEditionKey{
				model.AdminStoryEditionGrowingReaders,
				model.AdminStoryEditionStoryExplorers,
			},
			Result: ResultNeedsReview,
			Findings: []Finding{{
				Code:     FindingEditionProgressionQuestionable,
				Severity: FindingSeverityReview,
				Message:  "The narrative-scope reduction is not clearly established.",
			}},
		},
		{
			ContractVersion: VersionV1,
			AssessmentScope: AssessmentScopeBundle,
			EditionKeys: []model.AdminStoryEditionKey{
				model.AdminStoryEditionConfidentReaders,
				model.AdminStoryEditionLittleListeners,
			},
			Result: ResultFail,
			Findings: []Finding{{
				Code:     FindingEditionProgressionNotDistinct,
				Severity: FindingSeverityBlocking,
				Message:  "The two editions are materially the same manuscript.",
			}},
		},
	}

	for index, assessment := range tests {
		if err := assessment.Validate(); err != nil {
			t.Fatalf("assessment %d Validate() error = %v", index, err)
		}
		if err := assessment.ValidateSemantic(); err != nil {
			t.Fatalf("assessment %d ValidateSemantic() error = %v", index, err)
		}
	}
}

func TestCombinedEditionAssessmentMayCarryDeterministicStructuralFinding(t *testing.T) {
	assessment := Assessment{
		ContractVersion: VersionV1,
		AssessmentScope: AssessmentScopeEdition,
		EditionKey:      editionKey(model.AdminStoryEditionLittleListeners),
		Result:          ResultFail,
		Findings: []Finding{{
			Code:     FindingRawHTMLPresent,
			Severity: FindingSeverityBlocking,
			Message:  "Generated Markdown contains raw HTML.",
		}},
	}

	if err := assessment.Validate(); err != nil {
		t.Fatalf("combined Validate() error = %v", err)
	}
	if err := assessment.ValidateSemantic(); err == nil || !strings.Contains(err.Error(), "structural") {
		t.Fatalf("ValidateSemantic() error = %v, want structural finding rejection", err)
	}
}

func TestAssessmentValidationRejectsInvalidEnvelopes(t *testing.T) {
	validEdition := func() Assessment {
		return Assessment{
			ContractVersion: VersionV1,
			AssessmentScope: AssessmentScopeEdition,
			EditionKey:      editionKey(model.AdminStoryEditionStoryExplorers),
			Result:          ResultPass,
			Findings:        []Finding{},
		}
	}

	tests := []struct {
		name   string
		mutate func(*Assessment)
		want   string
	}{
		{
			name: "unknown contract version",
			mutate: func(a *Assessment) {
				a.ContractVersion = "panda-pages-adaptation-v2"
			},
			want: "contract version",
		},
		{
			name: "unknown scope",
			mutate: func(a *Assessment) {
				a.AssessmentScope = "other"
			},
			want: "assessment scope",
		},
		{
			name: "classic edition",
			mutate: func(a *Assessment) {
				a.EditionKey = editionKey(model.AdminStoryEditionClassic)
			},
			want: "canonical modern edition key",
		},
		{
			name: "missing edition key",
			mutate: func(a *Assessment) {
				a.EditionKey = nil
			},
			want: "exactly one edition key",
		},
		{
			name: "edition contains bundle keys",
			mutate: func(a *Assessment) {
				a.EditionKeys = []model.AdminStoryEditionKey{
					model.AdminStoryEditionStoryExplorers,
					model.AdminStoryEditionLittleListeners,
				}
			},
			want: "must not contain editionKeys",
		},
		{
			name: "unknown result",
			mutate: func(a *Assessment) {
				a.Result = "maybe"
			},
			want: "assessment result",
		},
		{
			name: "pass with finding",
			mutate: func(a *Assessment) {
				a.Findings = []Finding{{
					Code:     FindingVocabularyMismatch,
					Severity: FindingSeverityReview,
					Message:  "Review this.",
				}}
			},
			want: "pass assessments must contain no findings",
		},
		{
			name: "needs review without finding",
			mutate: func(a *Assessment) {
				a.Result = ResultNeedsReview
			},
			want: "needs_review assessments require",
		},
		{
			name: "needs review with blocking finding",
			mutate: func(a *Assessment) {
				a.Result = ResultNeedsReview
				a.Findings = []Finding{{
					Code:     FindingCorePlotChanged,
					Severity: FindingSeverityBlocking,
					Message:  "Core plot changed.",
				}}
			},
			want: "needs_review assessments require",
		},
		{
			name: "fail without blocking finding",
			mutate: func(a *Assessment) {
				a.Result = ResultFail
				a.Findings = []Finding{{
					Code:     FindingVocabularyMismatch,
					Severity: FindingSeverityReview,
					Message:  "Review this.",
				}}
			},
			want: "fail assessments require",
		},
		{
			name: "unknown finding code",
			mutate: func(a *Assessment) {
				a.Result = ResultFail
				a.Findings = []Finding{{
					Code:     "made_up",
					Severity: FindingSeverityBlocking,
					Message:  "Unknown.",
				}}
			},
			want: "unsupported finding code",
		},
		{
			name: "wrong canonical severity",
			mutate: func(a *Assessment) {
				a.Result = ResultNeedsReview
				a.Findings = []Finding{{
					Code:     FindingCorePlotChanged,
					Severity: FindingSeverityReview,
					Message:  "Wrong severity.",
				}}
			},
			want: "severity",
		},
		{
			name: "empty finding message",
			mutate: func(a *Assessment) {
				a.Result = ResultNeedsReview
				a.Findings = []Finding{{
					Code:     FindingVocabularyMismatch,
					Severity: FindingSeverityReview,
					Message:  "   ",
				}}
			},
			want: "message is required",
		},
		{
			name: "bundle-only finding in edition",
			mutate: func(a *Assessment) {
				a.Result = ResultFail
				a.Findings = []Finding{{
					Code:     FindingEditionProgressionInverted,
					Severity: FindingSeverityBlocking,
					Message:  "Progression inverted.",
				}}
			},
			want: "not allowed",
		},
		{
			name: "taxonomy-only structural finding in edition",
			mutate: func(a *Assessment) {
				a.Result = ResultFail
				a.Findings = []Finding{{
					Code:     FindingClassicSourceChanged,
					Severity: FindingSeverityBlocking,
					Message:  "Classic changed.",
				}}
			},
			want: "not allowed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assessment := validEdition()
			test.mutate(&assessment)
			err := assessment.Validate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestBundleValidationRejectsInvalidTargetsAndFindingScope(t *testing.T) {
	validBundle := func() Assessment {
		return Assessment{
			ContractVersion: VersionV1,
			AssessmentScope: AssessmentScopeBundle,
			EditionKeys: []model.AdminStoryEditionKey{
				model.AdminStoryEditionGrowingReaders,
				model.AdminStoryEditionStoryExplorers,
			},
			Result:   ResultPass,
			Findings: []Finding{},
		}
	}

	tests := []struct {
		name   string
		mutate func(*Assessment)
		want   string
	}{
		{
			name: "bundle contains editionKey",
			mutate: func(a *Assessment) {
				a.EditionKey = editionKey(model.AdminStoryEditionGrowingReaders)
			},
			want: "must not contain editionKey",
		},
		{
			name: "fewer than two keys",
			mutate: func(a *Assessment) {
				a.EditionKeys = []model.AdminStoryEditionKey{model.AdminStoryEditionGrowingReaders}
			},
			want: "at least two edition keys",
		},
		{
			name: "duplicate keys",
			mutate: func(a *Assessment) {
				a.EditionKeys = []model.AdminStoryEditionKey{
					model.AdminStoryEditionGrowingReaders,
					model.AdminStoryEditionGrowingReaders,
				}
			},
			want: "must be distinct",
		},
		{
			name: "out of canonical order",
			mutate: func(a *Assessment) {
				a.EditionKeys = []model.AdminStoryEditionKey{
					model.AdminStoryEditionStoryExplorers,
					model.AdminStoryEditionGrowingReaders,
				}
			},
			want: "canonical modern edition order",
		},
		{
			name: "classic key",
			mutate: func(a *Assessment) {
				a.EditionKeys = []model.AdminStoryEditionKey{
					model.AdminStoryEditionGrowingReaders,
					model.AdminStoryEditionClassic,
				}
			},
			want: "canonical modern edition key",
		},
		{
			name: "edition-only finding",
			mutate: func(a *Assessment) {
				a.Result = ResultFail
				a.Findings = []Finding{{
					Code:     FindingCorePlotChanged,
					Severity: FindingSeverityBlocking,
					Message:  "Core plot changed.",
				}}
			},
			want: "not allowed",
		},
		{
			name: "structural finding",
			mutate: func(a *Assessment) {
				a.Result = ResultFail
				a.Findings = []Finding{{
					Code:     FindingRawHTMLPresent,
					Severity: FindingSeverityBlocking,
					Message:  "Raw HTML exists.",
				}}
			},
			want: "not allowed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assessment := validBundle()
			test.mutate(&assessment)
			err := assessment.Validate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.want)
			}
		})
	}
}
