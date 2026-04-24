package providerhost

import (
	"context"
	"errors"
	"strings"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

const DefaultExternalCredentialSocketEnv = "GESTALT_EXTERNAL_CREDENTIAL_SOCKET"

type externalCredentialProviderServer struct {
	proto.UnimplementedExternalCredentialProviderServer
	provider core.ExternalCredentialProvider
}

func NewExternalCredentialProviderServer(provider core.ExternalCredentialProvider) proto.ExternalCredentialProviderServer {
	return &externalCredentialProviderServer{provider: provider}
}

func (s *externalCredentialProviderServer) UpsertCredential(ctx context.Context, req *proto.UpsertExternalCredentialRequest) (*proto.ExternalCredential, error) {
	if req == nil || req.GetCredential() == nil {
		return nil, status.Error(codes.InvalidArgument, "credential is required")
	}
	credential := externalCredentialFromProto(req.GetCredential())
	var err error
	if req.GetPreserveTimestamps() {
		err = s.provider.RestoreCredential(ctx, credential)
	} else {
		err = s.provider.PutCredential(ctx, credential)
	}
	if err != nil {
		return nil, externalCredentialToGRPCError("upsert external credential", err)
	}
	stored, err := s.provider.GetCredential(ctx, credential.SubjectID, credential.Integration, credential.Connection, credential.Instance)
	if err != nil {
		return nil, externalCredentialToGRPCError("read stored external credential", err)
	}
	return externalCredentialToProto(stored), nil
}

func (s *externalCredentialProviderServer) GetCredential(ctx context.Context, req *proto.GetExternalCredentialRequest) (*proto.ExternalCredential, error) {
	if req == nil || req.GetLookup() == nil {
		return nil, status.Error(codes.InvalidArgument, "lookup is required")
	}
	lookup := req.GetLookup()
	credential, err := s.provider.GetCredential(ctx, lookup.GetSubjectId(), lookup.GetIntegration(), lookup.GetConnection(), lookup.GetInstance())
	if err != nil {
		return nil, externalCredentialToGRPCError("get external credential", err)
	}
	return externalCredentialToProto(credential), nil
}

func (s *externalCredentialProviderServer) ListCredentials(ctx context.Context, req *proto.ListExternalCredentialsRequest) (*proto.ListExternalCredentialsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	subjectID := strings.TrimSpace(req.GetSubjectId())
	if subjectID == "" {
		return nil, status.Error(codes.InvalidArgument, "subject_id is required")
	}

	var (
		credentials []*core.ExternalCredential
		err         error
	)
	switch {
	case req.GetIntegration() != "" && req.GetConnection() != "":
		credentials, err = s.provider.ListCredentialsForConnection(ctx, subjectID, req.GetIntegration(), req.GetConnection())
	case req.GetIntegration() != "":
		credentials, err = s.provider.ListCredentialsForProvider(ctx, subjectID, req.GetIntegration())
	default:
		credentials, err = s.provider.ListCredentials(ctx, subjectID)
	}
	if err != nil {
		return nil, externalCredentialToGRPCError("list external credentials", err)
	}

	filtered := make([]*proto.ExternalCredential, 0, len(credentials))
	for _, credential := range credentials {
		if credential == nil {
			continue
		}
		if req.GetIntegration() != "" && credential.Integration != req.GetIntegration() {
			continue
		}
		if req.GetConnection() != "" && credential.Connection != req.GetConnection() {
			continue
		}
		if req.GetInstance() != "" && credential.Instance != req.GetInstance() {
			continue
		}
		filtered = append(filtered, externalCredentialToProto(credential))
	}
	return &proto.ListExternalCredentialsResponse{Credentials: filtered}, nil
}

func (s *externalCredentialProviderServer) DeleteCredential(ctx context.Context, req *proto.DeleteExternalCredentialRequest) (*emptypb.Empty, error) {
	if req == nil || strings.TrimSpace(req.GetId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "credential id is required")
	}
	if err := s.provider.DeleteCredential(ctx, req.GetId()); err != nil && !errors.Is(err, core.ErrNotFound) {
		return nil, externalCredentialToGRPCError("delete external credential", err)
	}
	return &emptypb.Empty{}, nil
}

func externalCredentialToGRPCError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, core.ErrNotFound) {
		return status.Error(codes.NotFound, err.Error())
	}
	return status.Errorf(codes.Unknown, "%s: %v", operation, err)
}
