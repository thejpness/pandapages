package storybenchmark

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
	"pandapages/api/internal/storygeneration"
)

type FixtureKind string

const FixtureKindSyntheticControlled FixtureKind = "synthetic_controlled"

var fixtureIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type ControlledStory struct {
	Slug                string
	Title               string
	Author              string
	Language            string
	SourceURL           string
	Rights              map[string]any
	CanonicalSourcePath string
	AnalysisPath        string
	CanonicalSource     string
	Analysis            storygeneration.StoryAnalysis
}

type ControlledEdition struct {
	EditionKey           model.AdminStoryEditionKey
	Path                 string
	Markdown             string
	StructuralValidation adaptationcontract.StructuralValidation
}

type ControlledCase struct {
	ID          string
	Description string
	Expectation AssessmentExpectation
	Editions    []ControlledEdition
}

type ControlledFixtureSet struct {
	BenchmarkVersion Version
	FixtureKind      FixtureKind
	Story            ControlledStory
	Cases            []ControlledCase
}

type fixtureManifest struct {
	BenchmarkVersion Version               `json:"benchmarkVersion"`
	FixtureKind      FixtureKind           `json:"fixtureKind"`
	Story            fixtureStoryManifest  `json:"story"`
	Cases            []fixtureCaseManifest `json:"cases"`
}

type fixtureStoryManifest struct {
	Slug                string         `json:"slug"`
	Title               string         `json:"title"`
	Author              string         `json:"author"`
	Language            string         `json:"language"`
	SourceURL           string         `json:"sourceUrl"`
	Rights              map[string]any `json:"rights"`
	CanonicalSourcePath string         `json:"canonicalSourcePath"`
	AnalysisPath        string         `json:"analysisPath"`
}

type fixtureCaseManifest struct {
	ID          string                `json:"id"`
	Description string                `json:"description"`
	Expectation AssessmentExpectation `json:"expectation"`
	Editions    []fixtureEditionRef   `json:"editions"`
}

type fixtureEditionRef struct {
	EditionKey model.AdminStoryEditionKey `json:"editionKey"`
	Path       string                     `json:"path"`
}

func LoadControlledFixtureSet(root string) (ControlledFixtureSet, error) {
	rootPath, err := resolveFixtureRoot(root)
	if err != nil {
		return ControlledFixtureSet{}, err
	}

	manifestData, err := readFixtureFile(rootPath, "manifest.json")
	if err != nil {
		return ControlledFixtureSet{}, fmt.Errorf("read controlled fixture manifest: %w", err)
	}

	var manifest fixtureManifest
	if err := decodeStrictJSON(manifestData, &manifest); err != nil {
		return ControlledFixtureSet{}, fmt.Errorf("decode controlled fixture manifest: %w", err)
	}
	if manifest.BenchmarkVersion != VersionV1 {
		return ControlledFixtureSet{}, fmt.Errorf("controlled fixture benchmark version must equal %q", VersionV1)
	}
	if manifest.FixtureKind != FixtureKindSyntheticControlled {
		return ControlledFixtureSet{}, fmt.Errorf(
			"controlled fixture kind must equal %q",
			FixtureKindSyntheticControlled,
		)
	}

	story, err := loadControlledStory(rootPath, manifest.Story)
	if err != nil {
		return ControlledFixtureSet{}, err
	}
	if len(manifest.Cases) == 0 {
		return ControlledFixtureSet{}, fmt.Errorf("controlled fixture manifest must contain at least one case")
	}

	cases := make([]ControlledCase, 0, len(manifest.Cases))
	seenIDs := make(map[string]struct{}, len(manifest.Cases))
	for index, manifestCase := range manifest.Cases {
		loadedCase, err := loadControlledCase(rootPath, story, manifestCase)
		if err != nil {
			return ControlledFixtureSet{}, fmt.Errorf("controlled fixture case %d: %w", index+1, err)
		}
		if _, exists := seenIDs[loadedCase.ID]; exists {
			return ControlledFixtureSet{}, fmt.Errorf("controlled fixture case ID %q is duplicated", loadedCase.ID)
		}
		seenIDs[loadedCase.ID] = struct{}{}
		cases = append(cases, loadedCase)
	}

	return ControlledFixtureSet{
		BenchmarkVersion: manifest.BenchmarkVersion,
		FixtureKind:      manifest.FixtureKind,
		Story:            story,
		Cases:            cases,
	}, nil
}

func loadControlledStory(root string, manifest fixtureStoryManifest) (ControlledStory, error) {
	if !fixtureIDPattern.MatchString(strings.TrimSpace(manifest.Slug)) {
		return ControlledStory{}, fmt.Errorf("controlled fixture story slug is invalid")
	}
	if strings.TrimSpace(manifest.Title) == "" {
		return ControlledStory{}, fmt.Errorf("controlled fixture story title is required")
	}
	if strings.TrimSpace(manifest.Author) == "" {
		return ControlledStory{}, fmt.Errorf("controlled fixture story author is required")
	}
	if strings.TrimSpace(manifest.Language) == "" {
		return ControlledStory{}, fmt.Errorf("controlled fixture story language is required")
	}
	if strings.TrimSpace(manifest.SourceURL) == "" {
		return ControlledStory{}, fmt.Errorf("controlled fixture story source URL is required")
	}
	if err := validateControlledStoryRights(manifest.Rights); err != nil {
		return ControlledStory{}, err
	}

	sourceData, err := readFixtureFile(root, manifest.CanonicalSourcePath)
	if err != nil {
		return ControlledStory{}, fmt.Errorf("read controlled fixture canonical source: %w", err)
	}
	canonicalSource := string(sourceData)
	if strings.TrimSpace(canonicalSource) == "" {
		return ControlledStory{}, fmt.Errorf("controlled fixture canonical source is empty")
	}

	analysisData, err := readFixtureFile(root, manifest.AnalysisPath)
	if err != nil {
		return ControlledStory{}, fmt.Errorf("read controlled fixture StoryAnalysis: %w", err)
	}
	analysis, err := storygeneration.DecodeStoryAnalysisJSON(analysisData)
	if err != nil {
		return ControlledStory{}, fmt.Errorf("decode controlled fixture StoryAnalysis: %w", err)
	}

	return ControlledStory{
		Slug:                strings.TrimSpace(manifest.Slug),
		Title:               strings.TrimSpace(manifest.Title),
		Author:              strings.TrimSpace(manifest.Author),
		Language:            strings.TrimSpace(manifest.Language),
		SourceURL:           strings.TrimSpace(manifest.SourceURL),
		Rights:              cloneStringAnyMap(manifest.Rights),
		CanonicalSourcePath: filepath.Clean(manifest.CanonicalSourcePath),
		AnalysisPath:        filepath.Clean(manifest.AnalysisPath),
		CanonicalSource:     canonicalSource,
		Analysis:            analysis,
	}, nil
}

func validateControlledStoryRights(rights map[string]any) error {
	if len(rights) == 0 {
		return fmt.Errorf("controlled fixture story rights are required")
	}
	publicationEligible, ok := rights["publicationEligible"].(bool)
	if !ok {
		return fmt.Errorf("controlled fixture story rights publicationEligible must be a boolean")
	}
	if publicationEligible {
		return fmt.Errorf("synthetic controlled fixtures must not be publication eligible")
	}
	return nil
}

func loadControlledCase(
	root string,
	story ControlledStory,
	manifest fixtureCaseManifest,
) (ControlledCase, error) {
	id := strings.TrimSpace(manifest.ID)
	if !fixtureIDPattern.MatchString(id) {
		return ControlledCase{}, fmt.Errorf("case ID %q is invalid", manifest.ID)
	}
	description := strings.TrimSpace(manifest.Description)
	if description == "" {
		return ControlledCase{}, fmt.Errorf("case %q description is required", id)
	}
	if err := manifest.Expectation.Validate(); err != nil {
		return ControlledCase{}, fmt.Errorf("case %q expectation is invalid: %w", id, err)
	}

	expectedKeys := expectationEditionKeys(manifest.Expectation)
	actualKeys := make([]model.AdminStoryEditionKey, 0, len(manifest.Editions))
	for _, edition := range manifest.Editions {
		actualKeys = append(actualKeys, edition.EditionKey)
	}
	if !sameEditionKeys(actualKeys, expectedKeys) {
		return ControlledCase{}, fmt.Errorf("case %q edition fixtures do not exactly match expectation targets", id)
	}

	editions := make([]ControlledEdition, 0, len(manifest.Editions))
	for index, editionRef := range manifest.Editions {
		markdownData, err := readFixtureFile(root, editionRef.Path)
		if err != nil {
			return ControlledCase{}, fmt.Errorf(
				"case %q edition %d: read Markdown: %w",
				id,
				index+1,
				err,
			)
		}
		markdown := string(markdownData)
		structural := adaptationcontract.ValidateGeneratedEdition(adaptationcontract.GeneratedEditionInput{
			EditionKey: editionRef.EditionKey,
			Slug:       story.Slug,
			Title:      story.Title,
			Author:     story.Author,
			Markdown:   markdown,
			Language:   story.Language,
			SourceURL:  story.SourceURL,
			Rights:     cloneStringAnyMap(story.Rights),
		})
		if !structural.Passed() {
			return ControlledCase{}, fmt.Errorf(
				"case %q edition %q is structurally invalid: %s",
				id,
				editionRef.EditionKey,
				summarizeFixtureStructuralFindings(structural.Findings),
			)
		}
		editions = append(editions, ControlledEdition{
			EditionKey:           editionRef.EditionKey,
			Path:                 filepath.Clean(editionRef.Path),
			Markdown:             markdown,
			StructuralValidation: structural,
		})
	}

	return ControlledCase{
		ID:          id,
		Description: description,
		Expectation: manifest.Expectation,
		Editions:    editions,
	}, nil
}

func expectationEditionKeys(expectation AssessmentExpectation) []model.AdminStoryEditionKey {
	if expectation.AssessmentScope == adaptationcontract.AssessmentScopeEdition {
		if expectation.EditionKey == nil {
			return nil
		}
		return []model.AdminStoryEditionKey{*expectation.EditionKey}
	}
	return append([]model.AdminStoryEditionKey(nil), expectation.EditionKeys...)
}

func resolveFixtureRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("controlled fixture root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve controlled fixture root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve controlled fixture root symlinks: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat controlled fixture root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("controlled fixture root is not a directory")
	}
	return resolved, nil
}

func readFixtureFile(root, relative string) ([]byte, error) {
	clean, err := safeFixtureRelativePath(relative)
	if err != nil {
		return nil, err
	}
	candidate := filepath.Join(root, clean)
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return nil, fmt.Errorf("resolve fixture path %q: %w", relative, err)
	}
	inside, err := filepath.Rel(root, resolved)
	if err != nil {
		return nil, fmt.Errorf("check fixture path %q: %w", relative, err)
	}
	if inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("fixture path %q escapes the controlled fixture root", relative)
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("read fixture path %q: %w", relative, err)
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("fixture path %q is not valid UTF-8", relative)
	}
	return data, nil
}

func safeFixtureRelativePath(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("fixture path is required")
	}
	if filepath.IsAbs(trimmed) {
		return "", fmt.Errorf("fixture path %q must be relative", value)
	}
	clean := filepath.Clean(trimmed)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("fixture path %q escapes the controlled fixture root", value)
	}
	return clean, nil
}

func decodeStrictJSON(data []byte, target any) error {
	if !utf8.Valid(data) {
		return fmt.Errorf("JSON must be valid UTF-8")
	}
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return err
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := ensureDecoderEOF(decoder); err != nil {
		return err
	}
	return nil
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := walkJSONValue(decoder, "$"); err != nil {
		return err
	}
	token, err := decoder.Token()
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("unexpected trailing JSON token %v", token)
}

func walkJSONValue(decoder *json.Decoder, path string) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}

	switch delimiter {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("%s object key is not a string", path)
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("%s contains duplicate key %q", path, key)
			}
			seen[key] = struct{}{}
			if err := walkJSONValue(decoder, path+"."+key); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim('}') {
			return fmt.Errorf("%s object is not closed", path)
		}
	case '[':
		index := 0
		for decoder.More() {
			if err := walkJSONValue(decoder, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
			index++
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim(']') {
			return fmt.Errorf("%s array is not closed", path)
		}
	default:
		return fmt.Errorf("%s contains unexpected JSON delimiter %q", path, delimiter)
	}
	return nil
}

func ensureDecoderEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("unexpected trailing JSON value")
}

func cloneStringAnyMap(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func summarizeFixtureStructuralFindings(findings []adaptationcontract.Finding) string {
	if len(findings) == 0 {
		return "unknown structural validation failure"
	}
	codes := make([]string, 0, len(findings))
	for _, finding := range findings {
		codes = append(codes, string(finding.Code))
	}
	return strings.Join(codes, ", ")
}
