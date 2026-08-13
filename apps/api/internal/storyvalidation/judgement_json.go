package storyvalidation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"pandapages/api/internal/adaptationcontract"
)

var editionJudgementJSONFields = []string{
	"validationVersion",
	"specificationVersion",
	"assessmentScope",
	"editionKey",
	"result",
	"findings",
}

var bundleJudgementJSONFields = []string{
	"validationVersion",
	"specificationVersion",
	"assessmentScope",
	"editionKeys",
	"result",
	"findings",
}

var judgementFindingJSONFields = []string{
	"code",
	"severity",
	"message",
	"evidence",
}

var evidenceReferenceJSONFields = []string{
	"segmentId",
	"explanation",
}

// DecodeSemanticJudgementJSON decodes exactly one model-facing v3 semantic
// judgement. It applies the same strict JSON boundary as final assessments:
// valid UTF-8, no duplicate keys, no unknown/missing fields, no null arrays,
// no trailing values, and full semantic-contract validation.
func DecodeSemanticJudgementJSON(data []byte) (SemanticJudgement, error) {
	if !utf8.Valid(data) {
		return SemanticJudgement{}, fmt.Errorf(
			"semantic judgement JSON must be valid UTF-8",
		)
	}
	if err := validateSingleJSONValueWithoutDuplicateKeys(data); err != nil {
		return SemanticJudgement{}, fmt.Errorf(
			"invalid semantic judgement JSON: %w",
			err,
		)
	}

	top, err := decodeJSONObject(data, "semantic judgement")
	if err != nil {
		return SemanticJudgement{}, err
	}

	scopeRaw, ok := top["assessmentScope"]
	if !ok {
		return SemanticJudgement{}, fmt.Errorf(
			"semantic judgement is missing required field %q",
			"assessmentScope",
		)
	}

	var scope adaptationcontract.AssessmentScope
	if err := json.Unmarshal(scopeRaw, &scope); err != nil {
		return SemanticJudgement{}, fmt.Errorf("assessmentScope must be a string")
	}

	switch scope {
	case adaptationcontract.AssessmentScopeEdition:
		if err := requireExactJSONFields(
			top,
			editionJudgementJSONFields,
			"semantic judgement",
		); err != nil {
			return SemanticJudgement{}, err
		}
	case adaptationcontract.AssessmentScopeBundle:
		if err := requireExactJSONFields(
			top,
			bundleJudgementJSONFields,
			"semantic judgement",
		); err != nil {
			return SemanticJudgement{}, err
		}
	default:
		return SemanticJudgement{}, fmt.Errorf(
			"unsupported assessment scope %q",
			scope,
		)
	}

	findings, err := decodeRequiredJSONArray(top["findings"], "findings")
	if err != nil {
		return SemanticJudgement{}, err
	}

	for findingIndex, rawFinding := range findings {
		label := fmt.Sprintf("findings[%d]", findingIndex)

		finding, err := decodeJSONObject(rawFinding, label)
		if err != nil {
			return SemanticJudgement{}, err
		}
		if err := requireExactJSONFields(
			finding,
			judgementFindingJSONFields,
			label,
		); err != nil {
			return SemanticJudgement{}, err
		}

		evidenceItems, err := decodeRequiredJSONArray(
			finding["evidence"],
			label+".evidence",
		)
		if err != nil {
			return SemanticJudgement{}, err
		}

		for evidenceIndex, rawEvidence := range evidenceItems {
			evidenceLabel := fmt.Sprintf(
				"%s.evidence[%d]",
				label,
				evidenceIndex,
			)

			evidence, err := decodeJSONObject(rawEvidence, evidenceLabel)
			if err != nil {
				return SemanticJudgement{}, err
			}
			if err := requireExactJSONFields(
				evidence,
				evidenceReferenceJSONFields,
				evidenceLabel,
			); err != nil {
				return SemanticJudgement{}, err
			}
		}
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var judgement SemanticJudgement
	if err := decoder.Decode(&judgement); err != nil {
		return SemanticJudgement{}, fmt.Errorf(
			"invalid semantic judgement object: %w",
			err,
		)
	}
	if err := ensureJSONDecoderEOF(decoder); err != nil {
		return SemanticJudgement{}, err
	}
	if err := judgement.Validate(); err != nil {
		return SemanticJudgement{}, fmt.Errorf(
			"invalid semantic judgement object: %w",
			err,
		)
	}

	return judgement, nil
}
