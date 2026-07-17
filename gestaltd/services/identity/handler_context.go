package identity

import (
	"context"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

// providerHandlerContext derives identity call context from verified ingress
// state on context. Request metadata is only consulted for the private
// gestaltd-to-provider transport where no verified caller subject exists.
func providerHandlerContext(ctx context.Context) context.Context {
	if subjectID := gestalt.TrustedCallerSubjectFromContext(ctx); subjectID != "" {
		return gestalt.WithIdentityCallContext(ctx, gestalt.IdentityCallContext{
			CallerSubjectID: subjectID,
		})
	}
	return gestalt.AuthCallContextFromIncoming(ctx)
}
