package model

import (
	"encoding/json"
	"time"
)

type StoryItem struct {
	Slug   string  `json:"slug"`
	Title  string  `json:"title"`
	Author *string `json:"author,omitempty"`
}

type StoryPayload struct {
	Slug         string  `json:"slug"`
	Title        string  `json:"title"`
	Author       *string `json:"author,omitempty"`
	Version      int     `json:"version"`
	RenderedHTML string  `json:"renderedHtml"`
}

type ProgressState struct {
	Version int             `json:"version"`
	Locator json.RawMessage `json:"locator"`
	Percent float64         `json:"percent"`
}

// Used by /api/v1/continue
type ContinueItem struct {
	Slug      string    `json:"slug"`
	Percent   float64   `json:"percent"`
	UpdatedAt time.Time `json:"updatedAt"`
}
