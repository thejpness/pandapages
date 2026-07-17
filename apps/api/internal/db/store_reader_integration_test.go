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
		if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), `"schema":2`) {
			t.Fatalf("progress GET = %d %s", getResponse.Code, getResponse.Body.String())
		}

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
		mismatch := locator
		mismatch.Segment.Key = strings.Repeat("e", 64)
		response := serveReaderRequest(t, handler, cookie, http.MethodPut, "/api/v1/progress/"+readerSlug, progressBody(t, story.Version, mismatch, 0.9))
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"locator_mismatch"`) {
			t.Fatalf("locator mismatch = %d %s", response.Code, response.Body.String())
		}

		if _, err := adminDB.Exec(`ALTER TABLE reading_progress RENAME TO reading_progress_unavailable`); err != nil {
			t.Fatalf("make progress storage unavailable: %v", err)
		}
		restored := false
		defer func() {
			if !restored {
				_, _ = adminDB.Exec(`ALTER TABLE reading_progress_unavailable RENAME TO reading_progress`)
			}
		}()
		failureResponse := serveReaderRequest(t, handler, cookie, http.MethodPut, "/api/v1/progress/"+readerSlug, validBody)
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
