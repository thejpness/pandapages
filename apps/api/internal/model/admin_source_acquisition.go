package model

import "errors"

var ErrAdminSourceAcquisitionNotFound = errors.New("source acquisition was not found")

type AdminSourceAcquisitionOutcome string

const (
	AdminSourceAcquisitionOutcomeCreated AdminSourceAcquisitionOutcome = "created"
	AdminSourceAcquisitionOutcomeReused  AdminSourceAcquisitionOutcome = "reused"
)

type AdminSourceAcquisitionReviewStatus string

const (
	AdminSourceAcquisitionReviewPending  AdminSourceAcquisitionReviewStatus = "pending"
	AdminSourceAcquisitionReviewApproved AdminSourceAcquisitionReviewStatus = "approved"
	AdminSourceAcquisitionReviewRejected AdminSourceAcquisitionReviewStatus = "rejected"
)

type AdminSourceAcquisitionContributor struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

type AdminSourceAcquisitionRepresentation struct {
	Label       *string `json:"label,omitempty"`
	MediaType   string  `json:"mediaType"`
	ProviderURL string  `json:"providerUrl"`
	SizeBytes   *int64  `json:"sizeBytes,omitempty"`
}

type AdminSourceAcquisitionReviewDimension struct {
	Status     AdminSourceAcquisitionReviewStatus `json:"status"`
	Note       *string                            `json:"note,omitempty"`
	ReviewedAt *string                            `json:"reviewedAt,omitempty"`
}

type AdminSourceAcquisitionReview struct {
	Rights    AdminSourceAcquisitionReviewDimension `json:"rights"`
	Editorial AdminSourceAcquisitionReviewDimension `json:"editorial"`
}

type AdminSourceAcquisitionSummary struct {
	ID                     string                               `json:"id"`
	Provider               string                               `json:"provider"`
	ExternalID             string                               `json:"externalId"`
	Title                  string                               `json:"title"`
	Contributors           []AdminSourceAcquisitionContributor  `json:"contributors"`
	Languages              []string                             `json:"languages"`
	LandingURL             string                               `json:"landingUrl"`
	ProviderRights         *string                              `json:"providerRights,omitempty"`
	SelectedRepresentation AdminSourceAcquisitionRepresentation `json:"selectedRepresentation"`
	NormalisationVersion   string                               `json:"normalisationVersion"`
	RetrievedContentHash   string                               `json:"retrievedContentHash"`
	NormalisedContentHash  string                               `json:"normalisedContentHash"`
	SnapshotHash           string                               `json:"snapshotHash"`
	CreatedAt              string                               `json:"createdAt"`
	Review                 AdminSourceAcquisitionReview         `json:"review"`
}

type AdminSourceAcquisitionDetail struct {
	AdminSourceAcquisitionSummary
	SourceText string `json:"sourceText"`
}

type AdminSourceAcquisitionsListResponse struct {
	Items []AdminSourceAcquisitionSummary `json:"items"`
}

type AdminSourceAcquisitionPersistResponse struct {
	Outcome     AdminSourceAcquisitionOutcome `json:"outcome"`
	Acquisition AdminSourceAcquisitionSummary `json:"acquisition"`
}

type AdminSourceAcquisitionReviewUpdateRequest struct {
	Status AdminSourceAcquisitionReviewStatus `json:"status"`
	Note   string                             `json:"note"`
}
