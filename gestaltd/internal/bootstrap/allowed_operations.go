package bootstrap

import (
	"context"
	"errors"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/apps/operationexposure"
)

func buildAllowedOperations(ctx context.Context, app string, entry *config.ProviderEntry, deps Deps) map[string]*config.OperationOverride {
	static := entry.EffectiveAllowedOperations()
	if deps.Services == nil || deps.Services.AppAllowedOperations == nil {
		return static
	}
	overlay, err := deps.Services.AppAllowedOperations.GetOverlay(ctx, app)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return static
		}
		return static
	}
	return operationexposure.MergeAllowedOperationsWithOverlay(static, overlay.Operations, overlay.Removed)
}
