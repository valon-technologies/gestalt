package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

const (
	boundaryCanonicalUserID = "1f8f6b0a-5f52-4f4a-8a3e-3f6f4b2c9d10"
	boundaryUserEmail       = "dave@valon.com"
)

// subjectScopedAuthzStub only grants the canonical subject, so any boundary
// that authorizes the raw token subject is denied.
type subjectScopedAuthzStub struct {
	core.AuthorizationProvider
	grantedSubjectID string
	relation         string
}

func (s subjectScopedAuthzStub) CheckAccess(
	_ context.Context,
	req *proto.CheckAccessRequest,
) (*proto.CheckAccessResponse, error) {
	if req.GetSubject().GetId() != s.grantedSubjectID {
		return &proto.CheckAccessResponse{}, nil
	}
	return &proto.CheckAccessResponse{Allowed: true, MatchedRelations: []string{s.relation}}, nil
}

func (subjectScopedAuthzStub) ListActiveModelResourceTypes(
	context.Context,
	*proto.ListActiveModelResourceTypesRequest,
) (*proto.ListActiveModelResourceTypesResponse, error) {
	return &proto.ListActiveModelResourceTypesResponse{}, nil
}

type boundaryUserStore struct {
	usersByEmail map[string]string
	err          error
}

func (b boundaryUserStore) FindOrCreateUser(_ context.Context, email string) (*core.User, error) {
	if b.err != nil {
		return nil, b.err
	}
	id, ok := b.usersByEmail[email]
	if !ok {
		return nil, nil
	}
	return &core.User{ID: id, Email: email}, nil
}

func (b boundaryUserStore) GetUser(_ context.Context, id string) (*core.User, error) {
	if b.err != nil {
		return nil, b.err
	}
	for email, userID := range b.usersByEmail {
		if userID == id {
			return &core.User{ID: id, Email: email}, nil
		}
	}
	return nil, core.ErrNotFound
}

func emailPrincipal() *principal.Principal {
	return &principal.Principal{
		SubjectID: principal.UserSubjectID(boundaryUserEmail),
		Identity:  &core.UserIdentity{Email: boundaryUserEmail},
		Kind:      principal.KindUser,
		Source:    principal.SourceBearer,
	}
}

func opaquePrincipal() *principal.Principal {
	return &principal.Principal{
		SubjectID: principal.UserSubjectID("auth0|abc123"),
		Kind:      principal.KindUser,
		Source:    principal.SourceBearer,
	}
}

func mountedUIFixture() MountedUI {
	return MountedUI{
		Name:                "app:g-issues",
		AppName:             "g-issues",
		AppLevelAuth:        true,
		AuthorizationPolicy: "g-issues",
		AllowedRoles:        []string{"viewer"},
	}
}

func TestMountedUIBoundaryResolvesEmailSubjectToCanonicalUser(t *testing.T) {
	t.Parallel()

	s := &Server{
		authorization: subjectScopedAuthzStub{
			grantedSubjectID: principal.UserSubjectID(boundaryCanonicalUserID),
			relation:         "viewer",
		},
		users: boundaryUserStore{usersByEmail: map[string]string{boundaryUserEmail: boundaryCanonicalUserID}},
	}

	access, allowed, err := s.authorizeMountedAppAccess(context.Background(), emailPrincipal(), mountedUIFixture())
	if err != nil {
		t.Fatalf("authorizeMountedAppAccess: %v", err)
	}
	if !allowed {
		t.Fatal("email-form subject was denied; it must resolve to the canonical user")
	}
	if access.Role != "viewer" {
		t.Fatalf("access.Role = %q, want viewer", access.Role)
	}
}

func TestMountedUIBoundaryAllowsCanonicalSubject(t *testing.T) {
	t.Parallel()

	s := &Server{
		authorization: subjectScopedAuthzStub{
			grantedSubjectID: principal.UserSubjectID(boundaryCanonicalUserID),
			relation:         "viewer",
		},
	}
	p := &principal.Principal{
		SubjectID: principal.UserSubjectID(boundaryCanonicalUserID),
		Kind:      principal.KindUser,
		Source:    principal.SourceBearer,
	}

	_, allowed, err := s.authorizeMountedAppAccess(context.Background(), p, mountedUIFixture())
	if err != nil {
		t.Fatalf("authorizeMountedAppAccess: %v", err)
	}
	if !allowed {
		t.Fatal("canonical subject was denied")
	}
}

func TestMountedUIBoundaryDeniesOpaqueSubject(t *testing.T) {
	t.Parallel()

	s := &Server{
		authorization: subjectScopedAuthzStub{
			grantedSubjectID: principal.UserSubjectID("auth0|abc123"),
			relation:         "viewer",
		},
		users: boundaryUserStore{usersByEmail: map[string]string{boundaryUserEmail: boundaryCanonicalUserID}},
	}

	_, allowed, err := s.authorizeMountedAppAccess(context.Background(), opaquePrincipal(), mountedUIFixture())
	if err != nil {
		t.Fatalf("authorizeMountedAppAccess: %v", err)
	}
	if allowed {
		t.Fatal("opaque subject was authorized")
	}
}

func TestMountedUIBoundaryFailsClosedOnUserStoreError(t *testing.T) {
	t.Parallel()

	s := &Server{
		authorization: subjectScopedAuthzStub{
			grantedSubjectID: principal.UserSubjectID(boundaryUserEmail),
			relation:         "viewer",
		},
		users: boundaryUserStore{err: errors.New("user store unavailable")},
	}

	_, allowed, err := s.authorizeMountedAppAccess(context.Background(), emailPrincipal(), mountedUIFixture())
	if err == nil {
		t.Fatal("user store failure did not surface an error")
	}
	if allowed {
		t.Fatal("user store failure fell back to the raw token subject")
	}
}

func TestMountedUIBoundaryFailsClosedWithoutUserStore(t *testing.T) {
	t.Parallel()

	s := &Server{
		authorization: subjectScopedAuthzStub{
			grantedSubjectID: principal.UserSubjectID(boundaryUserEmail),
			relation:         "viewer",
		},
	}

	_, allowed, err := s.authorizeMountedAppAccess(context.Background(), emailPrincipal(), mountedUIFixture())
	if err != nil {
		t.Fatalf("authorizeMountedAppAccess: %v", err)
	}
	if allowed {
		t.Fatal("missing user store fell back to the raw token subject")
	}
}

func appAdminRequest(t *testing.T, p *principal.Principal) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/apps/g-issues/admin/registry", nil)
	req = req.WithContext(principal.WithPrincipal(req.Context(), p))
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("app", "g-issues")
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}

func TestAppAdminBoundaryResolvesEmailSubjectToCanonicalUser(t *testing.T) {
	t.Parallel()

	s := &Server{
		authorization: subjectScopedAuthzStub{
			grantedSubjectID: principal.UserSubjectID(boundaryCanonicalUserID),
			relation:         "admin",
		},
		users: boundaryUserStore{usersByEmail: map[string]string{boundaryUserEmail: boundaryCanonicalUserID}},
	}
	called := false
	handler := s.appAdminAuthorizationMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, appAdminRequest(t, emailPrincipal()))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !called {
		t.Fatal("canonical app admin was denied")
	}
}

func TestAppAdminBoundaryDeniesOpaqueSubject(t *testing.T) {
	t.Parallel()

	s := &Server{
		authorization: subjectScopedAuthzStub{
			grantedSubjectID: principal.UserSubjectID("auth0|abc123"),
			relation:         "admin",
		},
		users: boundaryUserStore{usersByEmail: map[string]string{boundaryUserEmail: boundaryCanonicalUserID}},
	}
	called := false
	handler := s.appAdminAuthorizationMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, appAdminRequest(t, opaquePrincipal()))

	if called {
		t.Fatal("opaque subject reached the app admin handler")
	}
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestAppAdminBoundaryFailsClosedOnUserStoreError(t *testing.T) {
	t.Parallel()

	s := &Server{
		authorization: subjectScopedAuthzStub{
			grantedSubjectID: principal.UserSubjectID(boundaryUserEmail),
			relation:         "admin",
		},
		users: boundaryUserStore{err: errors.New("user store unavailable")},
	}
	called := false
	handler := s.appAdminAuthorizationMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, appAdminRequest(t, emailPrincipal()))

	if called {
		t.Fatal("user store failure fell back to the raw token subject")
	}
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}

// TestResolvePrincipalUserIDCanonicalizesOpaqueSubject covers the enrichment
// path that feeds every authorization boundary. A provider-opaque subject
// contains no "@", so it must not be mistaken for an already-canonical user
// ID and passed through to authorization unresolved.
func TestResolvePrincipalUserIDCanonicalizesOpaqueSubject(t *testing.T) {
	t.Parallel()

	s := &Server{users: boundaryUserStore{
		usersByEmail: map[string]string{boundaryUserEmail: boundaryCanonicalUserID},
	}}

	p := &principal.Principal{
		SubjectID: principal.UserSubjectID("auth0|abc123"),
		Identity:  &core.UserIdentity{Email: boundaryUserEmail},
		Kind:      principal.KindUser,
		Source:    principal.SourceBearer,
	}

	enriched, err := s.resolvePrincipalUserID(context.Background(), p)
	if err != nil {
		t.Fatalf("resolvePrincipalUserID: %v", err)
	}
	if got, want := enriched.SubjectID, principal.UserSubjectID(boundaryCanonicalUserID); got != want {
		t.Fatalf("subject ID = %q, want canonical %q", got, want)
	}
	if got := enriched.UserID; got != boundaryCanonicalUserID {
		t.Fatalf("user ID = %q, want %q", got, boundaryCanonicalUserID)
	}
}

// TestResolvePrincipalUserIDLeavesCanonicalSubjectUnchanged keeps the common
// path free of a user-store round trip.
func TestResolvePrincipalUserIDLeavesCanonicalSubjectUnchanged(t *testing.T) {
	t.Parallel()

	s := &Server{users: boundaryUserStore{err: errors.New("user store must not be called")}}

	p := &principal.Principal{
		SubjectID: principal.UserSubjectID(boundaryCanonicalUserID),
		Identity:  &core.UserIdentity{Email: boundaryUserEmail},
		Kind:      principal.KindUser,
		Source:    principal.SourceBearer,
	}

	enriched, err := s.resolvePrincipalUserID(context.Background(), p)
	if err != nil {
		t.Fatalf("resolvePrincipalUserID: %v", err)
	}
	if got, want := enriched.SubjectID, principal.UserSubjectID(boundaryCanonicalUserID); got != want {
		t.Fatalf("subject ID = %q, want %q", got, want)
	}
}
