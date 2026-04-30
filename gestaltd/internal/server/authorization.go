package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func (s *Server) allowProviderContext(ctx context.Context, p *principal.Principal, provider string) bool {
	if s.authorizer == nil {
		return true
	}
	return s.authorizer.AllowProvider(ctx, p, provider)
}

func (s *Server) allowProviderActionContext(ctx context.Context, p *principal.Principal, provider, action string) bool {
	if s.authorizer != nil {
		if actionAuthorizer, ok := s.authorizer.(authorization.ProviderActionAuthorizer); ok && actionAuthorizer.AllowProviderAction(ctx, p, provider, action) {
			return true
		}
	}
	if action != core.ProviderActionDevAttach {
		return false
	}
	entry := s.pluginDefs[provider]
	if entry == nil || entry.Dev == nil || len(entry.Dev.Attach.AllowedRoles) == 0 {
		return false
	}
	if strings.TrimSpace(entry.AuthorizationPolicy) == "" || s.authorizer == nil {
		return false
	}
	access, ok := s.authorizer.ResolveAccess(ctx, p, provider)
	if !ok || strings.TrimSpace(access.Policy) == "" || strings.TrimSpace(access.Role) == "" {
		return false
	}
	for _, role := range entry.Dev.Attach.AllowedRoles {
		if strings.TrimSpace(role) == access.Role {
			return true
		}
	}
	return false
}

func (s *Server) providerAccessContextWithContext(ctx context.Context, p *principal.Principal, provider string) invocation.AccessContext {
	if s.authorizer == nil {
		return invocation.AccessContext{}
	}
	access, _ := s.authorizer.ResolveAccess(ctx, p, provider)
	return access
}

func (s *Server) providerOverrideForContext(ctx context.Context, p *principal.Principal, provider string) (core.Provider, bool, error) {
	if s.providerDevSessions == nil {
		return nil, false, nil
	}
	return s.providerDevSessions.ResolveProviderOverride(ctx, p, provider)
}

func requireUserCaller(w http.ResponseWriter, p *principal.Principal) error {
	if !principal.IsNonUserPrincipal(p) {
		return nil
	}
	writeError(w, http.StatusForbidden, errUserRequired.Error())
	return errUserRequired
}
