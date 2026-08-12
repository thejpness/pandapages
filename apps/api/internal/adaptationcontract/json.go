package adaptationcontract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

var editionAssessmentJSONFields = map[string]struct{}{
	"contractVersion": {},
	"assessmentScope": {},
	"editionKey":      {},
	"result":          {},
	"findings":        {},
}

var bundleAssessmentJSONFields = map[string]struct{}{
	"contractVersion": {},
	"assessmentScope": {},
	"editionKeys":     {},
	"result":          {},
	"findings":        {},
}

// DecodeAssessmentJSON decodes one exact machine-readable assessment envelope.
// It rejects invalid UTF-8, duplicate object keys, unknown or missing top-level
// fields, unknown nested fields, trailing JSON values, and any envelope that
// fails the adaptation-contract invariants.
func DecodeAssessmentJSON(data []byte) (Assessment, error) {
	return decodeAssessmentJSON(data, false)
}

// DecodeSemanticAssessmentJSON applies the same strict JSON boundary and also
// rejects deterministic structural findings. This is the decoder intended for
// future semantic-assessor output.
func DecodeSemanticAssessmentJSON(data []byte) (Assessment, error) {
	return decodeAssessmentJSON(data, true)
}

func decodeAssessmentJSON(data []byte, semantic bool) (Assessment, error) {
	if !utf8.Valid(data) {
		return Assessment{}, fmt.Errorf("assessment JSON must be valid UTF-8")
	}
	if err := validateSingleJSONValueWithoutDuplicateKeys(data); err != nil {
		return Assessment{}, fmt.Errorf("invalid assessment JSON: %w", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return Assessment{}, fmt.Errorf("assessment JSON must be an object: %w", err)
	}
	if raw == nil {
		return Assessment{}, fmt.Errorf("assessment JSON must be an object")
	}

	scopeRaw, ok := raw["assessmentScope"]
	if !ok {
		return Assessment{}, fmt.Errorf("assessment JSON is missing required field %q", "assessmentScope")
	}
	var scope AssessmentScope
	if err := json.Unmarshal(scopeRaw, &scope); err != nil {
		return Assessment{}, fmt.Errorf("assessmentScope must be a string")
	}

	switch scope {
	case AssessmentScopeEdition:
		if err := requireExactJSONFields(raw, editionAssessmentJSONFields); err != nil {
			return Assessment{}, err
		}
	case AssessmentScopeBundle:
		if err := requireExactJSONFields(raw, bundleAssessmentJSONFields); err != nil {
			return Assessment{}, err
		}
	default:
		return Assessment{}, fmt.Errorf("unsupported assessment scope %q", scope)
	}

	if !isJSONArray(raw["findings"]) {
		return Assessment{}, fmt.Errorf("findings must be a JSON array")
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var assessment Assessment
	if err := decoder.Decode(&assessment); err != nil {
		return Assessment{}, fmt.Errorf("invalid assessment envelope: %w", err)
	}
	if err := ensureDecoderEOF(decoder); err != nil {
		return Assessment{}, err
	}

	var err error
	if semantic {
		err = assessment.ValidateSemantic()
	} else {
		err = assessment.Validate()
	}
	if err != nil {
		return Assessment{}, fmt.Errorf("invalid assessment envelope: %w", err)
	}
	return assessment, nil
}

func requireExactJSONFields(raw map[string]json.RawMessage, expected map[string]struct{}) error {
	for key := range expected {
		if _, ok := raw[key]; !ok {
			return fmt.Errorf("assessment JSON is missing required field %q", key)
		}
	}
	for key := range raw {
		if _, ok := expected[key]; !ok {
			return fmt.Errorf("assessment JSON contains unknown field %q", key)
		}
	}
	return nil
}

func isJSONArray(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '['
}

func ensureDecoderEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return fmt.Errorf("assessment JSON must contain exactly one value")
	}
	return fmt.Errorf("invalid trailing assessment JSON: %w", err)
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
