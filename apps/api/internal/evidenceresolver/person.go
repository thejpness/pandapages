package evidenceresolver

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// NormalisedPersonName applies deterministic identity normalisation to a
// structured bibliographic person name. It recognises canonical Unicode
// equivalents and the existing Last, First ordering, but deliberately does
// not transliterate, remove diacritics, or infer aliases.
func NormalisedPersonName(value string) string {
	value = norm.NFC.String(value)
	if parts := strings.Split(value, ","); len(parts) == 2 {
		value = strings.TrimSpace(parts[1]) + " " + strings.TrimSpace(parts[0])
	}
	var result strings.Builder
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// MatchesPersonName compares an external name only with the exact provider
// name and any explicit provider-authenticated variants. It intentionally
// performs no fuzzy, token, surname-only, or transliteration matching.
func MatchesPersonName(provider Person, externalName string) bool {
	external := NormalisedPersonName(externalName)
	if external == "" {
		return false
	}
	for _, candidate := range providerNames(provider) {
		if external == NormalisedPersonName(candidate) {
			return true
		}
	}
	return false
}

// QueryPersonNames returns the bounded provider-authenticated variants when
// available, otherwise the exact provider name. It preserves source-provided
// precedence and prevents aliases from widening a query into a fuzzy search.
func QueryPersonNames(provider Person, limit int) []string {
	if limit < 1 {
		return nil
	}
	candidates := provider.NameVariants
	if len(candidates) == 0 {
		candidates = []string{provider.Name}
	}
	result := make([]string, 0, min(limit, len(candidates)))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		key := NormalisedPersonName(candidate)
		if candidate == "" || key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, candidate)
		if len(result) == limit {
			break
		}
	}
	return result
}

func providerNames(provider Person) []string {
	values := make([]string, 0, len(provider.NameVariants)+1)
	if value := strings.TrimSpace(provider.Name); value != "" {
		values = append(values, value)
	}
	values = append(values, provider.NameVariants...)
	return values
}
