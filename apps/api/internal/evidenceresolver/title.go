package evidenceresolver

import (
	"strings"
	"unicode"
)

// NormalisedTitle applies deterministic catalogue-title normalisation. It is
// deliberately limited to the existing case, punctuation, whitespace, and
// unicode-letter behaviour plus the standard catalogue abbreviation Mr./Mr ↔
// mister. It is not a fuzzy-match function.
func NormalisedTitle(value string) string {
	var result strings.Builder
	for _, word := range titleWords(value) {
		if word == "mr" {
			word = "mister"
		}
		result.WriteString(word)
	}
	return result.String()
}

// TitleQueryVariants returns the small, deterministic set of exact catalogue
// title variants that correspond to NormalisedTitle. Bibliographic adapters
// can use it where their source requires exact title predicates.
func TitleQueryVariants(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	variants := []string{value}
	withoutStops := strings.ReplaceAll(value, ".", "")
	appendTitleVariant(&variants, withoutStops)
	appendTitleVariant(&variants, replaceTitleWord(withoutStops, "mr", "mister"))
	appendTitleVariant(&variants, replaceTitleWord(value, "mister", "mr."))
	appendTitleVariant(&variants, replaceTitleWord(withoutStops, "mister", "mr"))
	return variants
}

func appendTitleVariant(variants *[]string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	for _, existing := range *variants {
		if existing == value {
			return
		}
	}
	*variants = append(*variants, value)
}

func titleWords(value string) []string {
	words := make([]string, 0)
	var word strings.Builder
	flush := func() {
		if word.Len() == 0 {
			return
		}
		words = append(words, strings.ToLower(word.String()))
		word.Reset()
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return words
}

func replaceTitleWord(value, from, to string) string {
	var result strings.Builder
	var word strings.Builder
	flush := func() {
		if word.Len() == 0 {
			return
		}
		if strings.EqualFold(word.String(), from) {
			result.WriteString(to)
		} else {
			result.WriteString(word.String())
		}
		word.Reset()
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word.WriteRune(r)
			continue
		}
		flush()
		result.WriteRune(r)
	}
	flush()
	return result.String()
}
