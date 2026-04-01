package pluginhost

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/integration"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type remoteProviderBase struct {
	client      proto.ProviderPluginClient
	metadata    *proto.ProviderMetadata
	catalog     *catalog.Catalog
	iconSVG     string
	displayOver string
	descOver    string
	closer      io.Closer
}

// RemoteProviderOption configures a remote provider returned by NewRemoteProvider.
type RemoteProviderOption func(*remoteProviderBase)

// WithCloser attaches a closer that is called when the provider is closed.
// This is used to tie process lifecycle to provider lifecycle.
func WithCloser(c io.Closer) RemoteProviderOption {
	return func(b *remoteProviderBase) { b.closer = c }
}

func WithMetadataOverrides(displayName, description, iconSVG string) RemoteProviderOption {
	return func(b *remoteProviderBase) {
		if displayName != "" {
			b.displayOver = displayName
		}
		if description != "" {
			b.descOver = description
		}
		if iconSVG != "" {
			b.iconSVG = iconSVG
		}
	}
}

func (p *remoteProviderBase) Close() error {
	if p.closer != nil {
		return p.closer.Close()
	}
	return nil
}

func NewRemoteProvider(ctx context.Context, client proto.ProviderPluginClient, name string, config map[string]any, opts ...RemoteProviderOption) (core.Provider, error) {
	meta, err := client.GetMetadata(ctx, &emptypb.Empty{})
	if err != nil {
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
	ops := operationsFromProto(opsResp.GetOperations())

	base := &remoteProviderBase{
		client:   client,
		metadata: meta,
		catalog:  buildRemoteCatalog(meta, staticCatalog, ops),
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

func (p *remoteProviderBase) DisplayName() string {
	if p.displayOver != "" {
		return p.displayOver
	}
	return p.metadata.GetDisplayName()
}

func (p *remoteProviderBase) Description() string {
	if p.descOver != "" {
		return p.descOver
	}
	return p.metadata.GetDescription()
}

func (p *remoteProviderBase) ConnectionMode() core.ConnectionMode {
	return protoConnectionModeToCore(p.metadata.GetConnectionMode())
}

func (p *remoteProviderBase) ListOperations() []core.Operation {
	return integration.OperationsList(p.catalog)
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
	return slices.Contains(p.metadata.GetAuthTypes(), "manual")
}

func (p *remoteProviderBase) Catalog() *catalog.Catalog {
	if p.catalog == nil {
		if p.iconSVG == "" {
			return nil
		}
		return p.decorateCatalog(&catalog.Catalog{Name: p.metadata.GetName()})
	}
	return p.decorateCatalog(p.catalog)
}

func (p *remoteProviderBase) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return connectionParamDefsFromProto(p.metadata.GetConnectionParams())
}

func (p *remoteProviderBase) AuthTypes() []string {
	return slices.Clone(p.metadata.GetAuthTypes())
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

func buildRemoteCatalog(meta *proto.ProviderMetadata, staticCatalog *catalog.Catalog, ops []core.Operation) *catalog.Catalog {
	if staticCatalog == nil && len(ops) == 0 {
		return nil
	}

	var cat *catalog.Catalog
	if staticCatalog != nil {
		cat = staticCatalog.Clone()
	} else {
		cat = &catalog.Catalog{
			Name:        meta.GetName(),
			DisplayName: meta.GetDisplayName(),
			Description: meta.GetDescription(),
			Operations:  make([]catalog.CatalogOperation, 0, len(ops)),
		}
	}
	if cat.Name == "" {
		cat.Name = meta.GetName()
	}
	if cat.DisplayName == "" {
		cat.DisplayName = meta.GetDisplayName()
	}
	if cat.Description == "" {
		cat.Description = meta.GetDescription()
	}

	for _, op := range ops {
		catOp := ensureCatalogOperation(cat, op)
		if catOp.Description == "" {
			catOp.Description = op.Description
		}
		if catOp.Method == "" {
			catOp.Method = op.Method
		}
		if catOp.Transport == "" {
			catOp.Transport = catalog.TransportPlugin
		}
		catOp.Parameters = mergeCatalogParameters(catOp.Parameters, op.Parameters)
	}

	integration.CompileSchemas(cat)
	return cat
}

func ensureCatalogOperation(cat *catalog.Catalog, op core.Operation) *catalog.CatalogOperation {
	for i := range cat.Operations {
		if cat.Operations[i].ID == op.Name {
			return &cat.Operations[i]
		}
	}
	cat.Operations = append(cat.Operations, catalog.CatalogOperation{
		ID:          op.Name,
		Method:      op.Method,
		Description: op.Description,
		Transport:   catalog.TransportPlugin,
	})
	return &cat.Operations[len(cat.Operations)-1]
}

func mergeCatalogParameters(existing []catalog.CatalogParameter, params []core.Parameter) []catalog.CatalogParameter {
	if len(params) == 0 {
		return existing
	}
	if len(existing) == 0 {
		return catalogParametersFromCore(params)
	}

	indexByName := make(map[string]int, len(existing))
	for i := range existing {
		indexByName[existing[i].Name] = i
	}

	for _, param := range params {
		if idx, ok := indexByName[param.Name]; ok {
			if existing[idx].Type == "" {
				existing[idx].Type = param.Type
			}
			if existing[idx].Description == "" {
				existing[idx].Description = param.Description
			}
			if !existing[idx].Required {
				existing[idx].Required = param.Required
			}
			if existing[idx].Default == nil && param.Default != nil {
				existing[idx].Default = param.Default
			}
			continue
		}
		existing = append(existing, catalog.CatalogParameter{
			Name:        param.Name,
			Type:        param.Type,
			Description: param.Description,
			Required:    param.Required,
			Default:     param.Default,
		})
	}

	return existing
}

func catalogParametersFromCore(params []core.Parameter) []catalog.CatalogParameter {
	out := make([]catalog.CatalogParameter, 0, len(params))
	for _, param := range params {
		out = append(out, catalog.CatalogParameter{
			Name:        param.Name,
			Type:        param.Type,
			Description: param.Description,
			Required:    param.Required,
			Default:     param.Default,
		})
	}
	return out
}

func (p *remoteProviderBase) decorateCatalog(cat *catalog.Catalog) *catalog.Catalog {
	if cat == nil {
		return nil
	}
	decorated := cat.Clone()
	if decorated.DisplayName == "" || p.displayOver != "" {
		decorated.DisplayName = p.DisplayName()
	}
	if decorated.Description == "" || p.descOver != "" {
		decorated.Description = p.Description()
	}
	if p.iconSVG != "" {
		decorated.IconSVG = p.iconSVG
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
