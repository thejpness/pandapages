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
