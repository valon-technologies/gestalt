package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

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

func (s *Server) identityBinding(p *principal.Principal, provider string) (authorization.CredentialBinding, bool) {
	if s.authorizer == nil {
		return authorization.CredentialBinding{}, false
	}
	return s.authorizer.Binding(p, provider)
}

func rejectHeadlessSelectors(w http.ResponseWriter, p *principal.Principal, connection, instance string) error {
	if p == nil || p.HasUserContext() {
		return nil
	}
	if connection == "" && instance == "" {
		return nil
	}
	writeError(w, http.StatusForbidden, errHeadlessSelector.Error())
	return errHeadlessSelector
}

func (s *Server) identityBindingSelectors(p *principal.Principal, provider, connection, instance string) (string, string) {
	resolveConnection := func(connection string) string {
		return s.sessionCatalogConnections(provider, nil, connection)[0]
	}
	if p == nil || p.HasUserContext() {
		return resolveConnection(connection), instance
	}
	binding, ok := s.identityBinding(p, provider)
	if !ok {
		return resolveConnection(connection), instance
	}
	if connection == "" {
		connection = binding.Connection
	}
	if instance == "" {
		instance = binding.Instance
	}
	return resolveConnection(connection), instance
}

func (s *Server) identityBindingConnected(ctx context.Context, binding authorization.CredentialBinding, provider string) (bool, error) {
	switch binding.Mode {
	case core.ConnectionModeNone:
		return true, nil
	case core.ConnectionModeIdentity:
		if strings.TrimSpace(binding.CredentialOwnerID) == "" {
			return false, errors.New("identity binding missing owner identity")
		}
		_, err := s.tokens.IdentityToken(ctx, binding.CredentialOwnerID, provider, binding.Connection, binding.Instance)
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
