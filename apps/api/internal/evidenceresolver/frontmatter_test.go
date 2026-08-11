package evidenceresolver

import (
	"strings"
	"testing"
)

func TestExtractFrontMatterRecognisesOnlyBoundedPositiveContributorSignals(t *testing.T) {
	source := "Translated by Samuel Butler\nEdited by Example Editor\nIntroduction by Another Writer\n*** START OF THE PROJECT GUTENBERG EBOOK EXAMPLE ***\n\nTranslated by a character in the literary body.\n"
	front := ExtractFrontMatter(source)
	if len(front.Translators) != 1 || front.Translators[0] != "Samuel Butler" || len(front.TextualContributors) != 2 || front.Digest == "" {
		t.Fatalf("front=%#v", front)
	}
}

func TestExtractFrontMatterDoesNotTreatAbsenceOrBodyTextAsEvidence(t *testing.T) {
	bodyOnly := "*** START OF THE PROJECT GUTENBERG EBOOK EXAMPLE ***\nTranslated by a character.\n"
	if got := ExtractFrontMatter(bodyOnly); len(got.Translators) != 0 || len(got.TextualContributors) != 0 || got.Digest != "" {
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
