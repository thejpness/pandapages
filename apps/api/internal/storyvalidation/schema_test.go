package storyvalidation

import (
	"encoding/json"
	"testing"

	"pandapages/api/internal/adaptationcontract"
)

func TestSemanticAssessmentJSONSchemasAreValidStrictObjects(t *testing.T) {
	tests := []struct {
		name      string
		schema    json.RawMessage
		scope     string
		targetKey string
	}{
		{
			name:      "edition",
			schema:    EditionAssessmentJSONSchema(),
			scope:     string(adaptationcontract.AssessmentScopeEdition),
			targetKey: "editionKey",
		},
		{
			name:      "bundle",
			schema:    BundleAssessmentJSONSchema(),
			scope:     string(adaptationcontract.AssessmentScopeBundle),
			targetKey: "editionKeys",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !json.Valid(test.schema) {
				t.Fatal("schema must be valid JSON")
			}

			var decoded map[string]any
			if err := json.Unmarshal(test.schema, &decoded); err != nil {
				t.Fatalf("json.Unmarshal(schema) error = %v", err)
			}
			if decoded["type"] != "object" {
				t.Fatalf("type = %v", decoded["type"])
			}
			if decoded["additionalProperties"] != false {
				t.Fatalf("additionalProperties = %v", decoded["additionalProperties"])
			}

			properties := decoded["properties"].(map[string]any)
			scope := properties["assessmentScope"].(map[string]any)
			enum := scope["enum"].([]any)
			if len(enum) != 1 || enum[0] != test.scope {
				t.Fatalf("scope enum = %v", enum)
			}
			if _, ok := properties[test.targetKey]; !ok {
				t.Fatalf("schema missing target property %q", test.targetKey)
			}
		})
	}
}

func TestSemanticAssessmentSchemaDoesNotDuplicatePR91FindingTaxonomy(t *testing.T) {
	var decoded map[string]any
	if err := json.Unmarshal(EditionAssessmentJSONSchema(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(schema) error = %v", err)
	}

	properties := decoded["properties"].(map[string]any)
	findings := properties["findings"].(map[string]any)
	items := findings["items"].(map[string]any)
	findingProperties := items["properties"].(map[string]any)

	code := findingProperties["code"].(map[string]any)
	if _, exists := code["enum"]; exists {
		t.Fatal("finding-code enum must remain owned by PR91 runtime validation")
	}
	severity := findingProperties["severity"].(map[string]any)
	if _, exists := severity["enum"]; exists {
		t.Fatal("finding-severity mapping must remain owned by PR91 runtime validation")
	}
}

func TestEvidenceSchemaRequiresStableEditionKeyField(t *testing.T) {
	var decoded map[string]any
	if err := json.Unmarshal(EditionAssessmentJSONSchema(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(schema) error = %v", err)
	}

	properties := decoded["properties"].(map[string]any)
	findings := properties["findings"].(map[string]any)
	findingItems := findings["items"].(map[string]any)
	findingProperties := findingItems["properties"].(map[string]any)
	evidence := findingProperties["evidence"].(map[string]any)
	evidenceItems := evidence["items"].(map[string]any)

	required := evidenceItems["required"].([]any)
	found := false
	for _, value := range required {
		if value == "editionKey" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("evidence schema must require editionKey")
	}

	evidenceProperties := evidenceItems["properties"].(map[string]any)
	editionKey := evidenceProperties["editionKey"].(map[string]any)
	types := editionKey["type"].([]any)

	hasString := false
	hasNull := false
	for _, value := range types {
		if value == "string" {
			hasString = true
		}
		if value == "null" {
			hasNull = true
		}
	}
	if !hasString || !hasNull {
		t.Fatalf("evidence editionKey types = %v", types)
	}
}

func TestSemanticAssessmentSchemasUseCanonicalEditionOrder(t *testing.T) {
	var decoded map[string]any
	if err := json.Unmarshal(BundleAssessmentJSONSchema(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(schema) error = %v", err)
	}

	properties := decoded["properties"].(map[string]any)
	editionKeys := properties["editionKeys"].(map[string]any)
	items := editionKeys["items"].(map[string]any)
	enum := items["enum"].([]any)

	want := []any{
		"confident-readers",
		"growing-readers",
		"story-explorers",
		"little-listeners",
	}
	if len(enum) != len(want) {
		t.Fatalf("edition enum = %v", enum)
	}
	for index := range want {
		if enum[index] != want[index] {
			t.Fatalf("edition enum = %v, want %v", enum, want)
		}
	}
}
