package storygeneration

import (
	"strings"
	"testing"

	"pandapages/api/internal/model"
)

func validStoryAnalysis() StoryAnalysis {
	return StoryAnalysis{
		CentralPlot: "Jack trades the cow, obtains magic beans, climbs the beanstalk, takes treasure from the giant, and escapes.",
		Characters: []Character{
			{
				Name:                "Jack",
				Role:                "protagonist",
				ExplicitMotivations: []string{"Improve his and his mother's poverty"},
				FlawsOrAmbiguities:  []string{"Impulsive", "Takes property belonging to the giant"},
			},
			{
				Name:                "Jack's mother",
				Role:                "parent",
				ExplicitMotivations: []string{"Keep the household fed"},
				FlawsOrAmbiguities:  []string{"Angry when Jack returns without money"},
			},
			{
				Name:                "The giant",
				Role:                "threat",
				ExplicitMotivations: []string{"Protect his home and possessions"},
				FlawsOrAmbiguities:  []string{"Threatens to eat intruders"},
			},
		},
		Relationships: []Relationship{
			{
				Parties:       []string{"Jack", "Jack's mother"},
				Nature:        "parent and child",
				PowerDynamics: "Jack's mother sends him to sell the cow.",
			},
			{
				Parties:       []string{"Jack", "The giant"},
				Nature:        "intruder and threatened householder",
				PowerDynamics: "The giant is physically dominant; Jack steals and escapes.",
			},
		},
		CoreStoryBeats: []StoryBeat{
			{Summary: "Jack trades the cow for magic beans."},
			{Summary: "A beanstalk grows and Jack climbs it."},
			{Summary: "Jack enters the giant's home and takes treasure."},
			{Summary: "Jack escapes down the beanstalk."},
		},
		DevelopmentBeats: []StoryBeat{
			{Summary: "Jack's mother rejects the bean trade and throws the beans away."},
		},
		EnrichmentMaterial: []StoryBeat{
			{Summary: "Repeated visits increase the danger and treasure."},
		},
		CausalDependencies: []CausalDependency{
			{
				Cause:        "The household needs money, so Jack is sent to sell the cow.",
				Effect:       "Jack encounters the bean seller and makes the trade.",
				WhyItMatters: "Removing the poverty and sale would make Jack's journey arbitrary.",
			},
		},
		IconicMaterial: []IconicMaterial{
			{
				Kind:              "refrain",
				TextOrDescription: "The giant's repeated fee-fi-fo-fum threat.",
				Importance:        "Recognisable identity and escalating threat.",
			},
		},
		IntenseMaterial: []IntenseMaterial{
			{
				Kind:              IntenseMaterialFrightening,
				Description:       "The giant threatens to eat Jack.",
				NarrativeFunction: "Explains why Jack hides and why escape is urgent.",
			},
		},
		AdaptationRisks: []AdaptationRisk{
			{
				Kind:                AdaptationRiskMotivation,
				Description:         "Making Jack altruistic would change his source-grounded motives.",
				WhatMustBePreserved: "Jack's poverty, impulsiveness, and morally ambiguous taking of treasure.",
			},
			{
				Kind:                AdaptationRiskCausality,
				Description:         "Removing the giant's threat can make Jack's fear and escape incoherent.",
				WhatMustBePreserved: "Enough danger to explain concealment and escape.",
			},
		},
	}
}

func TestSpecificationV2IsLocked(t *testing.T) {
	if SpecificationV2 != "panda-pages-adaptation-v2" {
		t.Fatalf("SpecificationV2 = %q", SpecificationV2)
	}
	if GenerationModelV2 != "gpt-5.6-terra" {
		t.Fatalf("GenerationModelV2 = %q", GenerationModelV2)
	}

	want := []model.AdminStoryEditionKey{
		model.AdminStoryEditionConfidentReaders,
		model.AdminStoryEditionGrowingReaders,
		model.AdminStoryEditionStoryExplorers,
		model.AdminStoryEditionLittleListeners,
	}
	got := DerivedEditionKeysV2()
	if len(got) != len(want) {
		t.Fatalf("DerivedEditionKeysV2() length = %d, want %d", len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("DerivedEditionKeysV2()[%d] = %q, want %q", index, got[index], want[index])
		}
	}
	for _, key := range got {
		if key == model.AdminStoryEditionClassic {
			t.Fatal("Classic must not be a v2 generation target")
		}
	}
}

func TestStoryAnalysisValidateAcceptsSourceGroundedMap(t *testing.T) {
	if err := validStoryAnalysis().Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestStoryAnalysisValidateRejectsInvalidAnalysis(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*StoryAnalysis)
		want   string
	}{
		{
			name: "missing central plot",
			mutate: func(a *StoryAnalysis) {
				a.CentralPlot = " "
			},
			want: "central plot is required",
		},
		{
			name: "no characters",
			mutate: func(a *StoryAnalysis) {
				a.Characters = nil
			},
			want: "at least one character",
		},
		{
			name: "missing character role",
			mutate: func(a *StoryAnalysis) {
				a.Characters[0].Role = " "
			},
			want: "role is required",
		},
		{
			name: "duplicate character",
			mutate: func(a *StoryAnalysis) {
				a.Characters = append(a.Characters, Character{Name: "jack", Role: "duplicate"})
			},
			want: "duplicate character",
		},
		{
			name: "empty explicit motivation",
			mutate: func(a *StoryAnalysis) {
				a.Characters[0].ExplicitMotivations = []string{" "}
			},
			want: "explicit motivation",
		},
		{
			name: "relationship has one party",
			mutate: func(a *StoryAnalysis) {
				a.Relationships[0].Parties = []string{"Jack"}
			},
			want: "at least two parties",
		},
		{
			name: "relationship references unknown character",
			mutate: func(a *StoryAnalysis) {
				a.Relationships[0].Parties = []string{"Jack", "Nobody"}
			},
			want: "unknown character",
		},
		{
			name: "no core beats",
			mutate: func(a *StoryAnalysis) {
				a.CoreStoryBeats = nil
			},
			want: "at least one core story beat",
		},
		{
			name: "empty development beat",
			mutate: func(a *StoryAnalysis) {
				a.DevelopmentBeats[0].Summary = ""
			},
			want: "development beat",
		},
		{
			name: "incomplete causal dependency",
			mutate: func(a *StoryAnalysis) {
				a.CausalDependencies[0].WhyItMatters = ""
			},
			want: "whyItMatters",
		},
		{
			name: "unknown intense material kind",
			mutate: func(a *StoryAnalysis) {
				a.IntenseMaterial[0].Kind = "romance"
			},
			want: "unsupported kind",
		},
		{
			name: "unknown adaptation risk kind",
			mutate: func(a *StoryAnalysis) {
				a.AdaptationRisks[0].Kind = "morality"
			},
			want: "unsupported kind",
		},
		{
			name: "empty preservation requirement",
			mutate: func(a *StoryAnalysis) {
				a.AdaptationRisks[0].WhatMustBePreserved = ""
			},
			want: "whatMustBePreserved",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			analysis := validStoryAnalysis()
			test.mutate(&analysis)
			err := analysis.Validate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestOptionalAnalysisSectionsMayBeEmptyRatherThanInvented(t *testing.T) {
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

	if err := analysis.Validate(); err != nil {
		t.Fatalf("empty optional/source-absent sections must be valid: %v", err)
	}
}
