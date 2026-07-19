package model

import (
	"errors"
	"time"

	"pandapages/api/internal/readercontract"
)

var (
	// ErrAdminPublishNotFound preserves the existing client-visible missing
	// story/version status without disclosing ownership boundaries.
	ErrAdminPublishNotFound = errors.New("story version was not found")
	// ErrAdminPublishInvalid marks an expected publish refusal whose public
	// response must not reveal which internal invariant failed.
	ErrAdminPublishInvalid = errors.New("story version cannot be published")
	// ErrAdminVersionRepairRequired marks a corrupt idempotency target that must
	// not be reused or mutated as though it were a healthy immutable version.
	ErrAdminVersionRepairRequired = errors.New("stored story version requires repair")
)

type StoryItem struct {
	Slug             string                  `json:"slug"`
	Title            string                  `json:"title"`
	Author           *string                 `json:"author,omitempty"`
	Language         string                  `json:"language"`
	PublishedVersion int                     `json:"publishedVersion"`
	WordCount        int64                   `json:"wordCount"`
	ChapterCount     int64                   `json:"chapterCount"`
	Progress         *LibraryProgressSummary `json:"progress"`
}

// LibraryReadModel is the account-scoped bookshelf response. Items that cannot
// be represented safely from their immutable published version are omitted and
// counted without exposing their metadata or internal identifiers.
type LibraryReadModel struct {
	Items                []StoryItem `json:"items"`
	UnavailableItemCount int64       `json:"unavailableItemCount"`
}

type LibraryProgressSummary struct {
	Version          int       `json:"version"`
	Percent          float64   `json:"percent"`
	UpdatedAt        time.Time `json:"updatedAt"`
	IsCurrentVersion bool      `json:"isCurrentVersion"`
}

type ReaderStory struct {
	Slug     string          `json:"slug"`
	Title    string          `json:"title"`
	Author   *string         `json:"author"`
	Language string          `json:"language"`
	Version  int             `json:"version"`
	Segments []ReaderSegment `json:"segments"`
}

type ReaderSegment struct {
	Ordinal           int     `json:"ordinal"`
	Kind              string  `json:"kind"`
	HeadingLevel      *int    `json:"headingLevel"`
	ContentKey        string  `json:"contentKey"`
	ContentOccurrence int     `json:"contentOccurrence"`
	ChapterKey        *string `json:"chapterKey"`
	ChapterOccurrence *int    `json:"chapterOccurrence"`
	RenderedHTML      string  `json:"renderedHtml"`
	WordCount         int     `json:"wordCount"`
}

type Progress struct {
	Version int                    `json:"version"`
	Locator readercontract.Locator `json:"locator"`
	Percent float64                `json:"percent"`
}

type ProgressResponse struct {
	Progress *Progress `json:"progress"`
}

// Used by /api/v1/continue
type ContinueItem struct {
	Slug      string    `json:"slug"`
	Percent   float64   `json:"percent"`
	UpdatedAt time.Time `json:"updatedAt"`
}
