package storybenchmark

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	HumanReviewTemplateFilename  = "human-review-template.json"
	HumanReviewScoreJSONFilename = "human-review-score.json"
	HumanReviewReportMDFilename  = "human-review-report.md"
)

type WrittenEndToEndArtifacts struct {
	Directory           string
	ResultJSON          string
	ReportMD            string
	HumanReviewTemplate string
}

type WrittenHumanReviewArtifacts struct {
	ScoreJSON string
	ReportMD  string
}

func WriteEndToEndResultArtifacts(directory string, document EndToEndResultDocument) (WrittenEndToEndArtifacts, error) {
	if filepath.Clean(directory) == "." || directory == "" {
		return WrittenEndToEndArtifacts{}, fmt.Errorf("benchmark output directory is required")
	}
	jsonData, err := MarshalEndToEndResultJSON(document)
	if err != nil {
		return WrittenEndToEndArtifacts{}, err
	}
	markdown, err := RenderEndToEndMarkdown(document)
	if err != nil {
		return WrittenEndToEndArtifacts{}, err
	}
	review, err := BuildHumanReviewTemplate(document)
	if err != nil {
		return WrittenEndToEndArtifacts{}, err
	}
	reviewData, err := MarshalHumanReviewJSON(review)
	if err != nil {
		return WrittenEndToEndArtifacts{}, err
	}

	parent := filepath.Dir(directory)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return WrittenEndToEndArtifacts{}, fmt.Errorf("create benchmark output parent: %w", err)
	}
	if err := os.Mkdir(directory, 0o700); err != nil {
		return WrittenEndToEndArtifacts{}, fmt.Errorf("create benchmark output directory: %w", err)
	}

	paths := []struct {
		name string
		data []byte
	}{
		{name: ResultJSONFilename, data: jsonData},
		{name: ReportMDFilename, data: []byte(markdown)},
		{name: HumanReviewTemplateFilename, data: reviewData},
	}
	writtenPaths := make([]string, 0, len(paths))
	for _, item := range paths {
		path := filepath.Join(directory, item.name)
		if err := writeNewBenchmarkFile(path, item.data); err != nil {
			for _, written := range writtenPaths {
				_ = os.Remove(written)
			}
			_ = os.Remove(directory)
			return WrittenEndToEndArtifacts{}, err
		}
		writtenPaths = append(writtenPaths, path)
	}

	return WrittenEndToEndArtifacts{
		Directory:           directory,
		ResultJSON:          filepath.Join(directory, ResultJSONFilename),
		ReportMD:            filepath.Join(directory, ReportMDFilename),
		HumanReviewTemplate: filepath.Join(directory, HumanReviewTemplateFilename),
	}, nil
}

func WriteHumanReviewScoreArtifacts(directory string, document HumanReviewScoreDocument) (WrittenHumanReviewArtifacts, error) {
	info, err := os.Stat(directory)
	if err != nil {
		return WrittenHumanReviewArtifacts{}, fmt.Errorf("open benchmark result directory: %w", err)
	}
	if !info.IsDir() {
		return WrittenHumanReviewArtifacts{}, fmt.Errorf("benchmark result directory is not a directory")
	}
	jsonData, err := MarshalHumanReviewScoreJSON(document)
	if err != nil {
		return WrittenHumanReviewArtifacts{}, err
	}
	markdown, err := RenderHumanReviewScoreMarkdown(document)
	if err != nil {
		return WrittenHumanReviewArtifacts{}, err
	}

	jsonPath := filepath.Join(directory, HumanReviewScoreJSONFilename)
	markdownPath := filepath.Join(directory, HumanReviewReportMDFilename)
	if err := writeNewBenchmarkFile(jsonPath, jsonData); err != nil {
		return WrittenHumanReviewArtifacts{}, err
	}
	if err := writeNewBenchmarkFile(markdownPath, []byte(markdown)); err != nil {
		_ = os.Remove(jsonPath)
		return WrittenHumanReviewArtifacts{}, err
	}
	return WrittenHumanReviewArtifacts{ScoreJSON: jsonPath, ReportMD: markdownPath}, nil
}
