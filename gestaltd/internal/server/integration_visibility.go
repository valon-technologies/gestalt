package server

import (
	"context"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

func (s *Server) integrationHasUsableSurfaceContext(ctx context.Context, p *principal.Principal, provider string, prov core.Provider, info integrationInfo) bool {
	if info.MountedPath != "" {
		return true
	}
	if s.integrationHasSettingsSurface(p, info) {
		return true
	}
	return s.integrationHasVisibleHTTPOperationsContext(ctx, p, provider, prov)
}

func (s *Server) integrationHasSettingsSurface(p *principal.Principal, info integrationInfo) bool {
	if p != nil && !p.HasUserContext() {
		return false
	}
	return info.Connected || len(info.AuthTypes) > 0 || len(info.Connections) > 0
}

func (s *Server) integrationHasVisibleHTTPOperationsContext(ctx context.Context, p *principal.Principal, provider string, prov core.Provider) bool {
	cat := prov.Catalog()
	if cat == nil {
		return false
	}
	cat = invocation.FilterCatalogForPrincipal(ctx, cat, provider, p, s.authorizer)
	return len(httpVisibleCatalogOperations(cat.Operations)) > 0
}

func (s *Server) integrationMountedPathForPrincipalContext(ctx context.Context, p *principal.Principal, provider, mountedPath string) string {
	mountedPath = strings.TrimSpace(mountedPath)
	if mountedPath == "" {
		return ""
	}
	mounted, ok := s.mountedWebUIForProvider(provider, mountedPath)
	if !ok || !s.mountedWebUIRootAccessibleContext(ctx, p, mounted) {
		return ""
	}
	return mountedPath
}

func (s *Server) mountedWebUIForProvider(provider, mountedPath string) (MountedWebUI, bool) {
	for _, mounted := range s.mountedWebUIs {
		if mounted.Handler == nil || mounted.Path != mountedPath {
			continue
		}
		if mounted.PluginName == provider {
			return mounted, true
		}
	}
	for _, mounted := range s.mountedWebUIs {
		if mounted.Handler == nil || mounted.Path != mountedPath {
			continue
		}
		return mounted, true
	}
	return MountedWebUI{}, false
}

func (s *Server) mountedWebUIRootAccessibleContext(ctx context.Context, p *principal.Principal, mounted MountedWebUI) bool {
	if p == nil || !p.HasUserContext() {
		return false
	}
	if mounted.AuthorizationPolicy == "" {
		return true
	}
	if s.authorizer == nil {
		return false
	}

	var (
		access  invocation.AccessContext
		allowed bool
	)
	if mounted.PluginName != "" {
		access, allowed = s.authorizer.ResolveAccess(ctx, p, mounted.PluginName)
	} else {
		access, allowed = s.authorizer.ResolvePolicyAccess(ctx, p, mounted.AuthorizationPolicy)
	}
	if !allowed {
		return false
	}

	route, matched := mounted.routeForRequestPath(mountedWebUIRootRequestPath(mounted))
	return matched && len(route.AllowedRoles) > 0 && mountedWebUIRoleAllowed(access.Role, route.AllowedRoles)
}

func mountedWebUIRootRequestPath(mounted MountedWebUI) string {
	if mounted.Path == "/" {
		return "/"
	}
	return mounted.Path + "/"
}
