package model

type AdminSourceStatus string

const (
	AdminSourceStatusMissing        AdminSourceStatus = "missing"
	AdminSourceStatusReady          AdminSourceStatus = "ready"
	AdminSourceStatusRepairRequired AdminSourceStatus = "repair_required"
)

type AdminSourceOutcome string

const (
	AdminSourceOutcomeCreatedSource  AdminSourceOutcome = "created_source"
	AdminSourceOutcomeCreatedVersion AdminSourceOutcome = "created_version"
	AdminSourceOutcomeReused         AdminSourceOutcome = "reused"
)

type AdminSourceUpsertRequest struct {
	Title      string         `json:"title"`
	Author     *string        `json:"author"`
	Language   *string        `json:"language"`
	SourceURL  *string        `json:"sourceUrl"`
	Rights     map[string]any `json:"rights"`
	SourceText string         `json:"sourceText"`
}

type AdminSourceVersionPointerSummary struct {
	VersionID string `json:"versionId"`
	Version   int    `json:"version"`
}

type AdminStorySourceSummary struct {
	Status         AdminSourceStatus                 `json:"status"`
	CurrentVersion *AdminSourceVersionPointerSummary `json:"currentVersion"`
	VersionCount   int                               `json:"versionCount"`
	UpdatedAt      *string                           `json:"updatedAt"`
}

type AdminSourceVersionSummary struct {
	VersionID string         `json:"versionId"`
	Version   int            `json:"version"`
	Title     string         `json:"title"`
	Author    *string        `json:"author"`
	Language  string         `json:"language"`
	Rights    map[string]any `json:"rights"`
	SourceURL *string        `json:"sourceUrl"`
	CreatedAt string         `json:"createdAt"`
	IsCurrent bool           `json:"isCurrent"`
}

type AdminSourceDetailResponse struct {
	Slug           string                            `json:"slug"`
	Status         AdminSourceStatus                 `json:"status"`
	CurrentVersion *AdminSourceVersionPointerSummary `json:"currentVersion"`
	VersionCount   int                               `json:"versionCount"`
	UpdatedAt      *string                           `json:"updatedAt"`
	Versions       []AdminSourceVersionSummary       `json:"versions"`
}

type AdminSourceVersionResponse struct {
	Slug       string         `json:"slug"`
	VersionID  string         `json:"versionId"`
	Version    int            `json:"version"`
	Title      string         `json:"title"`
	Author     *string        `json:"author"`
	Language   string         `json:"language"`
	Rights     map[string]any `json:"rights"`
	SourceURL  *string        `json:"sourceUrl"`
	SourceText string         `json:"sourceText"`
	CreatedAt  string         `json:"createdAt"`
	IsCurrent  bool           `json:"isCurrent"`
}

type AdminSourceUpsertResponse struct {
	Slug      string             `json:"slug"`
	VersionID string             `json:"versionId"`
	Version   int                `json:"version"`
	Outcome   AdminSourceOutcome `json:"outcome"`
}
