package storygeneration

import (
	"fmt"
	"strings"
)

type StoryAnalysis struct {
	CentralPlot        string             `json:"centralPlot"`
	Characters         []Character        `json:"characters"`
	Relationships      []Relationship     `json:"relationships"`
	CoreStoryBeats     []StoryBeat        `json:"coreStoryBeats"`
	DevelopmentBeats   []StoryBeat        `json:"developmentBeats"`
	EnrichmentMaterial []StoryBeat        `json:"enrichmentMaterial"`
	CausalDependencies []CausalDependency `json:"causalDependencies"`
	IconicMaterial     []IconicMaterial   `json:"iconicMaterial"`
	IntenseMaterial    []IntenseMaterial  `json:"intenseMaterial"`
	AdaptationRisks    []AdaptationRisk   `json:"adaptationRisks"`
}

type Character struct {
	Name                string   `json:"name"`
	Role                string   `json:"role"`
	ExplicitMotivations []string `json:"explicitMotivations"`
	FlawsOrAmbiguities  []string `json:"flawsOrAmbiguities"`
}

type Relationship struct {
	Parties       []string `json:"parties"`
	Nature        string   `json:"nature"`
	PowerDynamics string   `json:"powerDynamics"`
}

type StoryBeat struct {
	Summary string `json:"summary"`
}

type CausalDependency struct {
	Cause        string `json:"cause"`
	Effect       string `json:"effect"`
	WhyItMatters string `json:"whyItMatters"`
}

type IconicMaterial struct {
	Kind              string `json:"kind"`
	TextOrDescription string `json:"textOrDescription"`
	Importance        string `json:"importance"`
}

type IntenseMaterialKind string

const (
	IntenseMaterialFrightening IntenseMaterialKind = "frightening"
	IntenseMaterialViolence    IntenseMaterialKind = "violence"
	IntenseMaterialDeath       IntenseMaterialKind = "death"
	IntenseMaterialInjury      IntenseMaterialKind = "injury"
)

type IntenseMaterial struct {
	Kind              IntenseMaterialKind `json:"kind"`
	Description       string              `json:"description"`
	NarrativeFunction string              `json:"narrativeFunction"`
}

type AdaptationRiskKind string

const (
	AdaptationRiskMotivation        AdaptationRiskKind = "motivation"
	AdaptationRiskCausality         AdaptationRiskKind = "causality"
	AdaptationRiskOwnership         AdaptationRiskKind = "ownership"
	AdaptationRiskBargain           AdaptationRiskKind = "bargain"
	AdaptationRiskPowerRelationship AdaptationRiskKind = "power_relationship"
	AdaptationRiskStoryIdentity     AdaptationRiskKind = "story_identity"
	AdaptationRiskOther             AdaptationRiskKind = "other"
)

type AdaptationRisk struct {
	Kind                AdaptationRiskKind `json:"kind"`
	Description         string             `json:"description"`
	WhatMustBePreserved string             `json:"whatMustBePreserved"`
}

func (analysis StoryAnalysis) Validate() error {
	if strings.TrimSpace(analysis.CentralPlot) == "" {
		return fmt.Errorf("central plot is required")
	}
	if len(analysis.Characters) == 0 {
		return fmt.Errorf("at least one character is required")
	}
	if len(analysis.CoreStoryBeats) == 0 {
		return fmt.Errorf("at least one core story beat is required")
	}

	characters := make(map[string]struct{}, len(analysis.Characters))
	for index, character := range analysis.Characters {
		name := strings.TrimSpace(character.Name)
		if name == "" {
			return fmt.Errorf("character %d: name is required", index+1)
		}
		if strings.TrimSpace(character.Role) == "" {
			return fmt.Errorf("character %d: role is required", index+1)
		}
		key := strings.ToLower(name)
		if _, exists := characters[key]; exists {
			return fmt.Errorf("character %d: duplicate character name %q", index+1, name)
		}
		characters[key] = struct{}{}
		if err := validateStringList(character.ExplicitMotivations, fmt.Sprintf("character %d explicit motivation", index+1)); err != nil {
			return err
		}
		if err := validateStringList(character.FlawsOrAmbiguities, fmt.Sprintf("character %d flaw or ambiguity", index+1)); err != nil {
			return err
		}
	}

	for index, relationship := range analysis.Relationships {
		if len(relationship.Parties) < 2 {
			return fmt.Errorf("relationship %d: at least two parties are required", index+1)
		}
		seen := make(map[string]struct{}, len(relationship.Parties))
		for partyIndex, raw := range relationship.Parties {
			party := strings.TrimSpace(raw)
			if party == "" {
				return fmt.Errorf("relationship %d party %d: name is required", index+1, partyIndex+1)
			}
			key := strings.ToLower(party)
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("relationship %d: duplicate party %q", index+1, party)
			}
			if _, exists := characters[key]; !exists {
				return fmt.Errorf("relationship %d: unknown character %q", index+1, party)
			}
			seen[key] = struct{}{}
		}
		if strings.TrimSpace(relationship.Nature) == "" {
			return fmt.Errorf("relationship %d: nature is required", index+1)
		}
	}

	for index, beat := range analysis.CoreStoryBeats {
		if strings.TrimSpace(beat.Summary) == "" {
			return fmt.Errorf("core story beat %d: summary is required", index+1)
		}
	}
	for index, beat := range analysis.DevelopmentBeats {
		if strings.TrimSpace(beat.Summary) == "" {
			return fmt.Errorf("development beat %d: summary is required", index+1)
		}
	}
	for index, beat := range analysis.EnrichmentMaterial {
		if strings.TrimSpace(beat.Summary) == "" {
			return fmt.Errorf("enrichment material %d: summary is required", index+1)
		}
	}

	for index, dependency := range analysis.CausalDependencies {
		if strings.TrimSpace(dependency.Cause) == "" {
			return fmt.Errorf("causal dependency %d: cause is required", index+1)
		}
		if strings.TrimSpace(dependency.Effect) == "" {
			return fmt.Errorf("causal dependency %d: effect is required", index+1)
		}
		if strings.TrimSpace(dependency.WhyItMatters) == "" {
			return fmt.Errorf("causal dependency %d: whyItMatters is required", index+1)
		}
	}

	for index, iconic := range analysis.IconicMaterial {
		if strings.TrimSpace(iconic.Kind) == "" {
			return fmt.Errorf("iconic material %d: kind is required", index+1)
		}
		if strings.TrimSpace(iconic.TextOrDescription) == "" {
			return fmt.Errorf("iconic material %d: textOrDescription is required", index+1)
		}
		if strings.TrimSpace(iconic.Importance) == "" {
			return fmt.Errorf("iconic material %d: importance is required", index+1)
		}
	}

	for index, intense := range analysis.IntenseMaterial {
		if !validIntenseMaterialKind(intense.Kind) {
			return fmt.Errorf("intense material %d: unsupported kind %q", index+1, intense.Kind)
		}
		if strings.TrimSpace(intense.Description) == "" {
			return fmt.Errorf("intense material %d: description is required", index+1)
		}
		if strings.TrimSpace(intense.NarrativeFunction) == "" {
			return fmt.Errorf("intense material %d: narrativeFunction is required", index+1)
		}
	}

	for index, risk := range analysis.AdaptationRisks {
		if !validAdaptationRiskKind(risk.Kind) {
			return fmt.Errorf("adaptation risk %d: unsupported kind %q", index+1, risk.Kind)
		}
		if strings.TrimSpace(risk.Description) == "" {
			return fmt.Errorf("adaptation risk %d: description is required", index+1)
		}
		if strings.TrimSpace(risk.WhatMustBePreserved) == "" {
			return fmt.Errorf("adaptation risk %d: whatMustBePreserved is required", index+1)
		}
	}

	return nil
}

func validateStringList(values []string, label string) error {
	for index, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s %d is empty", label, index+1)
		}
	}
	return nil
}

func validIntenseMaterialKind(kind IntenseMaterialKind) bool {
	switch kind {
	case IntenseMaterialFrightening,
		IntenseMaterialViolence,
		IntenseMaterialDeath,
		IntenseMaterialInjury:
		return true
	default:
		return false
	}
}

func validAdaptationRiskKind(kind AdaptationRiskKind) bool {
	switch kind {
	case AdaptationRiskMotivation,
		AdaptationRiskCausality,
		AdaptationRiskOwnership,
		AdaptationRiskBargain,
		AdaptationRiskPowerRelationship,
		AdaptationRiskStoryIdentity,
		AdaptationRiskOther:
		return true
	default:
		return false
	}
}
