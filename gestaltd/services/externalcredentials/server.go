package externalcredentials

import (
	"context"
	"errors"
	"strings"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

const DefaultSocketEnv = "GESTALT_EXTERNAL_CREDENTIAL_SOCKET"

type externalCredentialProviderServer struct {
	proto.UnimplementedExternalCredentialProviderServer
	provider core.ExternalCredentialProvider
}

func NewProviderServer(provider core.ExternalCredentialProvider) proto.ExternalCredentialProviderServer {
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
	stored, err := s.provider.GetCredential(ctx, credential.SubjectID, credential.ConnectionID, credential.Instance)
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
	credential, err := s.provider.GetCredential(ctx, lookup.GetSubjectId(), lookup.GetConnectionId(), lookup.GetInstance())
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
	case req.GetConnectionId() != "":
		credentials, err = s.provider.ListCredentialsForConnection(ctx, subjectID, req.GetConnectionId())
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
		if req.GetConnectionId() != "" && credential.ConnectionID != req.GetConnectionId() {
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

func (s *externalCredentialProviderServer) ValidateCredentialConfig(ctx context.Context, req *proto.ValidateExternalCredentialConfigRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	err := s.provider.ValidateCredentialConfig(ctx, &core.ValidateExternalCredentialConfigRequest{
		Provider:         strings.TrimSpace(req.GetProvider()),
		Connection:       strings.TrimSpace(req.GetConnection()),
		ConnectionID:     strings.TrimSpace(req.GetConnectionId()),
		Mode:             core.ConnectionMode(req.GetMode()),
		Auth:             externalCredentialAuthConfigFromProto(req.GetAuth()),
		ConnectionParams: cloneStringMap(req.GetConnectionParams()),
	})
	if err != nil {
		return nil, externalCredentialToGRPCError("validate external credential config", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *externalCredentialProviderServer) ResolveCredential(ctx context.Context, req *proto.ResolveExternalCredentialRequest) (*proto.ResolveExternalCredentialResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.ResolveCredential(ctx, &core.ResolveExternalCredentialRequest{
		Provider:            strings.TrimSpace(req.GetProvider()),
		Connection:          strings.TrimSpace(req.GetConnection()),
		ConnectionID:        strings.TrimSpace(req.GetConnectionId()),
		Mode:                core.ConnectionMode(req.GetMode()),
		CredentialSubjectID: strings.TrimSpace(req.GetCredentialSubjectId()),
		ActorSubjectID:      strings.TrimSpace(req.GetActorSubjectId()),
		Instance:            strings.TrimSpace(req.GetInstance()),
		Auth:                externalCredentialAuthConfigFromProto(req.GetAuth()),
		ConnectionParams:    cloneStringMap(req.GetConnectionParams()),
	})
	if err != nil {
		return nil, externalCredentialToGRPCError("resolve external credential", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "provider returned nil response")
	}
	return &proto.ResolveExternalCredentialResponse{
		Token:        resp.Token,
		ExpiresAt:    timeToProto(resp.ExpiresAt),
		MetadataJson: resp.MetadataJSON,
		Params:       cloneStringMap(resp.Params),
		Credential:   externalCredentialToProto(resp.Credential),
	}, nil
}

func (s *externalCredentialProviderServer) ExchangeCredential(ctx context.Context, req *proto.ExchangeExternalCredentialRequest) (*proto.ExchangeExternalCredentialResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.ExchangeCredential(ctx, &core.ExchangeExternalCredentialRequest{
		Provider:            strings.TrimSpace(req.GetProvider()),
		Connection:          strings.TrimSpace(req.GetConnection()),
		ConnectionID:        strings.TrimSpace(req.GetConnectionId()),
		CredentialSubjectID: strings.TrimSpace(req.GetCredentialSubjectId()),
		ActorSubjectID:      strings.TrimSpace(req.GetActorSubjectId()),
		Instance:            strings.TrimSpace(req.GetInstance()),
		Auth:                externalCredentialAuthConfigFromProto(req.GetAuth()),
		CredentialJSON:      req.GetCredentialJson(),
		ConnectionParams:    cloneStringMap(req.GetConnectionParams()),
	})
	if err != nil {
		return nil, externalCredentialToGRPCError("exchange external credential", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "provider returned nil response")
	}
	return &proto.ExchangeExternalCredentialResponse{
		TokenResponse: externalCredentialTokenResponseToProto(resp.TokenResponse),
	}, nil
}

func externalCredentialToGRPCError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, core.ErrNotFound) {
		return status.Error(codes.NotFound, err.Error())
	}
	if errors.Is(err, core.ErrAmbiguousCredential) {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	if errors.Is(err, core.ErrReconnectRequired) {
		return status.Error(codes.Unauthenticated, err.Error())
	}
	return status.Errorf(codes.Unknown, "%s: %v", operation, err)
}
