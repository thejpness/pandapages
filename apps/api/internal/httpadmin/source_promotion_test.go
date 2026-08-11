package httpadmin

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pandapages/api/internal/model"
)

func TestSourceAcquisitionPromotionRouteIsProtectedAndStrict(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	store := ownerStore()
	store.promoteSourceAcquisitionResponse = model.AdminSourceAcquisitionPromotionResponse{
		Outcome:   model.AdminSourceAcquisitionPromotionCreated,
		Promotion: model.AdminSourceAcquisitionPromotion{StoryID: "22222222-2222-4222-8222-222222222222", StorySlug: "alice", StoryTitle: "Alice", SourceVersionID: "33333333-3333-4333-8333-333333333333", SourceVersion: 1},
	}
	handler := sourceProviderHandler(t, store, eligibilityDiscovery())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, providerRequest(http.MethodPost, "/api/v1/admin/source-acquisitions/"+id+"/promote", `{"target":{"mode":"new_story","title":"Alice","slug":"alice"}}`))
	if response.Code != http.StatusCreated || response.Header().Get("Cache-Control") != "no-store" || store.promoteSourceAcquisitionCalls != 1 || store.promoteSourceAcquisitionID != id || store.promoteSourceAcquisitionRequest.Target.Mode != model.AdminSourceAcquisitionPromotionTargetNewStory {
		t.Fatalf("promotion=%d/%s/store=%+v", response.Code, response.Body.String(), store)
	}

	existing := httptest.NewRecorder()
	handler.ServeHTTP(existing, providerRequest(http.MethodPost, "/api/v1/admin/source-acquisitions/"+id+"/promote", `{"target":{"mode":"existing_story","storySlug":"alice"}}`))
	if existing.Code != http.StatusCreated || store.promoteSourceAcquisitionCalls != 2 || store.promoteSourceAcquisitionRequest.Target.Mode != model.AdminSourceAcquisitionPromotionTargetExistingStory {
		t.Fatalf("existing promotion=%d/%s/store=%+v", existing.Code, existing.Body.String(), store)
	}

	bad := httptest.NewRecorder()
	handler.ServeHTTP(bad, providerRequest(http.MethodPost, "/api/v1/admin/source-acquisitions/"+id+"/promote", `{"target":{"mode":"new_story","title":"Alice","slug":"alice"},"sourceText":"forged"}`))
	if bad.Code != http.StatusBadRequest || store.promoteSourceAcquisitionCalls != 2 {
		t.Fatalf("authority body=%d/%s calls=%d", bad.Code, bad.Body.String(), store.promoteSourceAcquisitionCalls)
	}
	oversized := httptest.NewRecorder()
	oversizedBody := `{"target":{"mode":"new_story","title":"` + strings.Repeat("a", maxSourcePromotionBody) + `","slug":"alice"}}`
	handler.ServeHTTP(oversized, providerRequest(http.MethodPost, "/api/v1/admin/source-acquisitions/"+id+"/promote", oversizedBody))
	if oversized.Code != http.StatusRequestEntityTooLarge || store.promoteSourceAcquisitionCalls != 2 {
		t.Fatalf("oversized=%d/%s calls=%d", oversized.Code, oversized.Body.String(), store.promoteSourceAcquisitionCalls)
	}
}

func TestSourceAcquisitionPromotionMapsFiniteSafeFailures(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	for name, err := range map[string]error{
		"not-ready":        model.ErrAdminSourceAcquisitionNotReady,
		"different-target": model.ErrAdminSourceAcquisitionAlreadyPromoted,
		"target":           model.ErrAdminSourceAcquisitionPromotionTarget,
		"corruption":       errors.New("sql: private source text"),
	} {
		t.Run(name, func(t *testing.T) {
			store := ownerStore()
			store.promoteSourceAcquisitionErr = err
			response := httptest.NewRecorder()
			sourceProviderHandler(t, store, eligibilityDiscovery()).ServeHTTP(response, providerRequest(http.MethodPost, "/api/v1/admin/source-acquisitions/"+id+"/promote", `{"target":{"mode":"existing_story","storySlug":"alice"}}`))
			if response.Code < 400 || strings.Contains(response.Body.String(), "private source text") {
				t.Fatalf("error=%d/%s", response.Code, response.Body.String())
			}
		})
	}
}
