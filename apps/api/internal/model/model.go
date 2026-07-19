package model

import (
	"time"

	"pandapages/api/internal/readercontract"
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
