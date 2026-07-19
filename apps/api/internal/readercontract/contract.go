package readercontract

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
)

type SegmentKind string

const (
	SegmentKindHeading   SegmentKind = "heading"
	SegmentKindParagraph SegmentKind = "paragraph"
	SegmentKindOther     SegmentKind = "other"
	canonicalSeparator               = '\x1f'
)

var (
	contentKeyPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	ErrLocatorMismatch = errors.New("reader locator does not match the selected story version")
)

type SegmentIdentityInput struct {
	Kind         SegmentKind
	HeadingLevel *int
	Markdown     string
}

type SegmentIdentity struct {
	Kind              SegmentKind
	HeadingLevel      *int
	ContentKey        string
	ContentOccurrence int
	ChapterKey        *string
	ChapterOccurrence *int
}

// StoredSegmentIdentity is the immutable Reader 2 identity persisted for a
// published segment. It intentionally excludes rendered content: callers can
// validate ordering, occurrences, and chapter propagation without taking
// ownership of Reader rendering or locator policy.
type StoredSegmentIdentity struct {
	Ordinal           int
	Kind              SegmentKind
	HeadingLevel      *int
	ContentKey        string
	ContentOccurrence int
	ChapterKey        *string
	ChapterOccurrence *int
}

type Locator struct {
	Schema  int             `json:"schema"`
	Segment LocatorSegment  `json:"segment"`
	Chapter *LocatorChapter `json:"chapter,omitempty"`
}

type LocatorSegment struct {
	Key        string  `json:"key"`
	Occurrence int     `json:"occurrence"`
	Ordinal    int     `json:"ordinal"`
	Offset     float64 `json:"offset"`
}

type LocatorChapter struct {
	Key        string `json:"key"`
	Occurrence int    `json:"occurrence"`
}

func normalizeLineEndings(markdown string) string {
	markdown = strings.ReplaceAll(markdown, "\r\n", "\n")
	return strings.ReplaceAll(markdown, "\r", "\n")
}

// ContentKey returns the canonical Reader 2 identity for one Markdown block.
// The input is not trimmed or case-folded; only line endings are normalized.
func ContentKey(kind SegmentKind, headingLevel int, markdown string) string {
	canonical := string(kind) + string(canonicalSeparator) + fmt.Sprintf("%d", headingLevel) + string(canonicalSeparator) + normalizeLineEndings(markdown)
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

func validateIdentityInput(input SegmentIdentityInput) (int, error) {
	switch input.Kind {
	case SegmentKindHeading:
		if input.HeadingLevel == nil || *input.HeadingLevel < 1 || *input.HeadingLevel > 6 {
			return 0, fmt.Errorf("heading level must be between 1 and 6")
		}
		return *input.HeadingLevel, nil
	case SegmentKindParagraph, SegmentKindOther:
		if input.HeadingLevel != nil {
			return 0, fmt.Errorf("heading level is only valid for heading segments")
		}
		return 0, nil
	default:
		return 0, fmt.Errorf("unsupported segment kind %q", input.Kind)
	}
}

// AssignSegmentIdentities computes version-scoped content and H2 chapter
// occurrences in stable segment order.
func AssignSegmentIdentities(inputs []SegmentIdentityInput) ([]SegmentIdentity, error) {
	identities := make([]SegmentIdentity, 0, len(inputs))
	contentOccurrences := make(map[string]int)
	chapterOccurrences := make(map[string]int)
	var currentChapterKey *string
	var currentChapterOccurrence *int

	for index, input := range inputs {
		headingLevel, err := validateIdentityInput(input)
		if err != nil {
			return nil, fmt.Errorf("segment %d: %w", index+1, err)
		}

		key := ContentKey(input.Kind, headingLevel, input.Markdown)
		contentOccurrences[key]++
		identity := SegmentIdentity{
			Kind:              input.Kind,
			HeadingLevel:      input.HeadingLevel,
			ContentKey:        key,
			ContentOccurrence: contentOccurrences[key],
		}

		if input.Kind == SegmentKindHeading && headingLevel == 2 {
			chapterOccurrences[key]++
			chapterKey := key
			chapterOccurrence := chapterOccurrences[key]
			currentChapterKey = &chapterKey
			currentChapterOccurrence = &chapterOccurrence
		}

		if currentChapterKey != nil && currentChapterOccurrence != nil {
			chapterKey := *currentChapterKey
			chapterOccurrence := *currentChapterOccurrence
			identity.ChapterKey = &chapterKey
			identity.ChapterOccurrence = &chapterOccurrence
		}
		identities = append(identities, identity)
	}

	return identities, nil
}

// ValidateStoredSegmentIdentities verifies that a stored version still obeys
// the sequence contract produced by AssignSegmentIdentities. Content keys
// cannot be recomputed without loading private Markdown, so this checks their
// canonical shape and version-scoped occurrence/chapter relationships.
func ValidateStoredSegmentIdentities(segments []StoredSegmentIdentity) (int, error) {
	contentOccurrences := make(map[string]int)
	chapterOccurrences := make(map[string]int)
	var currentChapterKey *string
	var currentChapterOccurrence *int
	chapterCount := 0

	for index, segment := range segments {
		if segment.Ordinal != index+1 {
			return 0, fmt.Errorf("segment %d: ordinal must be contiguous from 1", index+1)
		}
		headingLevel, err := validateIdentityInput(SegmentIdentityInput{
			Kind:         segment.Kind,
			HeadingLevel: segment.HeadingLevel,
		})
		if err != nil {
			return 0, fmt.Errorf("segment %d: %w", index+1, err)
		}
		if !ValidContentKey(segment.ContentKey) {
			return 0, fmt.Errorf("segment %d: content key must be lowercase SHA-256 hex", index+1)
		}

		contentOccurrences[segment.ContentKey]++
		if segment.ContentOccurrence != contentOccurrences[segment.ContentKey] {
			return 0, fmt.Errorf("segment %d: content occurrence is not sequential", index+1)
		}

		if segment.Kind == SegmentKindHeading && headingLevel == 2 {
			chapterOccurrences[segment.ContentKey]++
			chapterKey := segment.ContentKey
			chapterOccurrence := chapterOccurrences[segment.ContentKey]
			currentChapterKey = &chapterKey
			currentChapterOccurrence = &chapterOccurrence
			chapterCount++
		}

		if currentChapterKey == nil {
			if segment.ChapterKey != nil || segment.ChapterOccurrence != nil {
				return 0, fmt.Errorf("segment %d: chapter identity exists before the first H2", index+1)
			}
			continue
		}
		if segment.ChapterKey == nil || segment.ChapterOccurrence == nil ||
			*segment.ChapterKey != *currentChapterKey ||
			*segment.ChapterOccurrence != *currentChapterOccurrence {
			return 0, fmt.Errorf("segment %d: chapter identity does not match the current H2", index+1)
		}
	}

	return chapterCount, nil
}

func ValidContentKey(key string) bool {
	return contentKeyPattern.MatchString(key)
}

func (locator Locator) Validate() error {
	if locator.Schema != 2 {
		return fmt.Errorf("schema must equal 2")
	}
	if !ValidContentKey(locator.Segment.Key) {
		return fmt.Errorf("segment key must be lowercase SHA-256 hex")
	}
	if locator.Segment.Occurrence < 1 {
		return fmt.Errorf("segment occurrence must be positive")
	}
	if locator.Segment.Ordinal < 1 {
		return fmt.Errorf("segment ordinal must be positive")
	}
	if math.IsNaN(locator.Segment.Offset) || math.IsInf(locator.Segment.Offset, 0) || locator.Segment.Offset < 0 || locator.Segment.Offset > 1 {
		return fmt.Errorf("segment offset must be between 0 and 1")
	}
	if locator.Chapter != nil {
		if !ValidContentKey(locator.Chapter.Key) {
			return fmt.Errorf("chapter key must be lowercase SHA-256 hex")
		}
		if locator.Chapter.Occurrence < 1 {
			return fmt.Errorf("chapter occurrence must be positive")
		}
	}
	return nil
}
