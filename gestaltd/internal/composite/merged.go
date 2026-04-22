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
	_ core.Provider              = (*MergedProvider)(nil)
	_ core.GraphQLSurfaceInvoker = (*MergedProvider)(nil)
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
		return nil, fmt.Errorf("unknown operation %q", op)
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

func (m *MergedProvider) Close() error {
	var err error
	for i := len(m.owned) - 1; i >= 0; i-- {
		if c, ok := m.owned[i].(io.Closer); ok {
			err = errors.Join(err, c.Close())
		}
	}
	return err
}
