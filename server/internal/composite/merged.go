package composite

import (
	"context"
	"errors"
	"fmt"
	"io"

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
	_ core.Provider                    = (*MergedProvider)(nil)
	_ core.OperationConnectionProvider = (*MergedProvider)(nil)
)

func NewMerged(name, displayName, desc, iconSVG string, providers ...core.Provider) (*MergedProvider, error) {
	bound := make([]BoundProvider, len(providers))
	for i, p := range providers {
		bound[i] = BoundProvider{Provider: p}
	}
	return NewMergedWithConnections(name, displayName, desc, iconSVG, bound...)
}

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
		for _, op := range cat.Operations {
			if owner, exists := m.route[op.ID]; exists {
				return nil, fmt.Errorf("operation %q provided by both %q and %q", op.ID, owner.Name(), p.Name())
			}
			m.route[op.ID] = p
			m.catalog.Operations = append(m.catalog.Operations, op)
			if bound.Connection != "" {
				m.opConn[op.ID] = bound.Connection
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

func (m *MergedProvider) Close() error {
	var err error
	for i := len(m.owned) - 1; i >= 0; i-- {
		if c, ok := m.owned[i].(io.Closer); ok {
			err = errors.Join(err, c.Close())
		}
	}
	return err
}

func (m *MergedProvider) DisownProvider(p core.Provider) {
	for i, owned := range m.owned {
		if owned == p {
			m.owned = append(m.owned[:i], m.owned[i+1:]...)
			return
		}
	}
}
