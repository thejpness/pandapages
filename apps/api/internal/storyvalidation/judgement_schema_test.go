package storyvalidation

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
	"pandapages/api/internal/storygeneration"
)

func TestSemanticJudgementJSONSchemasAreStrictV3Envelopes(t *testing.T) {
	index := judgementSchemaTestIndex(t)
	tests := []struct {
		name       string
		build      func(EvidenceIndex) (json.RawMessage, error)
		scope      adaptationcontract.AssessmentScope
		targetName string
		fields     []string
	}{
		{
			name:       "edition",
			build:      EditionJudgementJSONSchema,
			scope:      adaptationcontract.AssessmentScopeEdition,
			targetName: "editionKey",
			fields: []string{
				"validationVersion", "specificationVersion", "assessmentScope",
				"editionKey", "result", "findings",
			},
		},
		{
			name:       "bundle",
			build:      BundleJudgementJSONSchema,
			scope:      adaptationcontract.AssessmentScopeBundle,
			targetName: "editionKeys",
			fields: []string{
				"validationVersion", "specificationVersion", "assessmentScope",
				"editionKeys", "result", "findings",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			schema, err := test.build(index)
			if err != nil {
				t.Fatalf("build schema error = %v", err)
			}
			if !json.Valid(schema) {
				t.Fatal("schema must be valid JSON")
			}

			decoded := decodeJudgementSchema(t, schema)
			if decoded["type"] != "object" || decoded["additionalProperties"] != false {
				t.Fatalf("top-level schema = %#v", decoded)
			}

			properties := decoded["properties"].(map[string]any)
			assertExactSchemaProperties(t, properties, test.fields)
			if !reflect.DeepEqual(schemaStringValues(decoded["required"]), test.fields) {
				t.Fatalf("required = %v, want %v", decoded["required"], test.fields)
			}

			if got := schemaStringValues(
				properties["validationVersion"].(map[string]any)["enum"],
			); !reflect.DeepEqual(got, []string{string(ValidationV3)}) {
				t.Fatalf("validationVersion enum = %v", got)
			}
			if got := schemaStringValues(
				properties["specificationVersion"].(map[string]any)["enum"],
			); !reflect.DeepEqual(got, []string{string(storygeneration.SpecificationV2)}) {
				t.Fatalf("specificationVersion enum = %v", got)
			}
			if got := schemaStringValues(
				properties["assessmentScope"].(map[string]any)["enum"],
			); !reflect.DeepEqual(got, []string{string(test.scope)}) {
				t.Fatalf("assessmentScope enum = %v", got)
			}
			if _, ok := properties[test.targetName]; !ok {
				t.Fatalf("schema is missing target property %q", test.targetName)
			}
		})
	}
}

func TestSemanticJudgementJSONSchemaUsesOnlySuppliedEvidenceIndexIDs(t *testing.T) {
	first := judgementSchemaTestIndex(t)
	schema, err := EditionJudgementJSONSchema(first)
	if err != nil {
		t.Fatalf("EditionJudgementJSONSchema() error = %v", err)
	}

	actualIDs := judgementSchemaSegmentIDs(t, schema)
	wantIDs := make([]string, 0, len(first.IDs()))
	for _, id := range first.IDs() {
		wantIDs = append(wantIDs, string(id))
	}
	if !reflect.DeepEqual(actualIDs, wantIDs) {
		t.Fatalf("segmentId enum = %v, want EvidenceIndex.IDs() %v", actualIDs, wantIDs)
	}

	var canonical, analysis, generated bool
	for _, id := range actualIDs {
		canonical = canonical || strings.HasPrefix(id, "src:")
		analysis = analysis || strings.HasPrefix(id, "ana:")
		generated = generated || strings.HasPrefix(id, "gen:")
	}
	if !canonical || !analysis || !generated {
		t.Fatalf("segmentId enum must contain canonical, analysis, and generated IDs: %v", actualIDs)
	}

	second, err := BuildEvidenceIndex(
		"Different canonical source.",
		evidenceIndexTestAnalysis(),
		[]storygeneration.GeneratedEditionArtifact{{
			EditionKey: model.AdminStoryEditionStoryExplorers,
			Markdown:   "# Different Edition\n\nDifferent generated block.",
		}},
	)
	if err != nil {
		t.Fatalf("BuildEvidenceIndex(second) error = %v", err)
	}
	secondSchema, err := EditionJudgementJSONSchema(second)
	if err != nil {
		t.Fatalf("EditionJudgementJSONSchema(second) error = %v", err)
	}
	for _, id := range judgementSchemaSegmentIDs(t, secondSchema) {
		if id == "gen:growing-readers:p0001" || id == "src:p0002" {
			t.Fatalf("second schema retained ID %q from the first index", id)
		}
	}
}

func TestSemanticJudgementJSONSchemaKeepsModelEvidenceBoundaryAndPR91Ownership(t *testing.T) {
	schema, err := EditionJudgementJSONSchema(judgementSchemaTestIndex(t))
	if err != nil {
		t.Fatalf("EditionJudgementJSONSchema() error = %v", err)
	}

	properties := decodeJudgementSchema(t, schema)["properties"].(map[string]any)
	finding := properties["findings"].(map[string]any)["items"].(map[string]any)
	findingProperties := finding["properties"].(map[string]any)
	for _, name := range []string{"code", "severity"} {
		if _, exists := findingProperties[name].(map[string]any)["enum"]; exists {
			t.Fatalf("finding %s enum must remain owned by PR91 runtime validation", name)
		}
	}

	evidence := findingProperties["evidence"].(map[string]any)["items"].(map[string]any)
	if evidence["type"] != "object" || evidence["additionalProperties"] != false {
		t.Fatalf("evidence-reference schema = %#v", evidence)
	}
	evidenceProperties := evidence["properties"].(map[string]any)
	assertExactSchemaProperties(t, evidenceProperties, []string{"segmentId", "explanation"})
	if got := schemaStringValues(evidence["required"]); !reflect.DeepEqual(got, []string{"segmentId", "explanation"}) {
		t.Fatalf("evidence-reference required = %v", got)
	}
	for _, forbidden := range []string{"excerpt", "location", "editionKey"} {
		if _, exists := evidenceProperties[forbidden]; exists {
			t.Fatalf("evidence-reference schema must not expose %q", forbidden)
		}
	}
}

func TestSemanticJudgementJSONSchemaRejectsEmptyEvidenceIndex(t *testing.T) {
	for _, build := range []func(EvidenceIndex) (json.RawMessage, error){
		EditionJudgementJSONSchema,
		BundleJudgementJSONSchema,
	} {
		if _, err := build(EvidenceIndex{}); err == nil {
			t.Fatal("empty EvidenceIndex must fail closed")
		}
	}
}

func TestSemanticJudgementJSONSchemaLeavesV2AssessmentSchemaUnchanged(t *testing.T) {
	var v2 map[string]any
	if err := json.Unmarshal(EditionAssessmentJSONSchema(), &v2); err != nil {
		t.Fatalf("json.Unmarshal(V2 schema) error = %v", err)
	}
	v2Properties := v2["properties"].(map[string]any)
	if got := schemaStringValues(v2Properties["validationVersion"].(map[string]any)["enum"]); !reflect.DeepEqual(got, []string{string(ValidationV2)}) {
		t.Fatalf("V2 validationVersion enum = %v", got)
	}
}

func judgementSchemaTestIndex(t *testing.T) EvidenceIndex {
	t.Helper()
	index, err := BuildEvidenceIndex(
		"Canonical first block.\n\nCanonical second block.",
		evidenceIndexTestAnalysis(),
		[]storygeneration.GeneratedEditionArtifact{{
			EditionKey: model.AdminStoryEditionGrowingReaders,
			Markdown:   "# Growing Readers\n\nGenerated block.",
		}},
	)
	if err != nil {
		t.Fatalf("BuildEvidenceIndex() error = %v", err)
	}
	return index
}

func decodeJudgementSchema(t *testing.T, schema json.RawMessage) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(schema, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(schema) error = %v", err)
	}
	return decoded
}

func judgementSchemaSegmentIDs(t *testing.T, schema json.RawMessage) []string {
	t.Helper()
	properties := decodeJudgementSchema(t, schema)["properties"].(map[string]any)
	finding := properties["findings"].(map[string]any)["items"].(map[string]any)
	evidence := finding["properties"].(map[string]any)["evidence"].(map[string]any)
	item := evidence["items"].(map[string]any)
	return schemaStringValues(item["properties"].(map[string]any)["segmentId"].(map[string]any)["enum"])
}

func assertExactSchemaProperties(t *testing.T, properties map[string]any, want []string) {
	t.Helper()
	if len(properties) != len(want) {
		t.Fatalf("properties = %v, want exactly %v", properties, want)
	}
	for _, name := range want {
		if _, exists := properties[name]; !exists {
			t.Fatalf("schema is missing property %q", name)
		}
	}
}

func schemaStringValues(value any) []string {
	values := value.([]any)
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.(string))
	}
	return result
}
