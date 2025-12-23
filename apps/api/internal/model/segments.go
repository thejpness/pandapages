package model

import "encoding/json"

type Segment struct {
	Ordinal      int             `json:"ordinal"`
	Locator      json.RawMessage `json:"locator"`
	RenderedHTML string          `json:"renderedHtml"`
}

type StorySegmentsPayload struct {
	Slug     string    `json:"slug"`
	Version  int       `json:"version"`
	Segments []Segment `json:"segments"`
}
