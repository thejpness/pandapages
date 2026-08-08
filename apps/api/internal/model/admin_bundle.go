package model

type AdminEditionBundleInput struct {
	EditionKey AdminStoryEditionKey `json:"editionKey"`
	Markdown   string               `json:"markdown"`
}

type AdminEditionBundleUpsertRequest struct {
	Slug      string                    `json:"slug"`
	Title     string                    `json:"title"`
	Author    *string                   `json:"author"`
	Language  *string                   `json:"language"`
	SourceURL *string                   `json:"sourceUrl"`
	Rights    map[string]any            `json:"rights"`
	Editions  []AdminEditionBundleInput `json:"editions"`
}

type AdminEditionIngestOutcome string

const (
	AdminEditionIngestOutcomeCreated AdminEditionIngestOutcome = "created"
	AdminEditionIngestOutcomeReused  AdminEditionIngestOutcome = "reused"
)

type AdminEditionBundleResult struct {
	EditionKey   AdminStoryEditionKey      `json:"editionKey"`
	VersionID    string                    `json:"versionId"`
	Version      int                       `json:"version"`
	SegmentCount int                       `json:"segmentCount"`
	WordCount    int                       `json:"wordCount"`
	ChapterCount int                       `json:"chapterCount"`
	Outcome      AdminEditionIngestOutcome `json:"outcome"`
}

type AdminEditionBundleUpsertResponse struct {
	Slug    string                     `json:"slug"`
	Results []AdminEditionBundleResult `json:"results"`
}
