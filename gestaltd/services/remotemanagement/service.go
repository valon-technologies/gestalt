package remotemanagement

import (
	"context"
	"errors"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/valon-technologies/gestalt/server/core"
	coredata "github.com/valon-technologies/gestalt/server/internal/coredata"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

const reversePublicationResourceType = "reversePublication"
const reversePublicationResourceID = "reversePublication"
const legacyAdminResourceType = "gestaltAdmin"
const legacyAdminResourceID = "gestaltAdmin"

// EndpointValidator dials a candidate tunnel and runs the tunnel-only
// RegistrationLifecycle.Check. RR-6/RR-7 supply the real one; tests inject a fake.
type EndpointValidator interface {
	Validate(ctx context.Context, tunnel *proto.TunnelEndpoint, providers []*proto.RemoteProviderDefinition) error
}

// Config carries the upstream-derived bootstrap values returned by ListRemotes.
type Config struct {
	ServerIdentity *proto.ServerIdentity
	Tunnel         *proto.TunnelBootstrap
	LeaseDuration  time.Duration
	ConnectURL     string
}

type Service struct {
	proto.UnimplementedRemoteManagementServer

	store    *coredata.RemoteRegistrationService
	authz    core.AuthorizationProvider
	users    principal.CredentialUserResolver
	validate EndpointValidator
	config   Config
}

func New(store *coredata.RemoteRegistrationService, authz core.AuthorizationProvider, users principal.CredentialUserResolver, validate EndpointValidator, config Config) (*Service, error) {
	if config.LeaseDuration <= 0 {
		return nil, status.Errorf(codes.FailedPrecondition, "remote management lease duration must be positive")
	}
	return &Service{
		store:    store,
		authz:    authz,
		users:    users,
		validate: validate,
		config:   config,
	}, nil
}

func (s *Service) CreateRemote(ctx context.Context, req *proto.CreateRemoteRequest) (*proto.Remote, error) {
	if s == nil || s.store == nil {
		return nil, status.Error(codes.FailedPrecondition, "remote management is not configured")
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := validateCreateRequest(req); err != nil {
		return nil, err
	}

	p := principal.FromContext(ctx)
	if err := s.authorizePublication(ctx, p, "create"); err != nil {
		return nil, err
	}
	owner, err := principal.ResolveCredentialSubjectID(ctx, s.users, p)
	if err != nil {
		return nil, resolveSubjectError(err)
	}

	defs := req.GetProviders()
	providers, err := providerDefinitionsToCore(defs)
	if err != nil {
		return nil, err
	}
	tunnel := req.GetTunnel()
	reg := &coredata.RemoteRegistration{
		ID:                "", // assigned by the store from the owner index or the caller's id
		OwnerSubjectID:    owner,
		TunnelHost:        strings.TrimSpace(tunnel.GetHost()),
		TunnelCertificate: append([]byte(nil), tunnel.GetCertificate()...),
		ServerSPKISHA256:  strings.TrimSpace(tunnel.GetServerSpkiSha256()),
		ConnectURL:        s.config.ConnectURL,
		LeaseExpiresAt:    s.storeNow().Add(s.config.LeaseDuration),
	}

	if reg.ID == "" {
		reg.ID = owner
	}

	if s.validate != nil {
		if err := s.validate.Validate(ctx, tunnel, defs); err != nil {
			return nil, status.Errorf(codes.FailedPrecondition, "tunnel validation failed: %v", err)
		}
	}

	stored, err := s.store.Replace(ctx, owner, reg, providers, req.GetExpectedGeneration())
	if err != nil {
		return nil, mapStoreError(err)
	}
	return remoteToProto(stored, defs), nil
}

func (s *Service) ListRemotes(ctx context.Context, _ *proto.ListRemotesRequest) (*proto.ListRemotesResponse, error) {
	if s == nil || s.store == nil {
		return nil, status.Error(codes.FailedPrecondition, "remote management is not configured")
	}
	p := principal.FromContext(ctx)
	if err := s.authorizePublication(ctx, p, "read"); err != nil {
		return nil, err
	}
	owner, err := principal.ResolveCredentialSubjectID(ctx, s.users, p)
	if err != nil {
		return nil, resolveSubjectError(err)
	}
	reg, providers, err := s.store.ListByOwner(ctx, owner)
	if err != nil {
		if errors.Is(err, coredata.ErrNotRegistered) {
			return &proto.ListRemotesResponse{
				ServerIdentity: s.config.ServerIdentity,
				Tunnel:         s.config.Tunnel,
				LeaseDuration:  durationpb.New(s.config.LeaseDuration),
			}, nil
		}
		return nil, mapStoreError(err)
	}
	return &proto.ListRemotesResponse{
		Remotes:        []*proto.Remote{remoteToProto(reg, coreProvidersToProtoDefinitions(providers))},
		ServerIdentity: s.config.ServerIdentity,
		Tunnel:         s.config.Tunnel,
		LeaseDuration:  durationpb.New(s.config.LeaseDuration),
	}, nil
}

func (s *Service) DeleteRemote(ctx context.Context, req *proto.DeleteRemoteRequest) (*emptypb.Empty, error) {
	if s == nil || s.store == nil {
		return nil, status.Error(codes.FailedPrecondition, "remote management is not configured")
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	id := strings.TrimSpace(req.GetId())
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	if req.GetExpectedGeneration() == 0 {
		return nil, status.Error(codes.InvalidArgument, "expected_generation is required for delete")
	}
	p := principal.FromContext(ctx)
	if p == nil {
		return nil, status.Error(codes.Unauthenticated, "authenticated subject is required")
	}
	if err := s.authorizePublication(ctx, p, "delete"); err != nil {
		return nil, err
	}
	owner, err := principal.ResolveCredentialSubjectID(ctx, s.users, p)
	if err != nil {
		return nil, resolveSubjectError(err)
	}
	if id != owner {
		return nil, status.Error(codes.NotFound, "unknown registration")
	}
	if err := s.store.Delete(ctx, id, req.GetExpectedGeneration()); err != nil {
		return nil, mapStoreError(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *Service) authorizePublication(ctx context.Context, p *principal.Principal, action string) error {
	if s == nil || s.authz == nil {
		return nil
	}
	subjectID, err := principal.ResolveCredentialSubjectID(ctx, s.users, p)
	if err != nil {
		return resolveSubjectError(err)
	}

	resources := []*proto.Resource{
		{Type: reversePublicationResourceType, Id: reversePublicationResourceID},
		// Preserve existing access while deployments migrate administrators to the
		// narrower reverse-publication permission.
		{Type: legacyAdminResourceType, Id: legacyAdminResourceID},
	}
	var authorizationFailed bool
	for _, resource := range resources {
		req := invocation.SubjectAccessRequest(subjectID, action, resource)
		allowed, checkErr := invocation.CheckSubjectAccess(ctx, s.authz, req)
		if checkErr != nil {
			authorizationFailed = true
			continue
		}
		if allowed {
			return nil
		}
	}
	if authorizationFailed {
		return status.Error(codes.Unavailable, "authorization provider unavailable")
	}
	return status.Error(codes.PermissionDenied, "access denied")
}

func (s *Service) storeNow() time.Time {
	if s == nil || s.store == nil {
		return time.Now().UTC().Truncate(time.Millisecond)
	}
	return s.store.Now()
}

func validateCreateRequest(req *proto.CreateRemoteRequest) error {
	tunnel := req.GetTunnel()
	if tunnel == nil {
		return status.Error(codes.InvalidArgument, "tunnel is required")
	}
	if strings.TrimSpace(tunnel.GetHost()) == "" {
		return status.Error(codes.InvalidArgument, "tunnel host is required")
	}
	if len(tunnel.GetCertificate()) == 0 {
		return status.Error(codes.InvalidArgument, "tunnel certificate is required")
	}
	if strings.TrimSpace(tunnel.GetServerSpkiSha256()) == "" {
		return status.Error(codes.InvalidArgument, "server_spki_sha256 is required")
	}
	if len(req.GetProviders()) == 0 {
		return status.Error(codes.InvalidArgument, "at least one provider is required")
	}
	return nil
}

func providerDefinitionsToCore(defs []*proto.RemoteProviderDefinition) ([]*coredata.RemoteProvider, error) {
	providers := make([]*coredata.RemoteProvider, 0, len(defs))
	seen := make(map[string]struct{}, len(defs))
	for _, def := range defs {
		kind := providermanifestv1.NormalizeKind(def.GetKind())
		name := strings.TrimSpace(def.GetName())
		if kind == "" || name == "" {
			return nil, status.Error(codes.InvalidArgument, "provider kind and name are required")
		}
		switch kind {
		case providermanifestv1.KindIdentity, providermanifestv1.KindAuthorization:
			return nil, status.Errorf(codes.InvalidArgument, "provider kind %q cannot be registered remotely", kind)
		}
		id := kind + "/" + name
		if _, ok := seen[id]; ok {
			return nil, status.Errorf(codes.InvalidArgument, "duplicate provider %s/%s", kind, name)
		}
		seen[id] = struct{}{}
		var definition map[string]any
		if st := def.GetDefinition(); st != nil {
			definition = st.AsMap()
		}
		if len(definition) == 0 {
			return nil, status.Errorf(codes.InvalidArgument, "provider definition is required for %s/%s", kind, name)
		}
		providers = append(providers, &coredata.RemoteProvider{
			ProviderKind: kind,
			ProviderName: name,
			Definition:   definition,
		})
	}
	return providers, nil
}

func coreProvidersToProtoDefinitions(providers []*coredata.RemoteProvider) []*proto.RemoteProviderDefinition {
	defs := make([]*proto.RemoteProviderDefinition, 0, len(providers))
	for _, p := range providers {
		var def *structpb.Struct
		if len(p.Definition) > 0 {
			def, _ = structpb.NewStruct(p.Definition)
		}
		defs = append(defs, &proto.RemoteProviderDefinition{
			Kind:       p.ProviderKind,
			Name:       p.ProviderName,
			Definition: def,
		})
	}
	return defs
}

func remoteToProto(reg *coredata.RemoteRegistration, defs []*proto.RemoteProviderDefinition) *proto.Remote {
	providers := make([]*proto.RemoteProviderSummary, 0, len(defs))
	for _, def := range defs {
		providers = append(providers, &proto.RemoteProviderSummary{
			Kind: def.GetKind(),
			Name: def.GetName(),
		})
	}
	out := &proto.Remote{
		Id:               reg.ID,
		OwnerSubjectId:   reg.OwnerSubjectID,
		Generation:       reg.Generation,
		Providers:        providers,
		ServerSpkiSha256: reg.ServerSPKISHA256,
		ConnectUrl:       reg.ConnectURL,
		CreatedAt:        timestamppb.New(reg.CreatedAt),
		UpdatedAt:        timestamppb.New(reg.UpdatedAt),
		LastError:        reg.LastError,
		LeaseExpiresAt:   timestamppb.New(reg.LeaseExpiresAt),
	}
	if reg.LastCheckedAt != nil {
		out.LastCheckedAt = timestamppb.New(*reg.LastCheckedAt)
	}
	if reg.LastSuccessfulHeartbeatAt != nil {
		out.LastSuccessfulHeartbeatAt = timestamppb.New(*reg.LastSuccessfulHeartbeatAt)
	}
	return out
}

func resolveSubjectError(err error) error {
	if errors.Is(err, principal.ErrCredentialSubjectRequired) {
		return status.Error(codes.Unauthenticated, "authenticated subject is required")
	}
	return status.Errorf(codes.Internal, "remote management: resolve credential subject: %v", err)
}

func mapStoreError(err error) error {
	switch {
	case errors.Is(err, coredata.ErrGenerationMismatch):
		return status.Error(codes.Aborted, "generation mismatch")
	case errors.Is(err, coredata.ErrProviderOwnedElsewhere):
		return status.Error(codes.AlreadyExists, "provider registered by another caller")
	case errors.Is(err, coredata.ErrNotRegistered):
		return status.Error(codes.NotFound, "unknown registration")
	default:
		return status.Errorf(codes.Internal, "remote management datastore: %v", err)
	}
}

var _ proto.RemoteManagementServer = (*Service)(nil)

func (s *Service) SetValidator(v EndpointValidator) {
	if s != nil {
		s.validate = v
	}
}

func (s *Service) AdvanceClock(d time.Duration) {
	if s == nil || s.store == nil {
		return
	}
	base := s.store.Now()
	s.store.SetClock(func() time.Time { return base.Add(d) })
}
