package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginapi/v1"
	"github.com/valon-technologies/gestalt/sdk/pluginsdk"
	"google.golang.org/protobuf/types/known/emptypb"
)

type runtimeServer struct {
	pluginapiv1.UnimplementedRuntimePluginServer
}

func (s *runtimeServer) Start(ctx context.Context, req *pluginapiv1.StartRuntimeRequest) (*emptypb.Empty, error) {
	log.Printf("runtime started: name=%s", req.GetName())

	if cfg := req.GetConfig(); cfg != nil {
		for k, v := range cfg.GetFields() {
			log.Printf("  config: %s = %v", k, v.AsInterface())
		}
	}

	for _, cap := range req.GetInitialCapabilities() {
		log.Printf("  initial capability: %s/%s", cap.GetProvider(), cap.GetOperation())
	}

	conn, host, err := pluginsdk.DialRuntimeHost(ctx)
	if err != nil {
		log.Printf("could not dial runtime host (expected in standalone mode): %v", err)
		return &emptypb.Empty{}, nil
	}
	defer conn.Close()

	caps, err := host.ListCapabilities(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}

	log.Printf("discovered %d capabilities from host:", len(caps.GetCapabilities()))
	for _, cap := range caps.GetCapabilities() {
		log.Printf("  %s/%s: %s", cap.GetProvider(), cap.GetOperation(), cap.GetDescription())
	}

	return &emptypb.Empty{}, nil
}

func (s *runtimeServer) Stop(_ context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	log.Println("runtime stopping")
	return &emptypb.Empty{}, nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Println("starting example runtime plugin")
	if err := pluginsdk.ServeRuntime(ctx, &runtimeServer{}); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
