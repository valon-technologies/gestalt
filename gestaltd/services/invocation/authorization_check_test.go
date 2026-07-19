package invocation

import (
	"context"
	"errors"
	"testing"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func TestSubjectAccessRequestShapeAndTrims(t *testing.T) {
	t.Parallel()

	resource := &proto.Resource{Type: "app", Id: "slack"}
	req := SubjectAccessRequest("  user:alice  ", "  chat.postMessage  ", resource)

	if got := req.GetSubject().GetType(); got != "subject" {
		t.Fatalf("Subject.Type = %q, want subject", got)
	}
	if got := req.GetSubject().GetId(); got != "user:alice" {
		t.Fatalf("Subject.Id = %q, want user:alice", got)
	}
	if got := req.GetAction().GetName(); got != "chat.postMessage" {
		t.Fatalf("Action.Name = %q, want chat.postMessage", got)
	}
	if got := req.GetResource(); got != resource {
		t.Fatalf("Resource = %p, want %p", got, resource)
	}
	if got := req.GetSubject().GetProperties(); got != nil {
		t.Fatalf("Subject.Properties = %#v, want nil (callers attach properties themselves)", got)
	}
}

func TestSubjectAccessRequestEmptySubjectPreserved(t *testing.T) {
	t.Parallel()

	req := SubjectAccessRequest("   ", "view", &proto.Resource{Type: "group", Id: "engineering"})
	if got := req.GetSubject().GetId(); got != "" {
		t.Fatalf("Subject.Id = %q, want empty (callers surface their own auth error)", got)
	}
}

func TestCheckSubjectAccessAllowed(t *testing.T) {
	t.Parallel()

	provider := &authorizationCheckTestProvider{allowed: true}
	allowed, err := CheckSubjectAccess(context.Background(), provider, &proto.CheckAccessRequest{})
	if err != nil {
		t.Fatalf("CheckSubjectAccess error = %v, want nil", err)
	}
	if !allowed {
		t.Fatal("CheckSubjectAccess allowed = false, want true")
	}
}

func TestCheckSubjectAccessDenied(t *testing.T) {
	t.Parallel()

	provider := &authorizationCheckTestProvider{allowed: false}
	allowed, err := CheckSubjectAccess(context.Background(), provider, &proto.CheckAccessRequest{})
	if err != nil {
		t.Fatalf("CheckSubjectAccess error = %v, want nil", err)
	}
	if allowed {
		t.Fatal("CheckSubjectAccess allowed = true, want false")
	}
}

func TestCheckSubjectAccessNilResponseDeniesWithoutError(t *testing.T) {
	t.Parallel()

	provider := &authorizationCheckTestProvider{response: nil}
	allowed, err := CheckSubjectAccess(context.Background(), provider, &proto.CheckAccessRequest{})
	if err != nil {
		t.Fatalf("CheckSubjectAccess error = %v, want nil on nil response", err)
	}
	if allowed {
		t.Fatal("CheckSubjectAccess allowed = true on nil response, want false")
	}
}

func TestCheckSubjectAccessErrorPassesThrough(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("authorization provider unavailable")
	provider := &authorizationCheckTestProvider{err: wantErr}
	allowed, err := CheckSubjectAccess(context.Background(), provider, &proto.CheckAccessRequest{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("CheckSubjectAccess error = %v, want %v", err, wantErr)
	}
	if allowed {
		t.Fatal("CheckSubjectAccess allowed = true on error, want false")
	}
}

type authorizationCheckTestProvider struct {
	allowed  bool
	response *proto.CheckAccessResponse
	err      error
	lastReq  *proto.CheckAccessRequest
}

func (p *authorizationCheckTestProvider) CheckAccess(_ context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	p.lastReq = req
	if p.err != nil {
		return nil, p.err
	}
	if p.response != nil {
		return p.response, nil
	}
	return &proto.CheckAccessResponse{Allowed: p.allowed}, nil
}

func (p *authorizationCheckTestProvider) CheckAccessMany(context.Context, *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	return &proto.CheckAccessManyResponse{}, nil
}

func (p *authorizationCheckTestProvider) ListRelationships(context.Context, *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	return &proto.ListRelationshipsResponse{}, nil
}

func (p *authorizationCheckTestProvider) AddRelationship(context.Context, *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	return &proto.AddRelationshipResponse{}, nil
}

func (p *authorizationCheckTestProvider) DeleteRelationship(context.Context, *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	return &proto.DeleteRelationshipResponse{}, nil
}

func (p *authorizationCheckTestProvider) SetAuthorizationState(context.Context, *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	return &proto.SetAuthorizationStateResponse{}, nil
}

func (p *authorizationCheckTestProvider) GetActiveModelRef(context.Context) (*proto.GetActiveModelRefResponse, error) {
	return &proto.GetActiveModelRefResponse{}, nil
}

func (p *authorizationCheckTestProvider) SetActiveModel(context.Context, *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	return &proto.SetActiveModelResponse{}, nil
}

func (p *authorizationCheckTestProvider) ListActiveModelResourceTypes(context.Context, *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	return &proto.ListActiveModelResourceTypesResponse{}, nil
}

func (p *authorizationCheckTestProvider) Ping(context.Context) error { return nil }

func (p *authorizationCheckTestProvider) Close() error { return nil }
