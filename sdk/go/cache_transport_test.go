package gestalt_test

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestTransportCacheNamedSocketEnv(t *testing.T) {
	t.Setenv(gestalt.EnvHostServiceSocket, "unix://"+testCacheSocket)
	client, err := gestalt.Cache("test")
	if err != nil {
		t.Fatalf("connect named cache: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	if err := client.Set(ctx, "named", []byte("ok"), gestalt.CacheSetOptions{}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	value, found, err := client.Get(ctx, "named")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found || string(value) != "ok" {
		t.Fatalf("Get = (%q, %v), want (%q, true)", value, found, "ok")
	}
}

func TestTransportCacheNamedBindingMetadata(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	harness := &cacheBindingMetadataHarness{binding: make(chan string, 1)}
	srv := grpc.NewServer()
	proto.RegisterCacheServer(srv, harness)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvHostServiceSocket, "tcp://"+lis.Addr().String())
	client, err := gestalt.Cache("archive")
	if err != nil {
		t.Fatalf("connect cache: %v", err)
	}
	defer func() { _ = client.Close() }()

	if err := client.Set(context.Background(), "key", []byte("value"), gestalt.CacheSetOptions{}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := <-harness.binding; got != "archive" {
		t.Fatalf("binding metadata = %q, want archive", got)
	}
}

type cacheBindingMetadataHarness struct {
	proto.UnimplementedCacheServer
	binding chan string
}

func (h *cacheBindingMetadataHarness) Set(ctx context.Context, _ *proto.CacheSetRequest) (*emptypb.Empty, error) {
	h.binding <- firstMetadataValue(ctx, gestalt.HostServiceBindingMetadata)
	return &emptypb.Empty{}, nil
}

func TestTransportCacheTCPTargetEnv(t *testing.T) {
	bin, target, cmd := buildAndStartTCPHarness("cachetransportd", "")
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = os.Remove(bin)
	})

	t.Setenv(gestalt.EnvHostServiceSocket, target)
	client, err := gestalt.Cache("tcp")
	if err != nil {
		t.Fatalf("connect tcp cache: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	if err := client.Set(ctx, "tcp", []byte("ok"), gestalt.CacheSetOptions{}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	value, found, err := client.Get(ctx, "tcp")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found || string(value) != "ok" {
		t.Fatalf("Get = (%q, %v), want (%q, true)", value, found, "ok")
	}
}

func TestTransportCacheTCPTargetTokenEnv(t *testing.T) {
	const token = "relay-token-go"
	bin, target, cmd := buildAndStartTCPHarness("cachetransportd", token)
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = os.Remove(bin)
	})

	t.Setenv(gestalt.EnvHostServiceSocket, target)
	t.Setenv(gestalt.EnvHostServiceToken, token)
	client, err := gestalt.Cache("tcp-token")
	if err != nil {
		t.Fatalf("connect tcp cache with token: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	if err := client.Set(ctx, "tcp-token", []byte("relay"), gestalt.CacheSetOptions{}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	value, found, err := client.Get(ctx, "tcp-token")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found || string(value) != "relay" {
		t.Fatalf("Get = (%q, %v), want (%q, true)", value, found, "relay")
	}
}

func TestTransportCacheRoundTrip(t *testing.T) {
	ctx := context.Background()
	if err := testCacheClient.SetMany(ctx, []gestalt.CacheEntry{
		{Key: "alpha", Value: []byte("one")},
		{Key: "beta", Value: []byte("two")},
	}, gestalt.CacheSetOptions{TTL: time.Minute}); err != nil {
		t.Fatalf("SetMany: %v", err)
	}

	values, err := testCacheClient.GetMany(ctx, []string{"alpha", "beta", "missing"})
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	if got := string(values["alpha"]); got != "one" {
		t.Fatalf(`GetMany["alpha"] = %q, want %q`, got, "one")
	}
	if got := string(values["beta"]); got != "two" {
		t.Fatalf(`GetMany["beta"] = %q, want %q`, got, "two")
	}
	if _, ok := values["missing"]; ok {
		t.Fatal(`GetMany["missing"] should be absent`)
	}

	touched, err := testCacheClient.Touch(ctx, "alpha", time.Minute)
	if err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if !touched {
		t.Fatal("Touch returned false, want true")
	}

	deleted, err := testCacheClient.Delete(ctx, "alpha")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !deleted {
		t.Fatal("Delete returned false, want true")
	}

	deletedCount, err := testCacheClient.DeleteMany(ctx, []string{"beta", "missing"})
	if err != nil {
		t.Fatalf("DeleteMany: %v", err)
	}
	if deletedCount != 1 {
		t.Fatalf("DeleteMany deleted = %d, want 1", deletedCount)
	}
}
