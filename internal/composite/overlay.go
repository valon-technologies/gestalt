package composite

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
)

type OverlayProvider struct {
	name       string
	base       core.Provider
	overlay    core.Provider
	overlayOps map[string]struct{}
}

var (
	_ core.Provider               = (*OverlayProvider)(nil)
	_ core.CatalogProvider        = (*OverlayProvider)(nil)
	_ core.SessionCatalogProvider = (*OverlayProvider)(nil)
	_ core.AuthTypeLister         = (*OverlayProvider)(nil)
)

func NewOverlay(name string, base core.Provider, overlay core.Provider) core.Provider {
	ops := make(map[string]struct{})
	for _, op := range overlay.ListOperations() {
		ops[op.Name] = struct{}{}
	}
	p := &OverlayProvider{name: name, base: base, overlay: overlay, overlayOps: ops}
	if oauthProv, ok := base.(core.OAuthProvider); ok {
		return &overlayOAuthProvider{OverlayProvider: p, oauthDelegator: oauthDelegator{oauth: oauthProv}}
	}
	return p
}

func (p *OverlayProvider) Name() string                        { return p.name }
func (p *OverlayProvider) DisplayName() string                 { return p.base.DisplayName() }
func (p *OverlayProvider) Description() string                 { return p.base.Description() }
func (p *OverlayProvider) ConnectionMode() core.ConnectionMode { return p.base.ConnectionMode() }

func (p *OverlayProvider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	if _, ok := p.overlayOps[operation]; ok {
		return p.overlay.Execute(ctx, operation, params, token)
	}
	return p.base.Execute(ctx, operation, params, token)
}

func (p *OverlayProvider) ListOperations() []core.Operation {
	baseOps := p.base.ListOperations()
	overlayOps := p.overlay.ListOperations()
	seen := make(map[string]struct{}, len(overlayOps))
	result := make([]core.Operation, 0, len(baseOps)+len(overlayOps))
	for _, op := range overlayOps {
		result = append(result, op)
		seen[op.Name] = struct{}{}
	}
	for _, op := range baseOps {
		if _, ok := seen[op.Name]; !ok {
			result = append(result, op)
		}
	}
	return result
}

func (p *OverlayProvider) Catalog() *catalog.Catalog {
	var baseCat, overlayCat *catalog.Catalog
	if cp, ok := p.base.(core.CatalogProvider); ok {
		baseCat = cp.Catalog()
	}
	if cp, ok := p.overlay.(core.CatalogProvider); ok {
		overlayCat = cp.Catalog()
	}
	if baseCat == nil && overlayCat == nil {
		return nil
	}
	if baseCat == nil {
		return tagPluginCatalog(overlayCat)
	}
	if overlayCat == nil {
		return baseCat
	}

	seen := make(map[string]struct{})
	merged := &catalog.Catalog{
		Name:        p.name,
		DisplayName: baseCat.DisplayName,
		Description: baseCat.Description,
		IconSVG:     baseCat.IconSVG,
		BaseURL:     baseCat.BaseURL,
		AuthStyle:   baseCat.AuthStyle,
		Headers:     copyHeaders(baseCat.Headers),
		Operations:  make([]catalog.CatalogOperation, 0, len(baseCat.Operations)+len(overlayCat.Operations)),
	}
	for i := range overlayCat.Operations {
		op := overlayCat.Operations[i]
		op.Transport = catalog.TransportPlugin
		merged.Operations = append(merged.Operations, op)
		seen[op.ID] = struct{}{}
	}
	for i := range baseCat.Operations {
		op := baseCat.Operations[i]
		if _, ok := seen[op.ID]; !ok {
			merged.Operations = append(merged.Operations, op)
		}
	}
	return merged
}

func (p *OverlayProvider) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	if scp, ok := p.base.(core.SessionCatalogProvider); ok {
		return scp.CatalogForRequest(ctx, token)
	}
	return nil, nil
}

func tagPluginCatalog(src *catalog.Catalog) *catalog.Catalog {
	out := src.Clone()
	for i := range out.Operations {
		out.Operations[i].Transport = catalog.TransportPlugin
	}
	return out
}

func copyHeaders(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func (p *OverlayProvider) SupportsManualAuth() bool {
	if mp, ok := p.base.(core.ManualProvider); ok {
		return mp.SupportsManualAuth()
	}
	return false
}

func (p *OverlayProvider) AuthTypes() []string {
	if atl, ok := p.base.(core.AuthTypeLister); ok {
		return atl.AuthTypes()
	}
	return nil
}

func (p *OverlayProvider) PostConnectHook() core.PostConnectHook {
	if pcp, ok := p.base.(core.PostConnectProvider); ok {
		return pcp.PostConnectHook()
	}
	return nil
}

func (p *OverlayProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	if cpp, ok := p.base.(core.ConnectionParamProvider); ok {
		return cpp.ConnectionParamDefs()
	}
	return nil
}

func (p *OverlayProvider) DiscoveryConfig() *core.DiscoveryConfig {
	if dcp, ok := p.base.(core.DiscoveryConfigProvider); ok {
		return dcp.DiscoveryConfig()
	}
	return nil
}

func (p *OverlayProvider) Close() error {
	var firstErr error
	if c, ok := p.overlay.(interface{ Close() error }); ok {
		if err := c.Close(); err != nil {
			firstErr = fmt.Errorf("closing overlay: %w", err)
		}
	}
	if c, ok := p.base.(interface{ Close() error }); ok {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("closing base: %w", err)
		}
	}
	return firstErr
}

type overlayOAuthProvider struct {
	*OverlayProvider
	oauthDelegator
}
