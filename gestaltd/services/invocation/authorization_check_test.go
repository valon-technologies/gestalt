package invocation

import (
	"context"
	"errors"
	"strconv"
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

func TestMatchedAllowedRoleUsesAllowedRoleOrder(t *testing.T) {
	t.Parallel()

	role := matchedAllowedRole([]string{"viewer", "admin"}, []string{"admin", "viewer"})
	if role != "admin" {
		t.Fatalf("matchedAllowedRole = %q, want admin", role)
	}
}

func TestMatchedAllowedRoleRejectsNonMatchingRole(t *testing.T) {
	t.Parallel()

	if role := matchedAllowedRole([]string{"viewer"}, []string{"admin"}); role != "" {
		t.Fatalf("matchedAllowedRole = %q, want empty", role)
	}
}

type authorizationCheckTestProvider struct {
	allowed     bool
	response    *proto.CheckAccessResponse
	nilResponse bool
	err         error
	lastReq     *proto.CheckAccessRequest
}

func (p *authorizationCheckTestProvider) CheckAccess(_ context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	p.lastReq = req
	if p.err != nil {
		return nil, p.err
	}
	if p.nilResponse {
		return nil, nil
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

func (p *authorizationCheckTestProvider) WriteRelationships(context.Context, *proto.WriteRelationshipsRequest) (*proto.WriteRelationshipsResponse, error) {
	return &proto.WriteRelationshipsResponse{}, nil
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

func testResourceAccessRequest(allowedRoles []string) ResourceAccessRequest {
	return ResourceAccessRequest{
		SubjectID:    "user:alice",
		Action:       "slack",
		Resource:     &proto.Resource{Type: "app", Id: "slack"},
		AllowedRoles: allowedRoles,
	}
}

func TestCheckResourceAccessReportsMatchedRole(t *testing.T) {
	t.Parallel()

	provider := &authorizationCheckTestProvider{response: &proto.CheckAccessResponse{
		Allowed:          true,
		MatchedRelations: []string{"viewer", "admin"},
	}}

	decision, err := CheckResourceAccess(context.Background(), provider, testResourceAccessRequest([]string{"admin"}))
	if err != nil {
		t.Fatalf("CheckResourceAccess error = %v, want nil", err)
	}
	if !decision.Allowed || decision.Role != "admin" {
		t.Fatalf("decision = %+v, want allowed admin", decision)
	}
	if got := provider.lastReq.GetAction().GetName(); got != "slack" {
		t.Fatalf("action = %q, want slack", got)
	}
}

func TestCheckResourceAccessDeniesRoleOutsideAllowedRoles(t *testing.T) {
	t.Parallel()

	provider := &authorizationCheckTestProvider{response: &proto.CheckAccessResponse{
		Allowed:          true,
		MatchedRelations: []string{"viewer"},
	}}

	decision, err := CheckResourceAccess(context.Background(), provider, testResourceAccessRequest([]string{"admin"}))
	if err != nil {
		t.Fatalf("CheckResourceAccess error = %v, want nil", err)
	}
	if decision.Allowed {
		t.Fatalf("decision = %+v, want denied", decision)
	}
}

func TestCheckResourceAccessDeniesWhenEvaluatorDenies(t *testing.T) {
	t.Parallel()

	provider := &authorizationCheckTestProvider{response: &proto.CheckAccessResponse{
		Allowed:          false,
		MatchedRelations: []string{"admin"},
	}}

	decision, err := CheckResourceAccess(context.Background(), provider, testResourceAccessRequest(nil))
	if err != nil {
		t.Fatalf("CheckResourceAccess error = %v, want nil", err)
	}
	if decision.Allowed {
		t.Fatalf("decision = %+v, want denied when the evaluator denies", decision)
	}
}

func TestCheckResourceAccessFailsClosedOnProviderError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("evaluator unavailable")
	provider := &authorizationCheckTestProvider{err: wantErr}

	decision, err := CheckResourceAccess(context.Background(), provider, testResourceAccessRequest(nil))
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	if decision.Allowed {
		t.Fatal("provider error was allowed")
	}
}

func TestCheckResourceAccessFailsClosedOnMalformedResponse(t *testing.T) {
	t.Parallel()

	provider := &authorizationCheckTestProvider{nilResponse: true}
	decision, err := CheckResourceAccess(context.Background(), provider, testResourceAccessRequest(nil))
	if !errors.Is(err, ErrMalformedAuthorizationDecision) {
		t.Fatalf("error = %v, want ErrMalformedAuthorizationDecision", err)
	}
	if decision.Allowed {
		t.Fatal("malformed response was allowed")
	}

	nilProvider, err := CheckResourceAccess(context.Background(), nil, testResourceAccessRequest(nil))
	if !errors.Is(err, ErrAuthorizationUnavailable) {
		t.Fatalf("error = %v, want ErrAuthorizationUnavailable", err)
	}
	if nilProvider.Allowed {
		t.Fatal("missing evaluator was allowed")
	}
}

func TestCheckResourceAccessBoundsMatchedRelations(t *testing.T) {
	t.Parallel()

	relations := make([]string, 0, maxEvaluatedRelations*4)
	for i := range cap(relations) {
		relations = append(relations, "relation-"+strconv.Itoa(i))
	}
	provider := &authorizationCheckTestProvider{response: &proto.CheckAccessResponse{
		Allowed:          true,
		MatchedRelations: relations,
	}}

	// A role past the bound is not honored, so an oversized response cannot
	// make the server do unbounded work to find a match.
	decision, err := CheckResourceAccess(
		context.Background(),
		provider,
		testResourceAccessRequest([]string{relations[len(relations)-1]}),
	)
	if err != nil {
		t.Fatalf("CheckResourceAccess error = %v, want nil", err)
	}
	if decision.Allowed {
		t.Fatalf("decision = %+v, want denied beyond the relation bound", decision)
	}
	if got := len(boundedRelations(relations)); got != maxEvaluatedRelations {
		t.Fatalf("boundedRelations length = %d, want %d", got, maxEvaluatedRelations)
	}
}

func TestBoundedRelationsNormalizes(t *testing.T) {
	t.Parallel()

	got := boundedRelations([]string{" admin ", "", "admin", "viewer"})
	if len(got) != 2 || got[0] != "admin" || got[1] != "viewer" {
		t.Fatalf("boundedRelations = %#v, want [admin viewer]", got)
	}
}
