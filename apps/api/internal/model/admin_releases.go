package model

type AdminReleaseEditionRequest struct {
	EditionKey AdminStoryEditionKey `json:"editionKey"`
	VersionID  string               `json:"versionId"`
}

type AdminCreateReleaseRequest struct {
	Editions []AdminReleaseEditionRequest `json:"editions"`
}

type AdminReleaseEditionSummary struct {
	EditionKey AdminStoryEditionKey `json:"editionKey"`
	VersionID  string               `json:"versionId"`
	Version    int                  `json:"version"`
}

type AdminReleaseSummary struct {
	Release   int                          `json:"release"`
	CreatedAt string                       `json:"createdAt"`
	Editions  []AdminReleaseEditionSummary `json:"editions"`
}

type AdminReleaseOutcome string

const (
	AdminReleaseOutcomeCreated       AdminReleaseOutcome = "created"
	AdminReleaseOutcomeReusedCurrent AdminReleaseOutcome = "reused_current"
)

type AdminCreateReleaseResponse struct {
	Slug    string              `json:"slug"`
	Outcome AdminReleaseOutcome `json:"outcome"`
	Release AdminReleaseSummary `json:"release"`
}
