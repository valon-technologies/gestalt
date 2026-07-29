package remotepublish

import (
	"context"
	"strings"

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
type TunnelAppProviderServer struct {
	proto.UnimplementedAppProviderServer
	providers *registry.ProviderMap[core.Provider]
}

func NewTunnelAppProviderServer(providers *registry.ProviderMap[core.Provider]) *TunnelAppProviderServer {
	return &TunnelAppProviderServer{providers: providers}
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

func (s *TunnelAppProviderServer) resolve(ctx context.Context) (core.Provider, error) {
	appName, err := appFromContext(ctx)
	if err != nil {
		return nil, err
	}
	prov, err := s.providers.Get(appName)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "app %q not found", appName)
	}
	return prov, nil
}

func (s *TunnelAppProviderServer) GetMetadata(ctx context.Context, _ *emptypb.Empty) (*proto.ProviderMetadata, error) {
	prov, err := s.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return appservice.NewProviderServer(prov).GetMetadata(ctx, &emptypb.Empty{})
}

func (s *TunnelAppProviderServer) StartProvider(ctx context.Context, req *proto.StartProviderRequest) (*proto.StartProviderResponse, error) {
	prov, err := s.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return appservice.NewProviderServer(prov).StartProvider(ctx, req)
}

func (s *TunnelAppProviderServer) Execute(ctx context.Context, req *proto.ExecuteRequest) (*proto.OperationResult, error) {
	prov, err := s.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return appservice.NewProviderServer(prov).Execute(ctx, req)
}

func (s *TunnelAppProviderServer) ExecuteStream(req *proto.ExecuteRequest, stream proto.AppProvider_ExecuteStreamServer) error {
	prov, err := s.resolve(stream.Context())
	if err != nil {
		return err
	}
	return appservice.NewProviderServer(prov).ExecuteStream(req, stream)
}

func (s *TunnelAppProviderServer) ResolveHTTPSubject(ctx context.Context, req *proto.ResolveHTTPSubjectRequest) (*proto.ResolveHTTPSubjectResponse, error) {
	prov, err := s.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return appservice.NewProviderServer(prov).ResolveHTTPSubject(ctx, req)
}

func (s *TunnelAppProviderServer) GetSessionCatalog(ctx context.Context, req *proto.GetSessionCatalogRequest) (*proto.GetSessionCatalogResponse, error) {
	prov, err := s.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return appservice.NewProviderServer(prov).GetSessionCatalog(ctx, req)
}

var _ proto.AppProviderServer = (*TunnelAppProviderServer)(nil)
