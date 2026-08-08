package model

type AdminStoryEditionKey string

const (
	AdminStoryEditionClassic          AdminStoryEditionKey = "classic"
	AdminStoryEditionConfidentReaders AdminStoryEditionKey = "confident-readers"
	AdminStoryEditionGrowingReaders   AdminStoryEditionKey = "growing-readers"
	AdminStoryEditionStoryExplorers   AdminStoryEditionKey = "story-explorers"
	AdminStoryEditionLittleListeners  AdminStoryEditionKey = "little-listeners"
)

var canonicalAdminStoryEditionKeys = [...]AdminStoryEditionKey{
	AdminStoryEditionClassic,
	AdminStoryEditionConfidentReaders,
	AdminStoryEditionGrowingReaders,
	AdminStoryEditionStoryExplorers,
	AdminStoryEditionLittleListeners,
}

func AdminStoryEditionKeys() []AdminStoryEditionKey {
	keys := make([]AdminStoryEditionKey, len(canonicalAdminStoryEditionKeys))
	copy(keys, canonicalAdminStoryEditionKeys[:])
	return keys
}

func ValidAdminStoryEditionKey(key AdminStoryEditionKey) bool {
	switch key {
	case AdminStoryEditionClassic,
		AdminStoryEditionConfidentReaders,
		AdminStoryEditionGrowingReaders,
		AdminStoryEditionStoryExplorers,
		AdminStoryEditionLittleListeners:
		return true
	default:
		return false
	}
}

type AdminStoryStatus string

const (
	AdminStoryStatusDraftOnly          AdminStoryStatus = "draft_only"
	AdminStoryStatusPublished          AdminStoryStatus = "published"
	AdminStoryStatusPublishedWithDraft AdminStoryStatus = "published_with_draft"
	AdminStoryStatusUnpublished        AdminStoryStatus = "unpublished"
	AdminStoryStatusRepairRequired     AdminStoryStatus = "repair_required"
)

type AdminEditionStatus string

const (
	AdminEditionStatusEmpty              AdminEditionStatus = "empty"
	AdminEditionStatusDraftOnly          AdminEditionStatus = "draft_only"
	AdminEditionStatusPublished          AdminEditionStatus = "published"
	AdminEditionStatusPublishedWithDraft AdminEditionStatus = "published_with_draft"
	AdminEditionStatusUnpublished        AdminEditionStatus = "unpublished"
	AdminEditionStatusRepairRequired     AdminEditionStatus = "repair_required"
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

type AdminEditionSummary struct {
	EditionKey       AdminStoryEditionKey        `json:"editionKey"`
	Status           AdminEditionStatus          `json:"status"`
	PublishedVersion *AdminVersionPointerSummary `json:"publishedVersion"`
	DraftVersion     *AdminVersionPointerSummary `json:"draftVersion"`
	VersionCount     int                         `json:"versionCount"`
	UpdatedAt        *string                     `json:"updatedAt"`
}

type AdminEditionDetail struct {
	EditionKey       AdminStoryEditionKey        `json:"editionKey"`
	Status           AdminEditionStatus          `json:"status"`
	PublishedVersion *AdminVersionPointerSummary `json:"publishedVersion"`
	DraftVersion     *AdminVersionPointerSummary `json:"draftVersion"`
	VersionCount     int                         `json:"versionCount"`
	UpdatedAt        *string                     `json:"updatedAt"`
	Versions         []AdminVersionSummary       `json:"versions"`
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
	Source           AdminStorySourceSummary     `json:"source"`
	Editions         []AdminEditionSummary       `json:"editions"`
	CurrentRelease   *AdminReleaseSummary        `json:"currentRelease"`
	ReleaseCount     int                         `json:"releaseCount"`
}

type AdminStoriesListResponse struct {
	Items []AdminStorySummary `json:"items"`
}

type AdminVersionSummary struct {
	EditionKey   AdminStoryEditionKey `json:"editionKey"`
	VersionID    string               `json:"versionId"`
	Version      int                  `json:"version"`
	CreatedAt    string               `json:"createdAt"`
	IsDraft      bool                 `json:"isDraft"`
	IsPublished  bool                 `json:"isPublished"`
	SegmentCount int                  `json:"segmentCount"`
	WordCount    int                  `json:"wordCount"`
	ChapterCount int                  `json:"chapterCount"`
	Health       AdminVersionHealth   `json:"health"`
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
	Source           AdminStorySourceSummary     `json:"source"`
	Versions         []AdminVersionSummary       `json:"versions"`
	Editions         []AdminEditionDetail        `json:"editions"`
	CurrentRelease   *AdminReleaseSummary        `json:"currentRelease"`
	ReleaseCount     int                         `json:"releaseCount"`
	Releases         []AdminReleaseSummary       `json:"releases"`
}

type AdminVersionSourceResponse struct {
	Slug         string               `json:"slug"`
	EditionKey   AdminStoryEditionKey `json:"editionKey"`
	VersionID    string               `json:"versionId"`
	Version      int                  `json:"version"`
	Title        string               `json:"title"`
	Author       *string              `json:"author"`
	Language     string               `json:"language"`
	Rights       map[string]any       `json:"rights"`
	SourceURL    *string              `json:"sourceUrl"`
	Markdown     string               `json:"markdown"`
	RenderedHTML string               `json:"renderedHtml"`
	SegmentCount int                  `json:"segmentCount"`
	WordCount    int                  `json:"wordCount"`
	ChapterCount int                  `json:"chapterCount"`
	CreatedAt    string               `json:"createdAt"`
	IsDraft      bool                 `json:"isDraft"`
	IsPublished  bool                 `json:"isPublished"`
	Health       AdminVersionHealth   `json:"health"`
}

type AdminStoryStatusResponse struct {
	Slug             string                      `json:"slug"`
	Status           AdminStoryStatus            `json:"status"`
	PublishedVersion *AdminVersionPointerSummary `json:"publishedVersion"`
	DraftVersion     *AdminVersionPointerSummary `json:"draftVersion"`
	VersionCount     int                         `json:"versionCount"`
	UpdatedAt        string                      `json:"updatedAt"`
	CurrentRelease   *AdminReleaseSummary        `json:"currentRelease"`
	ReleaseCount     int                         `json:"releaseCount"`
}
