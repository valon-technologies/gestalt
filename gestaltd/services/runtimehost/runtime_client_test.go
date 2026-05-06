package runtimehost

import (
	"context"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type fakeProviderLifecycleClient struct {
	getProviderIdentity func(context.Context, *emptypb.Empty, ...grpc.CallOption) (*proto.ProviderIdentity, error)
	configureProvider   func(context.Context, *proto.ConfigureProviderRequest, ...grpc.CallOption) (*proto.ConfigureProviderResponse, error)
	healthCheck         func(context.Context, *emptypb.Empty, ...grpc.CallOption) (*proto.HealthCheckResponse, error)
	startProvider       func(context.Context, *emptypb.Empty, ...grpc.CallOption) (*proto.StartRuntimeProviderResponse, error)
}

func (c *fakeProviderLifecycleClient) GetProviderIdentity(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*proto.ProviderIdentity, error) {
	return c.getProviderIdentity(ctx, in, opts...)
}

func (c *fakeProviderLifecycleClient) ConfigureProvider(ctx context.Context, in *proto.ConfigureProviderRequest, opts ...grpc.CallOption) (*proto.ConfigureProviderResponse, error) {
	return c.configureProvider(ctx, in, opts...)
}

func (c *fakeProviderLifecycleClient) HealthCheck(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*proto.HealthCheckResponse, error) {
	if c.healthCheck != nil {
		return c.healthCheck(ctx, in, opts...)
	}
	return &proto.HealthCheckResponse{Ready: true}, nil
}

func (c *fakeProviderLifecycleClient) StartProvider(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*proto.StartRuntimeProviderResponse, error) {
	if c.startProvider != nil {
		return c.startProvider(ctx, in, opts...)
	}
	return &proto.StartRuntimeProviderResponse{ProtocolVersion: proto.CurrentProtocolVersion}, nil
}

func TestConfigureRuntimeProviderRefreshesMetadataAfterConfigure(t *testing.T) {
	t.Parallel()

	getCalls := 0
	configured := false
	client := &fakeProviderLifecycleClient{
		getProviderIdentity: func(_ context.Context, _ *emptypb.Empty, _ ...grpc.CallOption) (*proto.ProviderIdentity, error) {
			getCalls++
			if configured {
				return &proto.ProviderIdentity{
					Kind:               proto.ProviderKind_PROVIDER_KIND_AUTHENTICATION,
					Name:               "oidc",
					DisplayName:        "Google",
					Description:        "Sign in with Google",
					Version:            "v1.2.3",
					Warnings:           []string{"configured"},
					MinProtocolVersion: proto.CurrentProtocolVersion,
					MaxProtocolVersion: proto.CurrentProtocolVersion,
				}, nil
			}
			return &proto.ProviderIdentity{
				Kind:               proto.ProviderKind_PROVIDER_KIND_AUTHENTICATION,
				Name:               "oidc",
				DisplayName:        "SSO",
				Description:        "Default sign in",
				Version:            "v1.2.3",
				Warnings:           []string{"default"},
				MinProtocolVersion: proto.CurrentProtocolVersion,
				MaxProtocolVersion: proto.CurrentProtocolVersion,
			}, nil
		},
		configureProvider: func(_ context.Context, req *proto.ConfigureProviderRequest, _ ...grpc.CallOption) (*proto.ConfigureProviderResponse, error) {
			if req.GetName() != "oidc" {
				t.Fatalf("ConfigureProvider name = %q, want %q", req.GetName(), "oidc")
			}
			if req.GetProtocolVersion() != proto.CurrentProtocolVersion {
				t.Fatalf("ConfigureProvider protocol version = %d, want %d", req.GetProtocolVersion(), proto.CurrentProtocolVersion)
			}
			if got := req.GetConfig().AsMap()["displayName"]; got != "Google" {
				t.Fatalf("ConfigureProvider displayName = %#v, want %q", got, "Google")
			}
			configured = true
			return &proto.ConfigureProviderResponse{ProtocolVersion: proto.CurrentProtocolVersion}, nil
		},
	}

	meta, err := ConfigureRuntimeProvider(
		context.Background(),
		client,
		proto.ProviderKind_PROVIDER_KIND_AUTHENTICATION,
		"oidc",
		map[string]any{"displayName": "Google"},
	)
	if err != nil {
		t.Fatalf("configureRuntimeProvider: %v", err)
	}

	if getCalls != 2 {
		t.Fatalf("GetProviderIdentity calls = %d, want 2", getCalls)
	}
	if meta.DisplayName != "Google" {
		t.Fatalf("DisplayName = %q, want %q", meta.DisplayName, "Google")
	}
	if meta.Description != "Sign in with Google" {
		t.Fatalf("Description = %q, want %q", meta.Description, "Sign in with Google")
	}
	if len(meta.Warnings) != 1 || meta.Warnings[0] != "configured" {
		t.Fatalf("Warnings = %#v, want %#v", meta.Warnings, []string{"configured"})
	}
}

func TestStartRuntimeProviderTreatsUnimplementedAsNoop(t *testing.T) {
	t.Parallel()

	client := &fakeProviderLifecycleClient{
		startProvider: func(context.Context, *emptypb.Empty, ...grpc.CallOption) (*proto.StartRuntimeProviderResponse, error) {
			return nil, status.Error(codes.Unimplemented, "old provider")
		},
	}
	if err := StartRuntimeProvider(context.Background(), client); err != nil {
		t.Fatalf("StartRuntimeProvider: %v", err)
	}
}

func TestStartRuntimeProviderUsesStartupTimeout(t *testing.T) {
	t.Parallel()

	var remaining time.Duration
	client := &fakeProviderLifecycleClient{
		startProvider: func(ctx context.Context, _ *emptypb.Empty, _ ...grpc.CallOption) (*proto.StartRuntimeProviderResponse, error) {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("StartProvider context has no deadline")
			}
			remaining = time.Until(deadline)
			return &proto.StartRuntimeProviderResponse{ProtocolVersion: proto.CurrentProtocolVersion}, nil
		},
	}
	if err := StartRuntimeProvider(context.Background(), client); err != nil {
		t.Fatalf("StartRuntimeProvider: %v", err)
	}
	if remaining <= ProviderRPCTimeout {
		t.Fatalf("StartProvider remaining deadline = %s, want above request timeout %s", remaining, ProviderRPCTimeout)
	}
	if remaining > providerStartTimeout {
		t.Fatalf("StartProvider remaining deadline = %s, want at most startup timeout %s", remaining, providerStartTimeout)
	}
}

func TestConfigureRuntimeProviderUsesParentDeadlineWhenPresent(t *testing.T) {
	t.Parallel()

	parentDeadline := time.Now().Add(2 * providerConfigureTimeout)
	var configureRemaining time.Duration
	client := &fakeProviderLifecycleClient{
		getProviderIdentity: func(context.Context, *emptypb.Empty, ...grpc.CallOption) (*proto.ProviderIdentity, error) {
			return &proto.ProviderIdentity{
				Kind:               proto.ProviderKind_PROVIDER_KIND_AGENT,
				Name:               "simple",
				MinProtocolVersion: proto.CurrentProtocolVersion,
				MaxProtocolVersion: proto.CurrentProtocolVersion,
			}, nil
		},
		configureProvider: func(ctx context.Context, _ *proto.ConfigureProviderRequest, _ ...grpc.CallOption) (*proto.ConfigureProviderResponse, error) {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("ConfigureProvider context has no deadline")
			}
			configureRemaining = time.Until(deadline)
			return &proto.ConfigureProviderResponse{ProtocolVersion: proto.CurrentProtocolVersion}, nil
		},
	}
	ctx, cancel := context.WithDeadline(context.Background(), parentDeadline)
	defer cancel()
	if _, err := ConfigureRuntimeProvider(ctx, client, proto.ProviderKind_PROVIDER_KIND_AGENT, "simple", nil); err != nil {
		t.Fatalf("ConfigureRuntimeProvider: %v", err)
	}
	if configureRemaining <= providerConfigureTimeout {
		t.Fatalf("ConfigureProvider remaining deadline = %s, want above configure timeout %s", configureRemaining, providerConfigureTimeout)
	}
}

func TestProviderSessionCreateContextUsesBoundedFallbackWithoutParentDeadline(t *testing.T) {
	t.Parallel()

	ctx, cancel := ProviderSessionCreateContext(context.Background())
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("ProviderSessionCreateContext returned context without deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= ProviderRPCTimeout {
		t.Fatalf("remaining deadline = %s, want above provider RPC timeout %s", remaining, ProviderRPCTimeout)
	}
	if remaining > ProviderSessionCreateTimeout {
		t.Fatalf("remaining deadline = %s, want at most session create timeout %s", remaining, ProviderSessionCreateTimeout)
	}
}

func TestProviderSessionCreateContextPreservesLongParentDeadline(t *testing.T) {
	t.Parallel()

	parentDeadline := time.Now().Add(2 * ProviderSessionCreateTimeout)
	parent, parentCancel := context.WithDeadline(context.Background(), parentDeadline)
	defer parentCancel()
	ctx, cancel := ProviderSessionCreateContext(parent)
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("ProviderSessionCreateContext returned context without deadline")
	}
	if !deadline.Equal(parentDeadline) {
		t.Fatalf("deadline = %s, want parent deadline %s", deadline, parentDeadline)
	}
	if remaining := time.Until(deadline); remaining <= ProviderSessionCreateTimeout {
		t.Fatalf("remaining deadline = %s, want above fallback timeout %s", remaining, ProviderSessionCreateTimeout)
	}
}

func TestProviderSessionCreateContextPreservesShortParentDeadline(t *testing.T) {
	t.Parallel()

	parentDeadline := time.Now().Add(ProviderRPCTimeout / 2)
	parent, parentCancel := context.WithDeadline(context.Background(), parentDeadline)
	defer parentCancel()
	ctx, cancel := ProviderSessionCreateContext(parent)
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("ProviderSessionCreateContext returned context without deadline")
	}
	if !deadline.Equal(parentDeadline) {
		t.Fatalf("deadline = %s, want parent deadline %s", deadline, parentDeadline)
	}
	if remaining := time.Until(deadline); remaining >= ProviderRPCTimeout {
		t.Fatalf("remaining deadline = %s, want below provider RPC timeout %s", remaining, ProviderRPCTimeout)
	}
	parentCancel()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("session create context did not observe parent cancellation")
	}
}

func TestStartRuntimeProviderValidatesProtocolVersion(t *testing.T) {
	t.Parallel()

	client := &fakeProviderLifecycleClient{
		startProvider: func(context.Context, *emptypb.Empty, ...grpc.CallOption) (*proto.StartRuntimeProviderResponse, error) {
			return &proto.StartRuntimeProviderResponse{ProtocolVersion: proto.CurrentProtocolVersion + 1}, nil
		},
	}
	if err := StartRuntimeProvider(context.Background(), client); err == nil {
		t.Fatal("StartRuntimeProvider should reject protocol mismatch")
	}
}
