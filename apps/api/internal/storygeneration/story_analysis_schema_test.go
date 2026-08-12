package storygeneration

import (
	"encoding/json"
	"testing"
)

func TestStoryAnalysisJSONSchemaIsValidStrictObjectSchema(t *testing.T) {
	schema := StoryAnalysisJSONSchema()
	if !json.Valid(schema) {
		t.Fatal("StoryAnalysisJSONSchema() must return valid JSON")
	}

	var decoded map[string]any
	if err := json.Unmarshal(schema, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(schema) error = %v", err)
	}
	if decoded["type"] != "object" {
		t.Fatalf("schema type = %v, want object", decoded["type"])
	}
	if decoded["additionalProperties"] != false {
		t.Fatalf("additionalProperties = %v, want false", decoded["additionalProperties"])
	}

	required, ok := decoded["required"].([]any)
	if !ok {
		t.Fatal("schema required must be an array")
	}
	if len(required) != len(storyAnalysisTopLevelFields) {
		t.Fatalf("required length = %d, want %d", len(required), len(storyAnalysisTopLevelFields))
	}

	properties, ok := decoded["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema properties must be an object")
	}
	for _, field := range storyAnalysisTopLevelFields {
		if _, exists := properties[field]; !exists {
			t.Fatalf("schema is missing property %q", field)
		}
	}
}

func TestStoryAnalysisJSONSchemaReturnsDefensiveCopy(t *testing.T) {
	first := StoryAnalysisJSONSchema()
	first[0] = '['

	second := StoryAnalysisJSONSchema()
	if second[0] != '{' {
		t.Fatal("schema caller must not be able to mutate the canonical schema")
	}
}

func TestStoryAnalysisJSONSchemaLocksFiniteRiskAndIntensityEnums(t *testing.T) {
	var decoded struct {
		Properties map[string]struct {
			Items struct {
				Properties map[string]struct {
					Enum []string `json:"enum"`
				} `json:"properties"`
			} `json:"items"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(StoryAnalysisJSONSchema(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(schema) error = %v", err)
	}

	intense := decoded.Properties["intenseMaterial"].Items.Properties["kind"].Enum
	if len(intense) != 4 {
		t.Fatalf("intenseMaterial kind enum = %v", intense)
	}

	risks := decoded.Properties["adaptationRisks"].Items.Properties["kind"].Enum
	if len(risks) != 7 {
		t.Fatalf("adaptationRisks kind enum = %v", risks)
	}
}
