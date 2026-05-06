package runtimehost

import (
	"context"
	"fmt"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	// ProviderRPCTimeout is the default deadline for individual provider RPCs.
	//
	// Hosted agent providers can do real setup work during session creation
	// after the runtime itself is ready. Keep this below the API route timeout,
	// but above short health-check style deadlines.
	ProviderRPCTimeout = 30 * time.Second
	// ProviderSessionCreateTimeout bounds agent CreateSession RPCs when callers
	// do not provide their own deadline.
	ProviderSessionCreateTimeout = 5 * time.Minute
	providerStartTimeout         = 2 * time.Minute
)

var providerConfigureTimeout = 30 * time.Second

type RuntimeProviderMetadata struct {
	Kind        proto.ProviderKind
	Name        string
	DisplayName string
	Description string
	Version     string
	Warnings    []string
}

func ConfigureRuntimeProvider(ctx context.Context, client proto.ProviderLifecycleClient, expectedKind proto.ProviderKind, name string, config map[string]any) (*RuntimeProviderMetadata, error) {
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

	// Some providers expose configured metadata such as display names only after
	// ConfigureProvider has applied the runtime config.
	configuredMetaCtx, configuredCancel := providerConfigureContext(ctx)
	defer configuredCancel()
	configuredMeta, err := client.GetProviderIdentity(configuredMetaCtx, &emptypb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("get configured provider identity: %w", err)
	}

	return &RuntimeProviderMetadata{
		Kind:        configuredMeta.GetKind(),
		Name:        configuredMeta.GetName(),
		DisplayName: configuredMeta.GetDisplayName(),
		Description: configuredMeta.GetDescription(),
		Version:     configuredMeta.GetVersion(),
		Warnings:    append([]string(nil), configuredMeta.GetWarnings()...),
	}, nil
}

func CheckRuntimeProviderHealth(ctx context.Context, client proto.ProviderLifecycleClient) error {
	if client == nil {
		return fmt.Errorf("runtime client is required")
	}
	healthCtx, cancel := ProviderCallContext(ctx)
	defer cancel()
	resp, err := client.HealthCheck(healthCtx, &emptypb.Empty{})
	if err != nil {
		return err
	}
	if resp == nil {
		return fmt.Errorf("provider health check returned nil response")
	}
	if !resp.GetReady() {
		message := strings.TrimSpace(resp.GetMessage())
		if message == "" {
			message = "not ready"
		}
		return fmt.Errorf("provider health check failed: %s", message)
	}
	return nil
}

func StartRuntimeProvider(ctx context.Context, client proto.ProviderLifecycleClient) error {
	if client == nil {
		return fmt.Errorf("runtime client is required")
	}
	startCtx, cancel := providerStartContext(ctx)
	defer cancel()
	resp, err := client.StartProvider(startCtx, &emptypb.Empty{})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return nil
		}
		return fmt.Errorf("start provider: %w", err)
	}
	if resp == nil {
		return fmt.Errorf("provider start returned nil response")
	}
	if resp.GetProtocolVersion() != proto.CurrentProtocolVersion {
		return fmt.Errorf("provider responded with protocol version %d, host requires %d", resp.GetProtocolVersion(), proto.CurrentProtocolVersion)
	}
	return nil
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

// ProviderCallContext returns a child context with the default provider RPC deadline.
func ProviderCallContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, ProviderRPCTimeout)
}

// ProviderSessionCreateContext returns a child context for agent CreateSession RPCs.
// It preserves caller deadlines when present and otherwise applies a bounded fallback.
func ProviderSessionCreateContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	if _, ok := parent.Deadline(); ok {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, ProviderSessionCreateTimeout)
}

func providerStartContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, providerStartTimeout)
}

func providerConfigureContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	if _, ok := parent.Deadline(); ok {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, providerConfigureTimeout)
}
