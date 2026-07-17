package hostserviceingress

import (
	"context"
	"strings"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
)

// ApplyCapability restores trusted caller-provider and principal context from a
// verified host-service capability.
func ApplyCapability(ctx context.Context, target runtimehost.HostServiceRelayTarget) context.Context {
	appName := strings.TrimSpace(target.AppName)
	if appName != "" {
		ctx = invocation.WithCallerProvider(ctx, invocation.ProviderKindApp, appName)
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
