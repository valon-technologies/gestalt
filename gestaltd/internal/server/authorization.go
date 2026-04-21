package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

func (s *Server) allowProviderContext(ctx context.Context, p *principal.Principal, provider string) bool {
	if s.authorizer == nil {
		return true
	}
	return s.authorizer.AllowProvider(ctx, p, provider)
}

func (s *Server) allowOperationContext(ctx context.Context, p *principal.Principal, provider, operation string) bool {
	if s.authorizer == nil {
		return true
	}
	return s.authorizer.AllowOperation(ctx, p, provider, operation)
}

func (s *Server) providerAccessContextWithContext(ctx context.Context, p *principal.Principal, provider string) invocation.AccessContext {
	if s.authorizer == nil {
		return invocation.AccessContext{}
	}
	access, _ := s.authorizer.ResolveAccess(ctx, p, provider)
	return access
}

func (s *Server) workloadBinding(p *principal.Principal, provider string) (authorization.CredentialBinding, bool) {
	if s.authorizer == nil {
		return authorization.CredentialBinding{}, false
	}
	return s.authorizer.Binding(p, provider)
}

func rejectWorkloadSelectors(w http.ResponseWriter, p *principal.Principal, connection, instance string) error {
	if p == nil || !p.IsStaticWorkloadToken() {
		return nil
	}
	if connection == "" && instance == "" {
		return nil
	}
	writeError(w, http.StatusForbidden, errWorkloadSelector.Error())
	return errWorkloadSelector
}

func (s *Server) workloadBindingSelectors(p *principal.Principal, provider, connection, instance string) (string, string) {
	return s.catalogSelectorConfig().WorkloadBindingSelectors(p, provider, connection, instance)
}

func (s *Server) workloadBindingConnected(ctx context.Context, binding authorization.CredentialBinding, provider string) (bool, error) {
	switch binding.Mode {
	case core.ConnectionModeNone:
		return true, nil
	case core.ConnectionModeUser:
		_, err := s.tokens.Token(ctx, binding.CredentialSubjectID, provider, binding.Connection, binding.Instance)
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, core.ErrNotFound):
			return false, nil
		default:
			return false, err
		}
	default:
		return false, nil
	}
}
