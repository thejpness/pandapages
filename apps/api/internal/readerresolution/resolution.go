package readerresolution

import (
	"fmt"
	"strings"

	"pandapages/api/internal/model"
)

type DecisionKind string

const (
	DecisionUnavailable DecisionKind = "unavailable"
	DecisionSelected    DecisionKind = "selected"
)

type SelectionSource string

const (
	SelectionOverride       SelectionSource = "override"
	SelectionProgress       SelectionSource = "progress"
	SelectionOnlyEligible   SelectionSource = "only_eligible"
	SelectionProfileDefault SelectionSource = "profile_default"
)

type ReleaseEdition struct {
	EditionKey model.ReaderEditionKey
	VersionID  string
}

type Input struct {
	ReadingLevel      model.ReaderEditionKey
	ReleaseEditions   []ReleaseEdition
	OverrideEdition   *model.ReaderEditionKey
	ProgressVersionID *string
}

type Decision struct {
	Kind     DecisionKind
	Selected *ReleaseEdition
	Source   SelectionSource
	Eligible []ReleaseEdition
}

func editionRank(key model.ReaderEditionKey) (int, bool) {
	for index, candidate := range model.ReaderEditionKeys() {
		if candidate == key {
			return index, true
		}
	}
	return 0, false
}

// Allows implements the Panda Pages reading-level hierarchy. Reading levels
// define the most complex edition a profile may access; every simpler canonical
// edition remains allowed.
func Allows(readingLevel, editionKey model.ReaderEditionKey) bool {
	levelRank, levelOK := editionRank(readingLevel)
	candidateRank, editionOK := editionRank(editionKey)
	return levelOK && editionOK && candidateRank >= levelRank
}

func AllowedEditionKeys(readingLevel model.ReaderEditionKey) []model.ReaderEditionKey {
	if _, ok := editionRank(readingLevel); !ok {
		return nil
	}
	keys := model.ReaderEditionKeys()
	allowed := make([]model.ReaderEditionKey, 0, len(keys))
	for _, key := range keys {
		if Allows(readingLevel, key) {
			allowed = append(allowed, key)
		}
	}
	return allowed
}

func Resolve(input Input) (Decision, error) {
	if !model.ValidReaderEditionKey(input.ReadingLevel) {
		return Decision{}, fmt.Errorf("invalid reader reading level")
	}

	releaseByKey := make(map[model.ReaderEditionKey]ReleaseEdition, len(input.ReleaseEditions))
	versionIDs := make(map[string]struct{}, len(input.ReleaseEditions))
	for _, edition := range input.ReleaseEditions {
		if !model.ValidReaderEditionKey(edition.EditionKey) {
			return Decision{}, fmt.Errorf("invalid current-release edition")
		}
		if edition.VersionID == "" || edition.VersionID != strings.TrimSpace(edition.VersionID) {
			return Decision{}, fmt.Errorf("invalid current-release version id")
		}
		if _, exists := releaseByKey[edition.EditionKey]; exists {
			return Decision{}, fmt.Errorf("duplicate current-release edition")
		}
		if _, exists := versionIDs[edition.VersionID]; exists {
			return Decision{}, fmt.Errorf("duplicate current-release version id")
		}
		releaseByKey[edition.EditionKey] = edition
		versionIDs[edition.VersionID] = struct{}{}
	}

	if input.OverrideEdition != nil && !model.ValidReaderEditionKey(*input.OverrideEdition) {
		return Decision{}, fmt.Errorf("invalid reader edition override")
	}
	if input.ProgressVersionID != nil &&
		(*input.ProgressVersionID == "" || *input.ProgressVersionID != strings.TrimSpace(*input.ProgressVersionID)) {
		return Decision{}, fmt.Errorf("invalid reader progress version id")
	}

	eligible := make([]ReleaseEdition, 0, len(releaseByKey))
	for _, key := range model.ReaderEditionKeys() {
		edition, exists := releaseByKey[key]
		if exists && Allows(input.ReadingLevel, key) {
			eligible = append(eligible, edition)
		}
	}

	if len(eligible) == 0 {
		return Decision{
			Kind:     DecisionUnavailable,
			Eligible: []ReleaseEdition{},
		}, nil
	}

	if input.OverrideEdition != nil {
		for _, edition := range eligible {
			if edition.EditionKey == *input.OverrideEdition {
				selected := edition
				return Decision{
					Kind:     DecisionSelected,
					Selected: &selected,
					Source:   SelectionOverride,
					Eligible: eligible,
				}, nil
			}
		}
	}

	if input.ProgressVersionID != nil {
		for _, edition := range eligible {
			if edition.VersionID == *input.ProgressVersionID {
				selected := edition
				return Decision{
					Kind:     DecisionSelected,
					Selected: &selected,
					Source:   SelectionProgress,
					Eligible: eligible,
				}, nil
			}
		}
	}

	if len(eligible) == 1 {
		selected := eligible[0]
		return Decision{
			Kind:     DecisionSelected,
			Selected: &selected,
			Source:   SelectionOnlyEligible,
			Eligible: eligible,
		}, nil
	}

	selected := eligible[0]
	return Decision{
		Kind:     DecisionSelected,
		Selected: &selected,
		Source:   SelectionProfileDefault,
		Eligible: eligible,
	}, nil
}
