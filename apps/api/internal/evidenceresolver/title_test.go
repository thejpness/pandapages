package evidenceresolver

import "testing"

func TestNormalisedTitleUsesOnlyBoundedCatalogueEquivalence(t *testing.T) {
	if got, want := NormalisedTitle("The strange case of Dr. Jekyll and Mr. Hyde"), NormalisedTitle("The strange case of Dr Jekyll and mister Hyde"); got != want {
		t.Fatalf("normalised title=%q want=%q", got, want)
	}
	if NormalisedTitle("The strange case of Dr. Jekyll and Mr. Hyde") == NormalisedTitle("The strange case of Dr. Jekyll and Mrs. Hyde") {
		t.Fatal("unrelated title matched")
	}
	if NormalisedTitle("The strange case of Dr. Jekyll and Mr. Hyde") == NormalisedTitle("The mysterious case of Dr. Jekyll and mister Hyde") {
		t.Fatal("different title matched")
	}
}

func TestTitleQueryVariantsRemainSmallAndExact(t *testing.T) {
	variants := TitleQueryVariants("The strange case of Dr. Jekyll and Mr. Hyde")
	if len(variants) != 3 || variants[0] != "The strange case of Dr. Jekyll and Mr. Hyde" || variants[1] != "The strange case of Dr Jekyll and Mr Hyde" || variants[2] != "The strange case of Dr Jekyll and mister Hyde" {
		t.Fatalf("variants=%#v", variants)
	}
}

func TestTitleQueryVariantsCoverMisterToMrAndMrStop(t *testing.T) {
	variants := TitleQueryVariants("The case of Mister Hyde")
	if len(variants) != 3 || variants[1] != "The case of mr. Hyde" || variants[2] != "The case of mr Hyde" {
		t.Fatalf("variants=%#v", variants)
	}
}
