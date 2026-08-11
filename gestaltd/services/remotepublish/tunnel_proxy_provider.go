package remotepublish

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/tunnel"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	appservice "github.com/valon-technologies/gestalt/server/services/apps"
	"github.com/valon-technologies/gestalt/server/services/egress"
)

// TunnelProxyConfig holds the parameters needed to build a tunnel proxy
// provider that forwards app invocations through a reverse tunnel.
type TunnelProxyConfig struct {
	AppName        string
	StaticHeaders  map[string]string
	TunnelHost     string
	PinnedSPKI     string
	ConnectAddr    string
	ClientIdentity tls.Certificate
}

// NewTunnelProxyProvider dials the tunnel and builds a core.Provider that
// forwards operations to the registering machine. It composes apps.NewRemote
// so the provider inherits session catalog support, HTTP subject resolution,
// workflow definitions, invocation capability behavior, and error mapping from
// the shared remoteProviderBase — no duplicated logic.
//
// The app name is injected as x-gestalt-app gRPC metadata on every call so the
// tunnel's TunnelAppProviderServer can dispatch to the correct local provider.
// StartProvider is skipped because the local provider is already running; the
// tunnel server returns Unimplemented for that RPC.
func NewTunnelProxyProvider(ctx context.Context, cfg TunnelProxyConfig) (core.Provider, error) {
	name := strings.TrimSpace(cfg.AppName)
	if name == "" {
		return nil, fmt.Errorf("tunnel proxy: app name is required")
	}

	dialer := tunnel.NewDialer(tunnel.DialerConfig{
		ConnectAddr:    cfg.ConnectAddr,
		TunnelHost:     cfg.TunnelHost,
		PinnedSPKI:     cfg.PinnedSPKI,
		ClientIdentity: cfg.ClientIdentity,
	})

	conn, err := grpc.NewClient("passthrough://tunnel",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp", "")
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("tunnel proxy: dial tunnel: %w", err)
	}

	client := &metadataAppProviderClient{
		inner:   proto.NewAppProviderClient(conn),
		appName: name,
	}

	provider, err := appservice.NewRemote(ctx, client, appservice.StaticProviderSpec{
		Name:          name,
		StaticHeaders: cfg.StaticHeaders,
	}, nil, appservice.WithCloser(conn))
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("tunnel proxy: build remote provider for %s: %w", name, err)
	}

	return provider, nil
}

// StaticHeadersFromDefinition returns the header names a published provider
// declared as overridable. Values are intentionally omitted from publication
// metadata because only the names are needed for invocation validation.
func StaticHeadersFromDefinition(definition map[string]any) map[string]string {
	raw, ok := definition["staticHeaders"]
	if !ok {
		return nil
	}
	var names []string
	switch values := raw.(type) {
	case []string:
		names = values
	case []any:
		names = make([]string, 0, len(values))
		for _, value := range values {
			if name, ok := value.(string); ok {
				names = append(names, name)
			}
		}
	}
	headers := make(map[string]string, len(names))
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			headers[name] = ""
		}
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}

// metadataAppProviderClient wraps an AppProviderClient to inject the
// x-gestalt-app metadata key on every call, so the tunnel's
// TunnelAppProviderServer can dispatch to the correct local provider.
// GetMetadata and StartProvider do not need the app metadata (GetMetadata
// fetches static metadata, StartProvider is Unimplemented on the tunnel).
type metadataAppProviderClient struct {
	inner   proto.AppProviderClient
	appName string
}

func (c *metadataAppProviderClient) withApp(ctx context.Context) context.Context {
	pairs := []string{tunnelAppMetadataKey, c.appName}
	if overrides := egress.OutboundHeaderOverridesFromContext(ctx); len(overrides) > 0 {
		if encoded, err := json.Marshal(overrides); err == nil {
			pairs = append(pairs, tunnelHeaderOverridesMetadataKey, string(encoded))
		}
	}
	return metadata.AppendToOutgoingContext(ctx, pairs...)
}

func (c *metadataAppProviderClient) GetMetadata(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*proto.ProviderMetadata, error) {
	return c.inner.GetMetadata(c.withApp(ctx), in, opts...)
}

func (c *metadataAppProviderClient) StartProvider(ctx context.Context, in *proto.StartProviderRequest, opts ...grpc.CallOption) (*proto.StartProviderResponse, error) {
	return c.inner.StartProvider(c.withApp(ctx), in, opts...)
}

func (c *metadataAppProviderClient) Execute(ctx context.Context, in *proto.ExecuteRequest, opts ...grpc.CallOption) (*proto.OperationResult, error) {
	return c.inner.Execute(c.withApp(ctx), in, opts...)
}

func (c *metadataAppProviderClient) ExecuteStream(ctx context.Context, in *proto.ExecuteRequest, opts ...grpc.CallOption) (proto.AppProvider_ExecuteStreamClient, error) {
	return c.inner.ExecuteStream(c.withApp(ctx), in, opts...)
}

func (c *metadataAppProviderClient) ResolveHTTPSubject(ctx context.Context, in *proto.ResolveHTTPSubjectRequest, opts ...grpc.CallOption) (*proto.ResolveHTTPSubjectResponse, error) {
	return c.inner.ResolveHTTPSubject(c.withApp(ctx), in, opts...)
}

func (c *metadataAppProviderClient) GetSessionCatalog(ctx context.Context, in *proto.GetSessionCatalogRequest, opts ...grpc.CallOption) (*proto.GetSessionCatalogResponse, error) {
	return c.inner.GetSessionCatalog(c.withApp(ctx), in, opts...)
}

var _ proto.AppProviderClient = (*metadataAppProviderClient)(nil)
