package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

type appAdminAuthzStub struct {
	core.AuthorizationProvider
}

func (appAdminAuthzStub) ListRelationships(context.Context, *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	return &proto.ListRelationshipsResponse{}, nil
}

func TestAppAdminAuthorizationMiddlewareRejectsNilPrincipal(t *testing.T) {
	t.Parallel()

	s := &Server{authorization: appAdminAuthzStub{}}
	called := false
	handler := s.appAdminAuthorizationMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodGet, "/apps/test-app/admin/registry", nil)
	req = req.WithContext(principal.WithPrincipal(req.Context(), nil))
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("app", "test-app")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if called {
		t.Fatal("next handler was called with nil principal")
	}
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}
