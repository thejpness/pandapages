package db

import (
	"encoding/json"
	"fmt"
	"strings"

	"pandapages/api/internal/model"
	"pandapages/api/internal/storyingest"
)

func (s *Store) AdminPreview(req model.AdminPreviewRequest) (model.AdminPreviewResponse, error) {
	out, err := storyingest.Ingest(storyingest.Input{
		Slug:     "preview",
		Title:    "Preview",
		Author:   "",
		Markdown: req.Markdown,
		Language: "en-GB",
	})
	if err != nil {
		return model.AdminPreviewResponse{}, err
	}

	segs := make([]model.AdminSegment, 0, len(out.Segments))
	for _, seg := range out.Segments {
		segs = append(segs, model.AdminSegment{
			Ordinal:      seg.Ordinal,
			Locator:      seg.Locator,
			RenderedHTML: seg.RenderedHTML,
		})
	}

	return model.AdminPreviewResponse{
		RenderedHTML: out.RenderedHTML,
		Segments:     segs,
	}, nil
}

func (s *Store) AdminDraftUpsert(req model.AdminDraftUpsertRequest) (model.AdminDraftUpsertResponse, error) {
	slug := strings.TrimSpace(req.Slug)
	title := strings.TrimSpace(req.Title)
	md := req.Markdown

	author := ""
	if req.Author != nil {
		author = strings.TrimSpace(*req.Author)
	}

	lang := "en-GB"
	if req.Language != nil && strings.TrimSpace(*req.Language) != "" {
		lang = strings.TrimSpace(*req.Language)
	}

	srcURL := ""
	if req.SourceURL != nil {
		srcURL = strings.TrimSpace(*req.SourceURL)
	}

	ing, err := storyingest.Ingest(storyingest.Input{
		Slug:      slug,
		Title:     title,
		Author:    author,
		Markdown:  md,
		Language:  lang,
		SourceURL: srcURL,
		Rights:    req.Rights,
	})
	if err != nil {
		return model.AdminDraftUpsertResponse{}, err
	}

	ctx, cancel := s.ctx()
	defer cancel()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AdminDraftUpsertResponse{}, err
	}
	defer func() { _ = tx.Rollback() }()

	// story upsert
	sourceJSON, _ := json.Marshal(ing.Source)
	rightsJSON, _ := json.Marshal(ing.Rights)

	var storyID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO stories (slug, title, author, language, source, rights, updated_at)
		VALUES ($1,$2,NULLIF(BTRIM($3),''),$4,$5::jsonb,$6::jsonb, now())
		ON CONFLICT (slug) DO UPDATE SET
			title=EXCLUDED.title,
			author=EXCLUDED.author,
			language=EXCLUDED.language,
			source=EXCLUDED.source,
			rights=EXCLUDED.rights,
			updated_at=now()
		RETURNING id
	`, ing.Slug, ing.Title, ing.Author, ing.Language, string(sourceJSON), string(rightsJSON)).Scan(&storyID)
	if err != nil {
		return model.AdminDraftUpsertResponse{}, err
	}

	// next version number
	var nextVersion int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version), 0) + 1
		FROM story_versions
		WHERE story_id = $1
	`, storyID).Scan(&nextVersion); err != nil {
		return model.AdminDraftUpsertResponse{}, err
	}

	fmJSON, _ := json.Marshal(ing.Frontmatter)

	var versionID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO story_versions (story_id, version, frontmatter, markdown, rendered_html, content_hash)
		VALUES ($1,$2,$3::jsonb,$4,$5,$6)
		RETURNING id
	`, storyID, nextVersion, string(fmJSON), ing.Markdown, ing.RenderedHTML, ing.ContentHash).Scan(&versionID)
	if err != nil {
		return model.AdminDraftUpsertResponse{}, err
	}

	// create 1 section for now (whole story) ordinal=1
	var sectionID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO story_sections (story_version_id, kind, title, ordinal)
		VALUES ($1, 'story', NULL, 1)
		RETURNING id
	`, versionID).Scan(&sectionID)
	if err != nil {
		return model.AdminDraftUpsertResponse{}, err
	}

	// insert segments
	for _, seg := range ing.Segments {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO story_segments (story_version_id, section_id, ordinal, locator, markdown, rendered_html, word_count)
			VALUES ($1,$2,$3,$4::jsonb,$5,$6,$7)
		`, versionID, sectionID, seg.Ordinal, string(seg.Locator), seg.Markdown, seg.RenderedHTML, seg.WordCount)
		if err != nil {
			return model.AdminDraftUpsertResponse{}, err
		}
	}

	// update draft pointer ONLY (publish is separate endpoint)
	_, err = tx.ExecContext(ctx, `
		UPDATE stories
		SET draft_version_id=$2,
		    updated_at=now()
		WHERE id=$1
	`, storyID, versionID)
	if err != nil {
		return model.AdminDraftUpsertResponse{}, err
	}

	// contributors: keep it simple—ensure author exists & link if provided
	if strings.TrimSpace(ing.Author) != "" {
		var contribID string
		err = tx.QueryRowContext(ctx, `
			INSERT INTO contributors (name)
			VALUES ($1)
			ON CONFLICT DO NOTHING
			RETURNING id
		`, ing.Author).Scan(&contribID)
		if err != nil {
			// if conflict, fetch
			_ = tx.QueryRowContext(ctx, `SELECT id FROM contributors WHERE name=$1`, ing.Author).Scan(&contribID)
		}

		if strings.TrimSpace(contribID) != "" {
			_, _ = tx.ExecContext(ctx, `
				INSERT INTO story_contributors (story_id, contributor_id, role)
				VALUES ($1,$2,'author')
				ON CONFLICT DO NOTHING
			`, storyID, contribID)
		}
	}

	if err := tx.Commit(); err != nil {
		return model.AdminDraftUpsertResponse{}, err
	}

	return model.AdminDraftUpsertResponse{
		StoryID:        storyID,
		StoryVersionID: versionID,
		Slug:           ing.Slug,
		Version:        nextVersion,
		SegmentsCount:  len(ing.Segments),
		RenderedHTML:   ing.RenderedHTML,
	}, nil
}

func (s *Store) AdminPublish(slug string, versionID string) error {
	ctx, cancel := s.ctx()
	defer cancel()

	slug = strings.TrimSpace(slug)
	versionID = strings.TrimSpace(versionID)
	if slug == "" || versionID == "" {
		return fmt.Errorf("slug and versionId required")
	}

	// ensure version belongs to slug
	var storyID string
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM stories WHERE slug=$1`, slug).Scan(&storyID); err != nil {
		return err
	}
	var ok string
	if err := s.db.QueryRowContext(ctx, `
		SELECT id FROM story_versions WHERE id=$1 AND story_id=$2
	`, versionID, storyID).Scan(&ok); err != nil {
		return err
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE stories
		SET published_version_id=$2,
		    is_published=true,
		    updated_at=now()
		WHERE id=$1
	`, storyID, versionID)
	return err
}
