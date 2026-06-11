package server

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
)

func TestAuthorizeMountedUIRouteLeavesGatewayInvokingSubjectEmpty(t *testing.T) {
	t.Parallel()

	authz := &mountedUIRecordingAuthorizationProvider{}
	s := &Server{authorization: authz}
	p := &principal.Principal{
		SubjectID:           "user:subject",
		CredentialSubjectID: "service_account:credential",
	}
	ctx := principal.WithPrincipal(context.Background(), p)

	access, allowed, err := s.authorizeMountedUIRoute(ctx, p, MountedUI{
		AppName: "dealHub",
		Routes: []MountedUIRoute{{
			Path:         "/*",
			AllowedRoles: []string{"viewer"},
		}},
	}, MountedUIRoute{
		Path:         "/*",
		AllowedRoles: []string{"viewer"},
	}, true)
	if err != nil {
		t.Fatalf("authorizeMountedUIRoute: %v", err)
	}
	if !allowed {
		t.Fatalf("allowed = false, want true")
	}
	if access.Policy != "dealHub" || access.Role != "viewer" {
		t.Fatalf("access = %+v, want dealHub viewer", access)
	}
	if got := authz.listRelationshipsInvokingSubjectID; got != "" {
		t.Fatalf("ListRelationships invoking subject = %q, want empty", got)
	}
	if got := authz.listActiveModelResourceTypesInvokingSubjectID; got != "" {
		t.Fatalf("ListActiveModelResourceTypes invoking subject = %q, want empty", got)
	}
	if got := authz.listRelationshipsTargetSubjectID; got != "service_account:credential" {
		t.Fatalf("ListRelationships target subject = %q, want service_account:credential", got)
	}
}

type mountedUIRecordingAuthorizationProvider struct {
	core.AuthorizationProvider

	listRelationshipsInvokingSubjectID            string
	listRelationshipsTargetSubjectID              string
	listActiveModelResourceTypesInvokingSubjectID string
}

func (p *mountedUIRecordingAuthorizationProvider) ListRelationships(ctx context.Context, req *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	p.listRelationshipsInvokingSubjectID = providergateway.InvokingSubjectIDFromContext(ctx)
	p.listRelationshipsTargetSubjectID = req.GetFilter().GetTarget().GetSubject().GetId()
	return &proto.ListRelationshipsResponse{}, nil
}

func (p *mountedUIRecordingAuthorizationProvider) ListActiveModelResourceTypes(ctx context.Context, _ *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	p.listActiveModelResourceTypesInvokingSubjectID = providergateway.InvokingSubjectIDFromContext(ctx)
	return &proto.ListActiveModelResourceTypesResponse{ResourceTypes: []*proto.AuthorizationModelResourceType{{
		Name:                "dealHub",
		DefaultAccessPolicy: proto.DefaultAccessPolicy_DEFAULT_ACCESS_POLICY_ALLOW,
	}}}, nil
}
