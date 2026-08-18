package evidenceresolver

import "testing"

func TestNormalisedPersonNameUsesCanonicalUnicodeIdentityOnly(t *testing.T) {
	if got, want := NormalisedPersonName("Brontë, Emily"), NormalisedPersonName("Emily Brontë"); got != want {
		t.Fatalf("normalised names differ: %q != %q", got, want)
	}
	if got, want := NormalisedPersonName("Carroll, Lewis"), NormalisedPersonName("lewis carroll"); got != want {
		t.Fatalf("comma/case names differ: %q != %q", got, want)
	}
	if got, want := NormalisedPersonName("O'Connor, Jean-Luc"), NormalisedPersonName("jean luc oconnor"); got != want {
		t.Fatalf("punctuation behaviour changed: %q != %q", got, want)
	}
	if NormalisedPersonName("Brontë") == NormalisedPersonName("Bronté") {
		t.Fatal("different accented names matched")
	}
	if NormalisedPersonName("Łukasz") == NormalisedPersonName("Lukasz") {
		t.Fatal("normalisation transliterated a person name")
	}
	if NormalisedPersonName("Emily Brontë") == NormalisedPersonName("Charlotte Brontë") {
		t.Fatal("unrelated people matched")
	}
}
