package storygeneration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

var storyAnalysisTopLevelFields = []string{
	"centralPlot",
	"characters",
	"relationships",
	"coreStoryBeats",
	"developmentBeats",
	"enrichmentMaterial",
	"causalDependencies",
	"iconicMaterial",
	"intenseMaterial",
	"adaptationRisks",
}

var characterFields = []string{
	"name",
	"role",
	"explicitMotivations",
	"flawsOrAmbiguities",
}

var relationshipFields = []string{
	"parties",
	"nature",
	"powerDynamics",
}

var storyBeatFields = []string{
	"summary",
}

var causalDependencyFields = []string{
	"cause",
	"effect",
	"whyItMatters",
}

var iconicMaterialFields = []string{
	"kind",
	"textOrDescription",
	"importance",
}

var intenseMaterialFields = []string{
	"kind",
	"description",
	"narrativeFunction",
}

var adaptationRiskFields = []string{
	"kind",
	"description",
	"whatMustBePreserved",
}

// DecodeStoryAnalysisJSON decodes exactly one StoryAnalysis JSON object.
//
// The machine boundary is deliberately stricter than StoryAnalysis.Validate:
// every contract field must be present, every array must be encoded as an
// array rather than null, duplicate keys are rejected, and unknown fields are
// rejected recursively. Optional source content is represented by [] rather
// than by omitting the field.
func DecodeStoryAnalysisJSON(data []byte) (StoryAnalysis, error) {
	if !utf8.Valid(data) {
		return StoryAnalysis{}, fmt.Errorf("story analysis JSON must be valid UTF-8")
	}
	if err := validateSingleJSONValueWithoutDuplicateKeys(data); err != nil {
		return StoryAnalysis{}, fmt.Errorf("invalid story analysis JSON: %w", err)
	}
	if err := validateStoryAnalysisJSONShape(data); err != nil {
		return StoryAnalysis{}, err
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var analysis StoryAnalysis
	if err := decoder.Decode(&analysis); err != nil {
		return StoryAnalysis{}, fmt.Errorf("invalid StoryAnalysis object: %w", err)
	}
	if err := ensureJSONDecoderEOF(decoder); err != nil {
		return StoryAnalysis{}, err
	}
	if err := analysis.Validate(); err != nil {
		return StoryAnalysis{}, fmt.Errorf("invalid StoryAnalysis object: %w", err)
	}
	return analysis, nil
}

func validateStoryAnalysisJSONShape(data []byte) error {
	top, err := decodeExactJSONObject(data, storyAnalysisTopLevelFields, "StoryAnalysis")
	if err != nil {
		return err
	}

	characters, err := decodeRequiredJSONArray(top["characters"], "characters")
	if err != nil {
		return err
	}
	for index, raw := range characters {
		object, err := decodeExactJSONObject(raw, characterFields, fmt.Sprintf("characters[%d]", index))
		if err != nil {
			return err
		}
		if _, err := decodeRequiredJSONArray(object["explicitMotivations"], fmt.Sprintf("characters[%d].explicitMotivations", index)); err != nil {
			return err
		}
		if _, err := decodeRequiredJSONArray(object["flawsOrAmbiguities"], fmt.Sprintf("characters[%d].flawsOrAmbiguities", index)); err != nil {
			return err
		}
	}

	relationships, err := decodeRequiredJSONArray(top["relationships"], "relationships")
	if err != nil {
		return err
	}
	for index, raw := range relationships {
		object, err := decodeExactJSONObject(raw, relationshipFields, fmt.Sprintf("relationships[%d]", index))
		if err != nil {
			return err
		}
		if _, err := decodeRequiredJSONArray(object["parties"], fmt.Sprintf("relationships[%d].parties", index)); err != nil {
			return err
		}
	}

	if err := validateObjectArray(top["coreStoryBeats"], "coreStoryBeats", storyBeatFields); err != nil {
		return err
	}
	if err := validateObjectArray(top["developmentBeats"], "developmentBeats", storyBeatFields); err != nil {
		return err
	}
	if err := validateObjectArray(top["enrichmentMaterial"], "enrichmentMaterial", storyBeatFields); err != nil {
		return err
	}
	if err := validateObjectArray(top["causalDependencies"], "causalDependencies", causalDependencyFields); err != nil {
		return err
	}
	if err := validateObjectArray(top["iconicMaterial"], "iconicMaterial", iconicMaterialFields); err != nil {
		return err
	}
	if err := validateObjectArray(top["intenseMaterial"], "intenseMaterial", intenseMaterialFields); err != nil {
		return err
	}
	if err := validateObjectArray(top["adaptationRisks"], "adaptationRisks", adaptationRiskFields); err != nil {
		return err
	}

	return nil
}

func validateObjectArray(raw json.RawMessage, label string, fields []string) error {
	items, err := decodeRequiredJSONArray(raw, label)
	if err != nil {
		return err
	}
	for index, item := range items {
		if _, err := decodeExactJSONObject(item, fields, fmt.Sprintf("%s[%d]", label, index)); err != nil {
			return err
		}
	}
	return nil
}

func decodeExactJSONObject(data []byte, expected []string, label string) (map[string]json.RawMessage, error) {
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

	expectedSet := make(map[string]struct{}, len(expected))
	for _, field := range expected {
		expectedSet[field] = struct{}{}
		if _, ok := object[field]; !ok {
			return nil, fmt.Errorf("%s is missing required field %q", label, field)
		}
	}
	for field := range object {
		if _, ok := expectedSet[field]; !ok {
			return nil, fmt.Errorf("%s contains unknown field %q", label, field)
		}
	}
	return object, nil
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
		return fmt.Errorf("story analysis JSON must contain exactly one value")
	}
	return fmt.Errorf("invalid trailing story analysis JSON: %w", err)
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
