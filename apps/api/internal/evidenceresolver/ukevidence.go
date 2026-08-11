package evidenceresolver

import "pandapages/api/internal/copyrighteligibility"

// ToUKEvidence maps only established factual findings into the existing pure
// policy input. Conflicting and insufficient findings deliberately remain the
// policy's unknown/unsupported zero values.
func ToUKEvidence(resolution Resolution) copyrighteligibility.UKEvidence {
	result := copyrighteligibility.UKEvidence{
		WorkCategory:                  copyrighteligibility.WorkCategoryUnknown,
		Authorship:                    copyrighteligibility.AuthorshipUnknown,
		Translation:                   copyrighteligibility.FactEvidence{State: copyrighteligibility.FactUnknown},
		AdditionalTextualContribution: copyrighteligibility.FactEvidence{State: copyrighteligibility.FactUnknown},
		SpecialCategory:               copyrighteligibility.FactEvidence{State: copyrighteligibility.FactUnknown},
		UnpublishedAtEnd1988:          copyrighteligibility.FactEvidence{State: copyrighteligibility.FactUnknown},
	}
	if resolution.WorkCategory.Status == ResolutionEstablished {
		result.WorkCategory = resolution.WorkCategory.Value
		result.WorkCategoryReferences = policyReferences(resolution.WorkCategory.Evidence)
	}
	if resolution.Authorship.Status == ResolutionEstablished {
		result.Authorship = resolution.Authorship.Value
		result.AuthorshipReferences = policyReferences(resolution.Authorship.Evidence)
	}
	if resolution.Author.Status == ResolutionEstablished {
		result.Author = copyrighteligibility.PersonEvidence{Name: resolution.Author.Name, DeathYear: resolution.Author.DeathYear, References: policyReferences(resolution.Author.Evidence)}
	}
	if resolution.FirstPublication.Status == ResolutionEstablished {
		result.FirstPublication = copyrighteligibility.PublicationEvidence{Year: resolution.FirstPublication.Year, References: policyReferences(resolution.FirstPublication.Evidence)}
	}
	result.Translation = policyFact(resolution.Translation)
	result.AdditionalTextualContribution = policyFact(resolution.AdditionalTextual)
	result.SpecialCategory = policyFact(resolution.SpecialCategory)
	result.UnpublishedAtEnd1988 = policyFact(resolution.UnpublishedAtEnd1988)
	return result
}

func policyFact(value ResolvedFact) copyrighteligibility.FactEvidence {
	if value.Status != ResolutionEstablished {
		return copyrighteligibility.FactEvidence{State: copyrighteligibility.FactUnknown}
	}
	return copyrighteligibility.FactEvidence{State: value.State, References: policyReferences(value.Evidence)}
}

func policyReferences(values []EvidenceItem) []copyrighteligibility.EvidenceReference {
	result := make([]copyrighteligibility.EvidenceReference, 0, len(values))
	for _, value := range values {
		result = append(result, copyrighteligibility.EvidenceReference{Source: value.Source, Fact: value.Fact, Locator: value.Locator, Identifier: value.Identifier, Digest: value.Digest})
	}
	return result
}
