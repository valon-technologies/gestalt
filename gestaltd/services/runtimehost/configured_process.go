package runtimehost

import (
	"context"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

// StartConfiguredAppProcess closes the started process if provider configuration fails.
func StartConfiguredAppProcess(ctx context.Context, cfg ProcessConfig, kind proto.ProviderKind, config map[string]any) (*AppProcess, proto.ProviderLifecycleClient, error) {
	proc, err := StartAppProcess(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	lifecycle := proc.Lifecycle()
	if _, err := ConfigureRuntimeProvider(ctx, lifecycle, kind, cfg.ProviderName, config); err != nil {
		_ = proc.Close()
		return nil, nil, err
	}
	return proc, lifecycle, nil
}
