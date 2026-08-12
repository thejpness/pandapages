package storyvalidation

import (
	"encoding/json"
	"strings"
	"testing"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
)

func validAssessmentJSON(t *testing.T) []byte {
	t.Helper()
	data, err := json.Marshal(validFailAssessment())
	if err != nil {
		t.Fatalf("json.Marshal(validFailAssessment()) error = %v", err)
	}
	return data
}

func mutateAssessmentJSON(t *testing.T, mutate func(map[string]json.RawMessage)) []byte {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(validAssessmentJSON(t), &object); err != nil {
		t.Fatalf("json.Unmarshal(validAssessmentJSON()) error = %v", err)
	}
	mutate(object)
	data, err := json.Marshal(object)
	if err != nil {
		t.Fatalf("json.Marshal(mutated assessment) error = %v", err)
	}
	return data
}

func TestDecodeAssessmentJSONAcceptsExactEvidenceBearingAssessment(t *testing.T) {
	assessment, err := DecodeAssessmentJSON(validAssessmentJSON(t))
	if err != nil {
		t.Fatalf("DecodeAssessmentJSON() error = %v", err)
	}
	if assessment.Result != adaptationcontract.ResultFail {
		t.Fatalf("result = %q", assessment.Result)
	}
	if len(assessment.Findings) != 1 || len(assessment.Findings[0].Evidence) != 2 {
		t.Fatalf("assessment = %#v", assessment)
	}
	if assessment.Findings[0].Evidence[0].EditionKey != nil {
		t.Fatal("canonical-source evidence editionKey must decode as nil")
	}
}

func TestEvidenceMachineJSONAlwaysContainsEditionKey(t *testing.T) {
	data := validAssessmentJSON(t)
	text := string(data)
	if !strings.Contains(text, `"location":"canonical_source","editionKey":null`) {
		t.Fatalf("canonical-source evidence must encode editionKey:null: %s", text)
	}
	if !strings.Contains(text, `"location":"generated_edition","editionKey":"growing-readers"`) {
		t.Fatalf("generated-edition evidence must encode concrete editionKey: %s", text)
	}
}

func TestDecodeAssessmentJSONRejectsNonExactBoundary(t *testing.T) {
	base := string(validAssessmentJSON(t))

	tests := []struct {
		name string
		data []byte
		want string
	}{
		{
			name: "invalid utf8",
			data: append([]byte(`{"validationVersion":"`), 0xff),
			want: "valid UTF-8",
		},
		{
			name: "not object",
			data: []byte(`[]`),
			want: "must be a JSON object",
		},
		{
			name: "missing top level field",
			data: mutateAssessmentJSON(t, func(object map[string]json.RawMessage) {
				delete(object, "validationVersion")
			}),
			want: `missing required field "validationVersion"`,
		},
		{
			name: "unknown top level field",
			data: mutateAssessmentJSON(t, func(object map[string]json.RawMessage) {
				object["confidence"] = json.RawMessage(`0.9`)
			}),
			want: `unknown field "confidence"`,
		},
		{
			name: "null findings",
			data: mutateAssessmentJSON(t, func(object map[string]json.RawMessage) {
				object["findings"] = json.RawMessage(`null`)
			}),
			want: "findings must be a JSON array",
		},
		{
			name: "unknown finding field",
			data: []byte(strings.Replace(base, `"code":"motivation_changed"`, `"code":"motivation_changed","confidence":0.9`, 1)),
			want: `contains unknown field "confidence"`,
		},
		{
			name: "missing finding evidence",
			data: []byte(strings.Replace(base, `,"evidence":[`, `,"evidence_missing":[`, 1)),
			want: `missing required field "evidence"`,
		},
		{
			name: "null evidence",
			data: []byte(strings.Replace(base, `"evidence":[`, `"evidence":null,"discarded":[`, 1)),
			want: `unknown field "discarded"`,
		},
		{
			name: "unknown evidence field",
			data: []byte(strings.Replace(base, `"location":"canonical_source"`, `"location":"canonical_source","confidence":1`, 1)),
			want: `contains unknown field "confidence"`,
		},
		{
			name: "missing evidence editionKey",
			data: []byte(strings.Replace(base, `,"editionKey":null`, ``, 1)),
			want: `missing required field "editionKey"`,
		},
		{
			name: "duplicate nested key",
			data: []byte(strings.Replace(base, `"location":"canonical_source"`, `"location":"canonical_source","location":"story_analysis"`, 1)),
			want: `duplicate object key "location"`,
		},
		{
			name: "duplicate top level key",
			data: []byte(strings.Replace(base, `"validationVersion":`, `"validationVersion":"duplicate","validationVersion":`, 1)),
			want: `duplicate object key "validationVersion"`,
		},
		{
			name: "trailing value",
			data: append([]byte(base), []byte(` {}`)...),
			want: "trailing",
		},
		{
			name: "semantic invariant failure",
			data: mutateAssessmentJSON(t, func(object map[string]json.RawMessage) {
				object["result"] = json.RawMessage(`"pass"`)
			}),
			want: "pass assessments",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeAssessmentJSON(test.data)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("DecodeAssessmentJSON() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestDecodeAssessmentJSONAcceptsBundleShapeAndRejectsCrossScopeFields(t *testing.T) {
	bundle := Assessment{
		ValidationVersion:    ValidationV2,
		SpecificationVersion: "panda-pages-adaptation-v2",
		AssessmentScope:      adaptationcontract.AssessmentScopeBundle,
		EditionKeys: []model.AdminStoryEditionKey{
			model.AdminStoryEditionGrowingReaders,
			model.AdminStoryEditionStoryExplorers,
		},
		Result:   adaptationcontract.ResultPass,
		Findings: []Finding{},
	}
	data, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("json.Marshal(bundle) error = %v", err)
	}
	if _, err := DecodeAssessmentJSON(data); err != nil {
		t.Fatalf("DecodeAssessmentJSON(bundle) error = %v", err)
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatalf("json.Unmarshal(bundle) error = %v", err)
	}
	object["editionKey"] = json.RawMessage(`"growing-readers"`)
	mutated, err := json.Marshal(object)
	if err != nil {
		t.Fatalf("json.Marshal(mutated bundle) error = %v", err)
	}
	_, err = DecodeAssessmentJSON(mutated)
	if err == nil || !strings.Contains(err.Error(), `unknown field "editionKey"`) {
		t.Fatalf("DecodeAssessmentJSON() error = %v", err)
	}
}
