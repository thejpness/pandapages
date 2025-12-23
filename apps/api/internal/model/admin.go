package model

import "encoding/json"

type AdminPreviewRequest struct {
	Markdown string `json:"markdown"`
}

type AdminPreviewResponse struct {
	RenderedHTML string         `json:"renderedHtml"`
	Segments     []AdminSegment `json:"segments"`
}

type AdminDraftUpsertRequest struct {
	Slug      string         `json:"slug"`
	Title     string         `json:"title"`
	Author    *string        `json:"author"`
	Markdown  string         `json:"markdown"`
	Language  *string        `json:"language"`
	SourceURL *string        `json:"sourceUrl"`
	Rights    map[string]any `json:"rights"`
}

type AdminDraftUpsertResponse struct {
	StoryID        string `json:"storyId"`
	StoryVersionID string `json:"storyVersionId"`
	Slug           string `json:"slug"`
	Version        int    `json:"version"`
	SegmentsCount  int    `json:"segmentsCount"`
	RenderedHTML   string `json:"renderedHtml"`
}

type AdminSegment struct {
	Ordinal      int             `json:"ordinal"`
	Locator      json.RawMessage `json:"locator"`
	RenderedHTML string          `json:"renderedHtml"`
}
