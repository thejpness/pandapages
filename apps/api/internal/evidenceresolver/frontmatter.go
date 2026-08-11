package evidenceresolver

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const maxFrontMatterBytes = 64 << 10

var (
	gutenbergStartMarker = regexp.MustCompile(`(?im)^\*{3}\s*START OF (?:THE |THIS )?PROJECT GUTENBERG EBOOK\b.*$`)
	translatorLine       = regexp.MustCompile(`(?im)^\s*(?:translated|translation)\s+by\s*[:,-]?\s*(.{1,500})\s*$|^\s*translator\s*:\s*(.{1,500})\s*$`)
	textualLine          = regexp.MustCompile(`(?im)^\s*(edited|editor|introduction|preface|notes|annotated|adapted|compiled)\s+by\s*[:,-]?\s*(.{1,500})\s*$|^\s*(editor|introduction|preface|notes|annotator|adapter|compiler)\s*:\s*(.{1,500})\s*$`)
)

type FrontMatterContributor struct {
	Role string
	Name string
}

// FrontMatter contains only positive, bounded signals from the provider wrapper.
// Lack of a signal is never an assertion that a contribution is absent.
type FrontMatter struct {
	Inspected           bool
	Digest              string
	Translators         []string
	TextualContributors []FrontMatterContributor
}

// ExtractFrontMatter inspects at most 64 KiB before the standard Gutenberg
// START marker. Literary-body text after the marker cannot affect this result.
func ExtractFrontMatter(source string) FrontMatter {
	if !utf8.ValidString(source) {
		return FrontMatter{}
	}
	if len(source) > maxFrontMatterBytes {
		source = source[:maxFrontMatterBytes]
	}
	marker := gutenbergStartMarker.FindStringIndex(source)
	if marker == nil || marker[0] == 0 {
		return FrontMatter{}
	}
	front := source[:marker[0]]
	sum := sha256.Sum256([]byte(front))
	result := FrontMatter{Inspected: true, Digest: hex.EncodeToString(sum[:])}
	for _, match := range translatorLine.FindAllStringSubmatch(front, -1) {
		if value := boundedContributor(firstNonEmpty(match[1:]...)); value != "" {
			result.Translators = append(result.Translators, value)
		}
	}
	for _, match := range textualLine.FindAllStringSubmatch(front, -1) {
		role, name := firstNonEmpty(match[1], match[3]), firstNonEmpty(match[2], match[4])
		if name = boundedContributor(name); name != "" {
			result.TextualContributors = append(result.TextualContributors, FrontMatterContributor{Role: strings.ToLower(role), Name: name})
		}
	}
	result.Translators = uniqueStrings(result.Translators)
	sort.Slice(result.TextualContributors, func(i, j int) bool {
		if result.TextualContributors[i].Role != result.TextualContributors[j].Role {
			return result.TextualContributors[i].Role < result.TextualContributors[j].Role
		}
		return result.TextualContributors[i].Name < result.TextualContributors[j].Name
	})
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func boundedContributor(value string) string {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) || len(value) == 0 || len(value) > 500 {
		return ""
	}
	return value
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; !exists {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
