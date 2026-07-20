package authorization

import (
	"context"
	"testing"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestClientCheckAccessCallsGRPCDirectly(t *testing.T) {
	grpcClient := &recordingAuthorizationClient{
		checkAccessResponse: &proto.CheckAccessResponse{Allowed: true, ModelId: "model-1"},
	}
	client := NewClient(grpcClient, Options{ProviderID: "authz"})

	resp, err := client.CheckAccess(context.Background(), &proto.CheckAccessRequest{
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
