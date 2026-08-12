package storyvalidation

import (
	"encoding/json"
	"fmt"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
	"pandapages/api/internal/storygeneration"
)

func EditionAssessmentJSONSchema() json.RawMessage {
	return semanticAssessmentJSONSchema(adaptationcontract.AssessmentScopeEdition)
}

func BundleAssessmentJSONSchema() json.RawMessage {
	return semanticAssessmentJSONSchema(adaptationcontract.AssessmentScopeBundle)
}

func semanticAssessmentJSONSchema(scope adaptationcontract.AssessmentScope) json.RawMessage {
	editionValues := make([]any, 0, len(storygeneration.DerivedEditionKeysV2()))
	for _, key := range storygeneration.DerivedEditionKeysV2() {
		editionValues = append(editionValues, string(key))
	}

	evidenceEditionValues := append([]any{nil}, editionValues...)

	evidenceSchema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"location": map[string]any{
				"type": "string",
				"enum": []string{
					string(EvidenceCanonicalSource),
					string(EvidenceStoryAnalysis),
					string(EvidenceGeneratedEdition),
				},
			},
			"editionKey": map[string]any{
				"type": []string{"string", "null"},
				"enum": evidenceEditionValues,
			},
			"excerpt": map[string]any{
				"type": "string",
			},
			"explanation": map[string]any{
				"type": "string",
			},
		},
		"required": evidenceJSONFields,
	}

	findingSchema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			// Finding code and severity intentionally remain strings here.
			// PR91 owns the canonical taxonomy, scope rules, and severity
			// mapping; DecodeAssessmentJSON applies those rules after decode
			// rather than duplicating the taxonomy in this schema.
			"code": map[string]any{
				"type": "string",
			},
			"severity": map[string]any{
				"type": "string",
			},
			"message": map[string]any{
				"type": "string",
			},
			"evidence": map[string]any{
				"type":     "array",
				"minItems": 1,
				"items":    evidenceSchema,
			},
		},
		"required": findingJSONFields,
	}

	properties := map[string]any{
		"validationVersion": map[string]any{
			"type": "string",
			"enum": []string{string(ValidationV2)},
		},
		"specificationVersion": map[string]any{
			"type": "string",
			"enum": []string{string(storygeneration.SpecificationV2)},
		},
		"assessmentScope": map[string]any{
			"type": "string",
			"enum": []string{string(scope)},
		},
		"result": map[string]any{
			"type": "string",
			"enum": []string{
				string(adaptationcontract.ResultPass),
				string(adaptationcontract.ResultNeedsReview),
				string(adaptationcontract.ResultFail),
			},
		},
		"findings": map[string]any{
			"type":  "array",
			"items": findingSchema,
		},
	}

	var required []string
	switch scope {
	case adaptationcontract.AssessmentScopeEdition:
		properties["editionKey"] = map[string]any{
			"type": "string",
			"enum": editionValues,
		}
		required = editionAssessmentJSONFields
	case adaptationcontract.AssessmentScopeBundle:
		properties["editionKeys"] = map[string]any{
			"type":     "array",
			"minItems": 2,
			"items": map[string]any{
				"type": "string",
				"enum": editionValues,
			},
		}
		required = bundleAssessmentJSONFields
	default:
		panic(fmt.Sprintf("unsupported semantic assessment scope %q", scope))
	}

	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
		"required":             required,
	}

	encoded, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("marshal semantic assessment schema: %v", err))
	}
	return encoded
}

// compile-time use of model keeps the schema's edition enum tied to the
// canonical AdminStoryEditionKey type rather than a separate local vocabulary.
var _ model.AdminStoryEditionKey
