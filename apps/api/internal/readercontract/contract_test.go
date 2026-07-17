package readercontract

import (
	"strings"
	"testing"
)

func intPointer(value int) *int {
	return &value
}

func TestContentKeyFixedVectors(t *testing.T) {
	tests := []struct {
		name         string
		kind         SegmentKind
		headingLevel int
		markdown     string
		want         string
	}{
		{
			name:         "UTF-8 heading",
			kind:         SegmentKindHeading,
			headingLevel: 1,
			markdown:     "# Café Panda 🐼",
			want:         "3356355f7cdbea17f247fcd38f581fe42cea6d5b3f7965bd9122a6645cd68b71",
		},
		{
			name:     "paragraph",
			kind:     SegmentKindParagraph,
			markdown: "Same words",
			want:     "5e92e2acdcf286b6d82be228f32aa1743e3a2912d4b5ee6d268a2d460d104942",
		},
		{
			name:         "H2 differs from paragraph",
			kind:         SegmentKindHeading,
			headingLevel: 2,
			markdown:     "Same words",
			want:         "85ec640a768ec54ad57c68d8e9e561a278f426c8f3c7ca7c71cafc9acb489787",
		},
		{
			name:         "H3 differs from H2",
			kind:         SegmentKindHeading,
			headingLevel: 3,
			markdown:     "Same words",
			want:         "3834b9971e0eb29227bb98f3dc156deb06e27c9b40f358b05c52f4df4e3449e8",
		},
		{
			name:     "line ending vector",
			kind:     SegmentKindParagraph,
			markdown: "Line one\nLine two",
			want:     "015af1d8c2b2f0983b6c8cbc952f42dca6e0a2f379dfdb306fd6f34179f50f29",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ContentKey(test.kind, test.headingLevel, test.markdown); got != test.want {
				t.Fatalf("ContentKey() = %q, want %q", got, test.want)
			}
		})
	}

	lf := ContentKey(SegmentKindParagraph, 0, "Line one\nLine two")
	for _, equivalent := range []string{"Line one\r\nLine two", "Line one\rLine two"} {
		if got := ContentKey(SegmentKindParagraph, 0, equivalent); got != lf {
			t.Fatalf("line-ending key = %q, want %q", got, lf)
		}
	}
}

func TestAssignSegmentIdentitiesUsesStableOccurrencesAndH2Chapters(t *testing.T) {
	inputs := []SegmentIdentityInput{
		{Kind: SegmentKindHeading, HeadingLevel: intPointer(1), Markdown: "# Title"},
		{Kind: SegmentKindParagraph, Markdown: "Repeated paragraph."},
		{Kind: SegmentKindHeading, HeadingLevel: intPointer(2), Markdown: "## Chapter"},
		{Kind: SegmentKindParagraph, Markdown: "Repeated paragraph."},
		{Kind: SegmentKindHeading, HeadingLevel: intPointer(3), Markdown: "### Detail"},
		{Kind: SegmentKindHeading, HeadingLevel: intPointer(2), Markdown: "## Chapter"},
		{Kind: SegmentKindParagraph, Markdown: "After repeat."},
	}

	got, err := AssignSegmentIdentities(inputs)
	if err != nil {
		t.Fatalf("AssignSegmentIdentities: %v", err)
	}
	if got[1].ContentKey != got[3].ContentKey || got[1].ContentOccurrence != 1 || got[3].ContentOccurrence != 2 {
		t.Fatalf("duplicate paragraph identities = %#v / %#v", got[1], got[3])
	}
	if got[0].ChapterKey != nil || got[1].ChapterKey != nil {
		t.Fatal("segments before the first H2 received a chapter")
	}
	if got[2].ChapterKey == nil || *got[2].ChapterKey != got[2].ContentKey || got[2].ChapterOccurrence == nil || *got[2].ChapterOccurrence != 1 {
		t.Fatalf("first H2 chapter identity = %#v", got[2])
	}
	if got[4].ChapterKey == nil || *got[4].ChapterKey != got[2].ContentKey || *got[4].ChapterOccurrence != 1 {
		t.Fatalf("H3 replaced current H2 chapter: %#v", got[4])
	}
	if got[5].ContentKey != got[2].ContentKey || got[5].ContentOccurrence != 2 ||
		got[5].ChapterKey == nil || *got[5].ChapterKey != got[5].ContentKey ||
		got[5].ChapterOccurrence == nil || *got[5].ChapterOccurrence != 2 {
		t.Fatalf("repeated H2 chapter identity = %#v", got[5])
	}
	if got[6].ChapterKey == nil || *got[6].ChapterKey != got[5].ContentKey || *got[6].ChapterOccurrence != 2 {
		t.Fatalf("repeated H2 did not propagate: %#v", got[6])
	}
}

func TestLocatorValidation(t *testing.T) {
	valid := Locator{
		Schema: 2,
		Segment: LocatorSegment{
			Key:        strings.Repeat("a", 64),
			Occurrence: 1,
			Ordinal:    4,
			Offset:     0.35,
		},
		Chapter: &LocatorChapter{Key: strings.Repeat("b", 64), Occurrence: 2},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid locator rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Locator)
	}{
		{name: "schema", mutate: func(locator *Locator) { locator.Schema = 1 }},
		{name: "segment key", mutate: func(locator *Locator) { locator.Segment.Key = strings.Repeat("A", 64) }},
		{name: "segment occurrence", mutate: func(locator *Locator) { locator.Segment.Occurrence = 0 }},
		{name: "ordinal", mutate: func(locator *Locator) { locator.Segment.Ordinal = 0 }},
		{name: "offset low", mutate: func(locator *Locator) { locator.Segment.Offset = -0.01 }},
		{name: "offset high", mutate: func(locator *Locator) { locator.Segment.Offset = 1.01 }},
		{name: "chapter key", mutate: func(locator *Locator) { locator.Chapter.Key = "bad" }},
		{name: "chapter occurrence", mutate: func(locator *Locator) { locator.Chapter.Occurrence = 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			locator := valid
			chapter := *valid.Chapter
			locator.Chapter = &chapter
			test.mutate(&locator)
			if err := locator.Validate(); err == nil {
				t.Fatalf("invalid locator accepted: %#v", locator)
			}
		})
	}
}
