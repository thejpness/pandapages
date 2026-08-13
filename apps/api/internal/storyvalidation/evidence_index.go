package storyvalidation

import (
	"fmt"
	"sort"
	"strings"

	"pandapages/api/internal/model"
	"pandapages/api/internal/storygeneration"
)

type EvidenceSegmentID string

type EvidenceSegment struct {
	ID         EvidenceSegmentID
	Location   EvidenceLocation
	EditionKey *model.AdminStoryEditionKey
	Text       string
}

type EvidenceIndex struct {
	segments []EvidenceSegment
	byID     map[EvidenceSegmentID]EvidenceSegment
}

func BuildEvidenceIndex(
	canonicalSource string,
	analysis storygeneration.StoryAnalysis,
	editions []storygeneration.GeneratedEditionArtifact,
) (EvidenceIndex, error) {
	if strings.TrimSpace(canonicalSource) == "" {
		return EvidenceIndex{}, fmt.Errorf("canonical source is required")
	}
	if err := analysis.Validate(); err != nil {
		return EvidenceIndex{}, fmt.Errorf("StoryAnalysis is invalid: %w", err)
	}
	if len(editions) == 0 {
		return EvidenceIndex{}, fmt.Errorf("at least one generated edition is required")
	}

	index := EvidenceIndex{
		segments: make([]EvidenceSegment, 0),
		byID:     make(map[EvidenceSegmentID]EvidenceSegment),
	}

	sourceBlocks := exactTextBlocks(canonicalSource)
	if len(sourceBlocks) == 0 {
		return EvidenceIndex{}, fmt.Errorf("canonical source contains no evidence blocks")
	}
	for blockIndex, text := range sourceBlocks {
		if err := index.add(EvidenceSegment{
			ID:       EvidenceSegmentID(fmt.Sprintf("src:p%04d", blockIndex+1)),
			Location: EvidenceCanonicalSource,
			Text:     text,
		}); err != nil {
			return EvidenceIndex{}, fmt.Errorf("index canonical source: %w", err)
		}
	}

	if err := addStoryAnalysisSegments(&index, analysis); err != nil {
		return EvidenceIndex{}, fmt.Errorf("index StoryAnalysis: %w", err)
	}

	orderedEditions, err := canonicalEvidenceEditions(editions)
	if err != nil {
		return EvidenceIndex{}, err
	}
	for _, edition := range orderedEditions {
		blocks := exactTextBlocks(edition.Markdown)
		if len(blocks) == 0 {
			return EvidenceIndex{}, fmt.Errorf(
				"generated edition %q contains no evidence blocks",
				edition.EditionKey,
			)
		}

		for blockIndex, text := range blocks {
			key := edition.EditionKey
			if err := index.add(EvidenceSegment{
				ID: EvidenceSegmentID(fmt.Sprintf(
					"gen:%s:p%04d",
					edition.EditionKey,
					blockIndex+1,
				)),
				Location:   EvidenceGeneratedEdition,
				EditionKey: &key,
				Text:       text,
			}); err != nil {
				return EvidenceIndex{}, fmt.Errorf(
					"index generated edition %q: %w",
					edition.EditionKey,
					err,
				)
			}
		}
	}

	return index, nil
}

func (index EvidenceIndex) Segments() []EvidenceSegment {
	segments := make([]EvidenceSegment, 0, len(index.segments))
	for _, segment := range index.segments {
		segments = append(segments, cloneEvidenceSegment(segment))
	}
	return segments
}

func (index EvidenceIndex) IDs() []EvidenceSegmentID {
	ids := make([]EvidenceSegmentID, 0, len(index.segments))
	for _, segment := range index.segments {
		ids = append(ids, segment.ID)
	}
	return ids
}

func (index EvidenceIndex) Resolve(id EvidenceSegmentID) (EvidenceSegment, error) {
	segment, ok := index.byID[id]
	if !ok {
		return EvidenceSegment{}, fmt.Errorf("unknown evidence segment %q", id)
	}
	return cloneEvidenceSegment(segment), nil
}

func (index *EvidenceIndex) add(segment EvidenceSegment) error {
	if strings.TrimSpace(string(segment.ID)) == "" {
		return fmt.Errorf("evidence segment ID is required")
	}
	if strings.TrimSpace(segment.Text) == "" {
		return fmt.Errorf("evidence segment %q text is required", segment.ID)
	}
	if _, exists := index.byID[segment.ID]; exists {
		return fmt.Errorf("duplicate evidence segment ID %q", segment.ID)
	}

	switch segment.Location {
	case EvidenceCanonicalSource, EvidenceStoryAnalysis:
		if segment.EditionKey != nil {
			return fmt.Errorf(
				"evidence segment %q at %q must not contain an edition key",
				segment.ID,
				segment.Location,
			)
		}
	case EvidenceGeneratedEdition:
		if segment.EditionKey == nil {
			return fmt.Errorf(
				"generated-edition evidence segment %q requires an edition key",
				segment.ID,
			)
		}
	default:
		return fmt.Errorf(
			"evidence segment %q has unsupported location %q",
			segment.ID,
			segment.Location,
		)
	}

	stored := cloneEvidenceSegment(segment)
	index.segments = append(index.segments, stored)
	index.byID[stored.ID] = stored
	return nil
}

func addStoryAnalysisSegments(index *EvidenceIndex, analysis storygeneration.StoryAnalysis) error {
	add := func(id EvidenceSegmentID, text string) error {
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return index.add(EvidenceSegment{
			ID:       id,
			Location: EvidenceStoryAnalysis,
			Text:     text,
		})
	}

	if err := add("ana:centralPlot", analysis.CentralPlot); err != nil {
		return err
	}

	for characterIndex, character := range analysis.Characters {
		prefix := fmt.Sprintf("ana:characters:%d", characterIndex)
		if err := add(EvidenceSegmentID(prefix+":name"), character.Name); err != nil {
			return err
		}
		if err := add(EvidenceSegmentID(prefix+":role"), character.Role); err != nil {
			return err
		}
		for valueIndex, value := range character.ExplicitMotivations {
			if err := add(
				EvidenceSegmentID(fmt.Sprintf("%s:explicitMotivations:%d", prefix, valueIndex)),
				value,
			); err != nil {
				return err
			}
		}
		for valueIndex, value := range character.FlawsOrAmbiguities {
			if err := add(
				EvidenceSegmentID(fmt.Sprintf("%s:flawsOrAmbiguities:%d", prefix, valueIndex)),
				value,
			); err != nil {
				return err
			}
		}
	}

	for relationshipIndex, relationship := range analysis.Relationships {
		prefix := fmt.Sprintf("ana:relationships:%d", relationshipIndex)
		for partyIndex, party := range relationship.Parties {
			if err := add(
				EvidenceSegmentID(fmt.Sprintf("%s:parties:%d", prefix, partyIndex)),
				party,
			); err != nil {
				return err
			}
		}
		if err := add(EvidenceSegmentID(prefix+":nature"), relationship.Nature); err != nil {
			return err
		}
		if err := add(
			EvidenceSegmentID(prefix+":powerDynamics"),
			relationship.PowerDynamics,
		); err != nil {
			return err
		}
	}

	for beatIndex, beat := range analysis.CoreStoryBeats {
		if err := add(
			EvidenceSegmentID(fmt.Sprintf("ana:coreStoryBeats:%d:summary", beatIndex)),
			beat.Summary,
		); err != nil {
			return err
		}
	}
	for beatIndex, beat := range analysis.DevelopmentBeats {
		if err := add(
			EvidenceSegmentID(fmt.Sprintf("ana:developmentBeats:%d:summary", beatIndex)),
			beat.Summary,
		); err != nil {
			return err
		}
	}
	for beatIndex, beat := range analysis.EnrichmentMaterial {
		if err := add(
			EvidenceSegmentID(fmt.Sprintf("ana:enrichmentMaterial:%d:summary", beatIndex)),
			beat.Summary,
		); err != nil {
			return err
		}
	}

	for dependencyIndex, dependency := range analysis.CausalDependencies {
		prefix := fmt.Sprintf("ana:causalDependencies:%d", dependencyIndex)
		if err := add(EvidenceSegmentID(prefix+":cause"), dependency.Cause); err != nil {
			return err
		}
		if err := add(EvidenceSegmentID(prefix+":effect"), dependency.Effect); err != nil {
			return err
		}
		if err := add(
			EvidenceSegmentID(prefix+":whyItMatters"),
			dependency.WhyItMatters,
		); err != nil {
			return err
		}
	}

	for iconicIndex, iconic := range analysis.IconicMaterial {
		prefix := fmt.Sprintf("ana:iconicMaterial:%d", iconicIndex)
		if err := add(EvidenceSegmentID(prefix+":kind"), iconic.Kind); err != nil {
			return err
		}
		if err := add(
			EvidenceSegmentID(prefix+":textOrDescription"),
			iconic.TextOrDescription,
		); err != nil {
			return err
		}
		if err := add(EvidenceSegmentID(prefix+":importance"), iconic.Importance); err != nil {
			return err
		}
	}

	for intenseIndex, intense := range analysis.IntenseMaterial {
		prefix := fmt.Sprintf("ana:intenseMaterial:%d", intenseIndex)
		if err := add(EvidenceSegmentID(prefix+":kind"), string(intense.Kind)); err != nil {
			return err
		}
		if err := add(EvidenceSegmentID(prefix+":description"), intense.Description); err != nil {
			return err
		}
		if err := add(
			EvidenceSegmentID(prefix+":narrativeFunction"),
			intense.NarrativeFunction,
		); err != nil {
			return err
		}
	}

	for riskIndex, risk := range analysis.AdaptationRisks {
		prefix := fmt.Sprintf("ana:adaptationRisks:%d", riskIndex)
		if err := add(EvidenceSegmentID(prefix+":kind"), string(risk.Kind)); err != nil {
			return err
		}
		if err := add(EvidenceSegmentID(prefix+":description"), risk.Description); err != nil {
			return err
		}
		if err := add(
			EvidenceSegmentID(prefix+":whatMustBePreserved"),
			risk.WhatMustBePreserved,
		); err != nil {
			return err
		}
	}

	return nil
}

func canonicalEvidenceEditions(
	editions []storygeneration.GeneratedEditionArtifact,
) ([]storygeneration.GeneratedEditionArtifact, error) {
	ranks := make(map[model.AdminStoryEditionKey]int)
	for rank, key := range storygeneration.DerivedEditionKeysV2() {
		ranks[key] = rank
	}

	seen := make(map[model.AdminStoryEditionKey]struct{}, len(editions))
	ordered := append([]storygeneration.GeneratedEditionArtifact(nil), editions...)

	for index, edition := range ordered {
		if !storygeneration.ValidV2DerivedEditionKey(edition.EditionKey) {
			return nil, fmt.Errorf(
				"generated edition %d has invalid edition key %q",
				index+1,
				edition.EditionKey,
			)
		}
		if _, duplicate := seen[edition.EditionKey]; duplicate {
			return nil, fmt.Errorf(
				"generated editions contain duplicate edition key %q",
				edition.EditionKey,
			)
		}
		seen[edition.EditionKey] = struct{}{}

		if strings.TrimSpace(edition.Markdown) == "" {
			return nil, fmt.Errorf(
				"generated edition %q Markdown is required",
				edition.EditionKey,
			)
		}
	}

	sort.Slice(ordered, func(left, right int) bool {
		return ranks[ordered[left].EditionKey] < ranks[ordered[right].EditionKey]
	})

	return ordered, nil
}

// exactTextBlocks identifies prose/Markdown blocks separated by one or more
// blank lines. Returned block text is always an exact byte slice from the
// supplied artefact: no trimming, whitespace normalisation, or rewriting is
// performed.
func exactTextBlocks(text string) []string {
	blocks := make([]string, 0)
	blockStart := -1
	lastContentEnd := -1

	for position := 0; position < len(text); {
		lineStart := position
		newlineOffset := strings.IndexByte(text[position:], '\n')

		var lineEnd int
		var next int
		if newlineOffset < 0 {
			lineEnd = len(text)
			next = len(text)
		} else {
			lineEnd = position + newlineOffset
			next = lineEnd + 1
		}

		contentEnd := lineEnd
		if contentEnd > lineStart && text[contentEnd-1] == '\r' {
			contentEnd--
		}

		line := text[lineStart:contentEnd]
		blank := strings.Trim(line, " \t") == ""

		if blank {
			if blockStart >= 0 {
				blocks = append(blocks, text[blockStart:lastContentEnd])
				blockStart = -1
				lastContentEnd = -1
			}
		} else {
			if blockStart < 0 {
				blockStart = lineStart
			}
			lastContentEnd = contentEnd
		}

		position = next
	}

	if blockStart >= 0 {
		blocks = append(blocks, text[blockStart:lastContentEnd])
	}

	return blocks
}

func cloneEvidenceSegment(segment EvidenceSegment) EvidenceSegment {
	clone := segment
	if segment.EditionKey != nil {
		key := *segment.EditionKey
		clone.EditionKey = &key
	}
	return clone
}
