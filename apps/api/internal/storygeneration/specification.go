package storygeneration

import (
	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
)

type SpecificationVersion string

const (
	SpecificationV2   SpecificationVersion = "panda-pages-adaptation-v2"
	GenerationModelV2                      = "gpt-5.6-terra"
)

// DerivedEditionKeysV2 returns the canonical modern edition ladder defined by
// the adaptation contract. Classic is authoritative source content and is not
// an AI generation target.
func DerivedEditionKeysV2() []model.AdminStoryEditionKey {
	return adaptationcontract.ModernEditionKeys()
}
