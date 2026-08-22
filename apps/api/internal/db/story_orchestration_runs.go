package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
	"pandapages/api/internal/storygeneration"
	"pandapages/api/internal/storyorchestration"
	"pandapages/api/internal/storyvalidation"
)

type storyOrchestrationArtifacts struct {
	AnalysisArtifact   storygeneration.StoryAnalysisArtifact      `json:"analysisArtifact"`
	Editions           []storygeneration.GeneratedEditionArtifact `json:"editions"`
	EditionAssessments []storyvalidation.AssessmentArtifact       `json:"editionAssessments"`
	BundleAssessment   storyvalidation.AssessmentArtifact         `json:"bundleAssessment"`
}

// PersistCompletedStoryOrchestrationRun atomically stores one fully validated
// orchestration result against the authoritative immutable source version.
func (s *Store) PersistCompletedStoryOrchestrationRun(
	sourceVersionID string,
	result storyorchestration.Result,
) (storyorchestration.PersistedRun, error) {
	return s.PersistCompletedStoryOrchestrationRunContext(context.Background(), sourceVersionID, result)
}

// PersistCompletedStoryOrchestrationRunContext atomically stores one fully
// validated orchestration result using a caller-owned lifecycle context.
// The transaction stays short and is never held while model calls execute.
func (s *Store) PersistCompletedStoryOrchestrationRunContext(
	parent context.Context,
	sourceVersionID string,
	result storyorchestration.Result,
) (storyorchestration.PersistedRun, error) {
	sourceVersionID = strings.TrimSpace(sourceVersionID)
	if !accountIDRe.MatchString(sourceVersionID) {
		return storyorchestration.PersistedRun{}, fmt.Errorf("source version ID is invalid")
	}

	ctx, cancel := s.ctxFrom(parent)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storyorchestration.PersistedRun{}, err
	}
	defer func() { _ = tx.Rollback() }()

	source, err := loadStoryOrchestrationSourceVersion(ctx, tx, sourceVersionID)
	if err != nil {
		return storyorchestration.PersistedRun{}, err
	}
	if err := storyorchestration.ValidateCompletedResult(result, sourceVersionID, source.SourceText); err != nil {
		return storyorchestration.PersistedRun{}, fmt.Errorf("validate completed story orchestration result: %w", err)
	}
	artifactsJSON, err := marshalStoryOrchestrationArtifacts(result)
	if err != nil {
		return storyorchestration.PersistedRun{}, err
	}

	var persisted storyorchestration.PersistedRun
	err = tx.QueryRowContext(ctx, `
		INSERT INTO story_orchestration_runs (
			source_version_id,
			source_sha256,
			semantic_result,
			artifacts
		)
		VALUES ($1, $2, $3, $4::jsonb)
		RETURNING id, source_version_id, created_at
	`, sourceVersionID, result.SourceSHA256, result.SemanticResult, string(artifactsJSON)).Scan(
		&persisted.ID,
		&persisted.SourceVersionID,
		&persisted.CreatedAt,
	)
	if err != nil {
		return storyorchestration.PersistedRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return storyorchestration.PersistedRun{}, err
	}
	persisted.Result = result
	persisted.CreatedAt = persisted.CreatedAt.UTC()
	return persisted, nil
}

// GetCompletedStoryOrchestrationRun loads and revalidates immutable stored
// orchestration evidence against its authoritative canonical source version.
func (s *Store) GetCompletedStoryOrchestrationRun(runID string) (storyorchestration.PersistedRun, error) {
	runID = strings.TrimSpace(runID)
	if !accountIDRe.MatchString(runID) {
		return storyorchestration.PersistedRun{}, fmt.Errorf("story orchestration run ID is invalid")
	}

	ctx, cancel := s.ctx()
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storyorchestration.PersistedRun{}, err
	}
	defer func() { _ = tx.Rollback() }()

	persisted, err := getCompletedStoryOrchestrationRunTx(ctx, tx, runID, false)
	if err != nil {
		return storyorchestration.PersistedRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return storyorchestration.PersistedRun{}, err
	}
	return persisted, nil
}

// getCompletedStoryOrchestrationRunTx reconstructs the exact persisted run
// against its authoritative source inside a caller-owned transaction. Ingest
// uses lockRun to serialize its current-approval check with review inserts;
// read-only PR104 and PR109 paths retain their existing non-locking behavior.
func getCompletedStoryOrchestrationRunTx(
	ctx context.Context,
	tx *sql.Tx,
	runID string,
	lockRun bool,
) (storyorchestration.PersistedRun, error) {
	lockClause := ""
	if lockRun {
		lockClause = " FOR UPDATE"
	}

	var (
		persisted      storyorchestration.PersistedRun
		sourceSHA256   string
		semanticResult string
		artifactsJSON  []byte
	)
	err := tx.QueryRowContext(ctx, `
		SELECT id, source_version_id, source_sha256, semantic_result, artifacts, created_at
		FROM story_orchestration_runs
		WHERE id = $1
	`+lockClause, runID).Scan(
		&persisted.ID,
		&persisted.SourceVersionID,
		&sourceSHA256,
		&semanticResult,
		&artifactsJSON,
		&persisted.CreatedAt,
	)
	if err != nil {
		return storyorchestration.PersistedRun{}, err
	}
	source, err := loadStoryOrchestrationSourceVersion(ctx, tx, persisted.SourceVersionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, errStoredSourceInvalid) {
			return storyorchestration.PersistedRun{}, fmt.Errorf("%w", model.ErrAdminStoryOrchestrationRunRepairRequired)
		}
		return storyorchestration.PersistedRun{}, err
	}
	result, err := unmarshalStoryOrchestrationArtifacts(
		artifactsJSON,
		persisted.SourceVersionID,
		sourceSHA256,
		adaptationcontract.Result(semanticResult),
	)
	if err != nil {
		return storyorchestration.PersistedRun{}, fmt.Errorf("%w", model.ErrAdminStoryOrchestrationRunRepairRequired)
	}
	if err := storyorchestration.ValidateCompletedResult(result, persisted.SourceVersionID, source.SourceText); err != nil {
		return storyorchestration.PersistedRun{}, fmt.Errorf("%w", model.ErrAdminStoryOrchestrationRunRepairRequired)
	}
	persisted.Result = result
	persisted.CreatedAt = persisted.CreatedAt.UTC()
	return persisted, nil
}

func loadStoryOrchestrationSourceVersion(
	ctx context.Context,
	tx *sql.Tx,
	sourceVersionID string,
) (adminSourceSnapshot, error) {
	var sourceID, storyID string
	if err := tx.QueryRowContext(ctx, `
		SELECT source_id, story_id
		FROM story_source_versions
		WHERE id = $1
	`, sourceVersionID).Scan(&sourceID, &storyID); err != nil {
		return adminSourceSnapshot{}, err
	}
	return loadAdminSourceVersionSnapshot(ctx, tx, storyID, sourceID, sourceVersionID)
}

func marshalStoryOrchestrationArtifacts(result storyorchestration.Result) ([]byte, error) {
	encoded, err := json.Marshal(storyOrchestrationArtifacts{
		AnalysisArtifact:   result.AnalysisArtifact,
		Editions:           result.Editions,
		EditionAssessments: result.EditionAssessments,
		BundleAssessment:   result.BundleAssessment,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal story orchestration artifacts: %w", err)
	}
	return encoded, nil
}

func unmarshalStoryOrchestrationArtifacts(
	raw []byte,
	sourceVersionID string,
	sourceSHA256 string,
	semanticResult adaptationcontract.Result,
) (storyorchestration.Result, error) {
	var artifacts storyOrchestrationArtifacts
	if err := json.Unmarshal(raw, &artifacts); err != nil {
		return storyorchestration.Result{}, fmt.Errorf("decode stored story orchestration artifacts: %w", err)
	}
	return storyorchestration.Result{
		SourceIdentity:     sourceVersionID,
		SourceSHA256:       sourceSHA256,
		AnalysisArtifact:   artifacts.AnalysisArtifact,
		Editions:           artifacts.Editions,
		EditionAssessments: artifacts.EditionAssessments,
		BundleAssessment:   artifacts.BundleAssessment,
		SemanticResult:     semanticResult,
	}, nil
}
