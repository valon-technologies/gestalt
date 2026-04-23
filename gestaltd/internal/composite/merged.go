package composite

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/integration"
)

type MergedProvider struct {
	catalog  *catalog.Catalog
	connMode core.ConnectionMode
	opConn   map[string]string
	route    map[string]core.Provider
	owned    []core.Provider
}

type BoundProvider struct {
	Provider   core.Provider
	Connection string
}

var (
	_ core.Provider               = (*MergedProvider)(nil)
	_ core.GraphQLSurfaceInvoker  = (*MergedProvider)(nil)
	_ core.SessionCatalogProvider = (*MergedProvider)(nil)
)

func NewMergedWithConnections(name, displayName, desc, iconSVG string, providers ...BoundProvider) (*MergedProvider, error) {
	owned := make([]core.Provider, len(providers))
	for i, p := range providers {
		owned[i] = p.Provider
	}
	m := &MergedProvider{
		catalog: &catalog.Catalog{
			Name:        name,
			DisplayName: displayName,
			Description: desc,
			IconSVG:     iconSVG,
			Operations:  make([]catalog.CatalogOperation, 0),
		},
		opConn: make(map[string]string),
		route:  make(map[string]core.Provider),
		owned:  owned,
	}
	for i, bound := range providers {
		p := bound.Provider
		if i == 0 {
			m.connMode = p.ConnectionMode()
		} else {
			m.connMode = stricterConnectionMode(m.connMode, p.ConnectionMode())
		}
		cat := p.Catalog()
		if cat == nil {
			continue
		}
		for j := range cat.Operations {
			op := &cat.Operations[j]
			if owner, exists := m.route[op.ID]; exists {
				return nil, fmt.Errorf("operation %q provided by both %q and %q", op.ID, owner.Name(), p.Name())
			}
			m.route[op.ID] = p
			m.catalog.Operations = append(m.catalog.Operations, *op)
			switch {
			case bound.Connection != "":
				m.opConn[op.ID] = bound.Connection
			case p.ConnectionForOperation(op.ID) != "":
				m.opConn[op.ID] = p.ConnectionForOperation(op.ID)
			}
		}
	}
	integration.CompileSchemas(m.catalog)
	return m, nil
}

func (m *MergedProvider) Name() string                        { return m.catalog.Name }
func (m *MergedProvider) DisplayName() string                 { return m.catalog.DisplayName }
func (m *MergedProvider) Description() string                 { return m.catalog.Description }
func (m *MergedProvider) ConnectionMode() core.ConnectionMode { return m.connMode }

func (m *MergedProvider) AuthTypes() []string {
	for _, provider := range m.owned {
		if authTypes := provider.AuthTypes(); len(authTypes) > 0 {
			return slices.Clone(authTypes)
		}
	}
	return nil
}

func (m *MergedProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	for _, provider := range m.owned {
		if defs := provider.ConnectionParamDefs(); len(defs) > 0 {
			return maps.Clone(defs)
		}
	}
	return nil
}

func (m *MergedProvider) CredentialFields() []core.CredentialFieldDef {
	for _, provider := range m.owned {
		if fields := provider.CredentialFields(); len(fields) > 0 {
			return slices.Clone(fields)
		}
	}
	return nil
}

func (m *MergedProvider) DiscoveryConfig() *core.DiscoveryConfig {
	for _, provider := range m.owned {
		if cfg := provider.DiscoveryConfig(); cfg != nil {
			value := *cfg
			if len(value.Metadata) > 0 {
				value.Metadata = maps.Clone(value.Metadata)
			}
			return &value
		}
	}
	return nil
}

func (m *MergedProvider) ConnectionForOperation(op string) string {
	return m.opConn[op]
}

func (m *MergedProvider) Catalog() *catalog.Catalog { return m.catalog.Clone() }

func (m *MergedProvider) Execute(ctx context.Context, op string, params map[string]any, token string) (*core.OperationResult, error) {
	p, ok := m.route[op]
	if !ok {
		sessionProvider, err := m.sessionProviderForOperation(ctx, op, token)
		if err != nil {
			return nil, err
		}
		if sessionProvider == nil {
			return nil, fmt.Errorf("unknown operation %q", op)
		}
		return sessionProvider.Execute(ctx, op, params, token)
	}
	return p.Execute(ctx, op, params, token)
}

func (m *MergedProvider) InvokeGraphQL(ctx context.Context, request core.GraphQLRequest, token string) (*core.OperationResult, error) {
	for _, provider := range m.owned {
		invoker, ok := provider.(core.GraphQLSurfaceInvoker)
		if !ok {
			continue
		}
		return invoker.InvokeGraphQL(ctx, request, token)
	}
	return nil, fmt.Errorf("graphql surface is not available")
}

func (m *MergedProvider) SupportsSessionCatalog() bool {
	for _, provider := range m.owned {
		if core.SupportsSessionCatalog(provider) {
			return true
		}
	}
	return false
}

func (m *MergedProvider) SupportsPostConnect() bool {
	for _, provider := range m.owned {
		if core.SupportsPostConnect(provider) {
			return true
		}
	}
	return false
}

func (m *MergedProvider) PostConnect(ctx context.Context, token *core.IntegrationToken) (map[string]string, error) {
	for _, provider := range m.owned {
		if !core.SupportsPostConnect(provider) {
			continue
		}
		metadata, supported, err := core.PostConnect(ctx, provider, token)
		if !supported {
			continue
		}
		return metadata, err
	}
	return nil, core.ErrPostConnectUnsupported
}

func (m *MergedProvider) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	if !m.SupportsSessionCatalog() {
		return nil, core.WrapSessionCatalogUnsupported(fmt.Errorf("provider %q does not support session catalogs", m.Name()))
	}

	merged := m.catalog.Clone()
	merged.Operations = nil
	seen := make(map[string]string)
	for _, provider := range m.owned {
		if !core.SupportsSessionCatalog(provider) {
			continue
		}
		cat, _, err := core.CatalogForRequest(ctx, provider, token)
		if err != nil {
			return nil, err
		}
		if cat == nil {
			continue
		}
		for i := range cat.Operations {
			op := cat.Operations[i]
			if op.Transport == "" {
				op.Transport = catalog.TransportREST
			}
			if owner, exists := seen[op.ID]; exists {
				return nil, fmt.Errorf("operation %q provided by both %q and %q", op.ID, owner, provider.Name())
			}
			seen[op.ID] = provider.Name()
			merged.Operations = append(merged.Operations, op)
		}
	}
	integration.CompileSchemas(merged)
	return merged, nil
}

func (m *MergedProvider) sessionProviderForOperation(ctx context.Context, operation, token string) (core.Provider, error) {
	var (
		match    core.Provider
		firstErr error
	)
	for _, provider := range m.owned {
		if !core.SupportsSessionCatalog(provider) {
			continue
		}
		cat, _, err := core.CatalogForRequest(ctx, provider, token)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if cat == nil {
			continue
		}
		for i := range cat.Operations {
			if cat.Operations[i].ID != operation {
				continue
			}
			if match != nil {
				return nil, fmt.Errorf("operation %q provided by both %q and %q", operation, match.Name(), provider.Name())
			}
			match = provider
			break
		}
	}
	if match != nil {
		return match, nil
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, nil
}

func (m *MergedProvider) Close() error {
	var err error
	for i := len(m.owned) - 1; i >= 0; i-- {
		if c, ok := m.owned[i].(io.Closer); ok {
			err = errors.Join(err, c.Close())
		}
	}
	return err
}
