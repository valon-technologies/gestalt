package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginapi/v1"
	pluginsdk "github.com/valon-technologies/gestalt/sdk/pluginsdk"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type exampleRuntime struct {
	pluginapiv1.UnimplementedRuntimePluginServer
	hostConn *grpc.ClientConn
	host     pluginapiv1.RuntimeHostClient
}

func (r *exampleRuntime) Start(ctx context.Context, req *pluginapiv1.StartRuntimeRequest) (*emptypb.Empty, error) {
	conn, host, err := pluginsdk.DialRuntimeHost(ctx)
	if err != nil {
		return nil, err
	}
	r.hostConn = conn
	r.host = host

	caps, err := host.ListCapabilities(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	log.Printf("example runtime %q started with %d capabilities", req.GetName(), len(caps.GetCapabilities()))
	return &emptypb.Empty{}, nil
}

func (r *exampleRuntime) Stop(_ context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	if r.hostConn != nil {
		_ = r.hostConn.Close()
	}
	log.Println("example runtime stopped")
	return &emptypb.Empty{}, nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := pluginsdk.ServeRuntime(ctx, &exampleRuntime{}); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
