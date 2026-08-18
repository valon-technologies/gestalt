package invocation

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/apps/apiexec"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func storedCredentialRejected(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrReconnectRequired) || errors.Is(err, core.ErrReconnectRequired) {
		return true
	}
	var upstream *apiexec.UpstreamHTTPError
	return errors.As(err, &upstream) && upstream != nil && upstream.Status == http.StatusUnauthorized
}

// persistReconnectRequired writes the catalog reconnect fact onto the Grant
// that this invocation actually used. HTTP, MCP, and gRPC all go through
// Broker.Invoke*, so a 401 here must survive the next catalog GET.
func (b *Broker) persistReconnectRequired(ctx context.Context, p *principal.Principal, providerName, connection, instance string, err error) {
	if b == nil || !storedCredentialRejected(err) || core.ExternalCredentialProviderMissing(b.externalCreds) {
		return
	}
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return
	}
	cred := CredentialContextFromContext(ctx)
	subjectID := strings.TrimSpace(cred.SubjectID)
	if subjectID == "" {
		resolved, resolveErr := principal.ResolveCredentialSubjectID(ctx, b.users, p)
		if resolveErr != nil {
			return
		}
		subjectID = strings.TrimSpace(resolved)
	}
	if subjectID == "" {
		return
	}
	if connection == "" {
		connection = cred.Connection
	}
	if instance == "" {
		instance = cred.Instance
	}
	connection = core.ResolveConnectionAlias(strings.TrimSpace(connection))
	instance = strings.TrimSpace(instance)
	connectionID := b.connectionID(providerName, connection)
	if instance == "" {
		credentials, listErr := b.externalCreds.ListCredentials(ctx, subjectID, connectionID)
		if listErr != nil {
			b.log().WarnContext(ctx, "listing credentials after stored-credential reject", "provider", providerName, "error", listErr)
			return
		}
		preferred := ""
		if b.connectionInstancePreferences != nil {
			preferred, _ = b.connectionInstancePreferences.PreferredInstance(ctx, subjectID, connectionID)
		}
		chosen, ok := chosenCredentialInstance(credentials, preferred)
		if !ok {
			return
		}
		instance = chosen
	}
	stored, getErr := b.externalCreds.GetCredential(ctx, subjectID, connectionID, instance)
	if getErr != nil || stored == nil || stored.Grant == nil {
		return
	}
	now := time.Now()
	if !core.MarkGrantReconnectRequired(stored.Grant, now) {
		return
	}
	stored.UpdatedAt = now
	if upsertErr := b.externalCreds.UpsertCredential(ctx, stored); upsertErr != nil {
		b.log().WarnContext(ctx, "persisting reconnect-required grant", "provider", providerName, "error", upsertErr)
	}
}
