package invocation

import (
	"cmp"
	"context"

	"github.com/valon-technologies/gestalt/server/core"
)

type credentialModeOverrideCtxKey struct{}

func WithCredentialModeOverride(ctx context.Context, mode core.ConnectionMode) context.Context {
	mode = normalizeCredentialModeOverride(mode)
	if mode == "" {
		return ctx
	}
	return context.WithValue(ctx, credentialModeOverrideCtxKey{}, mode)
}

func CredentialModeOverrideFromContext(ctx context.Context) core.ConnectionMode {
	mode, _ := cmp.Or(ctx, context.Background()).Value(credentialModeOverrideCtxKey{}).(core.ConnectionMode)
	return normalizeCredentialModeOverride(mode)
}

func effectiveConnectionMode(ctx context.Context, prov core.Provider) core.ConnectionMode {
	if override := CredentialModeOverrideFromContext(ctx); override != "" {
		return override
	}
	if prov == nil {
		return ""
	}
	return core.NormalizeConnectionMode(prov.ConnectionMode())
}

func normalizeCredentialModeOverride(mode core.ConnectionMode) core.ConnectionMode {
	normalized := core.NormalizeOptionalConnectionMode(mode)
	switch normalized {
	case core.ConnectionModeNone, core.ConnectionModeSubject:
		return normalized
	default:
		return ""
	}
}
