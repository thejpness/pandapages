package storygeneration

import "encoding/json"

// StoryAnalysisJSONSchema returns the strict Structured Outputs schema for the
// source-analysis call. The schema intentionally uses only structural JSON
// constraints; semantic/cardinality invariants are enforced again by
// DecodeStoryAnalysisJSON and StoryAnalysis.Validate.
func StoryAnalysisJSONSchema() json.RawMessage {
	return append(json.RawMessage(nil), storyAnalysisJSONSchema...)
}

var storyAnalysisJSONSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "centralPlot": {"type": "string"},
    "characters": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "properties": {
          "name": {"type": "string"},
          "role": {"type": "string"},
          "explicitMotivations": {"type": "array", "items": {"type": "string"}},
          "flawsOrAmbiguities": {"type": "array", "items": {"type": "string"}}
        },
        "required": ["name", "role", "explicitMotivations", "flawsOrAmbiguities"]
      }
    },
    "relationships": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "properties": {
          "parties": {"type": "array", "items": {"type": "string"}},
          "nature": {"type": "string"},
          "powerDynamics": {"type": "string"}
        },
        "required": ["parties", "nature", "powerDynamics"]
      }
    },
    "coreStoryBeats": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "properties": {"summary": {"type": "string"}},
        "required": ["summary"]
      }
    },
    "developmentBeats": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "properties": {"summary": {"type": "string"}},
        "required": ["summary"]
      }
    },
    "enrichmentMaterial": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "properties": {"summary": {"type": "string"}},
        "required": ["summary"]
      }
    },
    "causalDependencies": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "properties": {
          "cause": {"type": "string"},
          "effect": {"type": "string"},
          "whyItMatters": {"type": "string"}
        },
        "required": ["cause", "effect", "whyItMatters"]
      }
    },
    "iconicMaterial": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "properties": {
          "kind": {"type": "string"},
          "textOrDescription": {"type": "string"},
          "importance": {"type": "string"}
        },
        "required": ["kind", "textOrDescription", "importance"]
      }
    },
    "intenseMaterial": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "properties": {
          "kind": {"type": "string", "enum": ["frightening", "violence", "death", "injury"]},
          "description": {"type": "string"},
          "narrativeFunction": {"type": "string"}
        },
        "required": ["kind", "description", "narrativeFunction"]
      }
    },
    "adaptationRisks": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "properties": {
          "kind": {
            "type": "string",
            "enum": ["motivation", "causality", "ownership", "bargain", "power_relationship", "story_identity", "other"]
          },
          "description": {"type": "string"},
          "whatMustBePreserved": {"type": "string"}
        },
        "required": ["kind", "description", "whatMustBePreserved"]
      }
    }
  },
  "required": [
    "centralPlot",
    "characters",
    "relationships",
    "coreStoryBeats",
    "developmentBeats",
    "enrichmentMaterial",
    "causalDependencies",
    "iconicMaterial",
    "intenseMaterial",
    "adaptationRisks"
  ]
}`)
