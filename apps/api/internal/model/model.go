package model

import (
	"errors"
	"time"

	"pandapages/api/internal/readercontract"
)

var (
	// ErrAdminPublishNotFound preserves the existing client-visible missing
	// story/version status without disclosing ownership boundaries.
	ErrAdminPublishNotFound = errors.New("story version was not found")
	// ErrAdminPublishInvalid marks an expected publish refusal whose public
	// response must not reveal which internal invariant failed.
	ErrAdminPublishInvalid = errors.New("story version cannot be published")
	// ErrAdminReleaseNotFound covers missing story, edition, and version release
	// targets without disclosing ownership boundaries.
	ErrAdminReleaseNotFound = errors.New("story release target was not found")
	// ErrAdminReleaseInvalid marks a release refusal caused by unreadable
	// immutable content or inconsistent current-release projections.
	ErrAdminReleaseInvalid = errors.New("story release requires repair")
	// ErrAdminVersionRepairRequired marks a corrupt idempotency target that must
	// not be reused or mutated as though it were a healthy immutable version.
	ErrAdminVersionRepairRequired = errors.New("stored story version requires repair")
	// ErrAdminStoryNotFound intentionally covers missing, cross-account, and
	// cross-story admin targets so ownership boundaries are not disclosed.
	ErrAdminStoryNotFound = errors.New("admin story resource was not found")
	// ErrAdminSourceNotFound covers missing and out-of-account canonical-source
	// targets without disclosing ownership boundaries.
	ErrAdminSourceNotFound = errors.New("canonical story source was not found")
	// ErrAdminSourceRepairRequired refuses to present or reuse a canonical source
	// revision whose immutable snapshot no longer validates.
	ErrAdminSourceRepairRequired = errors.New("canonical story source requires repair")
	// ErrProfileNameConflict is returned when the account-local unique profile
	// name constraint rejects a create or rename request.
	ErrProfileNameConflict = errors.New("reader profile name already exists")
	// ErrProfilePINInvalid deliberately does not distinguish a wrong PIN from
	// any internal verification detail.
	ErrProfilePINInvalid = errors.New("reader profile PIN is invalid")
	// ErrProfilePINRateLimited means repeated failed attempts temporarily block
	// verification for this profile only.
	ErrProfilePINRateLimited = errors.New("reader profile PIN verification is rate limited")
)

type StoryItem struct {
	Slug             string                  `json:"slug"`
	Title            string                  `json:"title"`
	Author           *string                 `json:"author,omitempty"`
	Language         string                  `json:"language"`
	PublishedVersion int                     `json:"publishedVersion"`
	WordCount        int64                   `json:"wordCount"`
	ChapterCount     int64                   `json:"chapterCount"`
	Progress         *LibraryProgressSummary `json:"progress"`
}

// LibraryReadModel is the account-scoped bookshelf response. Items that cannot
// be represented safely from their immutable published version are omitted and
// counted without exposing their metadata or internal identifiers.
type LibraryReadModel struct {
	Items                []StoryItem `json:"items"`
	UnavailableItemCount int64       `json:"unavailableItemCount"`
}

type LibraryProgressSummary struct {
	Version          int       `json:"version"`
	Percent          float64   `json:"percent"`
	UpdatedAt        time.Time `json:"updatedAt"`
	IsCurrentVersion bool      `json:"isCurrentVersion"`
}

// ReaderLibraryEditionSummary describes one immutable current-release edition
// that the selected profile is allowed to open. The list is always in canonical
// Reading Level order.
type ReaderLibraryEditionSummary struct {
	EditionKey   ReaderEditionKey `json:"editionKey"`
	Version      int              `json:"version"`
	WordCount    int64            `json:"wordCount"`
	ChapterCount int64            `json:"chapterCount"`
}

// ReaderLibraryProgressSummary deliberately omits the persisted locator. The
// Library needs only truthful display state; Reader remains the owner of exact
// resume and cross-version mapping.
type ReaderLibraryProgressSummary struct {
	Version           int       `json:"version"`
	Percent           float64   `json:"percent"`
	UpdatedAt         time.Time `json:"updatedAt"`
	IsResolvedVersion bool      `json:"isResolvedVersion"`
}

// ReaderLibraryItem is profile-scoped. A chooser has no selected edition;
// selected items identify the exact edition chosen by the canonical resolver.
type ReaderLibraryItem struct {
	Slug             string                        `json:"slug"`
	Title            string                        `json:"title"`
	Author           *string                       `json:"author,omitempty"`
	Language         string                        `json:"language"`
	State            ReaderResolutionState         `json:"state"`
	EligibleEditions []ReaderLibraryEditionSummary `json:"eligibleEditions"`
	SelectedEdition  *ReaderEditionKey             `json:"selectedEdition"`
	Progress         *ReaderLibraryProgressSummary `json:"progress"`
}

// ReaderLibraryReadModel is the profile-scoped Reader bookshelf. Stories with
// no eligible current-release edition are invisible. Stories whose eligible
// immutable release state cannot be represented safely are omitted and counted.
type ReaderLibraryReadModel struct {
	Items                []ReaderLibraryItem `json:"items"`
	UnavailableItemCount int64               `json:"unavailableItemCount"`
}

type ReaderStory struct {
	Slug     string          `json:"slug"`
	Title    string          `json:"title"`
	Author   *string         `json:"author"`
	Language string          `json:"language"`
	Version  int             `json:"version"`
	Segments []ReaderSegment `json:"segments"`
}

type ReaderResolvedStory struct {
	ReaderStory
	EditionKey ReaderEditionKey `json:"editionKey"`
}

type ReaderResolutionState string

const (
	ReaderResolutionSelected ReaderResolutionState = "selected"
	ReaderResolutionChooser  ReaderResolutionState = "chooser"
)

type ReaderResolution struct {
	State            ReaderResolutionState `json:"state"`
	EligibleEditions []ReaderEditionKey    `json:"eligibleEditions"`
	Story            *ReaderResolvedStory  `json:"story"`
}

type ReaderSegment struct {
	Ordinal           int     `json:"ordinal"`
	Kind              string  `json:"kind"`
	HeadingLevel      *int    `json:"headingLevel"`
	ContentKey        string  `json:"contentKey"`
	ContentOccurrence int     `json:"contentOccurrence"`
	ChapterKey        *string `json:"chapterKey"`
	ChapterOccurrence *int    `json:"chapterOccurrence"`
	RenderedHTML      string  `json:"renderedHtml"`
	WordCount         int     `json:"wordCount"`
}

type Progress struct {
	Version int                    `json:"version"`
	Locator readercontract.Locator `json:"locator"`
	Percent float64                `json:"percent"`
}

type ProgressResponse struct {
	Progress *Progress `json:"progress"`
}

// Used by /api/v1/continue
type ContinueItem struct {
	Slug      string    `json:"slug"`
	Percent   float64   `json:"percent"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ReaderEditionKey = AdminStoryEditionKey

const (
	ReaderEditionClassic          = AdminStoryEditionClassic
	ReaderEditionConfidentReaders = AdminStoryEditionConfidentReaders
	ReaderEditionGrowingReaders   = AdminStoryEditionGrowingReaders
	ReaderEditionStoryExplorers   = AdminStoryEditionStoryExplorers
	ReaderEditionLittleListeners  = AdminStoryEditionLittleListeners
)

func ReaderEditionKeys() []ReaderEditionKey {
	return AdminStoryEditionKeys()
}

func ValidReaderEditionKey(key ReaderEditionKey) bool {
	return ValidAdminStoryEditionKey(key)
}

// ReaderProfile is the intentionally small, account-scoped profile selection
// representation. Profile ownership is always checked against the selected
// account by the caller's authorization boundary.
type ReaderProfile struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	PINEnabled   bool             `json:"pin_enabled"`
	ReadingLevel ReaderEditionKey `json:"reading_level"`
}
