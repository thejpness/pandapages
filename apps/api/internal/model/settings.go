package model

import "encoding/json"

type ChildProfile struct {
	ID            string   `json:"id,omitempty"`
	Name          string   `json:"name"`
	AgeMonths     int      `json:"ageMonths"`
	Interests     []string `json:"interests"`
	Sensitivities []string `json:"sensitivities"`
}

type PromptProfile struct {
	ID            string          `json:"id,omitempty"`
	Name          string          `json:"name"`
	SchemaVersion int             `json:"schemaVersion"`
	Rules         json.RawMessage `json:"rules"`
}

type SettingsPayload struct {
	Child  ChildProfile  `json:"child"`
	Prompt PromptProfile `json:"prompt"`
}

type SettingsUpsert struct {
	Child  ChildProfile  `json:"child"`
	Prompt PromptProfile `json:"prompt"`
}
