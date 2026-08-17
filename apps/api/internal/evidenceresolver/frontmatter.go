package evidenceresolver

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	maxFrontMatterBytes     = 64 << 10
	maxTitlePageSignalBytes = 8 << 10
	maxTitlePageSignalLines = 160
	maxSignalLookaheadLines = 3
)

var (
	gutenbergStartMarker = regexp.MustCompile(`(?im)^\*{3}\s*START OF (?:THE |THIS )?PROJECT GUTENBERG EBOOK\b.*$`)
	translatorLine       = regexp.MustCompile(`(?im)^\s*(?:translated|translation)\s+by\s*[:,-]?\s*(.{1,500})\s*$|^\s*translator\s*:\s*(.{1,500})\s*$`)
	textualLine          = regexp.MustCompile(`(?im)^\s*(edited|editor|introduction|preface|notes|annotations?|annotated|adapted|compiled|commentary)\s+by\s*[:,-]?\s*(.{1,500})\s*$|^\s*(editor|introduction|preface|notes|annotations?|annotator|adapter|compiler|commentary)\s*:\s*(.{1,500})\s*$`)
	titlePageTextualHead = regexp.MustCompile(`(?i)^\s*(?:with\s+(?:an?\s+)?)?(preface|introduction|notes|annotations?|commentary)\s+by\s*$`)
	titlePageBasedOn     = regexp.MustCompile(`(?i)^\s*the\s+text\s+is\s+based\s+on\s+translations?\s+from\s*$`)
	titlePageByLine      = regexp.MustCompile(`(?i)\bby\s*(.*)$`)
)

type FrontMatterContributor struct {
	Role string
	Name string
}

// FrontMatter contains bounded signals from the exact provider wrapper. The
// resolver combines a clean result with independent provider metadata under its
// explicit contributor-risk policy; this structure never makes a legal claim.
type FrontMatter struct {
	Inspected           bool
	Digest              string
	Translators         []string
	TextualContributors []FrontMatterContributor
}

// PostMarkerSignals contains only positive, title-page-style signals found in
// a tightly bounded prefix immediately after Gutenberg's START marker. Unlike
// FrontMatter, its absence is never used to establish an absence conclusion.
type PostMarkerSignals struct {
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

// ExtractPostMarkerSignals scans at most 8 KiB and 160 lines immediately
// after the Gutenberg START marker. It intentionally recognises only explicit
// title-page contributor statements; ordinary literary-body prose is not
// evidence and a clean scan never establishes a negative fact.
func ExtractPostMarkerSignals(source string) PostMarkerSignals {
	if !utf8.ValidString(source) {
		return PostMarkerSignals{}
	}
	if len(source) > maxFrontMatterBytes {
		source = source[:maxFrontMatterBytes]
	}
	marker := gutenbergStartMarker.FindStringIndex(source)
	if marker == nil {
		return PostMarkerSignals{}
	}
	titlePage := boundedTitlePage(source[marker[1]:])
	if titlePage == "" {
		return PostMarkerSignals{}
	}
	sum := sha256.Sum256([]byte(titlePage))
	result := PostMarkerSignals{Digest: hex.EncodeToString(sum[:])}
	lines := strings.Split(titlePage, "\n")
	for index, line := range lines {
		if match := titlePageTextualHead.FindStringSubmatch(line); match != nil {
			if name := nextBoundedLine(lines, index+1); name != "" {
				result.TextualContributors = append(result.TextualContributors, FrontMatterContributor{Role: strings.ToLower(match[1]), Name: name})
			}
		}
		if !titlePageBasedOn.MatchString(line) {
			continue
		}
		if name := titlePageTranslator(lines, index+1); name != "" {
			result.Translators = append(result.Translators, name)
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

func boundedTitlePage(value string) string {
	if len(value) > maxTitlePageSignalBytes {
		value = value[:maxTitlePageSignalBytes]
		for !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
	}
	lines := 0
	for index, r := range value {
		if r != '\n' {
			continue
		}
		lines++
		if lines >= maxTitlePageSignalLines {
			return value[:index]
		}
	}
	return value
}

func nextBoundedLine(lines []string, start int) string {
	end := start + maxSignalLookaheadLines
	if end > len(lines) {
		end = len(lines)
	}
	for _, line := range lines[start:end] {
		if name := boundedContributor(strings.Join(strings.Fields(line), " ")); name != "" {
			return name
		}
	}
	return ""
}

func titlePageTranslator(lines []string, start int) string {
	end := start + maxSignalLookaheadLines
	if end > len(lines) {
		end = len(lines)
	}
	for index := start; index < end; index++ {
		match := titlePageByLine.FindStringSubmatch(lines[index])
		if match == nil {
			continue
		}
		if name := boundedContributor(strings.Join(strings.Fields(match[1]), " ")); name != "" {
			return name
		}
		return nextBoundedLine(lines, index+1)
	}
	return ""
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
