package db

import (
	"testing"
	"time"
)

func TestLibraryVersionMetadataUsesOnlyTypedVersionValues(t *testing.T) {
	title, author, language, err := libraryVersionMetadata(
		[]byte(`{"title":" Published title ","author":null,"language":" en "}`),
	)
	if err != nil {
		t.Fatalf("version metadata: %v", err)
	}
	if title != "Published title" || author != nil || language != "en" {
		t.Fatalf("version metadata = %q / %#v / %q", title, author, language)
	}

	title, author, language, err = libraryVersionMetadata(
		[]byte(`{"title":"Published without an author","language":"en-GB"}`),
	)
	if err != nil {
		t.Fatalf("missing author metadata: %v", err)
	}
	if title != "Published without an author" || author != nil || language != "en-GB" {
		t.Fatalf("version metadata without author = %q / %#v / %q", title, author, language)
	}
}

func TestLibraryVersionMetadataRejectsPresentMalformedValues(t *testing.T) {
	for _, test := range []struct {
		name        string
		frontmatter string
	}{
		{name: "non-object", frontmatter: `[]`},
		{name: "missing title", frontmatter: `{"language":"en-GB"}`},
		{name: "non-string title", frontmatter: `{"title":42,"language":"en-GB"}`},
		{name: "empty title", frontmatter: `{"title":" ","language":"en-GB"}`},
		{name: "non-string author", frontmatter: `{"title":"Published title","author":[],"language":"en-GB"}`},
		{name: "missing language", frontmatter: `{"title":"Published title"}`},
		{name: "null language", frontmatter: `{"title":"Published title","language":null}`},
		{name: "empty language", frontmatter: `{"title":"Published title","language":""}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, _, err := libraryVersionMetadata(
				[]byte(test.frontmatter),
			); err == nil {
				t.Fatalf("accepted malformed frontmatter %s", test.frontmatter)
			}
		})
	}
	invalidUTF8 := []byte(`{"title":"Published title","language":"`)
	invalidUTF8 = append(invalidUTF8, 0xff)
	invalidUTF8 = append(invalidUTF8, []byte(`"}`)...)
	if _, _, _, err := libraryVersionMetadata(invalidUTF8); err == nil {
		t.Fatal("accepted raw invalid UTF-8 before JSON decoding")
	}
}

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

func TestValidLibraryProgressTimeMatchesRFC3339YearRange(t *testing.T) {
	for _, valid := range []time.Time{
		time.Date(1, time.January, 2, 0, 0, 0, 0, time.UTC),
		time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC),
	} {
		if !validLibraryProgressTime(valid) {
			t.Errorf("validLibraryProgressTime(%s) = false, want true", valid)
		}
	}
	for _, invalid := range []time.Time{
		{},
		time.Date(0, time.December, 31, 23, 59, 59, 0, time.UTC),
		time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC),
	} {
		if validLibraryProgressTime(invalid) {
			t.Errorf("validLibraryProgressTime(%s) = true, want false", invalid)
		}
	}
}
