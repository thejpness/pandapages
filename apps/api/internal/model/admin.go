package model

import "fmt"

type AdminStoryInput struct {
	Slug      string         `json:"slug"`
	Title     string         `json:"title"`
	Author    *string        `json:"author"`
	Markdown  string         `json:"markdown"`
	Language  *string        `json:"language"`
	SourceURL *string        `json:"sourceUrl"`
	Rights    map[string]any `json:"rights"`
}

// Preview and draft creation deliberately share one input contract and one
// canonicalisation path.
type AdminPreviewRequest = AdminStoryInput
type AdminDraftUpsertRequest = AdminStoryInput

type AdminValidationIssue struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type AdminValidationError struct {
	Issues []AdminValidationIssue
}

func (e *AdminValidationError) Error() string {
	return fmt.Sprintf("admin story input has %d validation issue(s)", len(e.Issues))
}

type AdminPreviewResponse struct {
	Slug         string                 `json:"slug"`
	Title        string                 `json:"title"`
	Author       *string                `json:"author"`
	Language     string                 `json:"language"`
	Rights       map[string]any         `json:"rights"`
	SourceURL    *string                `json:"sourceUrl"`
	RenderedHTML string                 `json:"renderedHtml"`
	SegmentCount int                    `json:"segmentCount"`
	WordCount    int                    `json:"wordCount"`
	ChapterCount int                    `json:"chapterCount"`
	Warnings     []AdminValidationIssue `json:"warnings"`
}

type AdminDraftOutcome string

const (
	AdminDraftOutcomeCreatedStory   AdminDraftOutcome = "created_story"
	AdminDraftOutcomeCreatedVersion AdminDraftOutcome = "created_version"
	AdminDraftOutcomeReused         AdminDraftOutcome = "reused"
)

type AdminDraftUpsertResponse struct {
	Slug         string            `json:"slug"`
	VersionID    string            `json:"versionId"`
	Version      int               `json:"version"`
	SegmentCount int               `json:"segmentCount"`
	WordCount    int               `json:"wordCount"`
	ChapterCount int               `json:"chapterCount"`
	RenderedHTML string            `json:"renderedHtml"`
	Outcome      AdminDraftOutcome `json:"outcome"`

	// These aliases keep existing Store-level tests and internal callers source
	// compatible without exposing database story IDs or legacy field names.
	StoryID        string `json:"-"`
	StoryVersionID string `json:"-"`
	SegmentsCount  int    `json:"-"`
}
