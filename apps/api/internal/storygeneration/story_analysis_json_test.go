package storygeneration

import (
	"encoding/json"
	"strings"
	"testing"
)

func validStoryAnalysisJSON(t *testing.T) []byte {
	t.Helper()
	data, err := json.Marshal(validStoryAnalysis())
	if err != nil {
		t.Fatalf("json.Marshal(validStoryAnalysis()) error = %v", err)
	}
	return data
}

func mutateStoryAnalysisJSON(t *testing.T, mutate func(map[string]json.RawMessage)) []byte {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(validStoryAnalysisJSON(t), &object); err != nil {
		t.Fatalf("json.Unmarshal(validStoryAnalysisJSON()) error = %v", err)
	}
	mutate(object)
	data, err := json.Marshal(object)
	if err != nil {
		t.Fatalf("json.Marshal(mutated StoryAnalysis) error = %v", err)
	}
	return data
}

func TestDecodeStoryAnalysisJSONAcceptsExactObject(t *testing.T) {
	data := validStoryAnalysisJSON(t)

	analysis, err := DecodeStoryAnalysisJSON(data)
	if err != nil {
		t.Fatalf("DecodeStoryAnalysisJSON() error = %v", err)
	}
	if analysis.CentralPlot != validStoryAnalysis().CentralPlot {
		t.Fatalf("central plot = %q", analysis.CentralPlot)
	}
}

func TestDecodeStoryAnalysisJSONAcceptsExplicitEmptyOptionalArrays(t *testing.T) {
	analysis := validStoryAnalysis()
	analysis.Relationships = []Relationship{}
	analysis.DevelopmentBeats = []StoryBeat{}
	analysis.EnrichmentMaterial = []StoryBeat{}
	analysis.CausalDependencies = []CausalDependency{}
	analysis.IconicMaterial = []IconicMaterial{}
	analysis.IntenseMaterial = []IntenseMaterial{}
	analysis.AdaptationRisks = []AdaptationRisk{}
	analysis.Characters[0].ExplicitMotivations = []string{}
	analysis.Characters[0].FlawsOrAmbiguities = []string{}

	data, err := json.Marshal(analysis)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if _, err := DecodeStoryAnalysisJSON(data); err != nil {
		t.Fatalf("DecodeStoryAnalysisJSON() error = %v", err)
	}
}

func TestDecodeStoryAnalysisJSONRejectsNonExactBoundary(t *testing.T) {
	base := string(validStoryAnalysisJSON(t))

	tests := []struct {
		name string
		data []byte
		want string
	}{
		{
			name: "invalid utf8",
			data: append([]byte(`{"centralPlot":"`), 0xff),
			want: "valid UTF-8",
		},
		{
			name: "not object",
			data: []byte(`[]`),
			want: "StoryAnalysis must be a JSON object",
		},
		{
			name: "unknown top level field",
			data: []byte(strings.Replace(base, `"centralPlot":`, `"confidence":0.9,"centralPlot":`, 1)),
			want: `unknown field "confidence"`,
		},
		{
			name: "missing top level field",
			data: mutateStoryAnalysisJSON(t, func(object map[string]json.RawMessage) {
				delete(object, "relationships")
			}),
			want: `missing required field "relationships"`,
		},
		{
			name: "null optional array",
			data: mutateStoryAnalysisJSON(t, func(object map[string]json.RawMessage) {
				object["relationships"] = json.RawMessage("null")
			}),
			want: "relationships must be a JSON array",
		},
		{
			name: "unknown nested character field",
			data: []byte(strings.Replace(base, `"name":"Jack"`, `"name":"Jack","invented":true`, 1)),
			want: `contains unknown field "invented"`,
		},
		{
			name: "missing nested character field",
			data: []byte(strings.Replace(base, `"role":"protagonist",`, "", 1)),
			want: `missing required field "role"`,
		},
		{
			name: "null nested array",
			data: []byte(strings.Replace(base, `"explicitMotivations":["Improve his and his mother's poverty"]`, `"explicitMotivations":null`, 1)),
			want: "explicitMotivations must be a JSON array",
		},
		{
			name: "duplicate top level key",
			data: []byte(strings.Replace(base, `"centralPlot":`, `"centralPlot":"duplicate","centralPlot":`, 1)),
			want: `duplicate object key "centralPlot"`,
		},
		{
			name: "duplicate nested key",
			data: []byte(strings.Replace(base, `"name":"Jack"`, `"name":"Jack","name":"Duplicate Jack"`, 1)),
			want: `duplicate object key "name"`,
		},
		{
			name: "trailing value",
			data: append([]byte(base), []byte(` {}`)...),
			want: "trailing",
		},
		{
			name: "invalid analysis invariant",
			data: mutateStoryAnalysisJSON(t, func(object map[string]json.RawMessage) {
				object["centralPlot"] = json.RawMessage(`"   "`)
			}),
			want: "central plot is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeStoryAnalysisJSON(test.data)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("DecodeStoryAnalysisJSON() error = %v, want substring %q", err, test.want)
			}
		})
	}
}
