package model

type AdminStoryStatus string

const (
	AdminStoryStatusDraftOnly          AdminStoryStatus = "draft_only"
	AdminStoryStatusPublished          AdminStoryStatus = "published"
	AdminStoryStatusPublishedWithDraft AdminStoryStatus = "published_with_draft"
	AdminStoryStatusUnpublished        AdminStoryStatus = "unpublished"
	AdminStoryStatusRepairRequired     AdminStoryStatus = "repair_required"
)

type AdminVersionHealth string

const (
	AdminVersionHealthReady          AdminVersionHealth = "ready"
	AdminVersionHealthRepairRequired AdminVersionHealth = "repair_required"
	AdminVersionHealthUnavailable    AdminVersionHealth = "unavailable"
)

type AdminVersionPointerSummary struct {
	VersionID string `json:"versionId"`
	Version   int    `json:"version"`
}

type AdminStorySummary struct {
	Slug             string                      `json:"slug"`
	Title            string                      `json:"title"`
	Author           *string                     `json:"author"`
	Language         string                      `json:"language"`
	Rights           map[string]any              `json:"rights"`
	SourceURL        *string                     `json:"sourceUrl"`
	Status           AdminStoryStatus            `json:"status"`
	PublishedVersion *AdminVersionPointerSummary `json:"publishedVersion"`
	DraftVersion     *AdminVersionPointerSummary `json:"draftVersion"`
	VersionCount     int                         `json:"versionCount"`
	UpdatedAt        string                      `json:"updatedAt"`
}

type AdminStoriesListResponse struct {
	Items []AdminStorySummary `json:"items"`
}

type AdminVersionSummary struct {
	VersionID    string             `json:"versionId"`
	Version      int                `json:"version"`
	CreatedAt    string             `json:"createdAt"`
	IsDraft      bool               `json:"isDraft"`
	IsPublished  bool               `json:"isPublished"`
	SegmentCount int                `json:"segmentCount"`
	WordCount    int                `json:"wordCount"`
	ChapterCount int                `json:"chapterCount"`
	Health       AdminVersionHealth `json:"health"`
}

type AdminStoryDetailResponse struct {
	Slug             string                      `json:"slug"`
	Title            string                      `json:"title"`
	Author           *string                     `json:"author"`
	Language         string                      `json:"language"`
	Rights           map[string]any              `json:"rights"`
	SourceURL        *string                     `json:"sourceUrl"`
	Status           AdminStoryStatus            `json:"status"`
	PublishedVersion *AdminVersionPointerSummary `json:"publishedVersion"`
	DraftVersion     *AdminVersionPointerSummary `json:"draftVersion"`
	VersionCount     int                         `json:"versionCount"`
	CreatedAt        string                      `json:"createdAt"`
	UpdatedAt        string                      `json:"updatedAt"`
	Versions         []AdminVersionSummary       `json:"versions"`
}

type AdminVersionSourceResponse struct {
	Slug         string             `json:"slug"`
	VersionID    string             `json:"versionId"`
	Version      int                `json:"version"`
	Title        string             `json:"title"`
	Author       *string            `json:"author"`
	Language     string             `json:"language"`
	Rights       map[string]any     `json:"rights"`
	SourceURL    *string            `json:"sourceUrl"`
	Markdown     string             `json:"markdown"`
	RenderedHTML string             `json:"renderedHtml"`
	SegmentCount int                `json:"segmentCount"`
	WordCount    int                `json:"wordCount"`
	ChapterCount int                `json:"chapterCount"`
	CreatedAt    string             `json:"createdAt"`
	IsDraft      bool               `json:"isDraft"`
	IsPublished  bool               `json:"isPublished"`
	Health       AdminVersionHealth `json:"health"`
}

type AdminStoryStatusResponse struct {
	Slug             string                      `json:"slug"`
	Status           AdminStoryStatus            `json:"status"`
	PublishedVersion *AdminVersionPointerSummary `json:"publishedVersion"`
	DraftVersion     *AdminVersionPointerSummary `json:"draftVersion"`
	VersionCount     int                         `json:"versionCount"`
	UpdatedAt        string                      `json:"updatedAt"`
}
