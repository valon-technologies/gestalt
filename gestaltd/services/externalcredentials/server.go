package externalcredentials

import (
	"context"
	"errors"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type externalCredentialProviderServer struct {
	proto.UnimplementedExternalCredentialsServer
	provider core.ExternalCredentialProvider
}

func NewProviderServer(provider core.ExternalCredentialProvider) proto.ExternalCredentialsServer {
	return &externalCredentialProviderServer{provider: provider}
}

func (s *externalCredentialProviderServer) CreateCredential(ctx context.Context, req *proto.CreateExternalCredentialRequest) (*proto.ExternalCredential, error) {
	if req == nil || req.GetCredential() == nil {
		return nil, status.Error(codes.InvalidArgument, "credential is required")
	}
	credential := externalCredentialFromProto(req.GetCredential())
	if err := s.provider.CreateCredential(ctx, credential); err != nil {
		return nil, externalCredentialToGRPCError("create external credential", err)
	}
	return externalCredentialToProto(credential), nil
}

func (s *externalCredentialProviderServer) UpsertCredential(ctx context.Context, req *proto.UpsertExternalCredentialRequest) (*proto.ExternalCredential, error) {
	if req == nil || req.GetCredential() == nil {
		return nil, status.Error(codes.InvalidArgument, "credential is required")
	}
	credential := externalCredentialFromProto(req.GetCredential())
	if err := s.provider.UpsertCredential(ctx, credential); err != nil {
		return nil, externalCredentialToGRPCError("upsert external credential", err)
	}
	stored, err := s.provider.GetCredential(ctx, credential.Subject, credential.Audience, credential.Qualifier)
	if err != nil {
		return nil, externalCredentialToGRPCError("read stored external credential", err)
	}
	return externalCredentialToProto(stored), nil
}

func (s *externalCredentialProviderServer) GetCredential(ctx context.Context, req *proto.GetExternalCredentialRequest) (*proto.ExternalCredential, error) {
	if req == nil || strings.TrimSpace(req.GetSubject()) == "" {
		return nil, status.Error(codes.InvalidArgument, "subject is required")
	}
	credential, err := s.provider.GetCredential(ctx, req.GetSubject(), req.GetAudience(), req.GetQualifier())
	if err != nil {
		return nil, externalCredentialToGRPCError("get external credential", err)
	}
	return externalCredentialToProto(credential), nil
}

func (s *externalCredentialProviderServer) ListCredentials(ctx context.Context, req *proto.ListExternalCredentialsRequest) (*proto.ListExternalCredentialsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	subject := strings.TrimSpace(req.GetSubject())
	if subject == "" {
		return nil, status.Error(codes.InvalidArgument, "subject is required")
	}

	credentials, err := s.provider.ListCredentials(ctx, subject, strings.TrimSpace(req.GetAudience()))
	if err != nil {
		return nil, externalCredentialToGRPCError("list external credentials", err)
	}

	out := make([]*proto.ExternalCredential, 0, len(credentials))
	for _, credential := range credentials {
		if credential == nil {
			continue
		}
		out = append(out, externalCredentialToProto(credential))
	}
	return &proto.ListExternalCredentialsResponse{Credentials: out}, nil
}

func (s *externalCredentialProviderServer) DeleteCredential(ctx context.Context, req *proto.DeleteExternalCredentialRequest) (*emptypb.Empty, error) {
	if req == nil || strings.TrimSpace(req.GetId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "credential id is required")
	}
	if _, ok := publicrpc.PublicOriginFromContext(ctx); ok {
		p := principal.FromContext(ctx)
		subjectID, err := principal.ResolveCredentialSubjectID(ctx, nil, p)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "not authenticated")
		}
		credentials, err := s.provider.ListCredentials(ctx, subjectID, "")
		if err != nil {
			return nil, externalCredentialToGRPCError("list external credentials", err)
		}
		owned := false
		for _, credential := range credentials {
			if credential != nil && credential.ID == req.GetId() {
				owned = true
				break
			}
		}
		if !owned {
			return &emptypb.Empty{}, nil
		}
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
		Mode:             core.NormalizeConnectionMode(core.ConnectionMode(req.GetMode())),
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
		Mode:                core.NormalizeConnectionMode(core.ConnectionMode(req.GetMode())),
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
	if errors.Is(err, core.ErrAlreadyExists) {
		return status.Error(codes.AlreadyExists, err.Error())
	}
	if errors.Is(err, core.ErrAmbiguousCredential) {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	if errors.Is(err, core.ErrReconnectRequired) {
		return status.Error(codes.Unauthenticated, err.Error())
	}
	return status.Errorf(codes.Unknown, "%s: %v", operation, err)
}
