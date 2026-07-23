package runtimehost

import (
	"context"
	"sync/atomic"
	"testing"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestConfigureRuntimeProviderRetriesTransientFailures(t *testing.T) {
	t.Parallel()

	var configureCalls atomic.Int32
	client := &fakeProviderLifecycleClient{
		getProviderIdentity: func(context.Context, *emptypb.Empty, ...grpc.CallOption) (*proto.ProviderIdentity, error) {
			return &proto.ProviderIdentity{
				Kind:               proto.ProviderKind_PROVIDER_KIND_AGENT,
				Name:               "simple",
				MinProtocolVersion: proto.CurrentProtocolVersion,
				MaxProtocolVersion: proto.CurrentProtocolVersion,
			}, nil
		},
		configureProvider: func(context.Context, *proto.ConfigureProviderRequest, ...grpc.CallOption) (*proto.ConfigureProviderResponse, error) {
			if configureCalls.Add(1) < 2 {
				return nil, status.Error(codes.Unavailable, "relay warming up")
			}
			return &proto.ConfigureProviderResponse{ProtocolVersion: proto.CurrentProtocolVersion}, nil
		},
	}

	meta, err := ConfigureRuntimeProvider(context.Background(), client, proto.ProviderKind_PROVIDER_KIND_AGENT, "simple", nil)
	if err != nil {
		t.Fatalf("ConfigureRuntimeProvider: %v", err)
	}
	if meta == nil || meta.Name != "simple" {
		t.Fatalf("meta = %#v", meta)
	}
	if got := configureCalls.Load(); got != 2 {
		t.Fatalf("ConfigureProvider calls = %d, want 2", got)
	}
}

func TestStartRuntimeProviderRetriesTransientFailures(t *testing.T) {
	t.Parallel()

	var startCalls atomic.Int32
	client := &fakeProviderLifecycleClient{
		startProvider: func(context.Context, *emptypb.Empty, ...grpc.CallOption) (*proto.StartRuntimeProviderResponse, error) {
			if startCalls.Add(1) < 2 {
				return nil, status.Error(codes.Unavailable, "relay warming up")
			}
			return &proto.StartRuntimeProviderResponse{ProtocolVersion: proto.CurrentProtocolVersion}, nil
		},
	}
	if err := StartRuntimeProvider(context.Background(), client); err != nil {
		t.Fatalf("StartRuntimeProvider: %v", err)
	}
	if got := startCalls.Load(); got != 2 {
		t.Fatalf("StartProvider calls = %d, want 2", got)
	}
}
