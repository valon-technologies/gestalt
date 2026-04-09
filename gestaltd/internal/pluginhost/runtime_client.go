package pluginhost

import (
	"context"
	"fmt"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	pluginRPCTimeout     = 10 * time.Second
	pluginMigrateTimeout = 2 * time.Minute
)

var pluginConfigureTimeout = 30 * time.Second

type runtimePluginMetadata struct {
	Kind        proto.PluginKind
	Name        string
	DisplayName string
	Description string
	Version     string
	Warnings    []string
}

func configureRuntimePlugin(ctx context.Context, client proto.ProviderLifecycleClient, expectedKind proto.PluginKind, name string, config map[string]any) (*runtimePluginMetadata, error) {
	if client == nil {
		return nil, fmt.Errorf("runtime client is required")
	}
	metaCtx, cancel := pluginConfigureContext(ctx)
	defer cancel()
	meta, err := client.GetPluginMetadata(metaCtx, &emptypb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("get plugin metadata: %w", err)
	}
	if expectedKind != proto.PluginKind_PLUGIN_KIND_UNSPECIFIED && meta.GetKind() != expectedKind {
		return nil, fmt.Errorf("plugin kind mismatch: got %s, want %s", meta.GetKind().String(), expectedKind.String())
	}
	if err := validateRuntimeProtocol(meta); err != nil {
		return nil, err
	}

	cfgStruct, err := structFromMap(config)
	if err != nil {
		return nil, fmt.Errorf("encode plugin config: %w", err)
	}
	configureCtx, configureCancel := pluginConfigureContext(ctx)
	defer configureCancel()
	resp, err := client.ConfigurePlugin(configureCtx, &proto.ConfigurePluginRequest{
		Name:            name,
		Config:          cfgStruct,
		ProtocolVersion: proto.CurrentProtocolVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("configure plugin: %w", err)
	}
	if resp.GetProtocolVersion() != proto.CurrentProtocolVersion {
		return nil, fmt.Errorf("plugin responded with protocol version %d, host requires %d", resp.GetProtocolVersion(), proto.CurrentProtocolVersion)
	}

	return &runtimePluginMetadata{
		Kind:        meta.GetKind(),
		Name:        meta.GetName(),
		DisplayName: meta.GetDisplayName(),
		Description: meta.GetDescription(),
		Version:     meta.GetVersion(),
		Warnings:    append([]string(nil), meta.GetWarnings()...),
	}, nil
}

func validateRuntimeProtocol(meta *proto.PluginMetadata) error {
	if meta == nil {
		return fmt.Errorf("plugin metadata is required")
	}
	minVersion := meta.GetMinProtocolVersion()
	maxVersion := meta.GetMaxProtocolVersion()
	if minVersion != 0 && proto.CurrentProtocolVersion < minVersion {
		return fmt.Errorf("plugin requires protocol version >= %d, host has %d", minVersion, proto.CurrentProtocolVersion)
	}
	if maxVersion != 0 && proto.CurrentProtocolVersion > maxVersion {
		return fmt.Errorf("plugin supports protocol version <= %d, host has %d", maxVersion, proto.CurrentProtocolVersion)
	}
	return nil
}

func pingRuntimePlugin(ctx context.Context, client proto.ProviderLifecycleClient) error {
	if client == nil {
		return fmt.Errorf("runtime client is required")
	}
	resp, err := client.HealthCheck(ctx, &emptypb.Empty{})
	if err != nil {
		return fmt.Errorf("plugin health check: %w", err)
	}
	if resp.GetReady() {
		return nil
	}
	if resp.GetMessage() == "" {
		return fmt.Errorf("plugin is not ready")
	}
	return fmt.Errorf("plugin is not ready: %s", resp.GetMessage())
}

func pluginCallContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, pluginRPCTimeout)
}

func pluginConfigureContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, pluginConfigureTimeout)
}

func pluginMigrateContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, pluginMigrateTimeout)
}
