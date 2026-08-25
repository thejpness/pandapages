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

func TestMatchesPersonNameUsesOnlyExplicitProviderVariants(t *testing.T) {
	baum := Person{
		Name:         "Baum, L. Frank (Lyman Frank)",
		NameVariants: []string{"L. Frank Baum", "Lyman Frank Baum", "Baum, L. Frank"},
	}
	for _, external := range []string{"L. Frank Baum", "Lyman Frank Baum", "Baum, L. Frank"} {
		if !MatchesPersonName(baum, external) {
			t.Fatalf("provider variants did not match %q", external)
		}
	}
	for _, external := range []string{"L. Frederick Baum", "L. Baum", "Baum, Frank L.", "L. Frank Brown"} {
		if MatchesPersonName(baum, external) {
			t.Fatalf("unrelated name matched %q", external)
		}
	}

	if !MatchesPersonName(Person{Name: "Carroll, Lewis"}, "Lewis Carroll") {
		t.Fatal("ordinary Last, First inversion no longer matched")
	}
}

func TestQueryPersonNamesUsesBoundedProviderPrecedence(t *testing.T) {
	provider := Person{Name: "Baum, L. Frank (Lyman Frank)", NameVariants: []string{"L. Frank Baum", "Lyman Frank Baum", "Baum, L. Frank", "Ignored Fourth Variant"}}
	got := QueryPersonNames(provider, 3)
	// The final Last, First variant is canonically identical to the first, so
	// it is skipped. The query limit still applies to distinct provider forms.
	want := []string{"L. Frank Baum", "Lyman Frank Baum", "Ignored Fourth Variant"}
	if len(got) != len(want) {
		t.Fatalf("names=%q want=%q", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("names=%q want=%q", got, want)
		}
	}
}
