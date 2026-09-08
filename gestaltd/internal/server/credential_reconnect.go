package server

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

// persistReconnectRequiredGrant writes the catalog reconnect fact onto the
// Grant this invocation used. Catalog GET does not probe the provider; it
// reads ExpiresAt + RefreshErrorCount. Persist failures are logged and must
// not change the invocation error returned to the caller.
//
// Identity comes from the invoke (credential context, then WithConnection),
// not from listing every grant for the subject. A 401 on archive must not
// expire a healthy default sibling.
func (s *Server) persistReconnectRequiredGrant(r *http.Request, providerName string, err error) {
	if s == nil || r == nil || !invocation.StoredCredentialRejected(err) {
		return
	}
	if core.ExternalCredentialProviderMissing(s.externalCredentials) {
		return
	}
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return
	}
	ctx := r.Context()
	subjectID, resolveErr := principal.ResolveCredentialSubjectID(ctx, s.users, PrincipalFromContext(ctx))
	if resolveErr != nil || strings.TrimSpace(subjectID) == "" {
		return
	}
	cred := invocation.CredentialContextFromContext(ctx)
	connection := firstNonEmpty(
		cred.Connection,
		invocation.ConnectionFromContext(ctx),
		r.URL.Query().Get(httpConnectionParam),
	)
	instance := firstNonEmpty(cred.Instance, r.URL.Query().Get(httpInstanceParam))
	audience := s.reconnectAudience(providerName, connection)
	if audience == "" {
		return
	}
	target := s.loadReconnectGrant(ctx, subjectID, audience, instance)
	if target == nil || target.Grant == nil {
		return
	}
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	if !core.MarkGrantReconnectRequired(target.Grant, now) {
		return
	}
	target.UpdatedAt = now
	if upsertErr := s.externalCredentials.UpsertCredential(ctx, target); upsertErr != nil {
		slog.WarnContext(ctx, "persisting reconnect-required grant", "provider", providerName, "error", upsertErr)
	}
}

func (s *Server) reconnectAudience(providerName, connection string) string {
	connection = config.ResolveConnectionAlias(strings.TrimSpace(connection))
	if connection == "" {
		connection = s.defaultConnectionName(providerName)
	}
	if connection == "" {
		return ""
	}
	if s.pluginDefs != nil {
		if entry := s.pluginDefs[providerName]; entry != nil && entry.Connections != nil {
			if def := entry.Connections[connection]; def != nil {
				if id := strings.TrimSpace(def.ConnectionID); id != "" {
					return id
				}
			}
		}
	}
	return strings.TrimSpace(providerName) + ":" + connection
}

func (s *Server) loadReconnectGrant(ctx context.Context, subjectID, audience, instance string) *core.ExternalCredential {
	instance = strings.TrimSpace(instance)
	if instance != "" {
		stored, err := s.externalCredentials.GetCredential(ctx, subjectID, audience, instance)
		if err != nil || stored == nil {
			return nil
		}
		return stored
	}
	credentials, err := s.externalCredentials.ListCredentials(ctx, subjectID, audience)
	if err != nil {
		slog.WarnContext(ctx, "listing credentials after stored-credential reject", "audience", audience, "error", err)
		return nil
	}
	preferred := ""
	if s.connectionInstancePreferences != nil {
		preferred, _ = s.connectionInstancePreferences.PreferredInstance(ctx, subjectID, audience)
	}
	if preferred != "" {
		for _, credential := range credentials {
			if credential != nil && credential.Grant != nil && strings.TrimSpace(credential.Qualifier) == preferred {
				return credential
			}
		}
		return nil
	}
	var sole *core.ExternalCredential
	for _, credential := range credentials {
		if credential == nil || credential.Grant == nil {
			continue
		}
		if sole != nil {
			// The request did not identify the grant and no preference exists;
			// never guess between credentials, especially after the broker has
			// already marked the grant it actually resolved.
			return nil
		}
		sole = credential
	}
	return sole
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
