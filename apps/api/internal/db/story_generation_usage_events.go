package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"pandapages/api/internal/model"
	"pandapages/api/internal/storygeneration"
	"pandapages/api/internal/storyorchestration"
)

// RecordStoryGenerationUsage appends one safely observed provider usage event
// for a durable job. A provider response ID is globally unique accounting
// identity: an identical replay is idempotent, while conflicting attribution
// is an integrity error.
func (s *Store) RecordStoryGenerationUsage(
	parent context.Context,
	generationJobID string,
	observation storygeneration.ResponsesUsageObservation,
) error {
	generationJobID = strings.TrimSpace(generationJobID)
	if !accountIDRe.MatchString(generationJobID) {
		return fmt.Errorf("story generation usage job ID is invalid")
	}
	if err := observation.Validate(); err != nil {
		return fmt.Errorf("story generation usage observation is invalid: %w", err)
	}

	ctx, cancel := s.ctxFrom(parent)
	defer cancel()
	var insertedID string
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO story_generation_usage_events (
			generation_job_id,
			operation,
			provider_response_id,
			requested_model,
			returned_model,
			input_tokens,
			cached_input_tokens,
			output_tokens,
			reasoning_tokens,
			total_tokens
		)
		SELECT
			job.id,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9,
			$10
		FROM story_generation_jobs AS job
		WHERE job.id = $1
			AND job.status = 'running'
		ON CONFLICT (provider_response_id) DO NOTHING
		RETURNING id
	`,
		generationJobID,
		observation.Operation,
		observation.ProviderResponseID,
		observation.RequestedModel,
		observation.ReturnedModel,
		observation.Usage.InputTokens,
		observation.Usage.CachedTokens,
		observation.Usage.OutputTokens,
		observation.Usage.ReasoningTokens,
		observation.Usage.TotalTokens,
	).Scan(&insertedID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	existing, err := s.storyGenerationUsageEventByProviderResponseID(ctx, observation.ProviderResponseID)
	if err != nil {
		return err
	}
	if existing.GenerationJobID != generationJobID ||
		existing.Operation != observation.Operation ||
		existing.ProviderResponseID != observation.ProviderResponseID ||
		existing.RequestedModel != observation.RequestedModel ||
		existing.ReturnedModel != observation.ReturnedModel ||
		existing.Usage != observation.Usage {
		return fmt.Errorf("story generation usage provider response ID conflicts with existing accounting event")
	}
	return nil
}

// ListStoryGenerationUsageEvents returns append-only accounting evidence in
// observation order for one durable generation job.
func (s *Store) ListStoryGenerationUsageEvents(
	parent context.Context,
	generationJobID string,
) ([]storygeneration.RecordedResponsesUsageEvent, error) {
	generationJobID = strings.TrimSpace(generationJobID)
	if !accountIDRe.MatchString(generationJobID) {
		return nil, fmt.Errorf("story generation usage job ID is invalid")
	}
	ctx, cancel := s.ctxFrom(parent)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			generation_job_id,
			operation,
			provider_response_id,
			requested_model,
			returned_model,
			input_tokens,
			cached_input_tokens,
			output_tokens,
			reasoning_tokens,
			total_tokens,
			observed_at
		FROM story_generation_usage_events
		WHERE generation_job_id = $1
		ORDER BY observed_at ASC, id ASC
	`, generationJobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]storygeneration.RecordedResponsesUsageEvent, 0)
	for rows.Next() {
		var event storygeneration.RecordedResponsesUsageEvent
		if err := rows.Scan(
			&event.GenerationJobID,
			&event.Operation,
			&event.ProviderResponseID,
			&event.RequestedModel,
			&event.ReturnedModel,
			&event.Usage.InputTokens,
			&event.Usage.CachedTokens,
			&event.Usage.OutputTokens,
			&event.Usage.ReasoningTokens,
			&event.Usage.TotalTokens,
			&event.ObservedAt,
		); err != nil {
			return nil, err
		}
		event.ObservedAt = event.ObservedAt.UTC()
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func (s *Store) storyGenerationUsageEventByProviderResponseID(
	ctx context.Context,
	providerResponseID string,
) (storygeneration.RecordedResponsesUsageEvent, error) {
	var event storygeneration.RecordedResponsesUsageEvent
	err := s.db.QueryRowContext(ctx, `
		SELECT
			generation_job_id,
			operation,
			provider_response_id,
			requested_model,
			returned_model,
			input_tokens,
			cached_input_tokens,
			output_tokens,
			reasoning_tokens,
			total_tokens,
			observed_at
		FROM story_generation_usage_events
		WHERE provider_response_id = $1
	`, providerResponseID).Scan(
		&event.GenerationJobID,
		&event.Operation,
		&event.ProviderResponseID,
		&event.RequestedModel,
		&event.ReturnedModel,
		&event.Usage.InputTokens,
		&event.Usage.CachedTokens,
		&event.Usage.OutputTokens,
		&event.Usage.ReasoningTokens,
		&event.Usage.TotalTokens,
		&event.ObservedAt,
	)
	if err != nil {
		return storygeneration.RecordedResponsesUsageEvent{}, err
	}
	event.ObservedAt = event.ObservedAt.UTC()
	return event, nil
}

func reconcileCompletedStoryGenerationUsageTx(
	ctx context.Context,
	tx *sql.Tx,
	generationJobID string,
	result storyorchestration.Result,
) error {
	expected, err := completedResultUsageObservations(result)
	if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT
			operation,
			provider_response_id,
			requested_model,
			returned_model,
			input_tokens,
			cached_input_tokens,
			output_tokens,
			reasoning_tokens,
			total_tokens
		FROM story_generation_usage_events
		WHERE generation_job_id = $1
	`, generationJobID)
	if err != nil {
		return err
	}
	defer rows.Close()

	actual := make(map[string]storygeneration.ResponsesUsageObservation, len(expected))
	for rows.Next() {
		var observation storygeneration.ResponsesUsageObservation
		if err := rows.Scan(
			&observation.Operation,
			&observation.ProviderResponseID,
			&observation.RequestedModel,
			&observation.ReturnedModel,
			&observation.Usage.InputTokens,
			&observation.Usage.CachedTokens,
			&observation.Usage.OutputTokens,
			&observation.Usage.ReasoningTokens,
			&observation.Usage.TotalTokens,
		); err != nil {
			return err
		}
		actual[observation.ProviderResponseID] = observation
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, observation := range expected {
		if actualObservation, ok := actual[observation.ProviderResponseID]; !ok || actualObservation != observation {
			return fmt.Errorf("completed story generation usage does not reconcile with retained response %q", observation.ProviderResponseID)
		}
	}
	return nil
}

func completedResultUsageObservations(result storyorchestration.Result) ([]storygeneration.ResponsesUsageObservation, error) {
	observations := make([]storygeneration.ResponsesUsageObservation, 0, 10)
	appendObservation := func(operation storygeneration.ResponsesOperation, responseID, requestedModel, returnedModel string, usage storygeneration.ResponsesUsage) error {
		observation := storygeneration.ResponsesUsageObservation{
			Operation:          operation,
			ProviderResponseID: responseID,
			RequestedModel:     requestedModel,
			ReturnedModel:      returnedModel,
			Usage:              usage,
		}
		if err := observation.Validate(); err != nil {
			return err
		}
		observations = append(observations, observation)
		return nil
	}
	if err := appendObservation(
		storygeneration.ResponsesOperationAnalyseSource,
		result.AnalysisArtifact.ResponseID,
		result.AnalysisArtifact.RequestedModel,
		result.AnalysisArtifact.ReturnedModel,
		result.AnalysisArtifact.Usage,
	); err != nil {
		return nil, err
	}
	for _, edition := range result.Editions {
		operation, ok := generationUsageOperationForEdition(edition.EditionKey, false)
		if !ok {
			return nil, fmt.Errorf("completed story generation edition operation is invalid")
		}
		if err := appendObservation(operation, edition.ResponseID, edition.RequestedModel, edition.ReturnedModel, edition.Usage); err != nil {
			return nil, err
		}
	}
	for _, assessment := range result.EditionAssessments {
		if assessment.EditionKey == nil {
			return nil, fmt.Errorf("completed story generation assessment target is invalid")
		}
		operation, ok := generationUsageOperationForEdition(*assessment.EditionKey, true)
		if !ok {
			return nil, fmt.Errorf("completed story generation validation operation is invalid")
		}
		if err := appendObservation(operation, assessment.ResponseID, assessment.RequestedModel, assessment.ReturnedModel, assessment.Usage); err != nil {
			return nil, err
		}
	}
	if err := appendObservation(
		storygeneration.ResponsesOperationValidateBundle,
		result.BundleAssessment.ResponseID,
		result.BundleAssessment.RequestedModel,
		result.BundleAssessment.ReturnedModel,
		result.BundleAssessment.Usage,
	); err != nil {
		return nil, err
	}
	return observations, nil
}

func generationUsageOperationForEdition(key model.AdminStoryEditionKey, validation bool) (storygeneration.ResponsesOperation, bool) {
	if validation {
		switch key {
		case model.AdminStoryEditionConfidentReaders:
			return storygeneration.ResponsesOperationValidateConfidentReaders, true
		case model.AdminStoryEditionGrowingReaders:
			return storygeneration.ResponsesOperationValidateGrowingReaders, true
		case model.AdminStoryEditionStoryExplorers:
			return storygeneration.ResponsesOperationValidateStoryExplorers, true
		case model.AdminStoryEditionLittleListeners:
			return storygeneration.ResponsesOperationValidateLittleListeners, true
		default:
			return "", false
		}
	}
	switch key {
	case model.AdminStoryEditionConfidentReaders:
		return storygeneration.ResponsesOperationGenerateConfidentReaders, true
	case model.AdminStoryEditionGrowingReaders:
		return storygeneration.ResponsesOperationGenerateGrowingReaders, true
	case model.AdminStoryEditionStoryExplorers:
		return storygeneration.ResponsesOperationGenerateStoryExplorers, true
	case model.AdminStoryEditionLittleListeners:
		return storygeneration.ResponsesOperationGenerateLittleListeners, true
	default:
		return "", false
	}
}
