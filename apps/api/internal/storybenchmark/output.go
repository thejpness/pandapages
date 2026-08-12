package storybenchmark

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	ResultJSONFilename = "result.json"
	ReportMDFilename   = "report.md"
)

type WrittenArtifacts struct {
	Directory  string
	ResultJSON string
	ReportMD   string
}

func WriteControlledResultArtifacts(directory string, document ControlledResultDocument) (WrittenArtifacts, error) {
	if filepath.Clean(directory) == "." || directory == "" {
		return WrittenArtifacts{}, fmt.Errorf("benchmark output directory is required")
	}

	jsonData, err := MarshalControlledResultJSON(document)
	if err != nil {
		return WrittenArtifacts{}, err
	}
	markdown, err := RenderControlledMarkdown(document)
	if err != nil {
		return WrittenArtifacts{}, err
	}

	parent := filepath.Dir(directory)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return WrittenArtifacts{}, fmt.Errorf("create benchmark output parent: %w", err)
	}
	if err := os.Mkdir(directory, 0o700); err != nil {
		return WrittenArtifacts{}, fmt.Errorf("create benchmark output directory: %w", err)
	}

	jsonPath := filepath.Join(directory, ResultJSONFilename)
	markdownPath := filepath.Join(directory, ReportMDFilename)
	if err := writeNewBenchmarkFile(jsonPath, jsonData); err != nil {
		_ = os.Remove(directory)
		return WrittenArtifacts{}, err
	}
	if err := writeNewBenchmarkFile(markdownPath, []byte(markdown)); err != nil {
		_ = os.Remove(jsonPath)
		_ = os.Remove(directory)
		return WrittenArtifacts{}, err
	}

	return WrittenArtifacts{
		Directory:  directory,
		ResultJSON: jsonPath,
		ReportMD:   markdownPath,
	}, nil
}

func writeNewBenchmarkFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create benchmark artifact %q: %w", filepath.Base(path), err)
	}
	failed := true
	defer func() {
		_ = file.Close()
		if failed {
			_ = os.Remove(path)
		}
	}()

	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write benchmark artifact %q: %w", filepath.Base(path), err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync benchmark artifact %q: %w", filepath.Base(path), err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close benchmark artifact %q: %w", filepath.Base(path), err)
	}
	failed = false
	return nil
}
