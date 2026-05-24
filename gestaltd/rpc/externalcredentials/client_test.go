package externalcredentials

import (
	"context"
	"testing"
	"time"

	sdkexternalcredentials "github.com/valon-technologies/gestalt/sdk/go/externalcredentials"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type externalCredentialStub struct {
	proto.ExternalCredentialProviderClient

	getCtx context.Context
}

func (s *externalCredentialStub) UpsertCredential(context.Context, *proto.UpsertExternalCredentialRequest, ...grpc.CallOption) (*proto.ExternalCredential, error) {
	return &proto.ExternalCredential{Id: "cred-1", SubjectId: "user:test", ConnectionId: "github:default"}, nil
}

func (s *externalCredentialStub) GetCredential(ctx context.Context, req *proto.GetExternalCredentialRequest, _ ...grpc.CallOption) (*proto.ExternalCredential, error) {
	s.getCtx = ctx
	return &proto.ExternalCredential{
		Id:           "cred-1",
		SubjectId:    req.GetLookup().GetSubjectId(),
		ConnectionId: req.GetLookup().GetConnectionId(),
		AccessToken:  "access-token",
		ExpiresAt:    timestamppb.New(time.Unix(1_700_000_000, 0).UTC()),
	}, nil
}

func (s *externalCredentialStub) ListCredentials(context.Context, *proto.ListExternalCredentialsRequest, ...grpc.CallOption) (*proto.ListExternalCredentialsResponse, error) {
	return &proto.ListExternalCredentialsResponse{Credentials: []*proto.ExternalCredential{
		{Id: "cred-1", SubjectId: "user:test", ConnectionId: "github:default"},
	}}, nil
}

func (s *externalCredentialStub) DeleteCredential(context.Context, *proto.DeleteExternalCredentialRequest, ...grpc.CallOption) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

func (s *externalCredentialStub) ValidateCredentialConfig(context.Context, *proto.ValidateExternalCredentialConfigRequest, ...grpc.CallOption) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

func (s *externalCredentialStub) ResolveCredential(context.Context, *proto.ResolveExternalCredentialRequest, ...grpc.CallOption) (*proto.ResolveExternalCredentialResponse, error) {
	return &proto.ResolveExternalCredentialResponse{
		Token: "resolved-token",
		Params: map[string]string{
			"workspace": "acme",
		},
	}, nil
}

func (s *externalCredentialStub) ExchangeCredential(context.Context, *proto.ExchangeExternalCredentialRequest, ...grpc.CallOption) (*proto.ExchangeExternalCredentialResponse, error) {
	return &proto.ExchangeExternalCredentialResponse{
		TokenResponse: &proto.ExternalCredentialTokenResponse{
			AccessToken: "exchanged-token",
			ExtraJson:   `{"ok":true}`,
		},
	}, nil
}

func TestClientRoundTrip(t *testing.T) {
	t.Parallel()

	client := NewClient(&externalCredentialStub{}, Options{})
	upserted, err := client.UpsertCredential(context.Background(), &sdkexternalcredentials.UpsertExternalCredentialRequest{
		Credential: &sdkexternalcredentials.ExternalCredential{SubjectID: "user:test", ConnectionID: "github:default"},
	})
	if err != nil {
		t.Fatalf("UpsertCredential: %v", err)
	}
	if upserted.GetId() != "cred-1" {
		t.Fatalf("upserted id = %q, want cred-1", upserted.GetId())
	}

	got, err := client.GetCredential(context.Background(), &sdkexternalcredentials.GetExternalCredentialRequest{
		Lookup: &sdkexternalcredentials.ExternalCredentialLookup{SubjectID: "user:test", ConnectionID: "github:default"},
	})
	if err != nil {
		t.Fatalf("GetCredential: %v", err)
	}
	if got.GetAccessToken() != "access-token" || got.GetExpiresAt() == nil {
		t.Fatalf("GetCredential = %+v, want access token and expiry", got)
	}

	listed, err := client.ListCredentials(context.Background(), &sdkexternalcredentials.ListExternalCredentialsRequest{SubjectID: "user:test"})
	if err != nil {
		t.Fatalf("ListCredentials: %v", err)
	}
	if len(listed.GetCredentials()) != 1 {
		t.Fatalf("credentials len = %d, want 1", len(listed.GetCredentials()))
	}

	resolved, err := client.ResolveCredential(context.Background(), &sdkexternalcredentials.ResolveExternalCredentialRequest{CredentialSubjectID: "user:test"})
	if err != nil {
		t.Fatalf("ResolveCredential: %v", err)
	}
	if resolved.GetToken() != "resolved-token" || resolved.GetParams()["workspace"] != "acme" {
		t.Fatalf("ResolveCredential = %+v, want token and params", resolved)
	}

	exchanged, err := client.ExchangeCredential(context.Background(), &sdkexternalcredentials.ExchangeExternalCredentialRequest{CredentialSubjectID: "user:test"})
	if err != nil {
		t.Fatalf("ExchangeCredential: %v", err)
	}
	if exchanged.GetTokenResponse().GetAccessToken() != "exchanged-token" {
		t.Fatalf("ExchangeCredential token = %q, want exchanged-token", exchanged.GetTokenResponse().GetAccessToken())
	}
}

func TestClientUsesUnaryTimeout(t *testing.T) {
	t.Parallel()

	const timeout = 30 * time.Second
	stub := &externalCredentialStub{}
	client := NewClient(stub, Options{UnaryTimeout: timeout})
	if _, err := client.GetCredential(context.Background(), &sdkexternalcredentials.GetExternalCredentialRequest{}); err != nil {
		t.Fatalf("GetCredential: %v", err)
	}
	deadline, ok := stub.getCtx.Deadline()
	if !ok {
		t.Fatal("GetCredential context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= timeout-2*time.Second || remaining > timeout {
		t.Fatalf("deadline remaining = %s, want within 2s of %s", remaining, timeout)
	}
}
