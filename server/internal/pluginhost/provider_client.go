package pluginhost

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"

	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginsdk/proto/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type remoteProviderBase struct {
	client   pluginapiv1.ProviderPluginClient
	metadata *pluginapiv1.ProviderMetadata
	ops      []core.Operation
	catalog  *catalog.Catalog
	iconSVG  string
	closer   io.Closer
}

// RemoteProviderOption configures a remote provider returned by NewRemoteProvider.
type RemoteProviderOption func(*remoteProviderBase)

// WithCloser attaches a closer that is called when the provider is closed.
// This is used to tie process lifecycle to provider lifecycle.
func WithCloser(c io.Closer) RemoteProviderOption {
	return func(b *remoteProviderBase) { b.closer = c }
}

func (p *remoteProviderBase) Close() error {
	if p.closer != nil {
		return p.closer.Close()
	}
	return nil
}

func NewRemoteProvider(ctx context.Context, client pluginapiv1.ProviderPluginClient, name string, config map[string]any, opts ...RemoteProviderOption) (core.Provider, error) {
	meta, err := client.GetMetadata(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	if err := checkProtocolCompatibility(meta); err != nil {
		return nil, err
	}
	if schemaJSON := meta.GetConfigSchemaJson(); schemaJSON != "" {
		slog.Warn("validating plugin config requires executing plugin binary", "plugin", name)
		validationTarget := config
		if validationTarget == nil {
			validationTarget = map[string]any{}
		}
		if err := validateConfigSchema(validationTarget, schemaJSON); err != nil {
			return nil, err
		}
	}
	if err := callStartProvider(ctx, client, name, config); err != nil {
		return nil, err
	}
	opsResp, err := client.ListOperations(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	staticCatalog, err := catalogFromJSON(meta.GetStaticCatalogJson())
	if err != nil {
		return nil, err
	}

	base := &remoteProviderBase{
		client:   client,
		metadata: meta,
		ops:      operationsFromProto(opsResp.GetOperations()),
		catalog:  staticCatalog,
	}
	for _, opt := range opts {
		opt(base)
	}

	if meta.GetSupportsSessionCatalog() {
		return &remoteProviderWithSessionCatalog{remoteProviderBase: base}, nil
	}
	return base, nil
}

func (p *remoteProviderBase) Name() string { return p.metadata.GetName() }

func (p *remoteProviderBase) DisplayName() string { return p.metadata.GetDisplayName() }

func (p *remoteProviderBase) Description() string { return p.metadata.GetDescription() }

func (p *remoteProviderBase) ConnectionMode() core.ConnectionMode {
	return protoConnectionModeToCore(p.metadata.GetConnectionMode())
}

func (p *remoteProviderBase) ListOperations() []core.Operation {
	out := make([]core.Operation, len(p.ops))
	copy(out, p.ops)
	return out
}

func (p *remoteProviderBase) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	msg, err := structFromMap(params)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Execute(ctx, &pluginapiv1.ExecuteRequest{
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
	return slices.Contains(p.metadata.GetAuthTypes(), "manual")
}

func (p *remoteProviderBase) SetIconSVG(svg string) { p.iconSVG = svg }

func (p *remoteProviderBase) Catalog() *catalog.Catalog {
	if p.catalog == nil {
		if p.iconSVG != "" {
			return &catalog.Catalog{IconSVG: p.iconSVG}
		}
		return nil
	}
	cat := p.catalog.Clone()
	if cat.IconSVG == "" && p.iconSVG != "" {
		cat.IconSVG = p.iconSVG
	}
	return cat
}

func (p *remoteProviderBase) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return connectionParamDefsFromProto(p.metadata.GetConnectionParams())
}

func (p *remoteProviderBase) AuthTypes() []string {
	return slices.Clone(p.metadata.GetAuthTypes())
}

func (p *remoteProviderBase) sessionCatalog(ctx context.Context, token string) (*catalog.Catalog, error) {
	resp, err := p.client.GetSessionCatalog(ctx, &pluginapiv1.GetSessionCatalogRequest{
		Token:            token,
		ConnectionParams: core.ConnectionParams(ctx),
	})
	if err != nil {
		return nil, err
	}
	return catalogFromJSON(resp.GetCatalogJson())
}

type remoteProviderWithSessionCatalog struct{ *remoteProviderBase }

func (p *remoteProviderWithSessionCatalog) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	return p.sessionCatalog(ctx, token)
}

func checkProtocolCompatibility(meta *pluginapiv1.ProviderMetadata) error {
	minV := meta.GetMinProtocolVersion()
	maxV := meta.GetMaxProtocolVersion()
	if minV == 0 && maxV == 0 {
		return nil
	}
	if maxV == 0 {
		if pluginapiv1.CurrentProtocolVersion < minV {
			return fmt.Errorf("plugin requires protocol version %d+, host speaks %d",
				minV, pluginapiv1.CurrentProtocolVersion)
		}
		return nil
	}
	if pluginapiv1.CurrentProtocolVersion < minV || pluginapiv1.CurrentProtocolVersion > maxV {
		return fmt.Errorf("plugin requires protocol version %d-%d, host speaks %d",
			minV, maxV, pluginapiv1.CurrentProtocolVersion)
	}
	return nil
}

func callStartProvider(ctx context.Context, client pluginapiv1.ProviderPluginClient, name string, config map[string]any) error {
	cfgStruct, err := structFromMap(config)
	if err != nil {
		return fmt.Errorf("encode provider config: %w", err)
	}
	resp, err := client.StartProvider(ctx, &pluginapiv1.StartProviderRequest{
		Name:            name,
		Config:          cfgStruct,
		ProtocolVersion: pluginapiv1.CurrentProtocolVersion,
	})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return nil
		}
		return fmt.Errorf("start provider: %w", err)
	}
	if v := resp.GetProtocolVersion(); v != pluginapiv1.CurrentProtocolVersion {
		return fmt.Errorf("plugin responded with protocol version %d, host requires %d",
			v, pluginapiv1.CurrentProtocolVersion)
	}
	return nil
}
