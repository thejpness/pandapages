package adaptationcontract

import (
	"strings"
	"testing"
)

func TestDecodeAssessmentJSONAcceptsExactEditionAndBundleEnvelopes(t *testing.T) {
	t.Run("edition", func(t *testing.T) {
		assessment, err := DecodeAssessmentJSON([]byte(`{
			"contractVersion":"panda-pages-adaptation-v1",
			"assessmentScope":"edition",
			"editionKey":"story-explorers",
			"result":"needs_review",
			"findings":[{
				"code":"vocabulary_mismatch",
				"severity":"review",
				"message":"Vocabulary may be too advanced."
			}]
		}`))
		if err != nil {
			t.Fatalf("DecodeAssessmentJSON() error = %v", err)
		}
		if assessment.Result != ResultNeedsReview {
			t.Fatalf("result = %q, want %q", assessment.Result, ResultNeedsReview)
		}
	})

	t.Run("bundle", func(t *testing.T) {
		assessment, err := DecodeSemanticAssessmentJSON([]byte(`{
			"contractVersion":"panda-pages-adaptation-v1",
			"assessmentScope":"bundle",
			"editionKeys":["confident-readers","growing-readers","story-explorers","little-listeners"],
			"result":"pass",
			"findings":[]
		}`))
		if err != nil {
			t.Fatalf("DecodeSemanticAssessmentJSON() error = %v", err)
		}
		if assessment.Result != ResultPass {
			t.Fatalf("result = %q, want %q", assessment.Result, ResultPass)
		}
	})
}

func TestDecodeAssessmentJSONRejectsNonExactJSONBoundary(t *testing.T) {
	tests := []struct {
		name string
		json []byte
		want string
	}{
		{
			name: "invalid utf8",
			json: append([]byte(`{"contractVersion":"panda-pages-adaptation-v1","assessmentScope":"edition","editionKey":"story-explorers","result":"pass","findings":[],"x":"`), 0xff),
			want: "valid UTF-8",
		},
		{
			name: "not object",
			json: []byte(`[]`),
			want: "assessment JSON must be an object",
		},
		{
			name: "missing findings",
			json: []byte(`{
				"contractVersion":"panda-pages-adaptation-v1",
				"assessmentScope":"edition",
				"editionKey":"story-explorers",
				"result":"pass"
			}`),
			want: `missing required field "findings"`,
		},
		{
			name: "null findings",
			json: []byte(`{
				"contractVersion":"panda-pages-adaptation-v1",
				"assessmentScope":"edition",
				"editionKey":"story-explorers",
				"result":"pass",
				"findings":null
			}`),
			want: "findings must be a JSON array",
		},
		{
			name: "unknown top level field",
			json: []byte(`{
				"contractVersion":"panda-pages-adaptation-v1",
				"assessmentScope":"edition",
				"editionKey":"story-explorers",
				"result":"pass",
				"findings":[],
				"confidence":0.99
			}`),
			want: `unknown field "confidence"`,
		},
		{
			name: "wrong target field for edition",
			json: []byte(`{
				"contractVersion":"panda-pages-adaptation-v1",
				"assessmentScope":"edition",
				"editionKey":"story-explorers",
				"editionKeys":["story-explorers","little-listeners"],
				"result":"pass",
				"findings":[]
			}`),
			want: `unknown field "editionKeys"`,
		},
		{
			name: "unknown finding field",
			json: []byte(`{
				"contractVersion":"panda-pages-adaptation-v1",
				"assessmentScope":"edition",
				"editionKey":"story-explorers",
				"result":"needs_review",
				"findings":[{
					"code":"vocabulary_mismatch",
					"severity":"review",
					"message":"Review.",
					"confidence":0.5
				}]
			}`),
			want: `unknown field "confidence"`,
		},
		{
			name: "duplicate top level key",
			json: []byte(`{
				"contractVersion":"panda-pages-adaptation-v1",
				"assessmentScope":"edition",
				"editionKey":"story-explorers",
				"result":"pass",
				"result":"fail",
				"findings":[]
			}`),
			want: `duplicate object key "result"`,
		},
		{
			name: "duplicate nested key",
			json: []byte(`{
				"contractVersion":"panda-pages-adaptation-v1",
				"assessmentScope":"edition",
				"editionKey":"story-explorers",
				"result":"needs_review",
				"findings":[{
					"code":"vocabulary_mismatch",
					"severity":"review",
					"severity":"blocking",
					"message":"Review."
				}]
			}`),
			want: `duplicate object key "severity"`,
		},
		{
			name: "trailing json value",
			json: []byte(`{
				"contractVersion":"panda-pages-adaptation-v1",
				"assessmentScope":"edition",
				"editionKey":"story-explorers",
				"result":"pass",
				"findings":[]
			} {}`),
			want: "trailing",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeAssessmentJSON(test.json)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("DecodeAssessmentJSON() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestDecodeSemanticAssessmentJSONRejectsStructuralFindings(t *testing.T) {
	data := []byte(`{
		"contractVersion":"panda-pages-adaptation-v1",
		"assessmentScope":"edition",
		"editionKey":"story-explorers",
		"result":"fail",
		"findings":[{
			"code":"raw_html_present",
			"severity":"blocking",
			"message":"Raw HTML is present."
		}]
	}`)

	if _, err := DecodeAssessmentJSON(data); err != nil {
		t.Fatalf("combined DecodeAssessmentJSON() error = %v", err)
	}

	_, err := DecodeSemanticAssessmentJSON(data)
	if err == nil || !strings.Contains(err.Error(), "structural") {
		t.Fatalf("DecodeSemanticAssessmentJSON() error = %v, want structural rejection", err)
	}
}

func TestDecodeAssessmentJSONStillAppliesContractValidation(t *testing.T) {
	data := []byte(`{
		"contractVersion":"panda-pages-adaptation-v1",
		"assessmentScope":"bundle",
		"editionKeys":["story-explorers","growing-readers"],
		"result":"pass",
		"findings":[]
	}`)

	_, err := DecodeAssessmentJSON(data)
	if err == nil || !strings.Contains(err.Error(), "canonical modern edition order") {
		t.Fatalf("DecodeAssessmentJSON() error = %v, want bundle order rejection", err)
	}
}
