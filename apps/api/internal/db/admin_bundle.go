package db

import (
	"errors"
	"fmt"
	"strings"

	"pandapages/api/internal/model"
)

func adminEditionBundleRequests(req model.AdminEditionBundleUpsertRequest) ([]model.AdminDraftUpsertRequest, error) {
	keys := model.AdminStoryEditionKeys()
	issues := make([]model.AdminValidationIssue, 0, 2)
	if len(req.Editions) != len(keys) {
		issues = append(issues, model.AdminValidationIssue{Field: "editions", Code: "incomplete", Message: "Choose exactly the five Panda Pages reading editions"})
	}

	byKey := make(map[model.AdminStoryEditionKey]string, len(keys))
	for _, item := range req.Editions {
		key := model.AdminStoryEditionKey(strings.TrimSpace(string(item.EditionKey)))
		if !model.ValidAdminStoryEditionKey(key) {
			issues = append(issues, model.AdminValidationIssue{Field: "editions", Code: "invalid", Message: "Choose only supported Panda Pages reading editions"})
			continue
		}
		if _, exists := byKey[key]; exists {
			issues = append(issues, model.AdminValidationIssue{Field: "editions", Code: "duplicate", Message: "Each reading edition can appear only once"})
			continue
		}
		byKey[key] = item.Markdown
	}
	for _, key := range keys {
		if _, ok := byKey[key]; !ok {
			issues = append(issues, model.AdminValidationIssue{Field: "editions", Code: "incomplete", Message: "Choose exactly the five Panda Pages reading editions"})
			break
		}
	}
	if len(issues) > 0 {
		return nil, &model.AdminValidationError{Issues: issues}
	}

	requests := make([]model.AdminDraftUpsertRequest, 0, len(keys))
	for _, key := range keys {
		editionKey := key
		requests = append(requests, model.AdminDraftUpsertRequest{
			Slug: req.Slug, EditionKey: &editionKey, Title: req.Title, Author: req.Author,
			Markdown: byKey[key], Language: req.Language, SourceURL: req.SourceURL, Rights: req.Rights,
		})
	}
	return requests, nil
}

func adminEditionBundleError(editionKey model.AdminStoryEditionKey, err error) error {
	var validationErr *model.AdminValidationError
	if !errors.As(err, &validationErr) {
		return err
	}
	issues := make([]model.AdminValidationIssue, len(validationErr.Issues))
	for i, issue := range validationErr.Issues {
		switch issue.Field {
		case "markdown":
			issue.Field = "editions." + string(editionKey) + ".markdown"
		case "editionKey":
			issue.Field = "editions"
		}
		issues[i] = issue
	}
	return &model.AdminValidationError{Issues: issues}
}

// AdminEditionBundleUpsert ingests exactly the five canonical reading editions
// in one transaction. It never changes publication state.
func (s *Store) AdminEditionBundleUpsert(req model.AdminEditionBundleUpsertRequest) (model.AdminEditionBundleUpsertResponse, error) {
	requests, err := adminEditionBundleRequests(req)
	if err != nil {
		return model.AdminEditionBundleUpsertResponse{}, err
	}

	ctx, cancel := s.ctx()
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AdminEditionBundleUpsertResponse{}, err
	}
	defer func() { _ = tx.Rollback() }()

	results := make([]model.AdminEditionBundleResult, 0, len(requests))
	slug := ""
	for _, draftReq := range requests {
		editionKey := *draftReq.EditionKey
		draft, err := adminDraftUpsertTx(ctx, tx, draftReq)
		if err != nil {
			return model.AdminEditionBundleUpsertResponse{}, adminEditionBundleError(editionKey, err)
		}
		if draft.EditionKey != editionKey {
			return model.AdminEditionBundleUpsertResponse{}, fmt.Errorf("edition ingest returned mismatched edition")
		}
		if slug == "" {
			slug = draft.Slug
		} else if slug != draft.Slug {
			return model.AdminEditionBundleUpsertResponse{}, fmt.Errorf("edition ingest returned mismatched story")
		}
		outcome := model.AdminEditionIngestOutcomeCreated
		if draft.Outcome == model.AdminDraftOutcomeReused {
			outcome = model.AdminEditionIngestOutcomeReused
		}
		results = append(results, model.AdminEditionBundleResult{
			EditionKey: editionKey, VersionID: draft.VersionID, Version: draft.Version,
			SegmentCount: draft.SegmentCount, WordCount: draft.WordCount, ChapterCount: draft.ChapterCount,
			Outcome: outcome,
		})
	}
	if len(results) != len(model.AdminStoryEditionKeys()) {
		return model.AdminEditionBundleUpsertResponse{}, fmt.Errorf("edition ingest result is incomplete")
	}
	if err := tx.Commit(); err != nil {
		return model.AdminEditionBundleUpsertResponse{}, err
	}
	return model.AdminEditionBundleUpsertResponse{Slug: slug, Results: results}, nil
}
