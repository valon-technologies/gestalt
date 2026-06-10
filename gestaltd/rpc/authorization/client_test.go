package authorization

import (
	"context"
	"strings"
	"testing"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	"google.golang.org/grpc"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestClientCheckAccessInvokesProviderGatewayBeforeGRPC(t *testing.T) {
	gateway := &recordingGateway{}
	grpcClient := &recordingAuthorizationClient{
		checkAccessResponse: &proto.CheckAccessResponse{Allowed: true, ModelId: "model-1"},
	}
	client := NewClient(grpcClient, Options{
		ProviderID: "authz",
	}, gateway)
	ctx := providergateway.WithInvokingSubjectID(context.Background(), "user:caller")

	resp, err := client.CheckAccess(ctx, &proto.CheckAccessRequest{
		Subject:  &proto.Subject{Type: "subject", Id: "user:checked"},
		Action:   &proto.Action{Name: "view"},
		Resource: &proto.Resource{Type: "team", Id: "servicing"},
	})
	if err != nil {
		t.Fatalf("CheckAccess: %v", err)
	}
	if !resp.GetAllowed() || resp.GetModelId() != "model-1" {
		t.Fatalf("response = %+v, want allowed model-1", resp)
	}
	if grpcClient.checkAccessRequest.GetSubject().GetId() != "user:checked" {
		t.Fatalf("grpc subject = %q, want user:checked", grpcClient.checkAccessRequest.GetSubject().GetId())
	}

	got := gateway.request
	if got.ProviderID != "authz" {
		t.Fatalf("ProviderID = %q, want authz", got.ProviderID)
	}
	if got.ProviderKind != providergateway.ProviderKindAuthorization {
		t.Fatalf("ProviderKind = %q, want authorization", got.ProviderKind)
	}
	if got.ServiceName != "gestalt.provider.v1.Authorization" {
		t.Fatalf("ServiceName = %q", got.ServiceName)
	}
	if got.Operation != "CheckAccess" {
		t.Fatalf("Operation = %q", got.Operation)
	}
	if got.InvokingSubjectID != "user:caller" {
		t.Fatalf("InvokingSubjectID = %q, want user:caller", got.InvokingSubjectID)
	}
	var payload proto.CheckAccessRequest
	if err := gproto.Unmarshal(got.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.GetSubject().GetId() != "user:checked" {
		t.Fatalf("payload subject = %q, want user:checked", payload.GetSubject().GetId())
	}
}

func TestClientCheckAccessFailsWithoutProviderGateway(t *testing.T) {
	grpcClient := &recordingAuthorizationClient{
		checkAccessResponse: &proto.CheckAccessResponse{Allowed: true},
	}
	client := NewClient(grpcClient, Options{ProviderID: "authz"}, nil)

	_, err := client.CheckAccess(context.Background(), &proto.CheckAccessRequest{
		Subject:  &proto.Subject{Type: "subject", Id: "user:checked"},
		Action:   &proto.Action{Name: "view"},
		Resource: &proto.Resource{Type: "team", Id: "servicing"},
	})
	if err == nil || !strings.Contains(err.Error(), "provider gateway is required") {
		t.Fatalf("CheckAccess error = %v, want provider gateway required", err)
	}
	if grpcClient.checkAccessRequest != nil {
		t.Fatalf("grpc CheckAccess was called without provider gateway")
	}
}

type recordingGateway struct {
	request providergateway.ProviderGatewayRequest
}

func (g *recordingGateway) Invoke(ctx context.Context, req providergateway.ProviderGatewayRequest, next providergateway.Next) (providergateway.ProviderGatewayResponse, error) {
	g.request = req
	return next(ctx, req)
}

type recordingAuthorizationClient struct {
	proto.AuthorizationClient
	checkAccessRequest  *proto.CheckAccessRequest
	checkAccessResponse *proto.CheckAccessResponse
}

func (c *recordingAuthorizationClient) CheckAccess(ctx context.Context, in *proto.CheckAccessRequest, opts ...grpc.CallOption) (*proto.CheckAccessResponse, error) {
	c.checkAccessRequest = in
	return c.checkAccessResponse, nil
}

func (c *recordingAuthorizationClient) CheckAccessMany(context.Context, *proto.CheckAccessManyRequest, ...grpc.CallOption) (*proto.CheckAccessManyResponse, error) {
	return &proto.CheckAccessManyResponse{}, nil
}

func (c *recordingAuthorizationClient) ListRelationships(context.Context, *proto.ListRelationshipsRequest, ...grpc.CallOption) (*proto.ListRelationshipsResponse, error) {
	return &proto.ListRelationshipsResponse{}, nil
}

func (c *recordingAuthorizationClient) AddRelationship(context.Context, *proto.AddRelationshipRequest, ...grpc.CallOption) (*proto.AddRelationshipResponse, error) {
	return &proto.AddRelationshipResponse{}, nil
}

func (c *recordingAuthorizationClient) DeleteRelationship(context.Context, *proto.DeleteRelationshipRequest, ...grpc.CallOption) (*proto.DeleteRelationshipResponse, error) {
	return &proto.DeleteRelationshipResponse{}, nil
}

func (c *recordingAuthorizationClient) SetAuthorizationState(context.Context, *proto.SetAuthorizationStateRequest, ...grpc.CallOption) (*proto.SetAuthorizationStateResponse, error) {
	return &proto.SetAuthorizationStateResponse{}, nil
}

func (c *recordingAuthorizationClient) GetActiveModelRef(context.Context, *emptypb.Empty, ...grpc.CallOption) (*proto.GetActiveModelRefResponse, error) {
	return &proto.GetActiveModelRefResponse{}, nil
}

func (c *recordingAuthorizationClient) SetActiveModel(context.Context, *proto.SetActiveModelRequest, ...grpc.CallOption) (*proto.SetActiveModelResponse, error) {
	return &proto.SetActiveModelResponse{}, nil
}

func (c *recordingAuthorizationClient) ListActiveModelResourceTypes(context.Context, *proto.ListActiveModelResourceTypesRequest, ...grpc.CallOption) (*proto.ListActiveModelResourceTypesResponse, error) {
	return &proto.ListActiveModelResourceTypesResponse{}, nil
}
