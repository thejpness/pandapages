package db

import (
	"time"

	"pandapages/api/internal/model"
)

func (stored storedSourceAcquisition) promotion() (*model.AdminSourceAcquisitionPromotion, error) {
	valuesPresent := stored.PromotionStoryID.Valid || stored.PromotionStorySlug.Valid || stored.PromotionStoryTitle.Valid || stored.PromotionVersionID.Valid || stored.PromotionVersion.Valid || stored.PromotionCreatedAt.Valid
	if !valuesPresent {
		return nil, nil
	}
	if !stored.PromotionStoryID.Valid || !stored.PromotionStorySlug.Valid || !stored.PromotionStoryTitle.Valid || !stored.PromotionVersionID.Valid || !stored.PromotionVersion.Valid || !stored.PromotionCreatedAt.Valid ||
		!accountIDRe.MatchString(stored.PromotionStoryID.String) || !accountIDRe.MatchString(stored.PromotionVersionID.String) ||
		!validSourceAcquisitionText(stored.PromotionStorySlug.String, 255) || !validSourceAcquisitionText(stored.PromotionStoryTitle.String, 1000) ||
		stored.PromotionVersion.Int64 < 1 || int64(int(stored.PromotionVersion.Int64)) != stored.PromotionVersion.Int64 {
		return nil, errStoredSourceAcquisitionInvalid
	}
	return &model.AdminSourceAcquisitionPromotion{
		StoryID:         stored.PromotionStoryID.String,
		StorySlug:       stored.PromotionStorySlug.String,
		StoryTitle:      stored.PromotionStoryTitle.String,
		SourceVersionID: stored.PromotionVersionID.String,
		SourceVersion:   int(stored.PromotionVersion.Int64),
		PromotedAt:      stored.PromotionCreatedAt.Time.UTC().Format(time.RFC3339Nano),
	}, nil
}
