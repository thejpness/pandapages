package storyvalidation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"

	"pandapages/api/internal/adaptationcontract"
)

var editionAssessmentJSONFields = []string{
	"validationVersion",
	"specificationVersion",
	"assessmentScope",
	"editionKey",
	"result",
	"findings",
}

var bundleAssessmentJSONFields = []string{
	"validationVersion",
	"specificationVersion",
	"assessmentScope",
	"editionKeys",
	"result",
	"findings",
}

var findingJSONFields = []string{
	"code",
	"severity",
	"message",
	"evidence",
}

var evidenceJSONFields = []string{
	"location",
	"editionKey",
	"excerpt",
	"explanation",
}

// DecodeAssessmentJSON decodes exactly one evidence-bearing semantic
// assessment. It rejects invalid UTF-8, duplicate keys, unknown or missing
// fields, null arrays, trailing JSON values, and any decoded object that fails
// the semantic-validation contract.
func DecodeAssessmentJSON(data []byte) (Assessment, error) {
	if !utf8.Valid(data) {
		return Assessment{}, fmt.Errorf("semantic assessment JSON must be valid UTF-8")
	}
	if err := validateSingleJSONValueWithoutDuplicateKeys(data); err != nil {
		return Assessment{}, fmt.Errorf("invalid semantic assessment JSON: %w", err)
	}

	top, err := decodeJSONObject(data, "semantic assessment")
	if err != nil {
		return Assessment{}, err
	}

	scopeRaw, ok := top["assessmentScope"]
	if !ok {
		return Assessment{}, fmt.Errorf("semantic assessment is missing required field %q", "assessmentScope")
	}
	var scope adaptationcontract.AssessmentScope
	if err := json.Unmarshal(scopeRaw, &scope); err != nil {
		return Assessment{}, fmt.Errorf("assessmentScope must be a string")
	}

	switch scope {
	case adaptationcontract.AssessmentScopeEdition:
		if err := requireExactJSONFields(top, editionAssessmentJSONFields, "semantic assessment"); err != nil {
			return Assessment{}, err
		}
	case adaptationcontract.AssessmentScopeBundle:
		if err := requireExactJSONFields(top, bundleAssessmentJSONFields, "semantic assessment"); err != nil {
			return Assessment{}, err
		}
	default:
		return Assessment{}, fmt.Errorf("unsupported assessment scope %q", scope)
	}

	findings, err := decodeRequiredJSONArray(top["findings"], "findings")
	if err != nil {
		return Assessment{}, err
	}
	for findingIndex, rawFinding := range findings {
		label := fmt.Sprintf("findings[%d]", findingIndex)
		finding, err := decodeJSONObject(rawFinding, label)
		if err != nil {
			return Assessment{}, err
		}
		if err := requireExactJSONFields(finding, findingJSONFields, label); err != nil {
			return Assessment{}, err
		}

		evidenceItems, err := decodeRequiredJSONArray(finding["evidence"], label+".evidence")
		if err != nil {
			return Assessment{}, err
		}
		for evidenceIndex, rawEvidence := range evidenceItems {
			evidenceLabel := fmt.Sprintf("%s.evidence[%d]", label, evidenceIndex)
			evidence, err := decodeJSONObject(rawEvidence, evidenceLabel)
			if err != nil {
				return Assessment{}, err
			}
			if err := requireExactJSONFields(evidence, evidenceJSONFields, evidenceLabel); err != nil {
				return Assessment{}, err
			}
		}
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var assessment Assessment
	if err := decoder.Decode(&assessment); err != nil {
		return Assessment{}, fmt.Errorf("invalid semantic assessment object: %w", err)
	}
	if err := ensureJSONDecoderEOF(decoder); err != nil {
		return Assessment{}, err
	}
	if err := assessment.Validate(); err != nil {
		return Assessment{}, fmt.Errorf("invalid semantic assessment object: %w", err)
	}
	return assessment, nil
}

func decodeJSONObject(data []byte, label string) (map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, fmt.Errorf("%s must be a JSON object", label)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &object); err != nil {
		return nil, fmt.Errorf("%s must be a JSON object: %w", label, err)
	}
	if object == nil {
		return nil, fmt.Errorf("%s must be a JSON object", label)
	}
	return object, nil
}

func requireExactJSONFields(object map[string]json.RawMessage, expected []string, label string) error {
	expectedSet := make(map[string]struct{}, len(expected))
	for _, field := range expected {
		expectedSet[field] = struct{}{}
		if _, ok := object[field]; !ok {
			return fmt.Errorf("%s is missing required field %q", label, field)
		}
	}
	for field := range object {
		if _, ok := expectedSet[field]; !ok {
			return fmt.Errorf("%s contains unknown field %q", label, field)
		}
	}
	return nil
}

func decodeRequiredJSONArray(raw json.RawMessage, label string) ([]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, fmt.Errorf("%s must be a JSON array", label)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(trimmed, &items); err != nil {
		return nil, fmt.Errorf("%s must be a JSON array: %w", label, err)
	}
	if items == nil {
		return nil, fmt.Errorf("%s must be a JSON array", label)
	}
	return items, nil
}

func ensureJSONDecoderEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return fmt.Errorf("semantic assessment JSON must contain exactly one value")
	}
	return fmt.Errorf("invalid trailing semantic assessment JSON: %w", err)
}

func validateSingleJSONValueWithoutDuplicateKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	if err := walkJSONValue(decoder); err != nil {
		return err
	}

	token, err := decoder.Token()
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("unexpected trailing JSON token %v", token)
}

func walkJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}

	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}

	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object key must be a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate object key %q", key)
			}
			seen[key] = struct{}{}
			if err := walkJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim('}') {
			return fmt.Errorf("object did not end correctly")
		}
	case '[':
		for decoder.More() {
			if err := walkJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim(']') {
			return fmt.Errorf("array did not end correctly")
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delim)
	}
	return nil
}
