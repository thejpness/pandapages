package storyvalidation

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"pandapages/api/internal/model"
	"pandapages/api/internal/storygeneration"
)

func TestBuildEvidenceIndexUsesDeterministicExactSegments(t *testing.T) {
	canonicalSource := "First line.\r\nstill first.\r\n\r\n  Second paragraph.  \r\n\r\n"
	analysis := evidenceIndexTestAnalysis()
	key := model.AdminStoryEditionGrowingReaders
	markdown := "# The Lantern Keeper\n\nFirst generated paragraph.\ncontinues.\n\nSecond generated paragraph."

	index, err := BuildEvidenceIndex(
		canonicalSource,
		analysis,
		[]storygeneration.GeneratedEditionArtifact{
			{
				EditionKey: key,
				Markdown:   markdown,
			},
		},
	)
	if err != nil {
		t.Fatalf("BuildEvidenceIndex() error = %v", err)
	}

	tests := []struct {
		id         EvidenceSegmentID
		location   EvidenceLocation
		editionKey *model.AdminStoryEditionKey
		text       string
	}{
		{
			id:       "src:p0001",
			location: EvidenceCanonicalSource,
			text:     "First line.\r\nstill first.",
		},
		{
			id:       "src:p0002",
			location: EvidenceCanonicalSource,
			text:     "  Second paragraph.  ",
		},
		{
			id:       "ana:characters:0:explicitMotivations:0",
			location: EvidenceStoryAnalysis,
			text:     "Keep the harbour light burning.",
		},
		{
			id:         EvidenceSegmentID(fmt.Sprintf("gen:%s:p0001", key)),
			location:   EvidenceGeneratedEdition,
			editionKey: &key,
			text:       "# The Lantern Keeper",
		},
		{
			id:         EvidenceSegmentID(fmt.Sprintf("gen:%s:p0002", key)),
			location:   EvidenceGeneratedEdition,
			editionKey: &key,
			text:       "First generated paragraph.\ncontinues.",
		},
		{
			id:         EvidenceSegmentID(fmt.Sprintf("gen:%s:p0003", key)),
			location:   EvidenceGeneratedEdition,
			editionKey: &key,
			text:       "Second generated paragraph.",
		},
	}

	for _, test := range tests {
		t.Run(string(test.id), func(t *testing.T) {
			segment, err := index.Resolve(test.id)
			if err != nil {
				t.Fatalf("Resolve(%q) error = %v", test.id, err)
			}
			if segment.Location != test.location {
				t.Fatalf("Location = %q, want %q", segment.Location, test.location)
			}
			if segment.Text != test.text {
				t.Fatalf("Text = %q, want exact %q", segment.Text, test.text)
			}
			if !sameEvidenceEditionKey(segment.EditionKey, test.editionKey) {
				t.Fatalf("EditionKey = %v, want %v", segment.EditionKey, test.editionKey)
			}
		})
	}

	again, err := BuildEvidenceIndex(
		canonicalSource,
		analysis,
		[]storygeneration.GeneratedEditionArtifact{
			{
				EditionKey: key,
				Markdown:   markdown,
			},
		},
	)
	if err != nil {
		t.Fatalf("second BuildEvidenceIndex() error = %v", err)
	}

	if !reflect.DeepEqual(index.Segments(), again.Segments()) {
		t.Fatal("BuildEvidenceIndex() must be deterministic for identical input")
	}
}

func TestBuildEvidenceIndexIndexesEveryStoryAnalysisStringAtom(t *testing.T) {
	index, err := BuildEvidenceIndex(
		"Canonical source.",
		evidenceIndexTestAnalysis(),
		[]storygeneration.GeneratedEditionArtifact{
			{
				EditionKey: model.AdminStoryEditionGrowingReaders,
				Markdown:   "# Story\n\nGenerated paragraph.",
			},
		},
	)
	if err != nil {
		t.Fatalf("BuildEvidenceIndex() error = %v", err)
	}

	expectedIDs := []EvidenceSegmentID{
		"ana:centralPlot",
		"ana:characters:0:name",
		"ana:characters:0:role",
		"ana:characters:0:explicitMotivations:0",
		"ana:characters:0:flawsOrAmbiguities:0",
		"ana:characters:1:name",
		"ana:characters:1:role",
		"ana:relationships:0:parties:0",
		"ana:relationships:0:parties:1",
		"ana:relationships:0:nature",
		"ana:relationships:0:powerDynamics",
		"ana:coreStoryBeats:0:summary",
		"ana:developmentBeats:0:summary",
		"ana:enrichmentMaterial:0:summary",
		"ana:causalDependencies:0:cause",
		"ana:causalDependencies:0:effect",
		"ana:causalDependencies:0:whyItMatters",
		"ana:iconicMaterial:0:kind",
		"ana:iconicMaterial:0:textOrDescription",
		"ana:iconicMaterial:0:importance",
		"ana:intenseMaterial:0:kind",
		"ana:intenseMaterial:0:description",
		"ana:intenseMaterial:0:narrativeFunction",
		"ana:adaptationRisks:0:kind",
		"ana:adaptationRisks:0:description",
		"ana:adaptationRisks:0:whatMustBePreserved",
	}

	for _, id := range expectedIDs {
		segment, err := index.Resolve(id)
		if err != nil {
			t.Errorf("Resolve(%q) error = %v", id, err)
			continue
		}
		if segment.Location != EvidenceStoryAnalysis {
			t.Errorf("Resolve(%q).Location = %q, want %q", id, segment.Location, EvidenceStoryAnalysis)
		}
		if segment.EditionKey != nil {
			t.Errorf("Resolve(%q).EditionKey = %v, want nil", id, segment.EditionKey)
		}
		if strings.TrimSpace(segment.Text) == "" {
			t.Errorf("Resolve(%q).Text is empty", id)
		}
	}
}

func TestBuildEvidenceIndexOrdersGeneratedEditionsCanonically(t *testing.T) {
	confident := model.AdminStoryEditionConfidentReaders
	explorers := model.AdminStoryEditionStoryExplorers

	index, err := BuildEvidenceIndex(
		"Canonical source.",
		evidenceIndexTestAnalysis(),
		[]storygeneration.GeneratedEditionArtifact{
			{
				EditionKey: explorers,
				Markdown:   "# Explorers\n\nExplorers text.",
			},
			{
				EditionKey: confident,
				Markdown:   "# Confident\n\nConfident text.",
			},
		},
	)
	if err != nil {
		t.Fatalf("BuildEvidenceIndex() error = %v", err)
	}

	confidentID := EvidenceSegmentID(fmt.Sprintf("gen:%s:p0001", confident))
	explorersID := EvidenceSegmentID(fmt.Sprintf("gen:%s:p0001", explorers))

	confidentPosition := evidenceIDPosition(index.IDs(), confidentID)
	explorersPosition := evidenceIDPosition(index.IDs(), explorersID)
	if confidentPosition < 0 || explorersPosition < 0 {
		t.Fatalf(
			"missing generated IDs: confident=%d explorers=%d",
			confidentPosition,
			explorersPosition,
		)
	}
	if confidentPosition >= explorersPosition {
		t.Fatalf(
			"generated editions are not in canonical order: confident=%d explorers=%d",
			confidentPosition,
			explorersPosition,
		)
	}
}

func TestBuildEvidenceIndexRejectsInvalidInputs(t *testing.T) {
	validAnalysis := evidenceIndexTestAnalysis()
	validEdition := storygeneration.GeneratedEditionArtifact{
		EditionKey: model.AdminStoryEditionGrowingReaders,
		Markdown:   "# Story\n\nGenerated paragraph.",
	}

	tests := []struct {
		name     string
		source   string
		analysis storygeneration.StoryAnalysis
		editions []storygeneration.GeneratedEditionArtifact
	}{
		{
			name:     "empty canonical source",
			source:   " \n\t",
			analysis: validAnalysis,
			editions: []storygeneration.GeneratedEditionArtifact{validEdition},
		},
		{
			name:     "invalid StoryAnalysis",
			source:   "Canonical source.",
			analysis: storygeneration.StoryAnalysis{},
			editions: []storygeneration.GeneratedEditionArtifact{validEdition},
		},
		{
			name:     "no editions",
			source:   "Canonical source.",
			analysis: validAnalysis,
		},
		{
			name:     "duplicate edition",
			source:   "Canonical source.",
			analysis: validAnalysis,
			editions: []storygeneration.GeneratedEditionArtifact{
				validEdition,
				validEdition,
			},
		},
		{
			name:     "invalid edition key",
			source:   "Canonical source.",
			analysis: validAnalysis,
			editions: []storygeneration.GeneratedEditionArtifact{
				{
					EditionKey: model.AdminStoryEditionKey("not-an-edition"),
					Markdown:   "# Story",
				},
			},
		},
		{
			name:     "empty edition Markdown",
			source:   "Canonical source.",
			analysis: validAnalysis,
			editions: []storygeneration.GeneratedEditionArtifact{
				{
					EditionKey: model.AdminStoryEditionGrowingReaders,
					Markdown:   "\n\t",
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := BuildEvidenceIndex(test.source, test.analysis, test.editions)
			if err == nil {
				t.Fatal("BuildEvidenceIndex() error = nil, want failure")
			}
		})
	}
}

func TestEvidenceIndexResolveUnknownFailsClosed(t *testing.T) {
	index, err := BuildEvidenceIndex(
		"Canonical source.",
		evidenceIndexTestAnalysis(),
		[]storygeneration.GeneratedEditionArtifact{
			{
				EditionKey: model.AdminStoryEditionGrowingReaders,
				Markdown:   "# Story",
			},
		},
	)
	if err != nil {
		t.Fatalf("BuildEvidenceIndex() error = %v", err)
	}

	if _, err := index.Resolve("src:p9999"); err == nil {
		t.Fatal("Resolve() error = nil for unknown segment")
	}
}

func evidenceIndexTestAnalysis() storygeneration.StoryAnalysis {
	return storygeneration.StoryAnalysis{
		CentralPlot: "Mara keeps the harbour lantern burning during a storm.",
		Characters: []storygeneration.Character{
			{
				Name:                "Mara",
				Role:                "lantern keeper",
				ExplicitMotivations: []string{"Keep the harbour light burning."},
				FlawsOrAmbiguities:  []string{"She is stubborn about accepting help."},
			},
			{
				Name: "Tomas",
				Role: "Mara's brother",
			},
		},
		Relationships: []storygeneration.Relationship{
			{
				Parties:       []string{"Mara", "Tomas"},
				Nature:        "siblings who care for one another",
				PowerDynamics: "Mara initially insists on making the decisions.",
			},
		},
		CoreStoryBeats: []storygeneration.StoryBeat{
			{Summary: "A storm threatens the harbour while Mara tends the lantern."},
		},
		DevelopmentBeats: []storygeneration.StoryBeat{
			{Summary: "Tomas offers to help Mara carry fuel upstairs."},
		},
		EnrichmentMaterial: []storygeneration.StoryBeat{
			{Summary: "Rain rattles against the lantern-room windows."},
		},
		CausalDependencies: []storygeneration.CausalDependency{
			{
				Cause:        "The storm hides the harbour entrance.",
				Effect:       "The lantern must remain visible to the boats.",
				WhyItMatters: "Without the light, the boats cannot navigate safely.",
			},
		},
		IconicMaterial: []storygeneration.IconicMaterial{
			{
				Kind:              "refrain",
				TextOrDescription: "Keep the light burning.",
				Importance:        "It expresses Mara's central duty.",
			},
		},
		IntenseMaterial: []storygeneration.IntenseMaterial{
			{
				Kind:              storygeneration.IntenseMaterialFrightening,
				Description:       "The storm puts boats at risk.",
				NarrativeFunction: "It creates the story's stakes.",
			},
		},
		AdaptationRisks: []storygeneration.AdaptationRisk{
			{
				Kind:                storygeneration.AdaptationRiskCausality,
				Description:         "Removing the storm would weaken the causal chain.",
				WhatMustBePreserved: "The lantern must matter because the boats need it.",
			},
		},
	}
}

func sameEvidenceEditionKey(
	left *model.AdminStoryEditionKey,
	right *model.AdminStoryEditionKey,
) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func evidenceIDPosition(ids []EvidenceSegmentID, target EvidenceSegmentID) int {
	for index, id := range ids {
		if id == target {
			return index
		}
	}
	return -1
}
