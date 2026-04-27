package server

import (
	"context"
	"net/http"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

func (s *Server) allowProviderContext(ctx context.Context, p *principal.Principal, provider string) bool {
	if s.authorizer == nil {
		return true
	}
	return s.authorizer.AllowProvider(ctx, p, provider)
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

func rejectWorkloadCaller(w http.ResponseWriter, p *principal.Principal) error {
	if !principal.IsWorkloadPrincipal(p) {
		return nil
	}
	writeError(w, http.StatusForbidden, errWorkloadForbidden.Error())
	return errWorkloadForbidden
}

func rejectWorkloadSelectors(w http.ResponseWriter, p *principal.Principal, connection, instance string) error {
	if !principal.IsWorkloadPrincipal(p) {
		return nil
	}
	if connection == "" && instance == "" {
		return nil
	}
	writeError(w, http.StatusForbidden, errWorkloadSelector.Error())
	return errWorkloadSelector
}
