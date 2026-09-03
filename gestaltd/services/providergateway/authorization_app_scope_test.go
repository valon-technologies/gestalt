package providergateway

import (
	"context"
	"testing"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

type appScopedAuthorizationProvider struct {
	*stubAuthorizationProvider
	appAdmin map[string]bool
}

func (p *appScopedAuthorizationProvider) CheckAccess(
	ctx context.Context,
	req *proto.CheckAccessRequest,
) (*proto.CheckAccessResponse, error) {
	resource := req.GetResource()
	if resource.GetType() == appAuthorizationResourceType {
		appID := resource.GetId()
		if p.appAdmin[appID] && req.GetAction().GetName() == appID {
			return &proto.CheckAccessResponse{
				Allowed:          true,
				MatchedRelations: []string{appAdminRelation},
			}, nil
		}
		return &proto.CheckAccessResponse{Allowed: false}, nil
	}
	if p.stubAuthorizationProvider != nil {
		return p.stubAuthorizationProvider.CheckAccess(ctx, req)
	}
	return &proto.CheckAccessResponse{Allowed: false}, nil
}

func TestAllowsAppScopedRelationshipMutation(t *testing.T) {
	t.Parallel()

	viewerSubject := &proto.Subject{Type: "subject", Id: "user:viewer@example.com"}
	tuple := &proto.RelationshipTuple{
		Resource: &proto.Resource{Type: "app", Id: "roadmap"},
		Relation: "viewer",
		Target: &proto.RelationshipTarget{
			Kind: &proto.RelationshipTarget_Subject{Subject: viewerSubject},
		},
	}

	provider := &appScopedAuthorizationProvider{
		appAdmin: map[string]bool{"roadmap": true},
	}

	allowed, err := allowsAppScopedRelationshipMutation(
		context.Background(),
		provider,
		"user:admin@example.com",
		tuple,
	)
	if err != nil {
		t.Fatalf("allowsAppScopedRelationshipMutation error = %v", err)
	}
	if !allowed {
		t.Fatal("allowsAppScopedRelationshipMutation allowed = false, want true")
	}
}

func TestAllowsAppScopedRelationshipMutationRejectsOtherApps(t *testing.T) {
	t.Parallel()

	tuple := &proto.RelationshipTuple{
		Resource: &proto.Resource{Type: "app", Id: "other-app"},
		Relation: "viewer",
		Target: &proto.RelationshipTarget{
			Kind: &proto.RelationshipTarget_Subject{
				Subject: &proto.Subject{Type: "subject", Id: "user:viewer@example.com"},
			},
		},
	}
	provider := &appScopedAuthorizationProvider{
		appAdmin: map[string]bool{"roadmap": true},
	}

	allowed, err := allowsAppScopedRelationshipMutation(
		context.Background(),
		provider,
		"user:admin@example.com",
		tuple,
	)
	if err != nil {
		t.Fatalf("allowsAppScopedRelationshipMutation error = %v", err)
	}
	if allowed {
		t.Fatal("allowsAppScopedRelationshipMutation allowed = true, want false")
	}
}

func TestAllowsAppScopedRelationshipMutationRejectsGlobalResources(t *testing.T) {
	t.Parallel()

	tuple := &proto.RelationshipTuple{
		Resource: &proto.Resource{Type: "gestalt", Id: "admin"},
		Relation: "admin",
		Target: &proto.RelationshipTarget{
			Kind: &proto.RelationshipTarget_Subject{
				Subject: &proto.Subject{Type: "subject", Id: "user:viewer@example.com"},
			},
		},
	}
	provider := &appScopedAuthorizationProvider{
		appAdmin: map[string]bool{"admin": true},
	}

	allowed, err := allowsAppScopedRelationshipMutation(
		context.Background(),
		provider,
		"user:admin@example.com",
		tuple,
	)
	if err != nil {
		t.Fatalf("allowsAppScopedRelationshipMutation error = %v", err)
	}
	if allowed {
		t.Fatal("allowsAppScopedRelationshipMutation allowed = true for gestalt tuple, want false")
	}
}

func TestEnforceAuthorizationPublicAccessAllowsAppAdminRelationshipWrite(t *testing.T) {
	t.Parallel()

	provider := &appScopedAuthorizationProvider{
		stubAuthorizationProvider: &stubAuthorizationProvider{
			allowedActions: map[string]bool{"viewer": true},
		},
		appAdmin: map[string]bool{"roadmap": true},
	}
	transport := &ProviderGatewayTransport{authorization: provider}

	req := &proto.AddRelationshipRequest{
		Relationship: &proto.Relationship{
			Tuple: &proto.RelationshipTuple{
				Resource: &proto.Resource{Type: "app", Id: "roadmap"},
				Relation: "viewer",
				Target: &proto.RelationshipTarget{
					Kind: &proto.RelationshipTarget_Subject{
						Subject: &proto.Subject{Type: "subject", Id: "user:viewer@example.com"},
					},
				},
			},
		},
	}
	err := transport.enforceAuthorizationPublicAccess(
		context.Background(),
		"user:admin@example.com",
		proto.Authorization_AddRelationship_FullMethodName,
		req,
	)
	if err != nil {
		t.Fatalf("enforceAuthorizationPublicAccess error = %v, want nil", err)
	}
}

func TestEnforceAuthorizationPublicAccessDeniesAppAdminModelWrite(t *testing.T) {
	t.Parallel()

	provider := &appScopedAuthorizationProvider{
		appAdmin: map[string]bool{"roadmap": true},
	}
	transport := &ProviderGatewayTransport{authorization: provider}

	err := transport.enforceAuthorizationPublicAccess(
		context.Background(),
		"user:admin@example.com",
		proto.Authorization_SetActiveModel_FullMethodName,
		&proto.SetActiveModelRequest{},
	)
	if err == nil {
		t.Fatal("enforceAuthorizationPublicAccess error = nil, want permission denied")
	}
}

func TestRelationshipTupleFromAuthorizationRequest(t *testing.T) {
	t.Parallel()

	addReq := &proto.AddRelationshipRequest{
		Relationship: &proto.Relationship{
			Tuple: &proto.RelationshipTuple{
				Resource: &proto.Resource{Type: "app", Id: "roadmap"},
				Relation: "viewer",
			},
		},
	}
	tuple, ok := relationshipTupleFromAuthorizationRequest(
		proto.Authorization_AddRelationship_FullMethodName,
		addReq,
	)
	if !ok || tuple.GetResource().GetId() != "roadmap" {
		t.Fatalf("relationshipTupleFromAuthorizationRequest add = %#v, %#v", tuple, ok)
	}

	deleteReq := &proto.DeleteRelationshipRequest{
		RelationshipTuple: &proto.RelationshipTuple{
			Resource: &proto.Resource{Type: "app", Id: "roadmap"},
			Relation: "viewer",
		},
	}
	tuple, ok = relationshipTupleFromAuthorizationRequest(
		proto.Authorization_DeleteRelationship_FullMethodName,
		deleteReq,
	)
	if !ok || tuple.GetResource().GetId() != "roadmap" {
		t.Fatalf("relationshipTupleFromAuthorizationRequest delete = %#v, %#v", tuple, ok)
	}
}

func TestAllowsAppScopedRelationshipMutationAllowsSubjectSetTarget(t *testing.T) {
	t.Parallel()

	tuple := &proto.RelationshipTuple{
		Resource: &proto.Resource{Type: "app", Id: "roadmap"},
		Relation: "viewer",
		Target: &proto.RelationshipTarget{
			Kind: &proto.RelationshipTarget_SubjectSet{
				SubjectSet: &proto.SubjectSet{
					Resource: &proto.Resource{Type: "group", Id: "valon-employees"},
					Relation: "member",
				},
			},
		},
	}
	provider := &appScopedAuthorizationProvider{
		appAdmin: map[string]bool{"roadmap": true},
	}

	allowed, err := allowsAppScopedRelationshipMutation(
		context.Background(),
		provider,
		"user:admin@example.com",
		tuple,
	)
	if err != nil {
		t.Fatalf("allowsAppScopedRelationshipMutation error = %v", err)
	}
	if !allowed {
		t.Fatal("allowsAppScopedRelationshipMutation allowed = false for subject set target, want true")
	}
}
