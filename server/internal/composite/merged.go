package composite

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
)

type MergedProvider struct {
	name, displayName, desc string
	connMode                core.ConnectionMode
	ops                     []core.Operation
	route                   map[string]core.Provider
	all                     []core.Provider
	owned                   []core.Provider
}

var (
	_ core.Provider        = (*MergedProvider)(nil)
	_ core.CatalogProvider = (*MergedProvider)(nil)
)

func NewMerged(name, displayName, desc string, providers ...core.Provider) (*MergedProvider, error) {
	all := make([]core.Provider, len(providers))
	copy(all, providers)
	m := &MergedProvider{
		name:        name,
		displayName: displayName,
		desc:        desc,
		route:       make(map[string]core.Provider),
		all:         all,
		owned:       providers,
	}
	for i, p := range providers {
		if i == 0 {
			m.connMode = p.ConnectionMode()
		} else {
			m.connMode = stricterConnectionMode(m.connMode, p.ConnectionMode())
		}
		for _, op := range p.ListOperations() {
			if owner, exists := m.route[op.Name]; exists {
				return nil, fmt.Errorf("operation %q provided by both %q and %q", op.Name, owner.Name(), p.Name())
			}
			m.route[op.Name] = p
			m.ops = append(m.ops, op)
		}
	}
	return m, nil
}

func (m *MergedProvider) Name() string                        { return m.name }
func (m *MergedProvider) DisplayName() string                 { return m.displayName }
func (m *MergedProvider) Description() string                 { return m.desc }
func (m *MergedProvider) ConnectionMode() core.ConnectionMode { return m.connMode }
func (m *MergedProvider) ListOperations() []core.Operation    { return m.ops }

func (m *MergedProvider) Catalog() *catalog.Catalog {
	richOps := make(map[string]catalog.CatalogOperation)
	for _, p := range m.all {
		cp, ok := p.(core.CatalogProvider)
		if !ok {
			continue
		}
		cat := cp.Catalog()
		if cat == nil {
			continue
		}
		for i := range cat.Operations {
			if _, ours := m.route[cat.Operations[i].ID]; ours {
				richOps[cat.Operations[i].ID] = cat.Operations[i]
			}
		}
	}

	cat := &catalog.Catalog{
		Name:        m.name,
		DisplayName: m.displayName,
		Description: m.desc,
	}
	for _, op := range m.ops {
		if rich, ok := richOps[op.Name]; ok {
			cat.Operations = append(cat.Operations, rich)
		} else {
			cat.Operations = append(cat.Operations, catalog.CatalogOperation{
				ID:          op.Name,
				Title:       op.Name,
				Description: op.Description,
				Transport:   catalog.TransportREST,
			})
		}
	}
	return cat
}

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
