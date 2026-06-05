package access

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

type recordingAuthorizationProvider struct {
	allowed  bool
	err      error
	requests []*proto.CheckAccessRequest
	core.AuthorizationProvider
}

func (p *recordingAuthorizationProvider) CheckAccess(ctx context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	p.requests = append(p.requests, req)
	if p.err != nil {
		return nil, p.err
	}
	return &proto.CheckAccessResponse{Allowed: p.allowed}, nil
}

func TestRequireAppRequestComposesScopeAndPolicy(t *testing.T) {
	t.Parallel()

	authz := &recordingAuthorizationProvider{allowed: true}
	enforcer := NewEnforcer(authz)
	p := scopedPrincipal("subject:user:123", "example.read")

	if err := enforcer.Require(context.Background(), p, AppOperation("example", "read")); err != nil {
		t.Fatalf("Require error = %v", err)
	}
	if len(authz.requests) != 1 {
		t.Fatalf("CheckAccess requests = %d, want 1", len(authz.requests))
	}
	req := authz.requests[0]
	if req.Subject.Id != "subject:user:123" {
		t.Fatalf("subject id = %q", req.Subject.Id)
	}
	if req.Resource.Type != "example" || req.Resource.Id != "example" {
		t.Fatalf("resource = %#v", req.Resource)
	}
	if req.Action.Name != "read" {
		t.Fatalf("action = %q", req.Action.Name)
	}
}

func TestRequireAppRequestScopeDeniedBeforePolicy(t *testing.T) {
	t.Parallel()

	authz := &recordingAuthorizationProvider{allowed: true}
	enforcer := NewEnforcer(authz)
	p := scopedPrincipal("subject:user:123", "other.read")

	err := enforcer.Require(context.Background(), p, AppOperation("example", "read"))
	if !errors.Is(err, ErrScopeDenied) || !IsOperationScopeDenied(err) {
		t.Fatalf("error = %v, want operation scope denied", err)
	}
	if len(authz.requests) != 0 {
		t.Fatalf("CheckAccess requests = %d, want 0", len(authz.requests))
	}
}

func TestAllowedScopeDeniedReturnsFalseWithoutPolicy(t *testing.T) {
	t.Parallel()

	authz := &recordingAuthorizationProvider{allowed: true}
	enforcer := NewEnforcer(authz)
	p := scopedPrincipal("subject:user:123", "other.read")

	allowed, err := enforcer.Allowed(context.Background(), p, AppOperation("example", "read"))
	if err != nil {
		t.Fatalf("Allowed error = %v", err)
	}
	if allowed {
		t.Fatal("Allowed = true, want false")
	}
	if len(authz.requests) != 0 {
		t.Fatalf("CheckAccess requests = %d, want 0", len(authz.requests))
	}
}

func TestRequireAppRequestPolicyDenied(t *testing.T) {
	t.Parallel()

	authz := &recordingAuthorizationProvider{allowed: false}
	enforcer := NewEnforcer(authz)
	p := scopedPrincipal("subject:user:123", "example.read")

	err := enforcer.Require(context.Background(), p, AppOperation("example", "read"))
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("error = %v, want policy denied", err)
	}
}

func TestRequireAppRequestPolicyUnavailable(t *testing.T) {
	t.Parallel()

	backendErr := errors.New("backend unavailable")
	authz := &recordingAuthorizationProvider{err: backendErr}
	enforcer := NewEnforcer(authz)
	p := scopedPrincipal("subject:user:123", "example.read")

	err := enforcer.Require(context.Background(), p, AppOperation("example", "read"))
	if !IsPolicyUnavailable(err) || !errors.Is(err, backendErr) {
		t.Fatalf("error = %v, want policy unavailable wrapping backend error", err)
	}
	if errors.Is(err, ErrDenied) {
		t.Fatalf("error = %v, should not be treated as denied", err)
	}
}

func TestRequireAppRequestNilProviderAppliesScopeOnly(t *testing.T) {
	t.Parallel()

	enforcer := NewEnforcer(nil)
	if enforcer.HasProvider() {
		t.Fatal("HasProvider = true, want false")
	}
	p := scopedPrincipal("subject:user:123", "example.read")

	if err := enforcer.Require(context.Background(), p, AppOperation("example", "read")); err != nil {
		t.Fatalf("Require error = %v", err)
	}
}

func TestProviderScopeRequestSkipsPolicy(t *testing.T) {
	t.Parallel()

	authz := &recordingAuthorizationProvider{allowed: false}
	enforcer := NewEnforcer(authz)
	p := scopedPrincipal("subject:user:123", "example.read")

	if err := enforcer.Require(context.Background(), p, ProviderScope("example")); err != nil {
		t.Fatalf("Require error = %v", err)
	}
	if len(authz.requests) != 0 {
		t.Fatalf("CheckAccess requests = %d, want 0", len(authz.requests))
	}
}

func TestSubjectFromPrincipalCanonicalizesUser(t *testing.T) {
	t.Parallel()

	p := &principal.Principal{UserID: "user_123"}
	subject := SubjectFromPrincipal(p)
	if subject == nil || subject.Id != "user:user_123" || subject.Type != "subject" {
		t.Fatalf("SubjectFromPrincipal = %#v", subject)
	}
}

func scopedPrincipal(subjectID string, permissions ...string) *principal.Principal {
	p := &principal.Principal{
		SubjectID:        subjectID,
		TokenPermissions: principal.PermissionSet{},
	}
	for _, permission := range permissions {
		app, op, ok := splitPermission(permission)
		if !ok {
			continue
		}
		ops := p.TokenPermissions[app]
		if ops == nil {
			ops = map[string]struct{}{}
			p.TokenPermissions[app] = ops
		}
		ops[op] = struct{}{}
	}
	return p
}

func splitPermission(permission string) (string, string, bool) {
	for i := range permission {
		if permission[i] == '.' {
			return permission[:i], permission[i+1:], true
		}
	}
	return "", "", false
}
