package remotepublish

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/protoutil"
	"github.com/valon-technologies/gestalt/server/internal/tunnel"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	appservice "github.com/valon-technologies/gestalt/server/services/apps"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

// TunnelProxyConfig holds the parameters needed to build a tunnel proxy
// provider that forwards app invocations through a reverse tunnel.
type TunnelProxyConfig struct {
	AppName        string
	TunnelHost     string
	PinnedSPKI     string
	ConnectAddr    string
	ClientIdentity tls.Certificate
}

// TunnelProxyProvider is a core.Provider that forwards operations through a
// reverse tunnel to the registering machine. It dials the tunnel host via
// tunnel.NewDialer and calls the AppProvider gRPC service on the remote side.
// The app name is sent as gRPC metadata so the tunnel's
// TunnelAppProviderServer can dispatch to the correct local provider.
type TunnelProxyProvider struct {
	appName string
	client  proto.AppProviderClient
	conn    *grpc.ClientConn

	displayName     string
	description     string
	connectionMode  core.ConnectionMode
	catalog         *catalog.Catalog
	authTypes       []string
	connParams      map[string]core.ConnectionParamDef
	supportsSession bool
}

// NewTunnelProxyProvider dials the tunnel and fetches provider metadata. The
// provider is ready to forward invocations after this returns.
func NewTunnelProxyProvider(ctx context.Context, cfg TunnelProxyConfig) (*TunnelProxyProvider, error) {
	appName := strings.TrimSpace(cfg.AppName)
	if appName == "" {
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

	client := proto.NewAppProviderClient(conn)
	provider := &TunnelProxyProvider{
		appName: appName,
		client:  &metadataAppProviderClient{inner: client, appName: appName},
		conn:    conn,
	}

	metaCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	meta, err := provider.client.GetMetadata(metaCtx, &emptypb.Empty{})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("tunnel proxy: get metadata for %s: %w", appName, err)
	}

	provider.displayName = strings.TrimSpace(meta.GetDisplayName())
	provider.description = strings.TrimSpace(meta.GetDescription())
	provider.connectionMode = core.ConnectionMode(meta.GetConnectionMode())
	provider.supportsSession = meta.GetSupportsSessionCatalog()
	if meta.GetStaticCatalog() != nil {
		if cat, cerr := appservice.CatalogFromProto(meta.GetStaticCatalog()); cerr == nil {
			provider.catalog = cat
		}
	}
	provider.authTypes = append([]string(nil), meta.GetAuthTypes()...)
	if len(meta.GetConnectionParams()) > 0 {
		provider.connParams = make(map[string]core.ConnectionParamDef, len(meta.GetConnectionParams()))
		for name, def := range meta.GetConnectionParams() {
			provider.connParams[name] = core.ConnectionParamDef{
				Required:     def.GetRequired(),
				Description:  def.GetDescription(),
				Default:      def.GetDefaultValue(),
				From:         def.GetFrom(),
				Field:        def.GetField(),
			}
		}
	}

	return provider, nil
}

func (p *TunnelProxyProvider) Close() error {
	if p.conn != nil {
		return p.conn.Close()
	}
	return nil
}

func (p *TunnelProxyProvider) Name() string { return p.appName }
func (p *TunnelProxyProvider) DisplayName() string {
	if p.displayName != "" {
		return p.displayName
	}
	return p.appName
}
func (p *TunnelProxyProvider) Description() string { return p.description }

func (p *TunnelProxyProvider) ConnectionMode() core.ConnectionMode {
	if p.connectionMode == "" {
		return core.ConnectionModeSubject
	}
	return core.NormalizeConnectionMode(p.connectionMode)
}

func (p *TunnelProxyProvider) AuthTypes() []string { return p.authTypes }

func (p *TunnelProxyProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return p.connParams
}

func (p *TunnelProxyProvider) CredentialFields() []core.CredentialFieldDef {
	return nil
}

func (p *TunnelProxyProvider) DiscoveryConfig() *core.DiscoveryConfig {
	return nil
}

func (p *TunnelProxyProvider) ConnectionForOperation(string) string { return "" }

func (p *TunnelProxyProvider) Catalog() *catalog.Catalog {
	if p.catalog == nil {
		return nil
	}
	decorated := p.catalog.Clone()
	if decorated.Name == "" {
		decorated.Name = p.appName
	}
	if decorated.DisplayName == "" {
		decorated.DisplayName = p.DisplayName()
	}
	for i := range decorated.Operations {
		if decorated.Operations[i].Transport == "" {
			decorated.Operations[i].Transport = catalog.TransportApp
		}
	}
	return decorated
}

func (p *TunnelProxyProvider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	msg, err := protoutil.StructFromMap(params)
	if err != nil {
		return nil, err
	}
	reqCtx, err := p.requestContextProto(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Execute(ctx, &proto.ExecuteRequest{
		Operation:        operation,
		Params:           msg,
		Token:            token,
		ConnectionParams: core.ConnectionParams(ctx),
		InvocationId:     invocationIDFromContext(ctx),
		IdempotencyKey:   invocation.IdempotencyKeyFromContext(ctx),
		Context:          reqCtx,
	})
	if err != nil {
		return nil, appservice.RemoteProviderExecuteError(err)
	}
	return &core.OperationResult{
		Status:  int(resp.GetStatus()),
		Headers: protoutil.StringListsFromProto(resp.GetHeaders()),
		Body:    resp.GetBody(),
	}, nil
}

func (p *TunnelProxyProvider) ExecuteStream(ctx context.Context, operation string, params map[string]any, token string) (core.StreamReader, error) {
	msg, err := protoutil.StructFromMap(params)
	if err != nil {
		return nil, err
	}
	reqCtx, err := p.requestContextProto(ctx)
	if err != nil {
		return nil, err
	}
	stream, err := p.client.ExecuteStream(ctx, &proto.ExecuteRequest{
		Operation:        operation,
		Params:           msg,
		Token:            token,
		ConnectionParams: core.ConnectionParams(ctx),
		InvocationId:     invocationIDFromContext(ctx),
		IdempotencyKey:   invocation.IdempotencyKeyFromContext(ctx),
		Context:          reqCtx,
	})
	if err != nil {
		return nil, appservice.RemoteProviderExecuteError(err)
	}
	return &tunnelStreamReader{stream: stream}, nil
}

func (p *TunnelProxyProvider) requestContextProto(ctx context.Context) (*proto.RequestContext, error) {
	return appaccessservice.RequestContextProto(ctx, "", invocation.CallerProvider{})
}

// metadataAppProviderClient wraps an AppProviderClient to inject the
// x-gestalt-app metadata key on every call, so the tunnel's
// TunnelAppProviderServer can dispatch to the correct local provider.
type metadataAppProviderClient struct {
	inner   proto.AppProviderClient
	appName string
}

func (c *metadataAppProviderClient) withApp(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx, tunnelAppMetadataKey, c.appName)
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

type tunnelStreamReader struct {
	stream proto.AppProvider_ExecuteStreamClient
}

func (r *tunnelStreamReader) Recv() (*core.InvokeFrame, error) {
	frame, err := r.stream.Recv()
	if err != nil {
		return nil, err
	}
	return tunnelInvokeFrameFromProto(frame), nil
}

func tunnelInvokeFrameFromProto(frame *proto.InvokeFrame) *core.InvokeFrame {
	if frame == nil {
		return &core.InvokeFrame{Metadata: &core.InvokeMetadata{Status: 500, MediaType: "application/json"}, Data: []byte(`{"error":"received nil stream frame"}`)}
	}
	switch v := frame.GetValue().(type) {
	case *proto.InvokeFrame_Metadata:
		return &core.InvokeFrame{
			Metadata: &core.InvokeMetadata{
				Status:    int(v.Metadata.GetStatus()),
				Headers:   protoutil.StringListsFromProto(v.Metadata.GetHeaders()),
				MediaType: v.Metadata.GetMediaType(),
			},
		}
	case *proto.InvokeFrame_Data:
		return &core.InvokeFrame{Data: append([]byte(nil), v.Data...)}
	}
	return &core.InvokeFrame{Metadata: &core.InvokeMetadata{Status: 500, MediaType: "application/json"}, Data: []byte(`{"error":"stream frame has no value set"}`)}
}

func invocationIDFromContext(ctx context.Context) string {
	meta := invocation.MetaFromContext(ctx)
	if meta == nil {
		return ""
	}
	return meta.RequestID
}

var _ core.Provider = (*TunnelProxyProvider)(nil)
var _ core.StreamingExecutor = (*TunnelProxyProvider)(nil)
