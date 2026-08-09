package db

import (
	"testing"
)

func TestValidLibrarySlugUsesCanonicalLowercaseContract(t *testing.T) {
	for _, valid := range []string{"story", "story-2", "the-three-little-pigs"} {
		if !validLibrarySlug(valid) {
			t.Errorf("validLibrarySlug(%q) = false, want true", valid)
		}
	}

	for _, invalid := range []string{
		"",
		"Story",
		"story_name",
		"story name",
		"-story",
		"story-",
		"story--name",
		"café",
		string([]byte{'b', 'a', 'd', 0xff}),
	} {
		if validLibrarySlug(invalid) {
			t.Errorf("validLibrarySlug(%q) = true, want false", invalid)
		}
	}
}
