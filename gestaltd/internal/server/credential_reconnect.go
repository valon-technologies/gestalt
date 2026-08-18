package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/apps/apiexec"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func invocationRejectedStoredCredential(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, invocation.ErrReconnectRequired) || errors.Is(err, core.ErrReconnectRequired) {
		return true
	}
	var upstream *apiexec.UpstreamHTTPError
	return errors.As(err, &upstream) && upstream != nil && upstream.Status == http.StatusUnauthorized
}

// persistReconnectRequiredGrant writes the catalog reconnect fact onto the
// stored OAuth grant after upstream rejected it. Catalog GET does not probe
// the provider; it reads ExpiresAt + RefreshErrorCount. Persist failures are
// logged and must not change the invocation error returned to the caller.
func (s *Server) persistReconnectRequiredGrant(r *http.Request, providerName string, err error) {
	if s == nil || r == nil || !invocationRejectedStoredCredential(err) {
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
	credentials, listErr := s.externalCredentials.ListCredentials(ctx, subjectID, "")
	if listErr != nil {
		slog.WarnContext(ctx, "listing credentials after stored-credential reject", "provider", providerName, "error", listErr)
		return
	}
	requestedConnection := strings.TrimSpace(r.URL.Query().Get(httpConnectionParam))
	requestedInstance := strings.TrimSpace(r.URL.Query().Get(httpInstanceParam))
	target := s.selectReconnectGrant(ctx, subjectID, providerName, requestedConnection, requestedInstance, credentials)
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

func (s *Server) selectReconnectGrant(
	ctx context.Context,
	subjectID, providerName, requestedConnection, requestedInstance string,
	credentials []*core.ExternalCredential,
) *core.ExternalCredential {
	requestedConnection = config.ResolveConnectionAlias(strings.TrimSpace(requestedConnection))
	requestedInstance = strings.TrimSpace(requestedInstance)
	matches := make([]*core.ExternalCredential, 0, 1)
	for _, credential := range credentials {
		if credential == nil || credential.Grant == nil {
			continue
		}
		if !s.credentialMatchesProvider(credential, providerName, requestedConnection) {
			continue
		}
		if requestedInstance != "" && strings.TrimSpace(credential.Qualifier) != requestedInstance {
			continue
		}
		matches = append(matches, credential)
	}
	switch len(matches) {
	case 0:
		return nil
	case 1:
		return matches[0]
	}

	byAudience := map[string][]*core.ExternalCredential{}
	for _, credential := range matches {
		audience := strings.TrimSpace(credential.Audience)
		byAudience[audience] = append(byAudience[audience], credential)
	}
	candidates := matches
	if requestedConnection == "" && len(byAudience) > 1 {
		defaultAudience := strings.TrimSpace(providerName) + ":" + config.AppConnectionName
		namedDefault := strings.TrimSpace(providerName) + ":" + defaultTokenInstance
		if group, ok := byAudience[namedDefault]; ok {
			candidates = group
		} else if group, ok := byAudience[defaultAudience]; ok {
			candidates = group
		} else {
			return nil
		}
	}
	if len(candidates) == 1 {
		return candidates[0]
	}
	preferred := s.preferredInstanceForConnection(ctx, subjectID, strings.TrimSpace(candidates[0].Audience))
	if preferred == "" {
		return nil
	}
	for _, credential := range candidates {
		if strings.TrimSpace(credential.Qualifier) == preferred {
			return credential
		}
	}
	return nil
}

func (s *Server) credentialMatchesProvider(credential *core.ExternalCredential, providerName, requestedConnection string) bool {
	if credential == nil {
		return false
	}
	for _, binding := range s.pluginConnectionBindingsForCredentialID(credential.Audience) {
		if binding.App != providerName {
			continue
		}
		if requestedConnection == "" {
			return true
		}
		if config.ResolveConnectionAlias(binding.Connection) == requestedConnection {
			return true
		}
	}
	prefix := strings.TrimSpace(providerName) + ":"
	audience := strings.TrimSpace(credential.Audience)
	if !strings.HasPrefix(audience, prefix) {
		return false
	}
	if requestedConnection == "" {
		return true
	}
	return audience == prefix+requestedConnection
}
