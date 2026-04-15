package providerhost

import (
	"context"
	"testing"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type fakeProviderLifecycleClient struct {
	getProviderIdentity func(context.Context, *emptypb.Empty, ...grpc.CallOption) (*proto.ProviderIdentity, error)
	configureProvider   func(context.Context, *proto.ConfigureProviderRequest, ...grpc.CallOption) (*proto.ConfigureProviderResponse, error)
	healthCheck         func(context.Context, *emptypb.Empty, ...grpc.CallOption) (*proto.HealthCheckResponse, error)
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

func TestConfigureRuntimeProviderRefreshesMetadataAfterConfigure(t *testing.T) {
	t.Parallel()

	getCalls := 0
	configured := false
	client := &fakeProviderLifecycleClient{
		getProviderIdentity: func(_ context.Context, _ *emptypb.Empty, _ ...grpc.CallOption) (*proto.ProviderIdentity, error) {
			getCalls++
			if configured {
				return &proto.ProviderIdentity{
					Kind:               proto.ProviderKind_PROVIDER_KIND_AUTH,
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
				Kind:               proto.ProviderKind_PROVIDER_KIND_AUTH,
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

	meta, err := configureRuntimeProvider(
		context.Background(),
		client,
		proto.ProviderKind_PROVIDER_KIND_AUTH,
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
