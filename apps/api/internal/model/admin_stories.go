package model

type AdminStoryListItem struct {
	Slug        string  `json:"slug"`
	Title       string  `json:"title"`
	Author      *string `json:"author,omitempty"`
	Language    string  `json:"language"`
	IsPublished bool    `json:"isPublished"`
	UpdatedAt   string  `json:"updatedAt"` // RFC3339 from DB
	CreatedAt   string  `json:"createdAt"` // RFC3339 from DB

	DraftVersionID     *string `json:"draftVersionId,omitempty"`
	PublishedVersionID *string `json:"publishedVersionId,omitempty"`
}

type AdminStoriesListResponse struct {
	Items []AdminStoryListItem `json:"items"`
}
