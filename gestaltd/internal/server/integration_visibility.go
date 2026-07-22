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
	if info.ManagementPath != "" {
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
	return info.CredentialState == credentialStateConnected ||
		info.CredentialState == credentialStateConfigured ||
		info.CredentialState == credentialStateNotRequired ||
		len(info.Connections) > 0
}

func (s *Server) integrationHasVisibleHTTPOperationsContext(ctx context.Context, p *principal.Principal, provider string, prov core.Provider) bool {
	cat := prov.Catalog()
	if cat == nil {
		return false
	}
	cat = invocation.FilterCatalogForPrincipal(ctx, cat, provider, p, nil)
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
	for i := range s.mountedUIs {
		mounted := &s.mountedUIs[i]
		if mounted.Handler == nil || mounted.Path != mountedPath {
			continue
		}
		if mounted.AppName == provider {
			return *mounted, true
		}
	}
	for i := range s.mountedUIs {
		mounted := &s.mountedUIs[i]
		if mounted.Handler == nil || mounted.Path != mountedPath {
			continue
		}
		return *mounted, true
	}
	return MountedUI{}, false
}

func (s *Server) mountedUIRootAccessibleContext(ctx context.Context, p *principal.Principal, mounted MountedUI) bool {
	if !mountedUIRequiresAuthorization(mounted) {
		return true
	}
	if p == nil || principal.IsNonUserPrincipal(p) {
		return false
	}

	_, allowed, err := s.authorizeMountedAppAccess(ctx, p, mounted)
	return err == nil && allowed
}
