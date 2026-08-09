package readerresolution

import (
	"reflect"
	"testing"

	"pandapages/api/internal/model"
)

func TestAllowsImplementsReadingLevelHierarchy(t *testing.T) {
	keys := model.ReaderEditionKeys()
	for levelIndex, level := range keys {
		for editionIndex, edition := range keys {
			want := editionIndex >= levelIndex
			if got := Allows(level, edition); got != want {
				t.Errorf("Allows(%q, %q) = %v, want %v", level, edition, got, want)
			}
		}
	}
	if Allows(model.ReaderEditionKey("unknown"), model.ReaderEditionClassic) {
		t.Fatal("unknown reading level was allowed")
	}
	if Allows(model.ReaderEditionClassic, model.ReaderEditionKey("unknown")) {
		t.Fatal("unknown edition was allowed")
	}
}

func TestAllowedEditionKeysReturnsCanonicalSuffix(t *testing.T) {
	tests := []struct {
		level model.ReaderEditionKey
		want  []model.ReaderEditionKey
	}{
		{
			level: model.ReaderEditionClassic,
			want:  model.ReaderEditionKeys(),
		},
		{
			level: model.ReaderEditionGrowingReaders,
			want: []model.ReaderEditionKey{
				model.ReaderEditionGrowingReaders,
				model.ReaderEditionStoryExplorers,
				model.ReaderEditionLittleListeners,
			},
		},
		{
			level: model.ReaderEditionLittleListeners,
			want:  []model.ReaderEditionKey{model.ReaderEditionLittleListeners},
		},
	}
	for _, test := range tests {
		if got := AllowedEditionKeys(test.level); !reflect.DeepEqual(got, test.want) {
			t.Errorf("AllowedEditionKeys(%q) = %#v, want %#v", test.level, got, test.want)
		}
	}
	if got := AllowedEditionKeys(model.ReaderEditionKey("unknown")); got != nil {
		t.Fatalf("invalid level allowed keys = %#v, want nil", got)
	}
}

func TestResolveUsesOverrideThenProgressThenOnlyEligible(t *testing.T) {
	growing := model.ReaderEditionGrowingReaders
	explorers := model.ReaderEditionStoryExplorers
	listeners := model.ReaderEditionLittleListeners
	release := []ReleaseEdition{
		{EditionKey: listeners, VersionID: "version-listeners"},
		{EditionKey: growing, VersionID: "version-growing"},
		{EditionKey: explorers, VersionID: "version-explorers"},
	}

	progress := "version-growing"
	override := explorers
	decision, err := Resolve(Input{
		ReadingLevel:      growing,
		ReleaseEditions:   release,
		OverrideEdition:   &override,
		ProgressVersionID: &progress,
	})
	if err != nil {
		t.Fatalf("Resolve override: %v", err)
	}
	if decision.Kind != DecisionSelected || decision.Source != SelectionOverride ||
		decision.Selected == nil || decision.Selected.EditionKey != explorers {
		t.Fatalf("override decision = %#v", decision)
	}

	staleOverride := model.ReaderEditionConfidentReaders
	decision, err = Resolve(Input{
		ReadingLevel:      growing,
		ReleaseEditions:   release,
		OverrideEdition:   &staleOverride,
		ProgressVersionID: &progress,
	})
	if err != nil {
		t.Fatalf("Resolve progress: %v", err)
	}
	if decision.Kind != DecisionSelected || decision.Source != SelectionProgress ||
		decision.Selected == nil || decision.Selected.EditionKey != growing {
		t.Fatalf("progress decision = %#v", decision)
	}

	staleProgress := "old-version"
	decision, err = Resolve(Input{
		ReadingLevel:      model.ReaderEditionLittleListeners,
		ReleaseEditions:   release,
		ProgressVersionID: &staleProgress,
	})
	if err != nil {
		t.Fatalf("Resolve only eligible: %v", err)
	}
	if decision.Kind != DecisionSelected || decision.Source != SelectionOnlyEligible ||
		decision.Selected == nil || decision.Selected.EditionKey != listeners {
		t.Fatalf("only-eligible decision = %#v", decision)
	}
}

func TestResolveNeverInventsAutomaticEditionChoice(t *testing.T) {
	release := []ReleaseEdition{
		{EditionKey: model.ReaderEditionClassic, VersionID: "classic-version"},
		{EditionKey: model.ReaderEditionGrowingReaders, VersionID: "growing-version"},
		{EditionKey: model.ReaderEditionLittleListeners, VersionID: "listener-version"},
	}

	decision, err := Resolve(Input{
		ReadingLevel:    model.ReaderEditionClassic,
		ReleaseEditions: release,
	})
	if err != nil {
		t.Fatalf("Resolve chooser: %v", err)
	}
	if decision.Kind != DecisionChooser || decision.Selected != nil || decision.Source != "" {
		t.Fatalf("multi-edition decision = %#v, want chooser without selection", decision)
	}
	want := []ReleaseEdition{
		{EditionKey: model.ReaderEditionClassic, VersionID: "classic-version"},
		{EditionKey: model.ReaderEditionGrowingReaders, VersionID: "growing-version"},
		{EditionKey: model.ReaderEditionLittleListeners, VersionID: "listener-version"},
	}
	if !reflect.DeepEqual(decision.Eligible, want) {
		t.Fatalf("eligible order = %#v, want %#v", decision.Eligible, want)
	}

	decision, err = Resolve(Input{
		ReadingLevel: model.ReaderEditionGrowingReaders,
		ReleaseEditions: []ReleaseEdition{
			{EditionKey: model.ReaderEditionClassic, VersionID: "classic-version"},
			{EditionKey: model.ReaderEditionConfidentReaders, VersionID: "confident-version"},
		},
	})
	if err != nil {
		t.Fatalf("Resolve unavailable: %v", err)
	}
	if decision.Kind != DecisionUnavailable || decision.Selected != nil || len(decision.Eligible) != 0 {
		t.Fatalf("unavailable decision = %#v", decision)
	}
}

func TestResolveIgnoresStaleSignalsButRejectsMalformedState(t *testing.T) {
	staleOverride := model.ReaderEditionClassic
	staleProgress := "old-version"
	decision, err := Resolve(Input{
		ReadingLevel: model.ReaderEditionGrowingReaders,
		ReleaseEditions: []ReleaseEdition{
			{EditionKey: model.ReaderEditionGrowingReaders, VersionID: "growing-version"},
			{EditionKey: model.ReaderEditionLittleListeners, VersionID: "listener-version"},
		},
		OverrideEdition:   &staleOverride,
		ProgressVersionID: &staleProgress,
	})
	if err != nil {
		t.Fatalf("Resolve stale signals: %v", err)
	}
	if decision.Kind != DecisionChooser {
		t.Fatalf("stale-signal decision = %#v, want chooser", decision)
	}

	badOverride := model.ReaderEditionKey("unknown")
	cases := []Input{
		{ReadingLevel: model.ReaderEditionKey("unknown")},
		{
			ReadingLevel: model.ReaderEditionClassic,
			ReleaseEditions: []ReleaseEdition{
				{EditionKey: model.ReaderEditionKey("unknown"), VersionID: "version"},
			},
		},
		{
			ReadingLevel: model.ReaderEditionClassic,
			ReleaseEditions: []ReleaseEdition{
				{EditionKey: model.ReaderEditionClassic, VersionID: ""},
			},
		},
		{
			ReadingLevel: model.ReaderEditionClassic,
			ReleaseEditions: []ReleaseEdition{
				{EditionKey: model.ReaderEditionClassic, VersionID: "same"},
				{EditionKey: model.ReaderEditionGrowingReaders, VersionID: "same"},
			},
		},
		{
			ReadingLevel: model.ReaderEditionClassic,
			ReleaseEditions: []ReleaseEdition{
				{EditionKey: model.ReaderEditionClassic, VersionID: "one"},
				{EditionKey: model.ReaderEditionClassic, VersionID: "two"},
			},
		},
		{
			ReadingLevel:    model.ReaderEditionClassic,
			OverrideEdition: &badOverride,
		},
	}
	for index, input := range cases {
		if _, err := Resolve(input); err == nil {
			t.Errorf("malformed case %d unexpectedly resolved", index)
		}
	}
}
