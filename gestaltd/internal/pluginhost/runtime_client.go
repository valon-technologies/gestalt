package pluginhost

import (
	"context"
	"fmt"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	providerRPCTimeout     = 10 * time.Second
	providerMigrateTimeout = 2 * time.Minute
)

var providerConfigureTimeout = 30 * time.Second

type runtimeProviderMetadata struct {
	Kind        proto.ProviderKind
	Name        string
	DisplayName string
	Description string
	Version     string
	Warnings    []string
}

func configureRuntimeProvider(ctx context.Context, client proto.ProviderLifecycleClient, expectedKind proto.ProviderKind, name string, config map[string]any) (*runtimeProviderMetadata, error) {
	if client == nil {
		return nil, fmt.Errorf("runtime client is required")
	}
	metaCtx, cancel := providerConfigureContext(ctx)
	defer cancel()
	meta, err := client.GetProviderIdentity(metaCtx, &emptypb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("get provider identity: %w", err)
	}
	if expectedKind != proto.ProviderKind_PROVIDER_KIND_UNSPECIFIED && meta.GetKind() != expectedKind {
		return nil, fmt.Errorf("provider kind mismatch: got %s, want %s", meta.GetKind().String(), expectedKind.String())
	}
	if err := validateRuntimeProtocol(meta); err != nil {
		return nil, err
	}

	cfgStruct, err := structFromMap(config)
	if err != nil {
		return nil, fmt.Errorf("encode provider config: %w", err)
	}
	configureCtx, configureCancel := providerConfigureContext(ctx)
	defer configureCancel()
	resp, err := client.ConfigureProvider(configureCtx, &proto.ConfigureProviderRequest{
		Name:            name,
		Config:          cfgStruct,
		ProtocolVersion: proto.CurrentProtocolVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("configure provider: %w", err)
	}
	if resp.GetProtocolVersion() != proto.CurrentProtocolVersion {
		return nil, fmt.Errorf("provider responded with protocol version %d, host requires %d", resp.GetProtocolVersion(), proto.CurrentProtocolVersion)
	}

	return &runtimeProviderMetadata{
		Kind:        meta.GetKind(),
		Name:        meta.GetName(),
		DisplayName: meta.GetDisplayName(),
		Description: meta.GetDescription(),
		Version:     meta.GetVersion(),
		Warnings:    append([]string(nil), meta.GetWarnings()...),
	}, nil
}

func validateRuntimeProtocol(meta *proto.ProviderIdentity) error {
	if meta == nil {
		return fmt.Errorf("provider identity is required")
	}
	minVersion := meta.GetMinProtocolVersion()
	maxVersion := meta.GetMaxProtocolVersion()
	if minVersion != 0 && proto.CurrentProtocolVersion < minVersion {
		return fmt.Errorf("provider requires protocol version >= %d, host has %d", minVersion, proto.CurrentProtocolVersion)
	}
	if maxVersion != 0 && proto.CurrentProtocolVersion > maxVersion {
		return fmt.Errorf("provider supports protocol version <= %d, host has %d", maxVersion, proto.CurrentProtocolVersion)
	}
	return nil
}

func pingRuntimeProvider(ctx context.Context, client proto.ProviderLifecycleClient) error {
	if client == nil {
		return fmt.Errorf("runtime client is required")
	}
	resp, err := client.HealthCheck(ctx, &emptypb.Empty{})
	if err != nil {
		return fmt.Errorf("provider health check: %w", err)
	}
	if resp.GetReady() {
		return nil
	}
	if resp.GetMessage() == "" {
		return fmt.Errorf("provider is not ready")
	}
	return fmt.Errorf("provider is not ready: %s", resp.GetMessage())
}

func providerCallContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, providerRPCTimeout)
}

func providerConfigureContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, providerConfigureTimeout)
}

func providerMigrateContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, providerMigrateTimeout)
}
