package pluginhost

import (
	"context"
	"fmt"
	"io"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type StaticProviderSpec struct {
	Name             string
	DisplayName      string
	Description      string
	IconSVG          string
	ConnectionMode   core.ConnectionMode
	Catalog          *catalog.Catalog
	AuthTypes        []string
	ConnectionParams map[string]core.ConnectionParamDef
	CredentialFields []core.CredentialFieldDef
	DiscoveryConfig  *core.DiscoveryConfig
}

type remoteProviderBase struct {
	client      proto.ProviderPluginClient
	name        string
	displayName string
	description string
	connection  core.ConnectionMode
	catalog     *catalog.Catalog
	iconSVG     string
	authTypes   []string
	connParams  map[string]core.ConnectionParamDef
	credFields  []core.CredentialFieldDef
	discovery   *core.DiscoveryConfig
	closer      io.Closer
}

// RemoteProviderOption configures a remote provider returned by NewRemoteProvider.
type RemoteProviderOption func(*remoteProviderBase)

// WithCloser attaches a closer that is called when the provider is closed.
// This is used to tie process lifecycle to provider lifecycle.
func WithCloser(c io.Closer) RemoteProviderOption {
	return func(b *remoteProviderBase) { b.closer = c }
}

func NewRemoteProvider(ctx context.Context, client proto.ProviderPluginClient, spec StaticProviderSpec, config map[string]any, opts ...RemoteProviderOption) (core.Provider, error) {
	supportsSessionCatalog, err := getSessionCatalogSupportWithRetry(ctx, client)
	if err != nil {
		return nil, err
	}
	if err := callStartProvider(ctx, client, spec.Name, config); err != nil {
		return nil, err
	}

	base := &remoteProviderBase{
		client:      client,
		name:        spec.Name,
		displayName: spec.DisplayName,
		description: spec.Description,
		connection:  spec.ConnectionMode,
		catalog:     spec.Catalog,
		iconSVG:     spec.IconSVG,
		authTypes:   spec.AuthTypes,
		connParams:  spec.ConnectionParams,
		credFields:  spec.CredentialFields,
		discovery:   spec.DiscoveryConfig,
	}
	for _, opt := range opts {
		opt(base)
	}

	if supportsSessionCatalog {
		return &remoteProviderWithSessionCatalog{remoteProviderBase: base}, nil
	}
	return base, nil
}

func getSessionCatalogSupportWithRetry(ctx context.Context, client proto.ProviderPluginClient) (bool, error) {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()

	for {
		meta, err := client.GetMetadata(ctx, &emptypb.Empty{})
		if err == nil {
			return meta.GetSupportsSessionCatalog(), nil
		}
		if status.Code(err) == codes.Unimplemented {
			return false, nil
		}
		if status.Code(err) != codes.Unavailable {
			return false, err
		}

		select {
		case <-ctx.Done():
			return false, err
		case <-ticker.C:
		}
	}
}

func (p *remoteProviderBase) Close() error {
	if p.closer != nil {
		return p.closer.Close()
	}
	return nil
}

func (p *remoteProviderBase) Name() string        { return p.name }
func (p *remoteProviderBase) DisplayName() string { return p.displayName }
func (p *remoteProviderBase) Description() string { return p.description }

func (p *remoteProviderBase) ConnectionMode() core.ConnectionMode {
	if p.connection == "" {
		return core.ConnectionModeUser
	}
	return p.connection
}

func (p *remoteProviderBase) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	msg, err := structFromMap(params)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Execute(ctx, &proto.ExecuteRequest{
		Operation:        operation,
		Params:           msg,
		Token:            token,
		ConnectionParams: core.ConnectionParams(ctx),
	})
	if err != nil {
		return nil, err
	}
	return &core.OperationResult{
		Status: int(resp.GetStatus()),
		Body:   resp.GetBody(),
	}, nil
}

func (p *remoteProviderBase) SupportsManualAuth() bool {
	for _, authType := range p.authTypes {
		if authType == "manual" {
			return true
		}
	}
	return false
}

func (p *remoteProviderBase) Catalog() *catalog.Catalog {
	return p.decorateCatalog(p.catalog)
}

func (p *remoteProviderBase) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return p.connParams
}

func (p *remoteProviderBase) AuthTypes() []string {
	return p.authTypes
}

func (p *remoteProviderBase) CredentialFields() []core.CredentialFieldDef {
	return p.credFields
}

func (p *remoteProviderBase) DiscoveryConfig() *core.DiscoveryConfig {
	return p.discovery
}

func (p *remoteProviderBase) sessionCatalog(ctx context.Context, token string) (*catalog.Catalog, error) {
	resp, err := p.client.GetSessionCatalog(ctx, &proto.GetSessionCatalogRequest{
		Token:            token,
		ConnectionParams: core.ConnectionParams(ctx),
	})
	if err != nil {
		return nil, err
	}
	cat, err := catalogFromJSON(resp.GetCatalogJson())
	if err != nil {
		return nil, err
	}
	return p.decorateCatalog(cat), nil
}

type remoteProviderWithSessionCatalog struct{ *remoteProviderBase }

func (p *remoteProviderWithSessionCatalog) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	return p.sessionCatalog(ctx, token)
}

func (p *remoteProviderBase) decorateCatalog(cat *catalog.Catalog) *catalog.Catalog {
	if cat == nil {
		return nil
	}
	decorated := cat.Clone()
	if decorated.Name == "" {
		decorated.Name = p.name
	}
	if decorated.DisplayName == "" {
		decorated.DisplayName = p.displayName
	}
	if decorated.Description == "" {
		decorated.Description = p.description
	}
	if p.iconSVG != "" {
		decorated.IconSVG = p.iconSVG
	}
	for i := range decorated.Operations {
		if decorated.Operations[i].Transport == "" {
			decorated.Operations[i].Transport = catalog.TransportPlugin
		}
	}
	return decorated
}

func callStartProvider(ctx context.Context, client proto.ProviderPluginClient, name string, config map[string]any) error {
	cfgStruct, err := structFromMap(config)
	if err != nil {
		return fmt.Errorf("encode provider config: %w", err)
	}
	resp, err := client.StartProvider(ctx, &proto.StartProviderRequest{
		Name:            name,
		Config:          cfgStruct,
		ProtocolVersion: proto.CurrentProtocolVersion,
	})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return nil
		}
		return fmt.Errorf("start provider: %w", err)
	}
	if v := resp.GetProtocolVersion(); v != proto.CurrentProtocolVersion {
		return fmt.Errorf("plugin responded with protocol version %d, host requires %d",
			v, proto.CurrentProtocolVersion)
	}
	return nil
}
