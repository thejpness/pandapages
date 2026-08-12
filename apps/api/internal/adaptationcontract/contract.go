package adaptationcontract

import (
	"fmt"
	"strings"

	"pandapages/api/internal/model"
)

type ContractVersion string

const VersionV1 ContractVersion = "panda-pages-adaptation-v1"

type AssessmentScope string

const (
	AssessmentScopeEdition AssessmentScope = "edition"
	AssessmentScopeBundle  AssessmentScope = "bundle"
)

type Result string

const (
	ResultPass        Result = "pass"
	ResultNeedsReview Result = "needs_review"
	ResultFail        Result = "fail"
)

type FindingSeverity string

const (
	FindingSeverityBlocking FindingSeverity = "blocking"
	FindingSeverityReview   FindingSeverity = "review"
)

type FindingKind string

const (
	FindingKindSemantic   FindingKind = "semantic"
	FindingKindStructural FindingKind = "structural"
)

type FindingCode string

const (
	FindingCorePlotChanged                       FindingCode = "core_plot_changed"
	FindingMajorOutcomeChanged                   FindingCode = "major_outcome_changed"
	FindingClimaxChanged                         FindingCode = "climax_changed"
	FindingResolutionChanged                     FindingCode = "resolution_changed"
	FindingMainCharacterChanged                  FindingCode = "main_character_changed"
	FindingRelationshipChanged                   FindingCode = "relationship_changed"
	FindingMotivationChanged                     FindingCode = "motivation_changed"
	FindingCausalChainBroken                     FindingCode = "causal_chain_broken"
	FindingStakesRemoved                         FindingCode = "stakes_removed"
	FindingSubstantialMaterialInvented           FindingCode = "substantial_material_invented"
	FindingInventedMoralising                    FindingCode = "invented_moralising"
	FindingContinuityError                       FindingCode = "continuity_error"
	FindingSurvivorContinuityError               FindingCode = "survivor_continuity_error"
	FindingCoercionRomanticised                  FindingCode = "coercion_romanticised"
	FindingEditionIdentityLost                   FindingCode = "edition_identity_lost"
	FindingEditionProgressionNotDistinct         FindingCode = "edition_progression_not_distinct"
	FindingEditionProgressionInverted            FindingCode = "edition_progression_inverted"
	FindingScopeTooRich                          FindingCode = "scope_too_rich"
	FindingScopeTooThin                          FindingCode = "scope_too_thin"
	FindingVocabularyMismatch                    FindingCode = "vocabulary_mismatch"
	FindingContentIntensityMismatch              FindingCode = "content_intensity_mismatch"
	FindingHistoricalContextQuestionable         FindingCode = "historical_context_questionable"
	FindingIconicLanguageRemoved                 FindingCode = "iconic_language_removed"
	FindingConnectiveMaterialQuestionable        FindingCode = "connective_material_questionable"
	FindingLethalOutcomeSubstitutionQuestionable FindingCode = "lethal_outcome_substitution_questionable"
	FindingEditionProgressionQuestionable        FindingCode = "edition_progression_questionable"
	FindingInvalidEditionKey                     FindingCode = "invalid_edition_key"
	FindingInvalidUTF8                           FindingCode = "invalid_utf8"
	FindingEmptyMarkdown                         FindingCode = "empty_markdown"
	FindingMissingH1Title                        FindingCode = "missing_h1_title"
	FindingRawHTMLPresent                        FindingCode = "raw_html_present"
	FindingClassicSourceChanged                  FindingCode = "classic_source_changed"
	FindingIngestIncompatible                    FindingCode = "ingest_incompatible"
)

type Finding struct {
	Code     FindingCode     `json:"code"`
	Severity FindingSeverity `json:"severity"`
	Message  string          `json:"message"`
}

type Assessment struct {
	ContractVersion ContractVersion              `json:"contractVersion"`
	AssessmentScope AssessmentScope              `json:"assessmentScope"`
	EditionKey      *model.AdminStoryEditionKey  `json:"editionKey,omitempty"`
	EditionKeys     []model.AdminStoryEditionKey `json:"editionKeys,omitempty"`
	Result          Result                       `json:"result"`
	Findings        []Finding                    `json:"findings"`
}

type findingSpec struct {
	severity       FindingSeverity
	kind           FindingKind
	editionAllowed bool
	bundleAllowed  bool
}

var findingSpecs = map[FindingCode]findingSpec{
	FindingCorePlotChanged:                       semanticEdition(FindingSeverityBlocking),
	FindingMajorOutcomeChanged:                   semanticEdition(FindingSeverityBlocking),
	FindingClimaxChanged:                         semanticEdition(FindingSeverityBlocking),
	FindingResolutionChanged:                     semanticEdition(FindingSeverityBlocking),
	FindingMainCharacterChanged:                  semanticEdition(FindingSeverityBlocking),
	FindingRelationshipChanged:                   semanticEdition(FindingSeverityBlocking),
	FindingMotivationChanged:                     semanticEdition(FindingSeverityBlocking),
	FindingCausalChainBroken:                     semanticEdition(FindingSeverityBlocking),
	FindingStakesRemoved:                         semanticEdition(FindingSeverityBlocking),
	FindingSubstantialMaterialInvented:           semanticEdition(FindingSeverityBlocking),
	FindingInventedMoralising:                    semanticEdition(FindingSeverityBlocking),
	FindingContinuityError:                       semanticEdition(FindingSeverityBlocking),
	FindingSurvivorContinuityError:               semanticEdition(FindingSeverityBlocking),
	FindingCoercionRomanticised:                  semanticEdition(FindingSeverityBlocking),
	FindingEditionIdentityLost:                   semanticEdition(FindingSeverityBlocking),
	FindingEditionProgressionNotDistinct:         semanticBundle(FindingSeverityBlocking),
	FindingEditionProgressionInverted:            semanticBundle(FindingSeverityBlocking),
	FindingScopeTooRich:                          semanticEdition(FindingSeverityReview),
	FindingScopeTooThin:                          semanticEdition(FindingSeverityReview),
	FindingVocabularyMismatch:                    semanticEdition(FindingSeverityReview),
	FindingContentIntensityMismatch:              semanticEdition(FindingSeverityReview),
	FindingHistoricalContextQuestionable:         semanticEdition(FindingSeverityReview),
	FindingIconicLanguageRemoved:                 semanticEdition(FindingSeverityReview),
	FindingConnectiveMaterialQuestionable:        semanticEdition(FindingSeverityReview),
	FindingLethalOutcomeSubstitutionQuestionable: semanticEdition(FindingSeverityReview),
	FindingEditionProgressionQuestionable:        semanticBundle(FindingSeverityReview),

	// Structural codes are contract vocabulary for deterministic validation.
	// The five modern-edition content findings can participate in a combined
	// edition assessment. invalid_edition_key and classic_source_changed are
	// validated outside modern semantic assessment envelopes.
	FindingInvalidEditionKey:    structural(FindingSeverityBlocking, false),
	FindingInvalidUTF8:          structural(FindingSeverityBlocking, true),
	FindingEmptyMarkdown:        structural(FindingSeverityBlocking, true),
	FindingMissingH1Title:       structural(FindingSeverityBlocking, true),
	FindingRawHTMLPresent:       structural(FindingSeverityBlocking, true),
	FindingClassicSourceChanged: structural(FindingSeverityBlocking, false),
	FindingIngestIncompatible:   structural(FindingSeverityBlocking, true),
}

func semanticEdition(severity FindingSeverity) findingSpec {
	return findingSpec{
		severity:       severity,
		kind:           FindingKindSemantic,
		editionAllowed: true,
	}
}

func semanticBundle(severity FindingSeverity) findingSpec {
	return findingSpec{
		severity:      severity,
		kind:          FindingKindSemantic,
		bundleAllowed: true,
	}
}

func structural(severity FindingSeverity, editionAllowed bool) findingSpec {
	return findingSpec{
		severity:       severity,
		kind:           FindingKindStructural,
		editionAllowed: editionAllowed,
	}
}

func ModernEditionKeys() []model.AdminStoryEditionKey {
	return []model.AdminStoryEditionKey{
		model.AdminStoryEditionConfidentReaders,
		model.AdminStoryEditionGrowingReaders,
		model.AdminStoryEditionStoryExplorers,
		model.AdminStoryEditionLittleListeners,
	}
}

func ValidModernEditionKey(key model.AdminStoryEditionKey) bool {
	switch key {
	case model.AdminStoryEditionConfidentReaders,
		model.AdminStoryEditionGrowingReaders,
		model.AdminStoryEditionStoryExplorers,
		model.AdminStoryEditionLittleListeners:
		return true
	default:
		return false
	}
}

func modernEditionRank(key model.AdminStoryEditionKey) (int, bool) {
	switch key {
	case model.AdminStoryEditionConfidentReaders:
		return 0, true
	case model.AdminStoryEditionGrowingReaders:
		return 1, true
	case model.AdminStoryEditionStoryExplorers:
		return 2, true
	case model.AdminStoryEditionLittleListeners:
		return 3, true
	default:
		return 0, false
	}
}

func ValidFindingCode(code FindingCode) bool {
	_, ok := findingSpecs[code]
	return ok
}

func CanonicalSeverity(code FindingCode) (FindingSeverity, bool) {
	spec, ok := findingSpecs[code]
	if !ok {
		return "", false
	}
	return spec.severity, true
}

func FindingKindFor(code FindingCode) (FindingKind, bool) {
	spec, ok := findingSpecs[code]
	if !ok {
		return "", false
	}
	return spec.kind, true
}

func (assessment Assessment) Validate() error {
	if assessment.ContractVersion != VersionV1 {
		return fmt.Errorf("contract version must equal %q", VersionV1)
	}
	if err := assessment.validateTarget(); err != nil {
		return err
	}
	if err := validateResult(assessment.Result); err != nil {
		return err
	}

	blocking := 0
	review := 0
	for index, finding := range assessment.Findings {
		spec, ok := findingSpecs[finding.Code]
		if !ok {
			return fmt.Errorf("finding %d: unsupported finding code %q", index+1, finding.Code)
		}
		if finding.Severity != spec.severity {
			return fmt.Errorf(
				"finding %d: severity for %q must equal %q",
				index+1,
				finding.Code,
				spec.severity,
			)
		}
		if strings.TrimSpace(finding.Message) == "" {
			return fmt.Errorf("finding %d: message is required", index+1)
		}
		if !findingAllowedForScope(spec, assessment.AssessmentScope) {
			return fmt.Errorf(
				"finding %d: %q is not allowed for %q assessments",
				index+1,
				finding.Code,
				assessment.AssessmentScope,
			)
		}

		switch finding.Severity {
		case FindingSeverityBlocking:
			blocking++
		case FindingSeverityReview:
			review++
		default:
			return fmt.Errorf("finding %d: unsupported severity %q", index+1, finding.Severity)
		}
	}

	switch assessment.Result {
	case ResultPass:
		if len(assessment.Findings) != 0 {
			return fmt.Errorf("pass assessments must contain no findings")
		}
	case ResultNeedsReview:
		if blocking != 0 || review == 0 {
			return fmt.Errorf("needs_review assessments require at least one review finding and no blocking findings")
		}
	case ResultFail:
		if blocking == 0 {
			return fmt.Errorf("fail assessments require at least one blocking finding")
		}
	}

	return nil
}

// ValidateSemantic applies the assessment-envelope contract and additionally
// rejects structural findings. Structural findings are owned by deterministic
// validation and must not be invented by a semantic assessor.
func (assessment Assessment) ValidateSemantic() error {
	if err := assessment.Validate(); err != nil {
		return err
	}
	for index, finding := range assessment.Findings {
		spec := findingSpecs[finding.Code]
		if spec.kind != FindingKindSemantic {
			return fmt.Errorf("finding %d: %q is structural and is not allowed in semantic assessments", index+1, finding.Code)
		}
	}
	return nil
}

func (assessment Assessment) validateTarget() error {
	switch assessment.AssessmentScope {
	case AssessmentScopeEdition:
		if assessment.EditionKey == nil {
			return fmt.Errorf("edition assessments require exactly one edition key")
		}
		if !ValidModernEditionKey(*assessment.EditionKey) {
			return fmt.Errorf("edition assessments require a canonical modern edition key")
		}
		if len(assessment.EditionKeys) != 0 {
			return fmt.Errorf("edition assessments must not contain editionKeys")
		}
	case AssessmentScopeBundle:
		if assessment.EditionKey != nil {
			return fmt.Errorf("bundle assessments must not contain editionKey")
		}
		if len(assessment.EditionKeys) < 2 {
			return fmt.Errorf("bundle assessments require at least two edition keys")
		}
		seen := make(map[model.AdminStoryEditionKey]struct{}, len(assessment.EditionKeys))
		lastRank := -1
		for index, key := range assessment.EditionKeys {
			rank, valid := modernEditionRank(key)
			if !valid {
				return fmt.Errorf("bundle edition key %d must be a canonical modern edition key", index+1)
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("bundle edition keys must be distinct")
			}
			if rank <= lastRank {
				return fmt.Errorf("bundle edition keys must follow canonical modern edition order")
			}
			seen[key] = struct{}{}
			lastRank = rank
		}
	default:
		return fmt.Errorf("unsupported assessment scope %q", assessment.AssessmentScope)
	}
	return nil
}

func findingAllowedForScope(spec findingSpec, scope AssessmentScope) bool {
	switch scope {
	case AssessmentScopeEdition:
		return spec.editionAllowed
	case AssessmentScopeBundle:
		return spec.bundleAllowed
	default:
		return false
	}
}

func validateResult(result Result) error {
	switch result {
	case ResultPass, ResultNeedsReview, ResultFail:
		return nil
	default:
		return fmt.Errorf("unsupported assessment result %q", result)
	}
}
