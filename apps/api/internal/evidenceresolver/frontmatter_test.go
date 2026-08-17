package evidenceresolver

import (
	"strings"
	"testing"
)

func TestExtractFrontMatterRecognisesOnlyBoundedPositiveContributorSignals(t *testing.T) {
	source := "Translator: Samuel Butler\nEditor: Example Editor\nIntroduction by Another Writer\nAnnotations by Another Writer\nCommentary: Another Writer\n*** START OF THE PROJECT GUTENBERG EBOOK EXAMPLE ***\n\nTranslated by a character in the literary body.\n"
	front := ExtractFrontMatter(source)
	if !front.Inspected || len(front.Translators) != 1 || front.Translators[0] != "Samuel Butler" || len(front.TextualContributors) != 4 || front.Digest == "" {
		t.Fatalf("front=%#v", front)
	}
}

func TestExtractFrontMatterDoesNotTreatAbsenceOrBodyTextAsEvidence(t *testing.T) {
	bodyOnly := "*** START OF THE PROJECT GUTENBERG EBOOK EXAMPLE ***\nTranslated by a character.\n"
	if got := ExtractFrontMatter(bodyOnly); got.Inspected || len(got.Translators) != 0 || len(got.TextualContributors) != 0 || got.Digest != "" {
		t.Fatalf("body=%#v", got)
	}
	if got := ExtractFrontMatter("No contributor statement\n*** START OF THE PROJECT GUTENBERG EBOOK EXAMPLE ***\n"); len(got.Translators) != 0 || len(got.TextualContributors) != 0 {
		t.Fatalf("absence=%#v", got)
	}
}

func TestExtractFrontMatterDoesNotReadPastItsBound(t *testing.T) {
	source := strings.Repeat("x", maxFrontMatterBytes) + "\nTranslated by Late Contributor\n*** START OF THE PROJECT GUTENBERG EBOOK EXAMPLE ***\n"
	if got := ExtractFrontMatter(source); len(got.Translators) != 0 {
		t.Fatalf("late signal=%#v", got)
	}
}

func TestExtractPostMarkerSignalsRecognisesBoundedPositiveTitlePageEvidence(t *testing.T) {
	source := "Project Gutenberg wrapper\n*** START OF THE PROJECT GUTENBERG EBOOK PRIDE AND PREJUDICE ***\n\nwith a Preface by\nGeorge Saintsbury\n\nThe text is based on translations from\nthe Grimms' Kinder und Hausmärchen by\nEdgar Taylor and Marian Edwardes.\n"
	signals := ExtractPostMarkerSignals(source)
	if signals.Digest == "" || len(signals.TextualContributors) != 1 || signals.TextualContributors[0] != (FrontMatterContributor{Role: "preface", Name: "George Saintsbury"}) || len(signals.Translators) != 1 || signals.Translators[0] != "Edgar Taylor and Marian Edwardes." {
		t.Fatalf("signals=%#v", signals)
	}
	front := ExtractFrontMatter(source)
	if !front.Inspected || len(front.TextualContributors) != 0 || len(front.Translators) != 0 {
		t.Fatalf("front=%#v", front)
	}
}

func TestExtractPostMarkerSignalsDoesNotTurnOrdinaryProseOrAbsenceIntoEvidence(t *testing.T) {
	for _, source := range []string{
		"*** START OF THE PROJECT GUTENBERG EBOOK EXAMPLE ***\nThe narrator said translated by an imaginary character.\n",
		"*** START OF THE PROJECT GUTENBERG EBOOK EXAMPLE ***\nNo contributor statement here.\n",
		"*** START OF THE PROJECT GUTENBERG EBOOK EXAMPLE ***\nThe text is based on translations from old tales by a narrator.\n",
	} {
		signals := ExtractPostMarkerSignals(source)
		if signals.Digest == "" || len(signals.Translators) != 0 || len(signals.TextualContributors) != 0 {
			t.Fatalf("source=%q signals=%#v", source, signals)
		}
	}
}

func TestExtractPostMarkerSignalsDoesNotTreatIllustrationCreditAsTextual(t *testing.T) {
	source := "*** START OF THE PROJECT GUTENBERG EBOOK PRIDE AND PREJUDICE ***\nIllustrations by\nHugh Thomson\n"
	signals := ExtractPostMarkerSignals(source)
	if signals.Digest == "" || len(signals.TextualContributors) != 0 || len(signals.Translators) != 0 {
		t.Fatalf("signals=%#v", signals)
	}
}

func TestExtractPostMarkerSignalsDoesNotReadPastItsBound(t *testing.T) {
	source := "*** START OF THE PROJECT GUTENBERG EBOOK EXAMPLE ***\n" + strings.Repeat("x\n", maxTitlePageSignalLines) + "with a Preface by\nLate Contributor\n"
	if signals := ExtractPostMarkerSignals(source); len(signals.TextualContributors) != 0 || len(signals.Translators) != 0 {
		t.Fatalf("signals=%#v", signals)
	}
}

func TestExtractPostMarkerSignalsDoesNotSearchBeyondProviderPrefix(t *testing.T) {
	source := strings.Repeat("x", maxFrontMatterBytes) + "\n*** START OF THE PROJECT GUTENBERG EBOOK EXAMPLE ***\nwith a Preface by\nLate Contributor\n"
	if signals := ExtractPostMarkerSignals(source); signals.Digest != "" || len(signals.TextualContributors) != 0 || len(signals.Translators) != 0 {
		t.Fatalf("signals=%#v", signals)
	}
}
