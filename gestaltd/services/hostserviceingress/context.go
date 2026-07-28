package hostserviceingress

import (
	"context"
	"strings"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
)

type indexedDBNamespaceContextKey struct{}

// ApplyCapability restores trusted caller-provider and principal context from a
// verified host-service capability.
func ApplyCapability(ctx context.Context, target runtimehost.HostServiceRelayTarget) context.Context {
	appName := strings.TrimSpace(target.AppName)
	if appName != "" {
		ctx = invocation.WithCallerProvider(ctx, invocation.ProviderKindApp, appName)
	}
	if target.IndexedDBNamespace != nil {
		ctx = context.WithValue(
			ctx,
			indexedDBNamespaceContextKey{},
			cloneIndexedDBNamespaceClaims(target.IndexedDBNamespace),
		)
	}
	if target.Caller == nil {
		return ctx
	}
	subjectID := strings.TrimSpace(target.Caller.SubjectID)
	if subjectID == "" {
		return ctx
	}
	ctx = principal.WithPrincipal(ctx, &principal.Principal{
		SubjectID: subjectID,
		Scopes:    append([]string(nil), target.Caller.Scopes...),
		ClientID:  strings.TrimSpace(target.Caller.ClientID),
		Audience:  append([]string(nil), target.Caller.Audience...),
	})
	return gestalt.WithTrustedCallerSubject(ctx, subjectID)
}

// IndexedDBNamespaceFromContext returns the verified namespace authority
// attached by ApplyCapability. Callers must not mutate the returned claims.
func IndexedDBNamespaceFromContext(ctx context.Context) (*runtimehost.IndexedDBNamespaceClaims, bool) {
	claims, ok := ctx.Value(indexedDBNamespaceContextKey{}).(*runtimehost.IndexedDBNamespaceClaims)
	return cloneIndexedDBNamespaceClaims(claims), ok
}

func cloneIndexedDBNamespaceClaims(claims *runtimehost.IndexedDBNamespaceClaims) *runtimehost.IndexedDBNamespaceClaims {
	if claims == nil {
		return nil
	}
	return &runtimehost.IndexedDBNamespaceClaims{
		NamespaceID:    strings.TrimSpace(claims.NamespaceID),
		RegistrationID: strings.TrimSpace(claims.RegistrationID),
		Generation:     claims.Generation,
		ProviderName:   strings.TrimSpace(claims.ProviderName),
		DatabaseName:   strings.TrimSpace(claims.DatabaseName),
		SessionID:      strings.TrimSpace(claims.SessionID),
		AppName:        strings.TrimSpace(claims.AppName),
	}
}
