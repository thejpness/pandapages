package db

import (
	"database/sql"
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

// AdminDraftUpsert is account-scoped and idempotent on (story_id, content_hash).
func (s *Store) AdminDraftUpsert(accountID string, req model.AdminDraftUpsertRequest) (model.AdminDraftUpsertResponse, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return model.AdminDraftUpsertResponse{}, fmt.Errorf("account required")
	}

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

	// story upsert (account-scoped)
	sourceJSON, _ := json.Marshal(ing.Source)
	rightsJSON, _ := json.Marshal(ing.Rights)

	var storyID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO stories (account_id, slug, title, author, language, source, rights, updated_at)
		VALUES ($1,$2,$3,NULLIF(BTRIM($4),''),$5,$6::jsonb,$7::jsonb, now())
		ON CONFLICT (account_id, slug) DO UPDATE SET
			title=EXCLUDED.title,
			author=EXCLUDED.author,
			language=EXCLUDED.language,
			source=EXCLUDED.source,
			rights=EXCLUDED.rights,
			updated_at=now()
		RETURNING id
	`, accountID, ing.Slug, ing.Title, ing.Author, ing.Language, string(sourceJSON), string(rightsJSON)).Scan(&storyID)
	if err != nil {
		return model.AdminDraftUpsertResponse{}, err
	}

	// ---- Idempotency: if this exact content already exists for this story, reuse it ----
	var existingVersionID string
	var existingVersion int
	var existingRendered string

	err = tx.QueryRowContext(ctx, `
		SELECT id, version, rendered_html
		FROM story_versions
		WHERE story_id = $1 AND content_hash = $2
		LIMIT 1
	`, storyID, ing.ContentHash).Scan(&existingVersionID, &existingVersion, &existingRendered)

	if err == nil && strings.TrimSpace(existingVersionID) != "" {
		// point draft at the existing version
		_, err = tx.ExecContext(ctx, `
			UPDATE stories
			SET draft_version_id=$2,
			    updated_at=now()
			WHERE id=$1
		`, storyID, existingVersionID)
		if err != nil {
			return model.AdminDraftUpsertResponse{}, err
		}

		// contributors link (still useful even if content existed)
		if strings.TrimSpace(ing.Author) != "" {
			var contribID string
			// No-op update returns id reliably (requires UNIQUE(contributors.name))
			_ = tx.QueryRowContext(ctx, `
				INSERT INTO contributors (name)
				VALUES ($1)
				ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
				RETURNING id
			`, ing.Author).Scan(&contribID)

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
			StoryVersionID: existingVersionID,
			Slug:           ing.Slug,
			Version:        existingVersion,
			SegmentsCount:  len(ing.Segments),
			RenderedHTML:   existingRendered,
		}, nil
	}

	if err != nil && err != sql.ErrNoRows {
		return model.AdminDraftUpsertResponse{}, err
	}

	// next version number (only for new content)
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

	// --- Sections (chapters) + segment section assignment ---
	type headingLoc struct {
		Type  string `json:"type"`
		H     int    `json:"h"`
		Index int    `json:"index"`
	}

	headingText := func(md string) string {
		s := strings.TrimSpace(md)
		s = strings.TrimLeft(s, "#")
		return strings.TrimSpace(s)
	}

	type chapter struct {
		StartSegOrdinal int
		Title           string
		SectionOrdinal  int
		ID              string
	}
	chapters := make([]chapter, 0, 16)

	for _, seg := range ing.Segments {
		var loc headingLoc
		if err := json.Unmarshal(seg.Locator, &loc); err != nil {
			continue
		}
		if loc.Type == "heading" && loc.H == 2 {
			t := headingText(seg.Markdown)
			if strings.TrimSpace(t) == "" {
				t = fmt.Sprintf("Chapter %d", len(chapters)+1)
			}
			chapters = append(chapters, chapter{
				StartSegOrdinal: seg.Ordinal,
				Title:           t,
				SectionOrdinal:  len(chapters) + 1,
			})
		}
	}

	sectionIDByStart := map[int]string{}

	if len(chapters) == 0 {
		// No chapters -> one generic section for whole story
		var sectionID string
		err = tx.QueryRowContext(ctx, `
			INSERT INTO story_sections (story_version_id, kind, title, ordinal)
			VALUES ($1, 'section', NULL, 1)
			RETURNING id
		`, versionID).Scan(&sectionID)
		if err != nil {
			return model.AdminDraftUpsertResponse{}, err
		}
		sectionIDByStart[1] = sectionID
	} else {
		for i := range chapters {
			var secID string
			err = tx.QueryRowContext(ctx, `
				INSERT INTO story_sections (story_version_id, kind, title, ordinal)
				VALUES ($1, 'chapter', $2, $3)
				RETURNING id
			`, versionID, chapters[i].Title, chapters[i].SectionOrdinal).Scan(&secID)
			if err != nil {
				return model.AdminDraftUpsertResponse{}, err
			}
			chapters[i].ID = secID
			sectionIDByStart[chapters[i].StartSegOrdinal] = secID
		}
	}

	var currentChapterID string

	for _, seg := range ing.Segments {
		var sectionArg any = nil

		var loc headingLoc
		_ = json.Unmarshal(seg.Locator, &loc)

		if len(chapters) == 0 {
			sectionArg = sectionIDByStart[1]
		} else {
			// H1 title stays unsectioned; H2 starts a chapter; everything after belongs to current chapter
			if loc.Type == "heading" && loc.H == 1 {
				sectionArg = nil
			} else if loc.Type == "heading" && loc.H == 2 {
				if id, ok := sectionIDByStart[seg.Ordinal]; ok {
					currentChapterID = id
					sectionArg = currentChapterID
				}
			} else if currentChapterID != "" {
				sectionArg = currentChapterID
			} else {
				sectionArg = nil
			}
		}

		_, err := tx.ExecContext(ctx, `
			INSERT INTO story_segments (story_version_id, section_id, ordinal, locator, markdown, rendered_html, word_count)
			VALUES ($1,$2,$3,$4::jsonb,$5,$6,$7)
		`, versionID, sectionArg, seg.Ordinal, string(seg.Locator), seg.Markdown, seg.RenderedHTML, seg.WordCount)
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

	// contributors: ensure author exists & link if provided
	if strings.TrimSpace(ing.Author) != "" {
		var contribID string
		// No-op update returns id reliably (requires UNIQUE(contributors.name))
		_ = tx.QueryRowContext(ctx, `
			INSERT INTO contributors (name)
			VALUES ($1)
			ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
			RETURNING id
		`, ing.Author).Scan(&contribID)

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

func (s *Store) AdminPublish(accountID string, slug string, versionID string) error {
	ctx, cancel := s.ctx()
	defer cancel()

	accountID = strings.TrimSpace(accountID)
	slug = strings.TrimSpace(slug)
	versionID = strings.TrimSpace(versionID)

	if accountID == "" || slug == "" || versionID == "" {
		return fmt.Errorf("account, slug and versionId required")
	}

	var storyID string
	if err := s.db.QueryRowContext(ctx, `
		SELECT id FROM stories WHERE account_id=$1 AND slug=$2
	`, accountID, slug).Scan(&storyID); err != nil {
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
