package ci_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	ciapi "github.com/pangobit/warpgate/warpd/api/ci"
	tursoconn "github.com/pangobit/warpgate/warpd/connectors/turso"
	"github.com/pangobit/warpgate/warpd/internal/identity"
	"github.com/pangobit/warpgate/warpd/internal/imagewatch"
	"github.com/pangobit/warpgate/warpd/usecase"
)

func adminIdentifier() identity.Identifier {
	return identity.StaticIdentifier{User: identity.User{
		Email:        "ci@warpgate",
		Capabilities: []string{identity.AdminCapability},
	}}
}

func newTestRouter(t *testing.T, refresh func()) (http.Handler, *tursoconn.MemoryStore) {
	t.Helper()
	store := tursoconn.NewMemoryStore()
	service := usecase.NewService(store, nil, nil, nil)
	return ciapi.NewRouter(service, adminIdentifier(), refresh), store
}

func TestRefreshSchedulesCallback(t *testing.T) {
	called := false
	router, _ := newTestRouter(t, func() { called = true })
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/refresh", nil))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", recorder.Code)
	}
	if !called {
		t.Fatal("refresh callback was not invoked")
	}
}

func TestStatusReportsPendingUpdates(t *testing.T) {
	router, store := newTestRouter(t, func() {})
	ctx := context.Background()
	if err := store.SaveImageCursor(ctx, imagewatch.Cursor{
		App:          "api",
		Service:      "api",
		Tag:          "1.2.0",
		CandidateTag: "1.2.5",
		Status:       imagewatch.StatusUpdateAvailable,
	}); err != nil {
		t.Fatalf("SaveImageCursor() error = %v", err)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/status", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var response ciapi.StatusResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.PendingUpdates) != 1 {
		t.Fatalf("pending updates = %+v, want 1", response.PendingUpdates)
	}
	update := response.PendingUpdates[0]
	if update.App != "api" || update.PinnedTag != "1.2.0" || update.CandidateTag != "1.2.5" {
		t.Fatalf("update = %+v", update)
	}
}

func TestRoutesRejectUnidentifiedCallers(t *testing.T) {
	store := tursoconn.NewMemoryStore()
	service := usecase.NewService(store, nil, nil, nil)
	router := ciapi.NewRouter(service, identity.StaticIdentifier{}, func() {})
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/refresh", nil),
		httptest.NewRequest(http.MethodGet, "/status", nil),
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status = %d, want 401", request.Method, request.URL.Path, recorder.Code)
		}
	}
}
