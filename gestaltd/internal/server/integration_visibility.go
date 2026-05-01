package server

import (
	"context"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
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
	if principal.IsNonUserPrincipal(p) {
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
	return len(s.publicHTTPOperations(provider, prov, cat.Operations)) > 0
}

func (s *Server) integrationMountedPathForPrincipalContext(ctx context.Context, p *principal.Principal, provider, mountedPath string) string {
	mountedPath = strings.TrimSpace(mountedPath)
	if mountedPath == "" {
		return ""
	}
	mounted, ok := s.mountedUIForProvider(provider, mountedPath)
	if !ok || !s.mountedUIRootAccessibleContext(ctx, p, mounted) {
		return ""
	}
	return mountedPath
}

func (s *Server) mountedUIForProvider(provider, mountedPath string) (MountedUI, bool) {
	for _, mounted := range s.mountedUIs {
		if mounted.Handler == nil || mounted.Path != mountedPath {
			continue
		}
		if mounted.PluginName == provider {
			return mounted, true
		}
	}
	for _, mounted := range s.mountedUIs {
		if mounted.Handler == nil || mounted.Path != mountedPath {
			continue
		}
		return mounted, true
	}
	return MountedUI{}, false
}

func (s *Server) mountedUIRootAccessibleContext(ctx context.Context, p *principal.Principal, mounted MountedUI) bool {
	if mounted.AuthorizationPolicy == "" {
		return mounted.Public
	}
	if s.authorizer == nil || p == nil || principal.IsNonUserPrincipal(p) {
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

	route, matched := mounted.routeForRequestPath(mountedUIRootRequestPath(mounted))
	return matched && len(route.AllowedRoles) > 0 && mountedUIRoleAllowed(access.Role, route.AllowedRoles)
}

func mountedUIRootRequestPath(mounted MountedUI) string {
	if mounted.Path == "/" {
		return "/"
	}
	return mounted.Path + "/"
}
