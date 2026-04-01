package gestalt_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestServeProviderRoundTrip(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "plugin.sock")
	t.Setenv(proto.EnvPluginSocket, socket)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- gestalt.ServeProvider(ctx, &stubProvider{
			name:        "test-provider",
			displayName: "Test Provider",
			connMode:    gestalt.ConnectionModeEither,
		})
	}()
	t.Cleanup(func() {
		cancel()
		waitServeResult(t, errCh)
	})

	conn := newUnixConn(t, socket)
	client := proto.NewProviderPluginClient(conn)

	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), time.Second)
	defer rpcCancel()

	meta, err := client.GetMetadata(rpcCtx, &emptypb.Empty{}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if meta.GetName() != "test-provider" || meta.GetConnectionMode() != proto.ConnectionMode_CONNECTION_MODE_EITHER {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
}
