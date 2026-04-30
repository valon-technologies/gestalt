package providerhost

import (
	"context"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
)

const (
	providerRPCTimeout = 10 * time.Second
)

type RuntimeProviderMetadata = runtimehost.RuntimeProviderMetadata

func ConfigureRuntimeProvider(ctx context.Context, client proto.ProviderLifecycleClient, expectedKind proto.ProviderKind, name string, config map[string]any) (*RuntimeProviderMetadata, error) {
	return runtimehost.ConfigureRuntimeProvider(ctx, client, expectedKind, name, config)
}

func CheckRuntimeProviderHealth(ctx context.Context, client proto.ProviderLifecycleClient) error {
	return runtimehost.CheckRuntimeProviderHealth(ctx, client)
}

func StartRuntimeProvider(ctx context.Context, client proto.ProviderLifecycleClient) error {
	return runtimehost.StartRuntimeProvider(ctx, client)
}

func providerCallContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, providerRPCTimeout)
}

func providerStreamContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithCancel(parent)
}
