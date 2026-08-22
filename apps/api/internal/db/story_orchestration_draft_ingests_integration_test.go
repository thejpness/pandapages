package db

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"pandapages/api/internal/adaptationcontract"
	"pandapages/api/internal/model"
	"pandapages/api/internal/storyeditorialreview"
	"pandapages/api/internal/storygeneration"
)

const (
	draftIngestIntegrationPrincipalID = "e1500000-0000-4000-8000-000000000001"
	draftIngestIntegrationAccountID   = "e1500000-0000-4000-8000-000000000002"
)

func TestStoryOrchestrationRunDraftIngestsIntegration(t *testing.T) {
	if os.Getenv(readerIntegrationGuardVar) != "1" {
		t.Skip("set PP_READER_STORE_TEST_DISPOSABLE=1 to run the disposable PostgreSQL integration test")
	}
	databaseURL := strings.TrimSpace(os.Getenv(readerIntegrationURLVar))
	if databaseURL == "" {
		t.Fatalf("%s is required", readerIntegrationURLVar)
	}
	adminDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	var databaseName string
	if err := adminDB.QueryRow(`SELECT current_database()`).Scan(&databaseName); err != nil || databaseName != readerIntegrationDBName {
		t.Fatalf("refusing draft ingest database %q: %v", databaseName, err)
	}

	const slugPrefix = "approved-run-draft-ingest-"
	t.Cleanup(func() {
		_, _ = adminDB.Exec(`
			DELETE FROM story_orchestration_run_draft_ingest_editions AS mapping
			USING story_orchestration_run_draft_ingests AS ingest
			JOIN story_orchestration_runs AS run ON run.id = ingest.run_id
			JOIN story_source_versions AS version ON version.id = run.source_version_id
			JOIN stories AS story ON story.id = version.story_id
			WHERE mapping.draft_ingest_id = ingest.id
			  AND story.slug LIKE $1
		`, slugPrefix+"%")
		_, _ = adminDB.Exec(`
			DELETE FROM story_orchestration_run_draft_ingests AS ingest
			USING story_orchestration_runs AS run
			JOIN story_source_versions AS version ON version.id = run.source_version_id
			JOIN stories AS story ON story.id = version.story_id
			WHERE ingest.run_id = run.id
			  AND story.slug LIKE $1
		`, slugPrefix+"%")
		_, _ = adminDB.Exec(`
			DELETE FROM story_orchestration_run_editorial_reviews AS review
			USING story_orchestration_runs AS run
			JOIN story_source_versions AS version ON version.id = run.source_version_id
			JOIN stories AS story ON story.id = version.story_id
			WHERE review.run_id = run.id
			  AND story.slug LIKE $1
		`, slugPrefix+"%")
		_, _ = adminDB.Exec(`
			DELETE FROM story_orchestration_runs
			WHERE source_version_id IN (
				SELECT version.id
				FROM story_source_versions AS version
				JOIN stories AS story ON story.id = version.story_id
				WHERE story.slug LIKE $1
			)
		`, slugPrefix+"%")
		_, _ = adminDB.Exec(`DELETE FROM stories WHERE slug LIKE $1`, slugPrefix+"%")
		_, _ = adminDB.Exec(`DELETE FROM account_memberships WHERE principal_id = $1 AND account_id = $2`, draftIngestIntegrationPrincipalID, draftIngestIntegrationAccountID)
		_, _ = adminDB.Exec(`DELETE FROM principals WHERE id = $1`, draftIngestIntegrationPrincipalID)
		_, _ = adminDB.Exec(`DELETE FROM accounts WHERE id = $1`, draftIngestIntegrationAccountID)
	})

	store := newReaderIntegrationStore(t, databaseURL)
	if _, err := adminDB.Exec(`INSERT INTO accounts (id, name) VALUES ($1, 'Draft ingest integration account')`, draftIngestIntegrationAccountID); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := adminDB.Exec(`INSERT INTO principals (id, display_name) VALUES ($1, 'Draft ingest integration principal')`, draftIngestIntegrationPrincipalID); err != nil {
		t.Fatalf("insert principal: %v", err)
	}
	if _, err := adminDB.Exec(`INSERT INTO account_memberships (principal_id, account_id, role) VALUES ($1, $2, 'owner')`, draftIngestIntegrationPrincipalID, draftIngestIntegrationAccountID); err != nil {
		t.Fatalf("insert membership: %v", err)
	}
	editorial, err := storyeditorialreview.New(storyeditorialreview.Config{ValidatedRunReader: store, Writer: store, Reader: store})
	if err != nil {
		t.Fatal(err)
	}

	createFixture := func(t *testing.T, slug string, semantic adaptationcontract.Result, withClassic bool) (model.AdminSourceUpsertResponse, string) {
		t.Helper()
		sourceText := "# The Draft Lantern\n\nA traveller follows a lantern home.\n"
		source, err := store.AdminSourceUpsert(slug, model.AdminSourceUpsertRequest{
			Title: "The Draft Lantern", Language: stringPointer("en-GB"), SourceText: sourceText,
		})
		if err != nil {
			t.Fatalf("create source: %v", err)
		}
		if withClassic {
			classic := model.AdminStoryEditionClassic
			if _, err := store.AdminDraftUpsert(model.AdminDraftUpsertRequest{
				Slug: slug, EditionKey: &classic, Title: "The Draft Lantern", Markdown: "# The Draft Lantern\n\nClassic working text.\n",
			}); err != nil {
				t.Fatalf("create Classic draft: %v", err)
			}
		}
		persisted, err := store.PersistCompletedStoryOrchestrationRun(source.VersionID, testCompletedOrchestrationResult(t, source.VersionID, sourceText, semantic))
		if err != nil {
			t.Fatalf("persist %q run: %v", semantic, err)
		}
		return source, persisted.ID
	}
	createReview := func(t *testing.T, runID string, decision model.AdminStoryOrchestrationEditorialDecision) model.AdminStoryOrchestrationEditorialReview {
		t.Helper()
		review, err := editorial.Create(model.AdminStoryOrchestrationEditorialReviewCreateInput{
			RunID: runID, Decision: decision,
			ReviewerPrincipalID: draftIngestIntegrationPrincipalID,
			ReviewerAccountID:   draftIngestIntegrationAccountID,
		})
		if err != nil {
			t.Fatalf("create %q review: %v", decision, err)
		}
		return review
	}
	createInput := func(runID, reviewID string) model.AdminStoryOrchestrationDraftIngestInput {
		return model.AdminStoryOrchestrationDraftIngestInput{RunID: runID, EditorialReviewID: reviewID}
	}

	t.Run("copies exact current approval without publication and reuses after later rejection", func(t *testing.T) {
		source, runID := createFixture(t, slugPrefix+"happy", adaptationcontract.ResultPass, true)
		var storyID, classicVersionID string
		if err := adminDB.QueryRow(`SELECT id FROM stories WHERE slug = $1`, slugPrefix+"happy").Scan(&storyID); err != nil {
			t.Fatal(err)
		}
		if err := adminDB.QueryRow(`SELECT draft_version_id FROM story_editions WHERE story_id = $1 AND edition_key = 'classic'`, storyID).Scan(&classicVersionID); err != nil {
			t.Fatal(err)
		}
		approval := createReview(t, runID, model.AdminStoryOrchestrationEditorialDecisionApproved)
		created, err := store.CreateStoryOrchestrationDraftIngest(createInput(runID, approval.ID))
		if err != nil {
			t.Fatalf("create approved ingest: %v", err)
		}
		if created.Outcome != model.AdminStoryOrchestrationDraftIngestOutcomeCreated || created.RunID != runID || created.EditorialReviewID != approval.ID || created.StorySlug != slugPrefix+"happy" || len(created.Editions) != 4 {
			t.Fatalf("created ingest = %#v", created)
		}
		keys := storygeneration.DerivedEditionKeysV2()
		for index, key := range keys {
			item := created.Editions[index]
			if item.EditionKey != key || item.EditionID == "" || item.StoryVersionID == "" {
				t.Fatalf("created edition %d = %#v", index, item)
			}
			var pointer string
			if err := adminDB.QueryRow(`SELECT draft_version_id FROM story_editions WHERE id = $1`, item.EditionID).Scan(&pointer); err != nil || pointer != item.StoryVersionID {
				t.Fatalf("edition %q pointer = %q / %v, want %q", key, pointer, err, item.StoryVersionID)
			}
		}
		var copiedCount, mappingCount, releaseCount, releaseEditionCount int
		var currentRelease sql.NullString
		if err := adminDB.QueryRow(`SELECT count(*) FROM story_versions WHERE story_id = $1`, storyID).Scan(&copiedCount); err != nil {
			t.Fatal(err)
		}
		if err := adminDB.QueryRow(`SELECT count(*) FROM story_orchestration_run_draft_ingest_editions WHERE draft_ingest_id = $1`, created.ID).Scan(&mappingCount); err != nil {
			t.Fatal(err)
		}
		if err := adminDB.QueryRow(`SELECT count(*) FROM story_releases WHERE story_id = $1`, storyID).Scan(&releaseCount); err != nil {
			t.Fatal(err)
		}
		if err := adminDB.QueryRow(`SELECT count(*) FROM story_release_editions WHERE story_id = $1`, storyID).Scan(&releaseEditionCount); err != nil {
			t.Fatal(err)
		}
		if err := adminDB.QueryRow(`SELECT current_release_id FROM stories WHERE id = $1`, storyID).Scan(&currentRelease); err != nil {
			t.Fatal(err)
		}
		if copiedCount != 5 || mappingCount != 4 || releaseCount != 0 || releaseEditionCount != 0 || currentRelease.Valid {
			t.Fatalf("versions/mappings/releases/release-editions/current-release = %d/%d/%d/%d/%v", copiedCount, mappingCount, releaseCount, releaseEditionCount, currentRelease)
		}
		var classicAfter string
		if err := adminDB.QueryRow(`SELECT draft_version_id FROM story_editions WHERE story_id = $1 AND edition_key = 'classic'`, storyID).Scan(&classicAfter); err != nil || classicAfter != classicVersionID {
			t.Fatalf("Classic pointer = %q / %v, want %q", classicAfter, err, classicVersionID)
		}
		if source.VersionID == "" {
			t.Fatal("fixture source version was unexpectedly empty")
		}

		createReview(t, runID, model.AdminStoryOrchestrationEditorialDecisionRejected)
		reused, err := store.CreateStoryOrchestrationDraftIngest(createInput(runID, approval.ID))
		if err != nil || reused.Outcome != model.AdminStoryOrchestrationDraftIngestOutcomeReused || reused.ID != created.ID {
			t.Fatalf("reused after later rejection = %#v / %v", reused, err)
		}
		var afterRetry int
		if err := adminDB.QueryRow(`SELECT count(*) FROM story_versions WHERE story_id = $1`, storyID).Scan(&afterRetry); err != nil || afterRetry != copiedCount {
			t.Fatalf("version count after retry = %d / %v, want %d", afterRetry, err, copiedCount)
		}
	})

	t.Run("reuses immutable ingest provenance after a normal later draft edit", func(t *testing.T) {
		slug := slugPrefix + "edited-after-ingest"
		_, runID := createFixture(t, slug, adaptationcontract.ResultPass, false)
		approval := createReview(t, runID, model.AdminStoryOrchestrationEditorialDecisionApproved)
		created, err := store.CreateStoryOrchestrationDraftIngest(createInput(runID, approval.ID))
		if err != nil {
			t.Fatalf("create approved ingest: %v", err)
		}
		if len(created.Editions) != 4 {
			t.Fatalf("created ingest editions = %#v, want four canonical mappings", created.Editions)
		}
		initialVersionByKey := make(map[model.AdminStoryEditionKey]string, len(created.Editions))
		for _, item := range created.Editions {
			initialVersionByKey[item.EditionKey] = item.StoryVersionID
		}

		key := model.AdminStoryEditionGrowingReaders
		initialV1 := initialVersionByKey[key]
		if initialV1 == "" {
			t.Fatalf("initial ingest did not contain %q: %#v", key, created.Editions)
		}
		edited, err := store.AdminDraftUpsert(model.AdminDraftUpsertRequest{
			Slug:       slug,
			EditionKey: &key,
			Title:      "The Draft Lantern",
			Markdown:   "# The Draft Lantern\n\nA human editor revises this imported growing-reader draft.\n",
		})
		if err != nil {
			t.Fatalf("normal later draft edit: %v", err)
		}
		if edited.Outcome != model.AdminDraftOutcomeCreatedVersion || edited.StoryVersionID == "" || edited.StoryVersionID == initialV1 {
			t.Fatalf("normal later draft edit = %#v, want new version after %q", edited, initialV1)
		}
		v2 := edited.StoryVersionID
		var pointerAfterEdit string
		if err := adminDB.QueryRow(`
			SELECT edition.draft_version_id
			FROM story_editions AS edition
			JOIN stories AS story ON story.id = edition.story_id
			WHERE story.slug = $1
			  AND edition.edition_key = $2
		`, slug, key).Scan(&pointerAfterEdit); err != nil || pointerAfterEdit != v2 {
			t.Fatalf("draft pointer after normal edit = %q / %v, want V2 %q", pointerAfterEdit, err, v2)
		}

		reused, err := store.CreateStoryOrchestrationDraftIngest(createInput(runID, approval.ID))
		if err != nil || reused.Outcome != model.AdminStoryOrchestrationDraftIngestOutcomeReused || reused.ID != created.ID {
			t.Fatalf("reuse after normal edit = %#v / %v", reused, err)
		}
		for _, item := range reused.Editions {
			if item.StoryVersionID != initialVersionByKey[item.EditionKey] {
				t.Fatalf("reused provenance for %q = %q, want original V1 %q", item.EditionKey, item.StoryVersionID, initialVersionByKey[item.EditionKey])
			}
		}
		var (
			pointerAfterReuse string
			ingestCount       int
			mappingCount      int
			versionCount      int
		)
		if err := adminDB.QueryRow(`
			SELECT edition.draft_version_id
			FROM story_editions AS edition
			JOIN stories AS story ON story.id = edition.story_id
			WHERE story.slug = $1
			  AND edition.edition_key = $2
		`, slug, key).Scan(&pointerAfterReuse); err != nil {
			t.Fatal(err)
		}
		if err := adminDB.QueryRow(`SELECT count(*) FROM story_orchestration_run_draft_ingests WHERE id = $1`, created.ID).Scan(&ingestCount); err != nil {
			t.Fatal(err)
		}
		if err := adminDB.QueryRow(`SELECT count(*) FROM story_orchestration_run_draft_ingest_editions WHERE draft_ingest_id = $1`, created.ID).Scan(&mappingCount); err != nil {
			t.Fatal(err)
		}
		if err := adminDB.QueryRow(`SELECT count(*) FROM story_versions WHERE story_id = (SELECT id FROM stories WHERE slug = $1)`, slug).Scan(&versionCount); err != nil {
			t.Fatal(err)
		}
		if pointerAfterReuse != v2 || ingestCount != 1 || mappingCount != 4 || versionCount != 5 {
			t.Fatalf("reuse after normal edit changed mutable/provenance state: pointer:%q ingests:%d mappings:%d versions:%d", pointerAfterReuse, ingestCount, mappingCount, versionCount)
		}
	})

	t.Run("requires exact current approved event and allows every machine outcome", func(t *testing.T) {
		for _, semantic := range []adaptationcontract.Result{adaptationcontract.ResultPass, adaptationcontract.ResultNeedsReview, adaptationcontract.ResultFail} {
			slug := slugPrefix + "machine-" + strings.ReplaceAll(string(semantic), "_", "-")
			_, runID := createFixture(t, slug, semantic, false)
			approvalA := createReview(t, runID, model.AdminStoryOrchestrationEditorialDecisionApproved)
			createReview(t, runID, model.AdminStoryOrchestrationEditorialDecisionRejected)
			if _, err := store.CreateStoryOrchestrationDraftIngest(createInput(runID, approvalA.ID)); !errors.Is(err, model.ErrAdminStoryOrchestrationDraftIngestConflict) {
				t.Fatalf("superseded approval for %q = %v", semantic, err)
			}
			approvalC := createReview(t, runID, model.AdminStoryOrchestrationEditorialDecisionApproved)
			if _, err := store.CreateStoryOrchestrationDraftIngest(createInput(runID, approvalA.ID)); !errors.Is(err, model.ErrAdminStoryOrchestrationDraftIngestConflict) {
				t.Fatalf("older approval after reversal for %q = %v", semantic, err)
			}
			out, err := store.CreateStoryOrchestrationDraftIngest(createInput(runID, approvalC.ID))
			if err != nil || out.Outcome != model.AdminStoryOrchestrationDraftIngestOutcomeCreated {
				t.Fatalf("current approval for machine %q = %#v / %v", semantic, out, err)
			}
		}
	})

	t.Run("rejects mismatched reviews and occupied modern drafts without partial writes", func(t *testing.T) {
		_, firstRunID := createFixture(t, slugPrefix+"mismatch-a", adaptationcontract.ResultPass, false)
		_, secondRunID := createFixture(t, slugPrefix+"mismatch-b", adaptationcontract.ResultPass, false)
		approval := createReview(t, firstRunID, model.AdminStoryOrchestrationEditorialDecisionApproved)
		if _, err := store.CreateStoryOrchestrationDraftIngest(createInput(secondRunID, approval.ID)); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("mismatched review error = %v", err)
		}

		slug := slugPrefix + "occupied"
		_, runID := createFixture(t, slug, adaptationcontract.ResultPass, false)
		key := model.AdminStoryEditionGrowingReaders
		if _, err := store.AdminDraftUpsert(model.AdminDraftUpsertRequest{Slug: slug, EditionKey: &key, Title: "The Draft Lantern", Markdown: "# The Draft Lantern\n\nManual growing draft.\n"}); err != nil {
			t.Fatalf("create occupied draft: %v", err)
		}
		approval = createReview(t, runID, model.AdminStoryOrchestrationEditorialDecisionApproved)
		if _, err := store.CreateStoryOrchestrationDraftIngest(createInput(runID, approval.ID)); !errors.Is(err, model.ErrAdminStoryOrchestrationDraftIngestConflict) {
			t.Fatalf("occupied draft error = %v", err)
		}
		var provenanceCount, versionCount int
		if err := adminDB.QueryRow(`SELECT count(*) FROM story_orchestration_run_draft_ingests WHERE run_id = $1`, runID).Scan(&provenanceCount); err != nil {
			t.Fatal(err)
		}
		if err := adminDB.QueryRow(`SELECT count(*) FROM story_versions WHERE story_id = (SELECT id FROM stories WHERE slug = $1)`, slug).Scan(&versionCount); err != nil {
			t.Fatal(err)
		}
		if provenanceCount != 0 || versionCount != 1 {
			t.Fatalf("occupied draft partial state = provenance:%d versions:%d", provenanceCount, versionCount)
		}
	})

	t.Run("fails closed when retained orchestration evidence is corrupt", func(t *testing.T) {
		_, runID := createFixture(t, slugPrefix+"corrupt", adaptationcontract.ResultPass, false)
		approval := createReview(t, runID, model.AdminStoryOrchestrationEditorialDecisionApproved)
		if _, err := adminDB.Exec(`
			UPDATE story_orchestration_runs
			SET artifacts = '{"analysisArtifact":{},"editions":[{},{},{},{}],"editionAssessments":[{},{},{},{}],"bundleAssessment":{}}'::jsonb
			WHERE id = $1
		`, runID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.CreateStoryOrchestrationDraftIngest(createInput(runID, approval.ID)); !errors.Is(err, model.ErrAdminStoryOrchestrationRunRepairRequired) {
			t.Fatalf("corrupt run ingest error = %v", err)
		}
		var ingestCount, versionCount int
		if err := adminDB.QueryRow(`SELECT count(*) FROM story_orchestration_run_draft_ingests WHERE run_id = $1`, runID).Scan(&ingestCount); err != nil {
			t.Fatal(err)
		}
		if err := adminDB.QueryRow(`SELECT count(*) FROM story_versions WHERE story_id = (SELECT version.story_id FROM story_orchestration_runs AS run JOIN story_source_versions AS version ON version.id = run.source_version_id WHERE run.id = $1)`, runID).Scan(&versionCount); err != nil {
			t.Fatal(err)
		}
		if ingestCount != 0 || versionCount != 0 {
			t.Fatalf("corrupt run persisted draft state = ingests:%d versions:%d", ingestCount, versionCount)
		}
	})

	t.Run("fails closed when the retained source binding is invalid", func(t *testing.T) {
		source, runID := createFixture(t, slugPrefix+"source-binding", adaptationcontract.ResultPass, false)
		approval := createReview(t, runID, model.AdminStoryOrchestrationEditorialDecisionApproved)
		if _, err := adminDB.Exec(`UPDATE story_source_versions SET source_text = 'tampered canonical source' WHERE id = $1`, source.VersionID); err != nil {
			t.Fatalf("tamper source binding: %v", err)
		}
		if _, err := store.CreateStoryOrchestrationDraftIngest(createInput(runID, approval.ID)); !errors.Is(err, model.ErrAdminStoryOrchestrationRunRepairRequired) {
			t.Fatalf("source-binding-invalid ingest error = %v", err)
		}
		var ingestCount, versionCount int
		if err := adminDB.QueryRow(`SELECT count(*) FROM story_orchestration_run_draft_ingests WHERE run_id = $1`, runID).Scan(&ingestCount); err != nil {
			t.Fatal(err)
		}
		if err := adminDB.QueryRow(`SELECT count(*) FROM story_versions WHERE story_id = (SELECT story_id FROM story_source_versions WHERE id = $1)`, source.VersionID).Scan(&versionCount); err != nil {
			t.Fatal(err)
		}
		if ingestCount != 0 || versionCount != 0 {
			t.Fatalf("source-binding-invalid persisted draft state = ingests:%d versions:%d", ingestCount, versionCount)
		}
	})

	t.Run("does not reuse incomplete retained ingest provenance", func(t *testing.T) {
		_, runID := createFixture(t, slugPrefix+"incomplete", adaptationcontract.ResultPass, false)
		approval := createReview(t, runID, model.AdminStoryOrchestrationEditorialDecisionApproved)
		created, err := store.CreateStoryOrchestrationDraftIngest(createInput(runID, approval.ID))
		if err != nil {
			t.Fatalf("create ingest: %v", err)
		}
		if _, err := adminDB.Exec(`
			DELETE FROM story_orchestration_run_draft_ingest_editions
			WHERE draft_ingest_id = $1
			  AND edition_id = (
				SELECT edition_id
				FROM story_orchestration_run_draft_ingest_editions
				WHERE draft_ingest_id = $1
				ORDER BY edition_id
				LIMIT 1
			  )
		`, created.ID); err != nil {
			t.Fatalf("tamper retained ingest provenance: %v", err)
		}
		if _, err := store.CreateStoryOrchestrationDraftIngest(createInput(runID, approval.ID)); !errors.Is(err, model.ErrAdminStoryOrchestrationDraftIngestConflict) {
			t.Fatalf("incomplete retained ingest reuse error = %v", err)
		}
		var versionCount int
		if err := adminDB.QueryRow(`SELECT count(*) FROM story_versions WHERE story_id = (SELECT story_id FROM story_source_versions WHERE id = (SELECT source_version_id FROM story_orchestration_runs WHERE id = $1))`, runID).Scan(&versionCount); err != nil {
			t.Fatal(err)
		}
		if versionCount != 4 {
			t.Fatalf("incomplete retained ingest created extra versions: %d", versionCount)
		}
	})

	t.Run("serializes concurrent retries into one created and one reused ingest", func(t *testing.T) {
		_, runID := createFixture(t, slugPrefix+"concurrent", adaptationcontract.ResultPass, false)
		approval := createReview(t, runID, model.AdminStoryOrchestrationEditorialDecisionApproved)
		input := createInput(runID, approval.ID)
		outcomes := make(chan model.AdminStoryOrchestrationDraftIngestResponse, 2)
		errorsByCaller := make(chan error, 2)
		var wait sync.WaitGroup
		for range 2 {
			wait.Add(1)
			go func() {
				defer wait.Done()
				out, err := store.CreateStoryOrchestrationDraftIngest(input)
				outcomes <- out
				errorsByCaller <- err
			}()
		}
		wait.Wait()
		close(outcomes)
		close(errorsByCaller)
		for err := range errorsByCaller {
			if err != nil {
				t.Fatalf("concurrent ingest: %v", err)
			}
		}
		outcomeValues := make([]string, 0, 2)
		for out := range outcomes {
			outcomeValues = append(outcomeValues, string(out.Outcome))
		}
		sort.Strings(outcomeValues)
		if strings.Join(outcomeValues, ",") != "created,reused" {
			t.Fatalf("concurrent outcomes = %v", outcomeValues)
		}
		var ingestCount, mappingCount, versionCount int
		if err := adminDB.QueryRow(`SELECT count(*) FROM story_orchestration_run_draft_ingests WHERE run_id = $1`, runID).Scan(&ingestCount); err != nil {
			t.Fatal(err)
		}
		if err := adminDB.QueryRow(`SELECT count(*) FROM story_orchestration_run_draft_ingest_editions AS mapping JOIN story_orchestration_run_draft_ingests AS ingest ON ingest.id = mapping.draft_ingest_id WHERE ingest.run_id = $1`, runID).Scan(&mappingCount); err != nil {
			t.Fatal(err)
		}
		if err := adminDB.QueryRow(`SELECT count(*) FROM story_versions WHERE story_id = (SELECT version.story_id FROM story_orchestration_runs AS run JOIN story_source_versions AS version ON version.id = run.source_version_id WHERE run.id = $1)`, runID).Scan(&versionCount); err != nil {
			t.Fatal(err)
		}
		if ingestCount != 1 || mappingCount != 4 || versionCount != 4 {
			t.Fatalf("concurrent persisted state = ingests:%d mappings:%d versions:%d", ingestCount, mappingCount, versionCount)
		}
	})

	t.Run("run lock serializes approval currentness with a concurrent rejection", func(t *testing.T) {
		_, runID := createFixture(t, slugPrefix+"lock", adaptationcontract.ResultPass, false)
		approval := createReview(t, runID, model.AdminStoryOrchestrationEditorialDecisionApproved)
		var storyID string
		if err := adminDB.QueryRow(`SELECT version.story_id FROM story_orchestration_runs AS run JOIN story_source_versions AS version ON version.id = run.source_version_id WHERE run.id = $1`, runID).Scan(&storyID); err != nil {
			t.Fatal(err)
		}
		blocker, err := adminDB.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = blocker.Rollback() }()
		if _, err := blocker.Exec(`SELECT id FROM stories WHERE id = $1 FOR UPDATE`, storyID); err != nil {
			t.Fatal(err)
		}

		ingestDone := make(chan error, 1)
		go func() {
			_, err := store.CreateStoryOrchestrationDraftIngest(createInput(runID, approval.ID))
			ingestDone <- err
		}()

		deadline := time.After(2 * time.Second)
		for {
			probe, err := adminDB.Begin()
			if err != nil {
				t.Fatal(err)
			}
			_, err = probe.Exec(`SELECT id FROM story_orchestration_runs WHERE id = $1 FOR KEY SHARE NOWAIT`, runID)
			_ = probe.Rollback()
			if err != nil {
				break
			}
			select {
			case err := <-ingestDone:
				t.Fatalf("ingest completed before acquiring serialized run lock: %v", err)
			case <-deadline:
				t.Fatal("ingest did not acquire the run lock")
			default:
				time.Sleep(10 * time.Millisecond)
			}
		}

		reviewDB, err := sql.Open("pgx", databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = reviewDB.Close() }()
		reviewConn, err := reviewDB.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = reviewConn.Close() }()
		var reviewPID int
		if err := reviewConn.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&reviewPID); err != nil {
			t.Fatal(err)
		}

		reviewContext, cancelReview := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelReview()
		reviewStarted := make(chan struct{})
		rejectionDone := make(chan error, 1)
		go func() {
			// Use the exact PR109 INSERT shape on a dedicated connection so the
			// observer can prove its real FK check is blocked by the ingest lock.
			close(reviewStarted)
			_, err := reviewConn.ExecContext(reviewContext, `
				INSERT INTO story_orchestration_run_editorial_reviews (
					run_id,
					decision,
					reviewer_principal_id,
					reviewer_account_id
				)
				VALUES ($1, $2, $3, $4)
			`, runID, model.AdminStoryOrchestrationEditorialDecisionRejected, draftIngestIntegrationPrincipalID, draftIngestIntegrationAccountID)
			rejectionDone <- err
		}()
		<-reviewStarted

		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		blocked := false
		for !blocked {
			if err := adminDB.QueryRowContext(reviewContext, `
				SELECT EXISTS (
					SELECT 1
					FROM pg_stat_activity AS waiting_review
					WHERE waiting_review.pid = $1
					  AND waiting_review.wait_event_type = 'Lock'
					  AND EXISTS (
						SELECT 1
						FROM unnest(pg_blocking_pids(waiting_review.pid)) AS blocker(pid)
						JOIN pg_locks AS held_lock ON held_lock.pid = blocker.pid
						WHERE held_lock.granted
						  AND held_lock.relation = 'story_orchestration_runs'::regclass
					  )
				)
			`, reviewPID).Scan(&blocked); err != nil {
				t.Fatal(err)
			}
			if blocked {
				break
			}
			select {
			case err := <-rejectionDone:
				t.Fatalf("review INSERT completed before observable FK lock wait: %v", err)
			case <-reviewContext.Done():
				t.Fatalf("review INSERT did not become observably blocked on the ingest run lock: %v", reviewContext.Err())
			case <-ticker.C:
			}
		}
		select {
		case err := <-rejectionDone:
			t.Fatalf("review INSERT completed while PostgreSQL reported it blocked: %v", err)
		default:
		}
		if err := blocker.Commit(); err != nil {
			t.Fatal(err)
		}
		if err := <-ingestDone; err != nil {
			t.Fatalf("serialized ingest failed: %v", err)
		}
		if err := <-rejectionDone; err != nil {
			t.Fatalf("rejection did not complete after ingest commit: %v", err)
		}
		var latestDecision model.AdminStoryOrchestrationEditorialDecision
		if err := adminDB.QueryRow(`
			SELECT decision
			FROM story_orchestration_run_editorial_reviews
			WHERE run_id = $1
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		`, runID).Scan(&latestDecision); err != nil || latestDecision != model.AdminStoryOrchestrationEditorialDecisionRejected {
			t.Fatalf("latest review after serialized insert = %q / %v", latestDecision, err)
		}
	})
}
