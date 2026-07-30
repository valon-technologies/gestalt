package remotepublish

import (
	"context"
	"strings"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	appservice "github.com/valon-technologies/gestalt/server/services/apps"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
)

// TunnelAppProviderServer implements proto.AppProviderServer over the reverse
// tunnel. Unlike appservice.ProviderServer (which wraps a single provider),
// this server dispatches to a provider in the local ProviderMap by reading the
// target app name from the x-gestalt-app gRPC metadata key. The remote
// gestaltd sets that metadata when it dials the tunnel to forward an
// invocation for a tunnel-registered app.
//
// StartProvider returns Unimplemented because the local provider is already
// running — apps.NewRemote handles this by skipping the start call.
//
// ProviderServer instances are cached per app name to avoid per-RPC allocation.
type TunnelAppProviderServer struct {
	proto.UnimplementedAppProviderServer
	providers *registry.ProviderMap[core.Provider]

	mu      sync.Mutex
	servers map[string]proto.AppProviderServer
}

func NewTunnelAppProviderServer(providers *registry.ProviderMap[core.Provider]) *TunnelAppProviderServer {
	return &TunnelAppProviderServer{
		providers: providers,
		servers:   make(map[string]proto.AppProviderServer),
	}
}

// tunnelAppMetadataKey is the gRPC metadata key carrying the target app name.
const tunnelAppMetadataKey = "x-gestalt-app"

func appFromContext(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.InvalidArgument, "app metadata is required")
	}
	values := md.Get(tunnelAppMetadataKey)
	if len(values) == 0 || strings.TrimSpace(values[0]) == "" {
		return "", status.Error(codes.InvalidArgument, "app metadata is required")
	}
	return strings.TrimSpace(values[0]), nil
}

// resolveServer looks up the provider for the app named in the metadata and
// returns a cached AppProviderServer wrapping it. The cache avoids
// reconstructing a ProviderServer on every RPC.
func (s *TunnelAppProviderServer) resolveServer(ctx context.Context) (proto.AppProviderServer, error) {
	appName, err := appFromContext(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if srv, ok := s.servers[appName]; ok {
		s.mu.Unlock()
		return srv, nil
	}
	s.mu.Unlock()

	prov, err := s.providers.GetWithContext(ctx, appName)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "app %q not found", appName)
	}
	srv := appservice.NewProviderServer(prov)

	s.mu.Lock()
	// Another goroutine may have created the same entry; prefer the existing
	// one to avoid a orphaned server.
	if existing, ok := s.servers[appName]; ok {
		s.mu.Unlock()
		return existing, nil
	}
	s.servers[appName] = srv
	s.mu.Unlock()

	return srv, nil
}

func (s *TunnelAppProviderServer) GetMetadata(ctx context.Context, _ *emptypb.Empty) (*proto.ProviderMetadata, error) {
	srv, err := s.resolveServer(ctx)
	if err != nil {
		return nil, err
	}
	return srv.GetMetadata(ctx, &emptypb.Empty{})
}

// StartProvider returns Unimplemented because the local provider is already
// running. apps.NewRemote handles this gracefully by skipping the start call.
func (s *TunnelAppProviderServer) StartProvider(_ context.Context, _ *proto.StartProviderRequest) (*proto.StartProviderResponse, error) {
	return nil, status.Error(codes.Unimplemented, "start provider is not supported over tunnel")
}

func (s *TunnelAppProviderServer) Execute(ctx context.Context, req *proto.ExecuteRequest) (*proto.OperationResult, error) {
	srv, err := s.resolveServer(ctx)
	if err != nil {
		return nil, err
	}
	return srv.Execute(ctx, req)
}

func (s *TunnelAppProviderServer) ExecuteStream(req *proto.ExecuteRequest, stream proto.AppProvider_ExecuteStreamServer) error {
	srv, err := s.resolveServer(stream.Context())
	if err != nil {
		return err
	}
	return srv.ExecuteStream(req, stream)
}

func (s *TunnelAppProviderServer) ResolveHTTPSubject(ctx context.Context, req *proto.ResolveHTTPSubjectRequest) (*proto.ResolveHTTPSubjectResponse, error) {
	srv, err := s.resolveServer(ctx)
	if err != nil {
		return nil, err
	}
	return srv.ResolveHTTPSubject(ctx, req)
}

func (s *TunnelAppProviderServer) GetSessionCatalog(ctx context.Context, req *proto.GetSessionCatalogRequest) (*proto.GetSessionCatalogResponse, error) {
	srv, err := s.resolveServer(ctx)
	if err != nil {
		return nil, err
	}
	return srv.GetSessionCatalog(ctx, req)
}

var _ proto.AppProviderServer = (*TunnelAppProviderServer)(nil)
