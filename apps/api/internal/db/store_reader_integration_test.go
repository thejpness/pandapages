package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"pandapages/api/internal/httpapi"
	"pandapages/api/internal/model"
	"pandapages/api/internal/readercontract"
	"pandapages/api/internal/session"
)

const (
	readerIntegrationURLVar   = "PP_READER_STORE_TEST_DATABASE_URL"
	readerIntegrationGuardVar = "PP_READER_STORE_TEST_DISPOSABLE"
	readerIntegrationDBName   = "pandapages_reader_store_test"
	readerAccountA            = "a2140000-0000-4000-8000-000000000001"
	readerAccountB            = "b2140000-0000-4000-8000-000000000001"
	readerAccountC            = "c2140000-0000-4000-8000-000000000001"
	readerSlug                = "reader-store-story"
)

func TestReaderStoreIntegration(t *testing.T) {
	if os.Getenv(readerIntegrationGuardVar) != "1" {
		t.Skip("set PP_READER_STORE_TEST_DISPOSABLE=1 to run the disposable PostgreSQL integration test")
	}
	databaseURL := strings.TrimSpace(os.Getenv(readerIntegrationURLVar))
	if databaseURL == "" {
		t.Fatalf("%s is required when %s=1", readerIntegrationURLVar, readerIntegrationGuardVar)
	}

	adminDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open disposable PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	var databaseName string
	if err := adminDB.QueryRow(`SELECT current_database()`).Scan(&databaseName); err != nil {
		t.Fatalf("read disposable database name: %v", err)
	}
	if databaseName != readerIntegrationDBName {
		t.Fatalf("refusing Reader setup in database %q; want %q", databaseName, readerIntegrationDBName)
	}

	if _, err := adminDB.Exec(`
		INSERT INTO accounts (id, name) VALUES
			($1, 'Reader Account A'),
			($2, 'Reader Account B'),
			($3, 'Reader Account C')
		ON CONFLICT (id) DO NOTHING
	`, readerAccountA, readerAccountB, readerAccountC); err != nil {
		t.Fatalf("insert Reader accounts: %v", err)
	}

	store := newReaderIntegrationStore(t, databaseURL)
	author := "Panda Pages Test Fixture"
	language := "en-GB"
	firstDraft, err := store.AdminDraftUpsert(readerAccountA, model.AdminDraftUpsertRequest{
		Slug:     readerSlug,
		Title:    "TEST ONLY — Coherent Reader",
		Author:   &author,
		Language: &language,
		Markdown: "# TEST ONLY — Coherent Reader\n\nOpening café 世界.\n\n## Chapter One\n\nRepeated paragraph.\n\n## Chapter Two\n\nRepeated paragraph.\n",
	})
	if err != nil {
		t.Fatalf("insert first Reader draft: %v", err)
	}
	if firstDraft.Version != 1 || firstDraft.SegmentsCount != 6 {
		t.Fatalf("first draft = %#v, want version 1 with six segments", firstDraft)
	}
	if err := store.AdminPublish(readerAccountA, readerSlug, firstDraft.StoryVersionID); err != nil {
		t.Fatalf("publish first Reader version: %v", err)
	}

	secondDraft, err := store.AdminDraftUpsert(readerAccountA, model.AdminDraftUpsertRequest{
		Slug:     readerSlug,
		Title:    "TEST ONLY — Coherent Reader",
		Author:   &author,
		Language: &language,
		Markdown: "# Updated Reader\n\nNewest paragraph.\n",
	})
	if err != nil {
		t.Fatalf("insert second Reader draft: %v", err)
	}
	if secondDraft.Version != 2 || secondDraft.SegmentsCount != 2 {
		t.Fatalf("second draft = %#v, want version 2 with two segments", secondDraft)
	}
	if err := store.AdminPublish(readerAccountA, readerSlug, firstDraft.StoryVersionID); err != nil {
		t.Fatalf("restore first publication: %v", err)
	}

	accountBDraft, err := store.AdminDraftUpsert(readerAccountB, model.AdminDraftUpsertRequest{
		Slug:     readerSlug,
		Title:    "Account B isolated story",
		Author:   &author,
		Language: &language,
		Markdown: "# Account B\n\nPrivate to account B.\n",
	})
	if err != nil {
		t.Fatalf("insert account B Reader draft: %v", err)
	}
	if err := store.AdminPublish(readerAccountB, readerSlug, accountBDraft.StoryVersionID); err != nil {
		t.Fatalf("publish account B story: %v", err)
	}
	if _, err := store.AdminDraftUpsert(readerAccountA, model.AdminDraftUpsertRequest{
		Slug:     "unpublished-reader-story",
		Title:    "Unpublished",
		Language: &language,
		Markdown: "# Unpublished\n",
	}); err != nil {
		t.Fatalf("insert unpublished Reader draft: %v", err)
	}

	t.Run("corrupt idempotent version requires repair instead of false reuse", func(t *testing.T) {
		req := model.AdminDraftUpsertRequest{
			Slug:     "idempotent-repair-story",
			Title:    "Idempotent repair story",
			Language: &language,
			Markdown: "# Idempotent repair story\n\nReadable body.\n",
		}
		draft, err := store.AdminDraftUpsert(readerAccountA, req)
		if err != nil {
			t.Fatalf("insert idempotency target: %v", err)
		}
		t.Cleanup(func() { _, _ = adminDB.Exec(`DELETE FROM stories WHERE id = $1`, draft.StoryID) })

		exactReuse, err := store.AdminDraftUpsert(readerAccountA, req)
		if err != nil {
			t.Fatalf("reuse exact immutable version: %v", err)
		}
		if exactReuse.StoryVersionID != draft.StoryVersionID || exactReuse.Version != draft.Version ||
			exactReuse.SegmentsCount != draft.SegmentsCount || exactReuse.RenderedHTML != draft.RenderedHTML {
			t.Fatalf("exact reuse response = %#v, want locked stored version %#v", exactReuse, draft)
		}

		changedAuthor := "Different author"
		changedLanguage := "cy"
		metadataChanges := []struct {
			name   string
			change func(*model.AdminDraftUpsertRequest)
		}{
			{name: "title", change: func(changed *model.AdminDraftUpsertRequest) {
				changed.Title = "Changed metadata with the same body"
			}},
			{name: "author", change: func(changed *model.AdminDraftUpsertRequest) {
				changed.Author = &changedAuthor
			}},
			{name: "language", change: func(changed *model.AdminDraftUpsertRequest) {
				changed.Language = &changedLanguage
			}},
			{name: "additive frontmatter", change: func(changed *model.AdminDraftUpsertRequest) {
				changed.Markdown = "---\ndisplayNote: Different safe metadata\n---\n" + req.Markdown
			}},
		}
		for _, metadataChange := range metadataChanges {
			t.Run("same body changed "+metadataChange.name, func(t *testing.T) {
				changed := req
				metadataChange.change(&changed)
				if _, err := store.AdminDraftUpsert(readerAccountA, changed); !errors.Is(err, model.ErrAdminVersionRepairRequired) {
					t.Fatalf("metadata-only reuse error = %v, want repair-required", err)
				}
				var (
					storedTitle    string
					storedAuthor   sql.NullString
					storedLanguage string
					draftPointer   string
				)
				if err := adminDB.QueryRow(`
					SELECT title, author, language, draft_version_id
					FROM stories
					WHERE id = $1
				`, draft.StoryID).Scan(&storedTitle, &storedAuthor, &storedLanguage, &draftPointer); err != nil {
					t.Fatalf("read story after metadata-only conflict: %v", err)
				}
				if storedTitle != req.Title || storedAuthor.Valid || storedLanguage != language ||
					draftPointer != draft.StoryVersionID {
					t.Fatalf(
						"metadata-only conflict changed stored state: title=%q author=%#v language=%q pointer=%s",
						storedTitle,
						storedAuthor,
						storedLanguage,
						draftPointer,
					)
				}
			})
		}

		// Preserve the segment count and structural shape while changing every
		// content-bearing class of persisted field. Structural validation still
		// passes; exact incoming-sequence comparison must refuse reuse.
		if _, err := adminDB.Exec(`
			UPDATE story_segments
			SET content_key = $2,
			    markdown = 'Tampered but structurally valid.',
			    rendered_html = '<p>Tampered but structurally valid.</p>',
			    word_count = 4
			WHERE story_version_id = $1 AND ordinal = 2
		`, draft.StoryVersionID, strings.Repeat("f", 64)); err != nil {
			t.Fatalf("tamper same-count idempotency target: %v", err)
		}
		if _, err := store.AdminDraftUpsert(readerAccountA, req); !errors.Is(err, model.ErrAdminVersionRepairRequired) {
			t.Fatalf("same-count tampered reuse error = %v, want repair-required", err)
		}
		var (
			versionCount int
			segmentCount int
			draftPointer sql.NullString
		)
		if err := adminDB.QueryRow(`SELECT count(*) FROM story_versions WHERE story_id = $1`, draft.StoryID).Scan(&versionCount); err != nil {
			t.Fatalf("count versions after same-count reuse refusal: %v", err)
		}
		if err := adminDB.QueryRow(`SELECT count(*) FROM story_segments WHERE story_version_id = $1`, draft.StoryVersionID).Scan(&segmentCount); err != nil {
			t.Fatalf("count segments after same-count reuse refusal: %v", err)
		}
		if err := adminDB.QueryRow(`SELECT draft_version_id FROM stories WHERE id = $1`, draft.StoryID).Scan(&draftPointer); err != nil {
			t.Fatalf("read draft pointer after same-count reuse refusal: %v", err)
		}
		if versionCount != 1 || segmentCount != draft.SegmentsCount ||
			!draftPointer.Valid || draftPointer.String != draft.StoryVersionID {
			t.Fatalf(
				"same-count reuse refusal changed stored state: versions=%d segments=%d pointer=%#v",
				versionCount,
				segmentCount,
				draftPointer,
			)
		}

		if _, err := adminDB.Exec(`DELETE FROM story_segments WHERE story_version_id = $1`, draft.StoryVersionID); err != nil {
			t.Fatalf("corrupt idempotency target: %v", err)
		}

		if _, err := store.AdminDraftUpsert(readerAccountA, req); !errors.Is(err, model.ErrAdminVersionRepairRequired) {
			t.Fatalf("corrupt idempotent reuse error = %v, want repair-required", err)
		}
		if err := store.AdminPublish(readerAccountA, req.Slug, draft.StoryVersionID); !errors.Is(err, model.ErrAdminPublishInvalid) {
			t.Fatalf("publish corrupt idempotency target error = %v, want publish-invalid", err)
		}

		var (
			isPublished bool
			publishedID sql.NullString
			draftID     sql.NullString
		)
		if err := adminDB.QueryRow(`
			SELECT is_published, published_version_id, draft_version_id
			FROM stories
			WHERE id = $1
		`, draft.StoryID).Scan(&isPublished, &publishedID, &draftID); err != nil {
			t.Fatalf("read corrupt idempotency target state: %v", err)
		}
		if isPublished || publishedID.Valid || !draftID.Valid || draftID.String != draft.StoryVersionID {
			t.Fatalf("corrupt reuse/publish mutated pointers: published=%t publishedID=%#v draftID=%#v", isPublished, publishedID, draftID)
		}
	})

	t.Run("idempotent reuse preserves full additive frontmatter", func(t *testing.T) {
		req := model.AdminDraftUpsertRequest{
			Slug:     "reuse-additive-frontmatter",
			Title:    "Reuse additive frontmatter",
			Language: &language,
			Markdown: "---\ndisplayNote: Keep this note\nlargeMeasure: 1e21\npresentation:\n  tone: calm\n---\n# Reuse additive frontmatter\n\nReadable body.\n",
		}
		draft, err := store.AdminDraftUpsert(readerAccountA, req)
		if err != nil {
			t.Fatalf("insert additive-frontmatter target: %v", err)
		}
		t.Cleanup(func() { _, _ = adminDB.Exec(`DELETE FROM stories WHERE id = $1`, draft.StoryID) })
		exact, err := store.AdminDraftUpsert(readerAccountA, req)
		if err != nil {
			t.Fatalf("reuse exact additive frontmatter: %v", err)
		}
		if exact.StoryVersionID != draft.StoryVersionID || exact.RenderedHTML != draft.RenderedHTML {
			t.Fatalf("exact additive-frontmatter reuse = %#v, want version %s", exact, draft.StoryVersionID)
		}

		changed := req
		changed.Markdown = strings.Replace(req.Markdown, "Keep this note", "Different note", 1)
		if _, err := store.AdminDraftUpsert(readerAccountA, changed); !errors.Is(err, model.ErrAdminVersionRepairRequired) {
			t.Fatalf("changed additive-frontmatter reuse error = %v, want repair-required", err)
		}
		var pointer string
		if err := adminDB.QueryRow(`SELECT draft_version_id FROM stories WHERE id = $1`, draft.StoryID).Scan(&pointer); err != nil {
			t.Fatalf("read additive-frontmatter pointer: %v", err)
		}
		if pointer != draft.StoryVersionID {
			t.Fatalf("changed additive frontmatter moved pointer to %s", pointer)
		}
	})

	t.Run("optional author frontmatter variants reuse canonically", func(t *testing.T) {
		req := model.AdminDraftUpsertRequest{
			Slug:     "reuse-optional-author",
			Title:    "Reuse optional author",
			Language: &language,
			Markdown: "# Reuse optional author\n\nReadable body.\n",
		}
		draft, err := store.AdminDraftUpsert(readerAccountA, req)
		if err != nil {
			t.Fatalf("insert optional-author target: %v", err)
		}
		t.Cleanup(func() { _, _ = adminDB.Exec(`DELETE FROM stories WHERE id = $1`, draft.StoryID) })
		variants := []struct {
			name      string
			statement string
		}{
			{name: "missing", statement: `UPDATE story_versions SET frontmatter = frontmatter - 'author' WHERE id = $1`},
			{name: "null", statement: `UPDATE story_versions SET frontmatter = jsonb_set(frontmatter, '{author}', 'null'::jsonb, true) WHERE id = $1`},
			{name: "blank", statement: `UPDATE story_versions SET frontmatter = jsonb_set(frontmatter, '{author}', to_jsonb('   '::text), true) WHERE id = $1`},
		}
		for _, variant := range variants {
			t.Run(variant.name, func(t *testing.T) {
				if _, err := adminDB.Exec(variant.statement, draft.StoryVersionID); err != nil {
					t.Fatalf("set %s optional author: %v", variant.name, err)
				}
				exact, err := store.AdminDraftUpsert(readerAccountA, req)
				if err != nil {
					t.Fatalf("reuse %s optional author: %v", variant.name, err)
				}
				if exact.StoryVersionID != draft.StoryVersionID {
					t.Fatalf("%s optional author reused version %s, want %s", variant.name, exact.StoryVersionID, draft.StoryVersionID)
				}
			})
		}
	})

	t.Run("idempotent reuse refuses each persisted mismatch", func(t *testing.T) {
		tests := []struct {
			name   string
			slug   string
			mutate func(*testing.T, model.AdminDraftUpsertResponse)
		}{
			{
				name: "valid but different immutable metadata",
				slug: "reuse-metadata-mismatch",
				mutate: func(t *testing.T, draft model.AdminDraftUpsertResponse) {
					t.Helper()
					if _, err := adminDB.Exec(`
						UPDATE story_versions
						SET frontmatter = jsonb_set(frontmatter, '{title}', to_jsonb('Different valid title'::text), true)
						WHERE id = $1
					`, draft.StoryVersionID); err != nil {
						t.Fatalf("change immutable metadata: %v", err)
					}
				},
			},
			{
				name: "valid but different immutable author",
				slug: "reuse-author-mismatch",
				mutate: func(t *testing.T, draft model.AdminDraftUpsertResponse) {
					t.Helper()
					if _, err := adminDB.Exec(`
						UPDATE story_versions
						SET frontmatter = jsonb_set(frontmatter, '{author}', to_jsonb('Different valid author'::text), true)
						WHERE id = $1
					`, draft.StoryVersionID); err != nil {
						t.Fatalf("change immutable author: %v", err)
					}
				},
			},
			{
				name: "valid but different immutable language",
				slug: "reuse-language-mismatch",
				mutate: func(t *testing.T, draft model.AdminDraftUpsertResponse) {
					t.Helper()
					if _, err := adminDB.Exec(`
						UPDATE story_versions
						SET frontmatter = jsonb_set(frontmatter, '{language}', to_jsonb('cy'::text), true)
						WHERE id = $1
					`, draft.StoryVersionID); err != nil {
						t.Fatalf("change immutable language: %v", err)
					}
				},
			},
			{
				name: "valid but different additive frontmatter",
				slug: "reuse-additive-mismatch",
				mutate: func(t *testing.T, draft model.AdminDraftUpsertResponse) {
					t.Helper()
					if _, err := adminDB.Exec(`
						UPDATE story_versions
						SET frontmatter = jsonb_set(frontmatter, '{displayNote}', to_jsonb('Different safe metadata'::text), true)
						WHERE id = $1
					`, draft.StoryVersionID); err != nil {
						t.Fatalf("change additive frontmatter: %v", err)
					}
				},
			},
			{
				name: "version markdown",
				slug: "reuse-version-markdown",
				mutate: func(t *testing.T, draft model.AdminDraftUpsertResponse) {
					t.Helper()
					if _, err := adminDB.Exec(`UPDATE story_versions SET markdown = markdown || E'\n' WHERE id = $1`, draft.StoryVersionID); err != nil {
						t.Fatalf("change version Markdown: %v", err)
					}
				},
			},
			{
				name: "version rendered HTML",
				slug: "reuse-version-rendered",
				mutate: func(t *testing.T, draft model.AdminDraftUpsertResponse) {
					t.Helper()
					if _, err := adminDB.Exec(`UPDATE story_versions SET rendered_html = '<p>Different rendering</p>' WHERE id = $1`, draft.StoryVersionID); err != nil {
						t.Fatalf("change version rendered HTML: %v", err)
					}
				},
			},
			{
				name: "version content hash",
				slug: "reuse-version-content-hash",
				mutate: func(t *testing.T, draft model.AdminDraftUpsertResponse) {
					t.Helper()
					if _, err := adminDB.Exec(`UPDATE story_versions SET content_hash = $2 WHERE id = $1`, draft.StoryVersionID, strings.Repeat("0", 64)); err != nil {
						t.Fatalf("change version content hash: %v", err)
					}
				},
			},
			{
				name: "segment markdown",
				slug: "reuse-segment-markdown",
				mutate: func(t *testing.T, draft model.AdminDraftUpsertResponse) {
					t.Helper()
					if _, err := adminDB.Exec(`UPDATE story_segments SET markdown = 'Different Markdown.' WHERE story_version_id = $1 AND ordinal = 2`, draft.StoryVersionID); err != nil {
						t.Fatalf("change segment Markdown: %v", err)
					}
				},
			},
			{
				name: "segment rendered HTML",
				slug: "reuse-segment-rendered",
				mutate: func(t *testing.T, draft model.AdminDraftUpsertResponse) {
					t.Helper()
					if _, err := adminDB.Exec(`UPDATE story_segments SET rendered_html = '<p>Different rendering.</p>' WHERE story_version_id = $1 AND ordinal = 2`, draft.StoryVersionID); err != nil {
						t.Fatalf("change segment rendered HTML: %v", err)
					}
				},
			},
			{
				name: "segment identity",
				slug: "reuse-segment-identity",
				mutate: func(t *testing.T, draft model.AdminDraftUpsertResponse) {
					t.Helper()
					if _, err := adminDB.Exec(`UPDATE story_segments SET content_key = $2 WHERE story_version_id = $1 AND ordinal = 2`, draft.StoryVersionID, strings.Repeat("f", 64)); err != nil {
						t.Fatalf("change segment identity: %v", err)
					}
				},
			},
			{
				name: "segment word count",
				slug: "reuse-segment-word-count",
				mutate: func(t *testing.T, draft model.AdminDraftUpsertResponse) {
					t.Helper()
					if _, err := adminDB.Exec(`UPDATE story_segments SET word_count = word_count + 1 WHERE story_version_id = $1 AND ordinal = 2`, draft.StoryVersionID); err != nil {
						t.Fatalf("change segment word count: %v", err)
					}
				},
			},
			{
				name: "reordered segments",
				slug: "reuse-reordered-segments",
				mutate: func(t *testing.T, draft model.AdminDraftUpsertResponse) {
					t.Helper()
					tx, err := adminDB.Begin()
					if err != nil {
						t.Fatalf("begin segment reorder: %v", err)
					}
					defer func() { _ = tx.Rollback() }()
					for _, statement := range []string{
						`UPDATE story_segments SET ordinal = 100 WHERE story_version_id = $1 AND ordinal = 1`,
						`UPDATE story_segments SET ordinal = 1 WHERE story_version_id = $1 AND ordinal = 2`,
						`UPDATE story_segments SET ordinal = 2 WHERE story_version_id = $1 AND ordinal = 100`,
					} {
						if _, err := tx.Exec(statement, draft.StoryVersionID); err != nil {
							t.Fatalf("reorder segments: %v", err)
						}
					}
					if err := tx.Commit(); err != nil {
						t.Fatalf("commit segment reorder: %v", err)
					}
				},
			},
			{
				name: "one missing segment",
				slug: "reuse-missing-segment",
				mutate: func(t *testing.T, draft model.AdminDraftUpsertResponse) {
					t.Helper()
					if _, err := adminDB.Exec(`DELETE FROM story_segments WHERE story_version_id = $1 AND ordinal = 3`, draft.StoryVersionID); err != nil {
						t.Fatalf("delete one segment: %v", err)
					}
				},
			},
			{
				name: "all segments deleted",
				slug: "reuse-deleted-segments",
				mutate: func(t *testing.T, draft model.AdminDraftUpsertResponse) {
					t.Helper()
					if _, err := adminDB.Exec(`DELETE FROM story_segments WHERE story_version_id = $1`, draft.StoryVersionID); err != nil {
						t.Fatalf("delete all segments: %v", err)
					}
				},
			},
		}

		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				req := model.AdminDraftUpsertRequest{
					Slug:     test.slug,
					Title:    "Persisted mismatch target",
					Language: &language,
					Markdown: "# Persisted mismatch target\n\nFirst paragraph.\n\nSecond paragraph.\n",
				}
				draft, err := store.AdminDraftUpsert(readerAccountA, req)
				if err != nil {
					t.Fatalf("insert persisted mismatch target: %v", err)
				}
				t.Cleanup(func() { _, _ = adminDB.Exec(`DELETE FROM stories WHERE id = $1`, draft.StoryID) })
				test.mutate(t, draft)

				if _, err := store.AdminDraftUpsert(readerAccountA, req); !errors.Is(err, model.ErrAdminVersionRepairRequired) {
					t.Fatalf("corrupt reuse error = %v, want repair-required", err)
				}
				var (
					versionCount int
					draftPointer string
				)
				if err := adminDB.QueryRow(`SELECT count(*) FROM story_versions WHERE story_id = $1`, draft.StoryID).Scan(&versionCount); err != nil {
					t.Fatalf("count versions after reuse refusal: %v", err)
				}
				if err := adminDB.QueryRow(`SELECT draft_version_id FROM stories WHERE id = $1`, draft.StoryID).Scan(&draftPointer); err != nil {
					t.Fatalf("read pointer after reuse refusal: %v", err)
				}
				if versionCount != 1 || draftPointer != draft.StoryVersionID {
					t.Fatalf("reuse refusal changed version state: count=%d pointer=%s", versionCount, draftPointer)
				}
			})
		}
	})

	t.Run("idempotent reuse reads one locked version snapshot", func(t *testing.T) {
		req := model.AdminDraftUpsertRequest{
			Slug:     "reuse-version-lock",
			Title:    "Reuse version lock",
			Language: &language,
			Markdown: "# Reuse version lock\n\nReadable body.\n",
		}
		draft, err := store.AdminDraftUpsert(readerAccountA, req)
		if err != nil {
			t.Fatalf("insert reuse lock target: %v", err)
		}
		t.Cleanup(func() { _, _ = adminDB.Exec(`DELETE FROM stories WHERE id = $1`, draft.StoryID) })

		mutationTx, err := adminDB.Begin()
		if err != nil {
			t.Fatalf("begin concurrent version mutation: %v", err)
		}
		mutationFinished := false
		defer func() {
			if !mutationFinished {
				_ = mutationTx.Rollback()
			}
		}()
		if _, err := mutationTx.Exec(`
			UPDATE story_versions
			SET rendered_html = '<p>Committed concurrent mismatch.</p>'
			WHERE id = $1
		`, draft.StoryVersionID); err != nil {
			t.Fatalf("hold concurrent version mutation: %v", err)
		}

		const reuseApplicationName = "reader_reuse_version_lock_test"
		lockingStore := newReaderIntegrationStoreWithApplicationName(t, databaseURL, reuseApplicationName)
		reuseResult := make(chan error, 1)
		go func() {
			_, err := lockingStore.AdminDraftUpsert(readerAccountA, req)
			reuseResult <- err
		}()

		lockDeadline := time.Now().Add(5 * time.Second)
		lockObserved := false
		for time.Now().Before(lockDeadline) {
			select {
			case err := <-reuseResult:
				t.Fatalf("AdminDraftUpsert returned before version mutation committed: %v", err)
			default:
			}
			if err := adminDB.QueryRow(`
				SELECT EXISTS (
					SELECT 1
					FROM pg_stat_activity
					WHERE application_name = $1
					  AND state = 'active'
					  AND wait_event_type = 'Lock'
					  AND query LIKE '%content_hash = $2%'
				)
			`, reuseApplicationName).Scan(&lockObserved); err != nil {
				t.Fatalf("inspect blocked idempotent reuse: %v", err)
			}
			if lockObserved {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !lockObserved {
			t.Fatal("AdminDraftUpsert did not wait for the concurrent version mutation")
		}
		if err := mutationTx.Commit(); err != nil {
			t.Fatalf("commit concurrent version mutation: %v", err)
		}
		mutationFinished = true

		select {
		case err := <-reuseResult:
			if !errors.Is(err, model.ErrAdminVersionRepairRequired) {
				t.Fatalf("reuse after committed version mutation = %v, want repair-required", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("AdminDraftUpsert remained blocked after version mutation committed")
		}

		var draftPointer string
		if err := adminDB.QueryRow(`SELECT draft_version_id FROM stories WHERE id = $1`, draft.StoryID).Scan(&draftPointer); err != nil {
			t.Fatalf("read pointer after concurrent reuse refusal: %v", err)
		}
		if draftPointer != draft.StoryVersionID {
			t.Fatalf("concurrent reuse refusal changed draft pointer to %s", draftPointer)
		}
	})

	t.Run("idempotent reuse locks the persisted segment sequence", func(t *testing.T) {
		req := model.AdminDraftUpsertRequest{
			Slug:     "reuse-segment-lock",
			Title:    "Reuse segment lock",
			Language: &language,
			Markdown: "# Reuse segment lock\n\nReadable body.\n",
		}
		draft, err := store.AdminDraftUpsert(readerAccountA, req)
		if err != nil {
			t.Fatalf("insert reuse segment-lock target: %v", err)
		}
		t.Cleanup(func() { _, _ = adminDB.Exec(`DELETE FROM stories WHERE id = $1`, draft.StoryID) })

		mutationTx, err := adminDB.Begin()
		if err != nil {
			t.Fatalf("begin concurrent segment reuse mutation: %v", err)
		}
		mutationFinished := false
		defer func() {
			if !mutationFinished {
				_ = mutationTx.Rollback()
			}
		}()
		if _, err := mutationTx.Exec(`
			UPDATE story_segments
			SET rendered_html = '<p>Committed segment mismatch.</p>'
			WHERE story_version_id = $1 AND ordinal = 2
		`, draft.StoryVersionID); err != nil {
			t.Fatalf("hold concurrent segment reuse mutation: %v", err)
		}

		const reuseApplicationName = "reader_reuse_segment_lock_test"
		lockingStore := newReaderIntegrationStoreWithApplicationName(t, databaseURL, reuseApplicationName)
		reuseResult := make(chan error, 1)
		go func() {
			_, err := lockingStore.AdminDraftUpsert(readerAccountA, req)
			reuseResult <- err
		}()

		lockDeadline := time.Now().Add(5 * time.Second)
		lockObserved := false
		for time.Now().Before(lockDeadline) {
			select {
			case err := <-reuseResult:
				t.Fatalf("AdminDraftUpsert returned before segment mutation committed: %v", err)
			default:
			}
			if err := adminDB.QueryRow(`
				SELECT EXISTS (
					SELECT 1
					FROM pg_stat_activity
					WHERE application_name = $1
					  AND state = 'active'
					  AND wait_event_type = 'Lock'
					  AND query LIKE '%FOR SHARE OF segment%'
				)
			`, reuseApplicationName).Scan(&lockObserved); err != nil {
				t.Fatalf("inspect blocked segment reuse: %v", err)
			}
			if lockObserved {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !lockObserved {
			t.Fatal("AdminDraftUpsert did not wait for the concurrent segment mutation")
		}
		if err := mutationTx.Commit(); err != nil {
			t.Fatalf("commit concurrent segment reuse mutation: %v", err)
		}
		mutationFinished = true

		select {
		case err := <-reuseResult:
			if !errors.Is(err, model.ErrAdminVersionRepairRequired) {
				t.Fatalf("reuse after committed segment mutation = %v, want repair-required", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("AdminDraftUpsert remained blocked after segment mutation committed")
		}

		var draftPointer string
		if err := adminDB.QueryRow(`SELECT draft_version_id FROM stories WHERE id = $1`, draft.StoryID).Scan(&draftPointer); err != nil {
			t.Fatalf("read pointer after concurrent segment reuse refusal: %v", err)
		}
		if draftPointer != draft.StoryVersionID {
			t.Fatalf("concurrent segment reuse refusal changed draft pointer to %s", draftPointer)
		}
	})

	t.Run("publication validates immutable metadata identities and readable content atomically", func(t *testing.T) {
		const slug = "publication-validation-story"
		first, err := store.AdminDraftUpsert(readerAccountA, model.AdminDraftUpsertRequest{
			Slug:     slug,
			Title:    "Publication validation v1",
			Language: &language,
			Markdown: "# Publication validation v1\n\nOriginal body.\n",
		})
		if err != nil {
			t.Fatalf("insert publication validation v1: %v", err)
		}
		t.Cleanup(func() { _, _ = adminDB.Exec(`DELETE FROM stories WHERE id = $1`, first.StoryID) })
		if err := store.AdminPublish(readerAccountA, slug, first.StoryVersionID); err != nil {
			t.Fatalf("publish validation v1: %v", err)
		}
		second, err := store.AdminDraftUpsert(readerAccountA, model.AdminDraftUpsertRequest{
			Slug:     slug,
			Title:    "Publication validation v2",
			Language: &language,
			Markdown: "# Publication validation v2\n\n## Chapter\n\nReplacement body.\n",
		})
		if err != nil {
			t.Fatalf("insert publication validation v2: %v", err)
		}

		assertPublishedPointer := func(want string) {
			t.Helper()
			var (
				isPublished bool
				publishedID string
			)
			if err := adminDB.QueryRow(`
				SELECT is_published, published_version_id
				FROM stories
				WHERE id = $1
			`, first.StoryID).Scan(&isPublished, &publishedID); err != nil {
				t.Fatalf("read publication pointer: %v", err)
			}
			if !isPublished || publishedID != want {
				t.Fatalf("publication pointer = published %t / %s, want true / %s", isPublished, publishedID, want)
			}
		}

		var (
			originalVersionMarkdown string
			originalVersionRendered string
			originalContentHash     string
		)
		if err := adminDB.QueryRow(`
			SELECT markdown, rendered_html, content_hash
			FROM story_versions
			WHERE id = $1
		`, second.StoryVersionID).Scan(&originalVersionMarkdown, &originalVersionRendered, &originalContentHash); err != nil {
			t.Fatalf("read canonical version fields: %v", err)
		}
		versionMutations := []struct {
			name      string
			column    string
			badValue  string
			goodValue string
		}{
			{name: "version Markdown", column: "markdown", badValue: originalVersionMarkdown + "\n", goodValue: originalVersionMarkdown},
			{name: "version rendered HTML", column: "rendered_html", badValue: "<p>Different version rendering.</p>", goodValue: originalVersionRendered},
			{name: "version content hash", column: "content_hash", badValue: strings.Repeat("0", 64), goodValue: originalContentHash},
		}
		for _, mutation := range versionMutations {
			t.Run(mutation.name, func(t *testing.T) {
				// Column names are fixed test constants; values remain parameters.
				if _, err := adminDB.Exec(
					`UPDATE story_versions SET `+mutation.column+` = $2 WHERE id = $1`,
					second.StoryVersionID,
					mutation.badValue,
				); err != nil {
					t.Fatalf("mutate %s: %v", mutation.name, err)
				}
				if err := store.AdminPublish(readerAccountA, slug, second.StoryVersionID); !errors.Is(err, model.ErrAdminPublishInvalid) {
					t.Fatalf("publish noncanonical %s error = %v, want publish-invalid", mutation.name, err)
				}
				assertPublishedPointer(first.StoryVersionID)
				if _, err := adminDB.Exec(
					`UPDATE story_versions SET `+mutation.column+` = $2 WHERE id = $1`,
					second.StoryVersionID,
					mutation.goodValue,
				); err != nil {
					t.Fatalf("restore %s: %v", mutation.name, err)
				}
			})
		}

		frontmatterMutations := []struct {
			name      string
			path      string
			badValue  string
			goodValue string
		}{
			{name: "noncanonical title whitespace", path: "title", badValue: " Publication validation v2 ", goodValue: "Publication validation v2"},
			{name: "noncanonical author whitespace", path: "author", badValue: " Padded author ", goodValue: ""},
			{name: "noncanonical language whitespace", path: "language", badValue: " en-GB ", goodValue: language},
		}
		for _, mutation := range frontmatterMutations {
			t.Run(mutation.name, func(t *testing.T) {
				if _, err := adminDB.Exec(`
					UPDATE story_versions
					SET frontmatter = jsonb_set(frontmatter, ARRAY[$2]::text[], to_jsonb($3::text), true)
					WHERE id = $1
				`, second.StoryVersionID, mutation.path, mutation.badValue); err != nil {
					t.Fatalf("mutate %s: %v", mutation.name, err)
				}
				if err := store.AdminPublish(readerAccountA, slug, second.StoryVersionID); !errors.Is(err, model.ErrAdminPublishInvalid) {
					t.Fatalf("publish %s error = %v, want publish-invalid", mutation.name, err)
				}
				assertPublishedPointer(first.StoryVersionID)
				if _, err := adminDB.Exec(`
					UPDATE story_versions
					SET frontmatter = jsonb_set(frontmatter, ARRAY[$2]::text[], to_jsonb($3::text), true)
					WHERE id = $1
				`, second.StoryVersionID, mutation.path, mutation.goodValue); err != nil {
					t.Fatalf("restore %s: %v", mutation.name, err)
				}
			})
		}

		var (
			originalSegmentMarkdown string
			originalSegmentRendered string
		)
		if err := adminDB.QueryRow(`
			SELECT markdown, rendered_html
			FROM story_segments
			WHERE story_version_id = $1 AND ordinal = 3
		`, second.StoryVersionID).Scan(&originalSegmentMarkdown, &originalSegmentRendered); err != nil {
			t.Fatalf("read canonical segment content: %v", err)
		}
		segmentMutations := []struct {
			name         string
			column       string
			badValue     string
			goodValue    string
			assertReader bool
		}{
			{name: "segment Markdown", column: "markdown", badValue: "Different segment Markdown.", goodValue: originalSegmentMarkdown},
			{name: "segment rendered HTML", column: "rendered_html", badValue: "<p>Different segment rendering.</p>", goodValue: originalSegmentRendered},
			{name: "unsafe event-handler rendered HTML", column: "rendered_html", badValue: `<img src=x onerror="globalThis.__ppUnsafe=true">`, goodValue: originalSegmentRendered, assertReader: true},
			{name: "unsafe JavaScript link rendered HTML", column: "rendered_html", badValue: `<a href="javascript:globalThis.__ppUnsafe=true">Unsafe</a>`, goodValue: originalSegmentRendered, assertReader: true},
		}
		for _, mutation := range segmentMutations {
			t.Run(mutation.name, func(t *testing.T) {
				if _, err := adminDB.Exec(
					`UPDATE story_segments SET `+mutation.column+` = $2 WHERE story_version_id = $1 AND ordinal = 3`,
					second.StoryVersionID,
					mutation.badValue,
				); err != nil {
					t.Fatalf("mutate %s: %v", mutation.name, err)
				}
				if err := store.AdminPublish(readerAccountA, slug, second.StoryVersionID); !errors.Is(err, model.ErrAdminPublishInvalid) {
					t.Fatalf("publish noncanonical %s error = %v, want publish-invalid", mutation.name, err)
				}
				assertPublishedPointer(first.StoryVersionID)
				if mutation.assertReader {
					readerStory, err := store.ReaderStory(readerAccountA, slug)
					if err != nil {
						t.Fatalf("read prior safe publication after %s refusal: %v", mutation.name, err)
					}
					if readerStory.Version != first.Version {
						t.Fatalf("Reader returned version %d after %s refusal, want %d", readerStory.Version, mutation.name, first.Version)
					}
					for _, segment := range readerStory.Segments {
						lower := strings.ToLower(segment.RenderedHTML)
						if strings.Contains(lower, "onerror") || strings.Contains(lower, "javascript:") ||
							strings.Contains(lower, "__ppunsafe") {
							t.Fatalf("Reader exposed refused active content after %s: %s", mutation.name, segment.RenderedHTML)
						}
					}
				}
				if _, err := adminDB.Exec(
					`UPDATE story_segments SET `+mutation.column+` = $2 WHERE story_version_id = $1 AND ordinal = 3`,
					second.StoryVersionID,
					mutation.goodValue,
				); err != nil {
					t.Fatalf("restore %s: %v", mutation.name, err)
				}
			})
		}

		t.Run("raw HTML only manual version has no readable segment", func(t *testing.T) {
			const (
				rawMarkdown        = "<script>alert(1)</script>\n"
				rawSegmentMarkdown = "<script>alert(1)</script>"
				rawRendered        = "<!-- raw HTML omitted -->\n"
				rawContentHash     = "cfc151a63b53ac09647ea69d07410784a48c62c857ab6079e2ee8b3a3c9efbbe"
				rawContentKey      = "4a2812dc34b4ed923adb2ca9c952e7c5308f36082a3097763c4d9481a64edab6"
			)
			var rawVersionID string
			if err := adminDB.QueryRow(`
				INSERT INTO story_versions (
					story_id, version, frontmatter, markdown, rendered_html, content_hash
				) VALUES (
					$1, 3, '{"title":"Unreadable manual version","author":"","language":"en-GB"}',
					$2, $3, $4
				)
				RETURNING id
			`, first.StoryID, rawMarkdown, rawRendered, rawContentHash).Scan(&rawVersionID); err != nil {
				t.Fatalf("insert raw-HTML-only version: %v", err)
			}
			t.Cleanup(func() { _, _ = adminDB.Exec(`DELETE FROM story_versions WHERE id = $1`, rawVersionID) })
			if _, err := adminDB.Exec(`
				INSERT INTO story_segments (
					story_version_id, ordinal, segment_kind, content_key, content_occurrence,
					markdown, rendered_html, word_count
				) VALUES ($1, 1, 'other', $2, 1, $3, $4, 1)
			`, rawVersionID, rawContentKey, rawSegmentMarkdown, rawRendered); err != nil {
				t.Fatalf("insert raw-HTML-only segment: %v", err)
			}

			if err := store.AdminPublish(readerAccountA, slug, rawVersionID); !errors.Is(err, model.ErrAdminPublishInvalid) {
				t.Fatalf("publish raw-HTML-only version error = %v, want publish-invalid", err)
			}
			assertPublishedPointer(first.StoryVersionID)
			readerStory, err := store.ReaderStory(readerAccountA, slug)
			if err != nil {
				t.Fatalf("read prior safe publication after raw-only refusal: %v", err)
			}
			if readerStory.Version != first.Version {
				t.Fatalf("Reader returned version %d after raw-only refusal, want %d", readerStory.Version, first.Version)
			}
		})

		if _, err := adminDB.Exec(`UPDATE story_versions SET frontmatter = frontmatter - 'title' WHERE id = $1`, second.StoryVersionID); err != nil {
			t.Fatalf("remove immutable title: %v", err)
		}
		if err := store.AdminPublish(readerAccountA, slug, second.StoryVersionID); !errors.Is(err, model.ErrAdminPublishInvalid) {
			t.Fatalf("publish missing immutable title error = %v, want publish-invalid", err)
		}
		assertPublishedPointer(first.StoryVersionID)
		if _, err := adminDB.Exec(`
			UPDATE story_versions
			SET frontmatter = jsonb_set(frontmatter, '{title}', to_jsonb($2::text), true)
			WHERE id = $1
		`, second.StoryVersionID, "Publication validation v2"); err != nil {
			t.Fatalf("restore immutable title: %v", err)
		}

		if _, err := adminDB.Exec(`
			UPDATE story_segments
			SET chapter_occurrence = 2
			WHERE story_version_id = $1 AND ordinal = 3
		`, second.StoryVersionID); err != nil {
			t.Fatalf("corrupt chapter propagation: %v", err)
		}
		if err := store.AdminPublish(readerAccountA, slug, second.StoryVersionID); !errors.Is(err, model.ErrAdminPublishInvalid) {
			t.Fatalf("publish invalid chapter identity error = %v, want publish-invalid", err)
		}
		assertPublishedPointer(first.StoryVersionID)
		if _, err := adminDB.Exec(`
			UPDATE story_segments
			SET chapter_occurrence = 1
			WHERE story_version_id = $1 AND ordinal = 3
		`, second.StoryVersionID); err != nil {
			t.Fatalf("restore chapter propagation: %v", err)
		}

		var originalRenderedHTML string
		if err := adminDB.QueryRow(`
			SELECT rendered_html
			FROM story_segments
			WHERE story_version_id = $1 AND ordinal = 3
		`, second.StoryVersionID).Scan(&originalRenderedHTML); err != nil {
			t.Fatalf("read original rendered segment: %v", err)
		}
		mutationTx, err := adminDB.Begin()
		if err != nil {
			t.Fatalf("begin concurrent segment mutation: %v", err)
		}
		defer func() { _ = mutationTx.Rollback() }()
		if _, err := mutationTx.Exec(`
			UPDATE story_segments
			SET rendered_html = ''
			WHERE story_version_id = $1 AND ordinal = 3
		`, second.StoryVersionID); err != nil {
			t.Fatalf("hold unreadable segment mutation: %v", err)
		}

		const publishApplicationName = "reader_publish_segment_lock_test"
		lockingStore := newReaderIntegrationStoreWithApplicationName(t, databaseURL, publishApplicationName)
		publishResult := make(chan error, 1)
		go func() {
			publishResult <- lockingStore.AdminPublish(readerAccountA, slug, second.StoryVersionID)
		}()
		lockDeadline := time.Now().Add(5 * time.Second)
		lockObserved := false
		for time.Now().Before(lockDeadline) {
			select {
			case err := <-publishResult:
				t.Fatalf("AdminPublish returned before segment mutation committed: %v", err)
			default:
			}
			if err := adminDB.QueryRow(`
				SELECT EXISTS (
					SELECT 1
					FROM pg_stat_activity
					WHERE application_name = $1
					  AND state = 'active'
					  AND wait_event_type = 'Lock'
					  AND query LIKE '%FOR SHARE OF segment%'
				)
			`, publishApplicationName).Scan(&lockObserved); err != nil {
				t.Fatalf("inspect blocked publication validation: %v", err)
			}
			if lockObserved {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !lockObserved {
			t.Fatal("AdminPublish did not wait for the concurrent segment mutation")
		}
		if err := mutationTx.Commit(); err != nil {
			t.Fatalf("commit unreadable segment mutation: %v", err)
		}
		select {
		case err := <-publishResult:
			if !errors.Is(err, model.ErrAdminPublishInvalid) {
				t.Fatalf("publish after unreadable segment mutation = %v, want publish-invalid", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("AdminPublish remained blocked after segment mutation committed")
		}
		assertPublishedPointer(first.StoryVersionID)
		if _, err := adminDB.Exec(`
			UPDATE story_segments
			SET rendered_html = $2
			WHERE story_version_id = $1 AND ordinal = 3
		`, second.StoryVersionID, originalRenderedHTML); err != nil {
			t.Fatalf("restore rendered segment: %v", err)
		}

		if err := store.AdminPublish(readerAccountA, slug, accountBDraft.StoryVersionID); !errors.Is(err, model.ErrAdminPublishNotFound) {
			t.Fatalf("cross-account version publish error = %v, want existing not-found semantics", err)
		}
		assertPublishedPointer(first.StoryVersionID)
		if err := store.AdminPublish(readerAccountA, slug, second.StoryVersionID); err != nil {
			t.Fatalf("publish restored validation v2: %v", err)
		}
		assertPublishedPointer(second.StoryVersionID)
	})

	t.Run("ingestion assigns six ordered identities and H2 chapters", func(t *testing.T) {
		story, err := store.ReaderStory(readerAccountA, readerSlug)
		if err != nil {
			t.Fatalf("ReaderStory: %v", err)
		}
		assertReaderVersionShape(t, story, 1, 6)
		if story.Slug != readerSlug || story.Language != language || story.Author == nil || *story.Author != author {
			t.Fatalf("Reader metadata = %#v", story)
		}
		if !strings.Contains(story.Segments[1].RenderedHTML, "café 世界") {
			t.Fatalf("UTF-8 segment = %q", story.Segments[1].RenderedHTML)
		}
		if story.Segments[0].ChapterKey != nil || story.Segments[1].ChapterKey != nil {
			t.Fatal("pre-H2 segments received a chapter")
		}
		firstChapter := story.Segments[2]
		secondChapter := story.Segments[4]
		if firstChapter.Kind != "heading" || firstChapter.HeadingLevel == nil || *firstChapter.HeadingLevel != 2 ||
			firstChapter.ChapterKey == nil || *firstChapter.ChapterKey != firstChapter.ContentKey ||
			firstChapter.ChapterOccurrence == nil || *firstChapter.ChapterOccurrence != 1 {
			t.Fatalf("first chapter identity = %#v", firstChapter)
		}
		if story.Segments[3].ChapterKey == nil || *story.Segments[3].ChapterKey != firstChapter.ContentKey {
			t.Fatalf("first chapter propagation = %#v", story.Segments[3])
		}
		if secondChapter.ChapterKey == nil || *secondChapter.ChapterKey != secondChapter.ContentKey ||
			story.Segments[5].ChapterKey == nil || *story.Segments[5].ChapterKey != secondChapter.ContentKey {
			t.Fatalf("second chapter propagation = %#v / %#v", secondChapter, story.Segments[5])
		}
		if story.Segments[3].ContentKey != story.Segments[5].ContentKey ||
			story.Segments[3].ContentOccurrence != 1 || story.Segments[5].ContentOccurrence != 2 {
			t.Fatalf("duplicate identities = %#v / %#v", story.Segments[3], story.Segments[5])
		}
	})

	t.Run("one statement never mixes versions during republish", func(t *testing.T) {
		stop := make(chan struct{})
		errCh := make(chan error, 1)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := 0; ; index++ {
				select {
				case <-stop:
					return
				default:
				}
				versionID := firstDraft.StoryVersionID
				if index%2 == 1 {
					versionID = secondDraft.StoryVersionID
				}
				if _, err := adminDB.Exec(`UPDATE stories SET published_version_id = $1 WHERE id = $2`, versionID, firstDraft.StoryID); err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
			}
		}()
		for range 150 {
			story, err := store.ReaderStory(readerAccountA, readerSlug)
			if err != nil {
				close(stop)
				wg.Wait()
				t.Fatalf("ReaderStory during republish: %v", err)
			}
			switch story.Version {
			case 1:
				assertReaderVersionShape(t, story, 1, 6)
			case 2:
				assertReaderVersionShape(t, story, 2, 2)
			default:
				t.Fatalf("unexpected coherent version: %#v", story)
			}
		}
		close(stop)
		wg.Wait()
		select {
		case err := <-errCh:
			t.Fatalf("republish loop: %v", err)
		default:
		}
		if err := store.AdminPublish(readerAccountA, readerSlug, firstDraft.StoryVersionID); err != nil {
			t.Fatalf("restore publication after race: %v", err)
		}
	})

	t.Run("account and publication boundaries return not found", func(t *testing.T) {
		accountBStory, err := store.ReaderStory(readerAccountB, readerSlug)
		if err != nil {
			t.Fatalf("ReaderStory account B: %v", err)
		}
		if accountBStory.Title != "Account B isolated story" || len(accountBStory.Segments) != 2 {
			t.Fatalf("account B ReaderStory = %#v", accountBStory)
		}
		for _, test := range []struct {
			account string
			slug    string
		}{
			{account: readerAccountC, slug: readerSlug},
			{account: readerAccountA, slug: "unpublished-reader-story"},
			{account: readerAccountA, slug: "missing-reader-story"},
		} {
			if _, err := store.ReaderStory(test.account, test.slug); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("ReaderStory(%s, %s) error = %v, want sql.ErrNoRows", test.account, test.slug, err)
			}
		}
	})

	story, err := store.ReaderStory(readerAccountA, readerSlug)
	if err != nil {
		t.Fatalf("load progress target: %v", err)
	}
	progressSegment := story.Segments[3]
	locator := locatorForReaderSegment(progressSegment, 0.35)
	draftLocator := locatorForStoredReaderSegment(t, adminDB, secondDraft.StoryVersionID, 2, 0.6)

	t.Run("progress validates the exact selected version identity", func(t *testing.T) {
		empty, err := store.ProgressGet(readerAccountA, readerSlug)
		if err != nil {
			t.Fatalf("ProgressGet empty: %v", err)
		}
		if empty.Progress != nil {
			t.Fatalf("empty progress = %#v", empty.Progress)
		}
		if err := store.ProgressPut(readerAccountA, readerSlug, story.Version, locator, 0.42); err != nil {
			t.Fatalf("ProgressPut valid: %v", err)
		}
		got, err := store.ProgressGet(readerAccountA, readerSlug)
		if err != nil {
			t.Fatalf("ProgressGet saved: %v", err)
		}
		if got.Progress == nil || got.Progress.Version != story.Version ||
			!reflect.DeepEqual(got.Progress.Locator, locator) || math.Abs(got.Progress.Percent-0.42) > 0.000001 {
			t.Fatalf("saved progress = %#v", got.Progress)
		}

		mismatches := []readercontract.Locator{locator, locator, locator, locator}
		mismatches[0].Segment.Key = strings.Repeat("f", 64)
		mismatches[1].Segment.Occurrence++
		mismatches[2].Segment.Ordinal++
		mismatches[3].Chapter = nil
		for index, mismatch := range mismatches {
			if err := store.ProgressPut(readerAccountA, readerSlug, story.Version, mismatch, 0.9); !errors.Is(err, readercontract.ErrLocatorMismatch) {
				t.Fatalf("mismatch %d error = %v", index, err)
			}
		}
		if err := store.ProgressPut(readerAccountA, readerSlug, 99, locator, 0.2); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("wrong version error = %v, want sql.ErrNoRows", err)
		}
		if err := store.ProgressPut(readerAccountC, readerSlug, story.Version, locator, 0.2); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("cross-account error = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("draft and previously published versions cannot replace current progress", func(t *testing.T) {
		if err := store.ProgressPut(readerAccountA, readerSlug, secondDraft.Version, draftLocator, 0.81); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("draft version ProgressPut error = %v, want sql.ErrNoRows", err)
		}
		got, err := store.ProgressGet(readerAccountA, readerSlug)
		if err != nil {
			t.Fatalf("ProgressGet after draft rejection: %v", err)
		}
		assertProgressState(t, got, firstDraft.Version, locator, 0.42)

		if err := store.AdminPublish(readerAccountA, readerSlug, secondDraft.StoryVersionID); err != nil {
			t.Fatalf("publish second Reader version: %v", err)
		}
		if err := store.ProgressPut(readerAccountA, readerSlug, firstDraft.Version, locator, 0.82); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("previous version ProgressPut error = %v, want sql.ErrNoRows", err)
		}
		got, err = store.ProgressGet(readerAccountA, readerSlug)
		if err != nil {
			t.Fatalf("ProgressGet after previous-version rejection: %v", err)
		}
		assertProgressState(t, got, firstDraft.Version, locator, 0.42)

		if err := store.ProgressPut(readerAccountA, readerSlug, secondDraft.Version, draftLocator, 0.83); err != nil {
			t.Fatalf("current second-version ProgressPut: %v", err)
		}
		got, err = store.ProgressGet(readerAccountA, readerSlug)
		if err != nil {
			t.Fatalf("ProgressGet second version: %v", err)
		}
		assertProgressState(t, got, secondDraft.Version, draftLocator, 0.83)

		if err := store.AdminPublish(readerAccountA, readerSlug, firstDraft.StoryVersionID); err != nil {
			t.Fatalf("restore first publication: %v", err)
		}
	})

	t.Run("publication update serialises with progress validation", func(t *testing.T) {
		if _, err := adminDB.Exec(`DELETE FROM reading_progress WHERE story_id = $1`, firstDraft.StoryID); err != nil {
			t.Fatalf("clear progress before lock test: %v", err)
		}
		if err := store.AdminPublish(readerAccountA, readerSlug, firstDraft.StoryVersionID); err != nil {
			t.Fatalf("publish first version before lock test: %v", err)
		}

		publicationTx, err := adminDB.Begin()
		if err != nil {
			t.Fatalf("begin publication transaction: %v", err)
		}
		publicationFinished := false
		defer func() {
			if !publicationFinished {
				_ = publicationTx.Rollback()
			}
		}()

		var publicationPID int
		if err := publicationTx.QueryRow(`SELECT pg_backend_pid()`).Scan(&publicationPID); err != nil {
			t.Fatalf("read publication backend PID: %v", err)
		}
		if _, err := publicationTx.Exec(`
			UPDATE stories
			SET published_version_id = $1, updated_at = now()
			WHERE id = $2
		`, secondDraft.StoryVersionID, firstDraft.StoryID); err != nil {
			t.Fatalf("hold uncommitted publication update: %v", err)
		}

		const progressApplicationName = "reader_progress_publication_lock_test"
		lockingStore := newReaderIntegrationStoreWithApplicationName(t, databaseURL, progressApplicationName)
		progressResult := make(chan error, 1)
		go func() {
			progressResult <- lockingStore.ProgressPut(readerAccountA, readerSlug, firstDraft.Version, locator, 0.91)
		}()

		lockDeadline := time.Now().Add(5 * time.Second)
		lockObserved := false
		for time.Now().Before(lockDeadline) {
			select {
			case err := <-progressResult:
				t.Fatalf("ProgressPut returned before publication committed: %v", err)
			default:
			}

			if err := adminDB.QueryRow(`
				SELECT EXISTS (
					SELECT 1
					FROM pg_stat_activity
					WHERE application_name = $1
					  AND state = 'active'
					  AND wait_event_type = 'Lock'
					  AND $2 = ANY(pg_blocking_pids(pid))
					  AND query LIKE '%FOR SHARE OF story%'
				)
			`, progressApplicationName, publicationPID).Scan(&lockObserved); err != nil {
				t.Fatalf("inspect blocked progress transaction: %v", err)
			}
			if lockObserved {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !lockObserved {
			t.Fatal("ProgressPut did not wait on the publication story-row lock")
		}

		if err := publicationTx.Commit(); err != nil {
			t.Fatalf("commit publication transaction: %v", err)
		}
		publicationFinished = true

		select {
		case err := <-progressResult:
			if !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("ProgressPut after publication commit = %v, want sql.ErrNoRows", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("ProgressPut remained blocked after publication commit")
		}

		var staleProgressCount int
		if err := adminDB.QueryRow(`SELECT count(*) FROM reading_progress WHERE story_id = $1`, firstDraft.StoryID).Scan(&staleProgressCount); err != nil {
			t.Fatalf("count progress after publication race: %v", err)
		}
		if staleProgressCount != 0 {
			t.Fatalf("stale progress rows = %d, want 0", staleProgressCount)
		}

		if err := store.AdminPublish(readerAccountA, readerSlug, firstDraft.StoryVersionID); err != nil {
			t.Fatalf("restore first publication after lock test: %v", err)
		}
	})

	t.Run("real HTTP exposes clean Reader and strict progress contracts", func(t *testing.T) {
		sessions, err := session.New("reader-store-integration-session-secret", false)
		if err != nil {
			t.Fatalf("create sessions: %v", err)
		}
		token, err := sessions.Issue(readerAccountA)
		if err != nil {
			t.Fatalf("issue session: %v", err)
		}
		cookie := &http.Cookie{Name: session.CookieName, Value: token}
		handler := httpapi.New(httpapi.Config{Passcode: "123456", Sessions: sessions}, store)

		readerResponse := serveReaderRequest(t, handler, cookie, http.MethodGet, "/api/v1/reader/"+readerSlug, "")
		if readerResponse.Code != http.StatusOK || !strings.Contains(readerResponse.Body.String(), "café 世界") {
			t.Fatalf("Reader HTTP response = %d %s", readerResponse.Code, readerResponse.Body.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(readerResponse.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode Reader response: %v", err)
		}
		if _, exists := payload["markdown"]; exists {
			t.Fatal("Reader response exposed Markdown")
		}
		for _, raw := range payload["segments"].([]any) {
			segment := raw.(map[string]any)
			for _, forbidden := range []string{"id", "storyVersionId", "markdown", "locator"} {
				if _, exists := segment[forbidden]; exists {
					t.Fatalf("Reader segment exposed %s: %#v", forbidden, segment)
				}
			}
		}

		for _, path := range []string{"/api/v1/story/" + readerSlug, "/api/v1/story/" + readerSlug + "/segments"} {
			response := serveReaderRequest(t, handler, cookie, http.MethodGet, path, "")
			if response.Code != http.StatusNotFound {
				t.Fatalf("removed path %s = %d", path, response.Code)
			}
		}
		if response := serveReaderRequest(t, handler, nil, http.MethodGet, "/api/v1/reader/"+readerSlug, ""); response.Code != http.StatusUnauthorized {
			t.Fatalf("unsigned Reader status = %d", response.Code)
		}
		for _, slug := range []string{"unpublished-reader-story", "missing-reader-story"} {
			response := serveReaderRequest(t, handler, cookie, http.MethodGet, "/api/v1/reader/"+slug, "")
			if response.Code != http.StatusNotFound {
				t.Fatalf("Reader %s status = %d", slug, response.Code)
			}
		}

		validBody := progressBody(t, story.Version, locator, 0.73)
		validResponse := serveReaderRequest(t, handler, cookie, http.MethodPut, "/api/v1/progress/"+readerSlug, validBody)
		if validResponse.Code != http.StatusOK || strings.TrimSpace(validResponse.Body.String()) != `{"ok":true}` {
			t.Fatalf("valid progress response = %d %s", validResponse.Code, validResponse.Body.String())
		}
		getResponse := serveReaderRequest(t, handler, cookie, http.MethodGet, "/api/v1/progress/"+readerSlug, "")
		assertHTTPProgressState(t, getResponse, firstDraft.Version, locator, 0.73)

		draftResponse := serveReaderRequest(
			t,
			handler,
			cookie,
			http.MethodPut,
			"/api/v1/progress/"+readerSlug,
			progressBody(t, secondDraft.Version, draftLocator, 0.81),
		)
		assertHTTPProgressNotFound(t, draftResponse)
		assertHTTPProgressState(
			t,
			serveReaderRequest(t, handler, cookie, http.MethodGet, "/api/v1/progress/"+readerSlug, ""),
			firstDraft.Version,
			locator,
			0.73,
		)

		if err := store.AdminPublish(readerAccountA, readerSlug, secondDraft.StoryVersionID); err != nil {
			t.Fatalf("publish second version for HTTP progress: %v", err)
		}
		staleResponse := serveReaderRequest(t, handler, cookie, http.MethodPut, "/api/v1/progress/"+readerSlug, progressBody(t, firstDraft.Version, locator, 0.82))
		assertHTTPProgressNotFound(t, staleResponse)
		assertHTTPProgressState(
			t,
			serveReaderRequest(t, handler, cookie, http.MethodGet, "/api/v1/progress/"+readerSlug, ""),
			firstDraft.Version,
			locator,
			0.73,
		)

		currentSecondBody := progressBody(t, secondDraft.Version, draftLocator, 0.83)
		currentSecondResponse := serveReaderRequest(t, handler, cookie, http.MethodPut, "/api/v1/progress/"+readerSlug, currentSecondBody)
		if currentSecondResponse.Code != http.StatusOK || strings.TrimSpace(currentSecondResponse.Body.String()) != `{"ok":true}` {
			t.Fatalf("current second-version progress = %d %s", currentSecondResponse.Code, currentSecondResponse.Body.String())
		}
		assertHTTPProgressState(
			t,
			serveReaderRequest(t, handler, cookie, http.MethodGet, "/api/v1/progress/"+readerSlug, ""),
			secondDraft.Version,
			draftLocator,
			0.83,
		)

		invalidBodies := []string{
			`{"version":1,"locator":{"mode":"scroll","scrollY":10},"percent":0.1}`,
			`{"version":1,"locator":{"schema":2,"segment":{"key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","occurrence":1,"ordinal":1,"offset":0,"extra":true}},"percent":0.1}`,
		}
		for _, body := range invalidBodies {
			response := serveReaderRequest(t, handler, cookie, http.MethodPut, "/api/v1/progress/"+readerSlug, body)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("malformed progress status = %d; body = %s", response.Code, response.Body.String())
			}
		}
		mismatch := draftLocator
		mismatch.Segment.Key = strings.Repeat("e", 64)
		response := serveReaderRequest(t, handler, cookie, http.MethodPut, "/api/v1/progress/"+readerSlug, progressBody(t, secondDraft.Version, mismatch, 0.9))
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"locator_mismatch"`) {
			t.Fatalf("locator mismatch = %d %s", response.Code, response.Body.String())
		}
		assertHTTPProgressNotFound(t, serveReaderRequest(
			t, handler, cookie, http.MethodPut, "/api/v1/progress/"+readerSlug, progressBody(t, 99, draftLocator, 0.9),
		))

		if _, err := adminDB.Exec(`ALTER TABLE reading_progress RENAME TO reading_progress_unavailable`); err != nil {
			t.Fatalf("make progress storage unavailable: %v", err)
		}
		restored := false
		defer func() {
			if !restored {
				_, _ = adminDB.Exec(`ALTER TABLE reading_progress_unavailable RENAME TO reading_progress`)
			}
		}()
		failureResponse := serveReaderRequest(t, handler, cookie, http.MethodPut, "/api/v1/progress/"+readerSlug, currentSecondBody)
		if failureResponse.Code != http.StatusInternalServerError || strings.Contains(failureResponse.Body.String(), `"ok":true`) {
			t.Fatalf("database failure returned false success: %d %s", failureResponse.Code, failureResponse.Body.String())
		}
		if strings.Contains(strings.ToLower(failureResponse.Body.String()), "relation") {
			t.Fatal("database detail leaked in progress failure")
		}
		if _, err := adminDB.Exec(`ALTER TABLE reading_progress_unavailable RENAME TO reading_progress`); err != nil {
			t.Fatalf("restore progress storage: %v", err)
		}
		restored = true
	})
}

func newReaderIntegrationStore(t *testing.T, databaseURL string) *Store {
	t.Helper()
	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open Reader Store database: %v", err)
	}
	database.SetMaxOpenConns(8)
	database.SetMaxIdleConns(4)
	if err := database.Ping(); err != nil {
		_ = database.Close()
		t.Fatalf("ping Reader Store database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return &Store{db: database, queryTimeout: 10 * time.Second, defaultProfileByAccount: map[string]string{}}
}

func newReaderIntegrationStoreWithApplicationName(t *testing.T, databaseURL, applicationName string) *Store {
	t.Helper()
	separator := "?"
	if strings.Contains(databaseURL, "?") {
		separator = "&"
	}
	store := newReaderIntegrationStore(t, databaseURL+separator+"application_name="+applicationName)
	store.db.SetMaxOpenConns(1)
	store.db.SetMaxIdleConns(1)
	return store
}

func assertReaderVersionShape(t *testing.T, story model.ReaderStory, version, segments int) {
	t.Helper()
	if story.Version != version || len(story.Segments) != segments {
		t.Fatalf("Reader version shape = version %d / %d segments, want %d / %d", story.Version, len(story.Segments), version, segments)
	}
	for index, segment := range story.Segments {
		if segment.Ordinal != index+1 || !readercontract.ValidContentKey(segment.ContentKey) || segment.ContentOccurrence < 1 {
			t.Fatalf("invalid ordered Reader segment %d: %#v", index, segment)
		}
	}
}

func locatorForReaderSegment(segment model.ReaderSegment, offset float64) readercontract.Locator {
	locator := readercontract.Locator{
		Schema: 2,
		Segment: readercontract.LocatorSegment{
			Key:        segment.ContentKey,
			Occurrence: segment.ContentOccurrence,
			Ordinal:    segment.Ordinal,
			Offset:     offset,
		},
	}
	if segment.ChapterKey != nil && segment.ChapterOccurrence != nil {
		locator.Chapter = &readercontract.LocatorChapter{Key: *segment.ChapterKey, Occurrence: *segment.ChapterOccurrence}
	}
	return locator
}

func locatorForStoredReaderSegment(t *testing.T, database *sql.DB, versionID string, ordinal int, offset float64) readercontract.Locator {
	t.Helper()
	var (
		key               string
		occurrence        int
		chapterKey        sql.NullString
		chapterOccurrence sql.NullInt64
	)
	if err := database.QueryRow(`
		SELECT content_key, content_occurrence, chapter_key, chapter_occurrence
		FROM story_segments
		WHERE story_version_id = $1
		  AND ordinal = $2
	`, versionID, ordinal).Scan(&key, &occurrence, &chapterKey, &chapterOccurrence); err != nil {
		t.Fatalf("load stored Reader segment identity: %v", err)
	}

	locator := readercontract.Locator{
		Schema: 2,
		Segment: readercontract.LocatorSegment{
			Key:        key,
			Occurrence: occurrence,
			Ordinal:    ordinal,
			Offset:     offset,
		},
	}
	if chapterKey.Valid {
		if !chapterOccurrence.Valid {
			t.Fatal("stored Reader segment has partial chapter identity")
		}
		locator.Chapter = &readercontract.LocatorChapter{
			Key:        chapterKey.String,
			Occurrence: int(chapterOccurrence.Int64),
		}
	}
	return locator
}

func assertHTTPProgressState(t *testing.T, response *httptest.ResponseRecorder, version int, locator readercontract.Locator, percent float64) {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("progress GET = %d %s", response.Code, response.Body.String())
	}
	var state model.ProgressResponse
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode progress GET: %v", err)
	}
	assertProgressState(t, state, version, locator, percent)
}

func assertHTTPProgressNotFound(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Code != http.StatusNotFound {
		t.Fatalf("progress PUT = %d, want 404; body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Error.Code != "not_found" {
		t.Fatalf("progress not-found body = %s", response.Body.String())
	}
}

func serveReaderRequest(t *testing.T, handler http.Handler, cookie *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func progressBody(t *testing.T, version int, locator readercontract.Locator, percent float64) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"version": version, "locator": locator, "percent": percent})
	if err != nil {
		t.Fatalf("marshal progress body: %v", err)
	}
	return string(body)
}
