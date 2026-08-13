package storyvalidation

import (
	"encoding/json"
	"fmt"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/storygeneration"
)

// EditionJudgementJSONSchema builds the strict V3 Structured Output schema for
// an edition-scoped SemanticJudgement. Evidence references are constrained to
// the supplied deterministic EvidenceIndex.
func EditionJudgementJSONSchema(index EvidenceIndex) (json.RawMessage, error) {
	return semanticJudgementJSONSchema(adaptationcontract.AssessmentScopeEdition, index)
}

// BundleJudgementJSONSchema builds the strict V3 Structured Output schema for
// a bundle-scoped SemanticJudgement. Evidence references are constrained to
// the supplied deterministic EvidenceIndex.
func BundleJudgementJSONSchema(index EvidenceIndex) (json.RawMessage, error) {
	return semanticJudgementJSONSchema(adaptationcontract.AssessmentScopeBundle, index)
}

func semanticJudgementJSONSchema(
	scope adaptationcontract.AssessmentScope,
	index EvidenceIndex,
) (json.RawMessage, error) {
	segmentIDs := index.IDs()
	if len(segmentIDs) == 0 {
		return nil, fmt.Errorf("evidence index must contain at least one segment ID")
	}

	segmentValues := make([]string, 0, len(segmentIDs))
	for _, id := range segmentIDs {
		segmentValues = append(segmentValues, string(id))
	}

	editionValues := make([]string, 0, len(storygeneration.DerivedEditionKeysV2()))
	for _, key := range storygeneration.DerivedEditionKeysV2() {
		editionValues = append(editionValues, string(key))
	}

	evidenceReferenceSchema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"segmentId": map[string]any{
				"type": "string",
				"enum": segmentValues,
			},
			"explanation": map[string]any{
				"type": "string",
			},
		},
		"required": []string{"segmentId", "explanation"},
	}

	findingSchema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			// Finding code and severity remain governed by the PR91 runtime
			// semantic contract; this schema intentionally leaves them as strings.
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
				"items":    evidenceReferenceSchema,
			},
		},
		"required": []string{"code", "severity", "message", "evidence"},
	}

	properties := map[string]any{
		"validationVersion": map[string]any{
			"type": "string",
			"enum": []string{string(ValidationV3)},
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
		required = []string{
			"validationVersion",
			"specificationVersion",
			"assessmentScope",
			"editionKey",
			"result",
			"findings",
		}
	case adaptationcontract.AssessmentScopeBundle:
		properties["editionKeys"] = map[string]any{
			"type":     "array",
			"minItems": 2,
			"items": map[string]any{
				"type": "string",
				"enum": editionValues,
			},
		}
		required = []string{
			"validationVersion",
			"specificationVersion",
			"assessmentScope",
			"editionKeys",
			"result",
			"findings",
		}
	default:
		return nil, fmt.Errorf("unsupported semantic judgement scope %q", scope)
	}

	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
		"required":             required,
	}

	encoded, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("marshal semantic judgement schema: %w", err)
	}
	return encoded, nil
}
