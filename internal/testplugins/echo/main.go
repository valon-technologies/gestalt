package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/pluginapi"
	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginapi/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: gestalt-plugin-echo <provider|runtime>")
	}
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch os.Args[1] {
	case "provider":
		return pluginapi.ServeProvider(ctx, newProxyProvider(&echoProvider{}))
	case "runtime":
		return pluginapi.ServeRuntime(ctx, &echoRuntimePlugin{})
	default:
		return fmt.Errorf("unknown mode %q", os.Args[1])
	}
}

var _ core.Provider = (*echoProvider)(nil)

type echoProvider struct{}

func (p *echoProvider) Name() string                        { return "echo" }
func (p *echoProvider) DisplayName() string                 { return "Echo" }
func (p *echoProvider) Description() string                 { return "Echoes back the input parameters" }
func (p *echoProvider) ConnectionMode() core.ConnectionMode { return core.ConnectionModeNone }

func (p *echoProvider) ListOperations() []core.Operation {
	return []core.Operation{
		{Name: "echo", Description: "Echo back input params as JSON", Method: http.MethodPost},
	}
}

func (p *echoProvider) Execute(_ context.Context, operation string, params map[string]any, _ string) (*core.OperationResult, error) {
	if operation != "echo" {
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}
	body, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshaling params: %w", err)
	}
	return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
}

type echoRuntimePlugin struct {
	pluginapiv1.UnimplementedRuntimePluginServer
	hostConn *grpc.ClientConn
	host     pluginapiv1.RuntimeHostClient
	name     string
}

func (p *echoRuntimePlugin) Start(ctx context.Context, req *pluginapiv1.StartRuntimeRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	if p.hostConn == nil {
		conn, host, err := pluginapi.DialRuntimeHost(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Unavailable, "dial runtime host: %v", err)
		}
		p.hostConn = conn
		p.host = host
	}

	capsResp, err := p.host.ListCapabilities(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "list capabilities: %v", err)
	}

	cfg := map[string]any(nil)
	if req.GetConfig() != nil {
		cfg = req.GetConfig().AsMap()
	}

	record := map[string]any{
		"name":             req.GetName(),
		"capability_count": len(capsResp.GetCapabilities()),
		"capabilities":     capabilityNames(capsResp.GetCapabilities()),
	}
	if len(cfg) > 0 {
		record["config"] = cfg
	}

	probeProvider, _ := cfg["probe_provider"].(string)
	probeOperation, _ := cfg["probe_operation"].(string)
	if probeProvider != "" && probeOperation != "" {
		probeParams, _ := cfg["probe_params"].(map[string]any)
		params, err := structpb.NewStruct(probeParams)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "probe_params: %v", err)
		}
		resp, err := p.host.Invoke(ctx, &pluginapiv1.InvokeRequest{
			Principal: &pluginapiv1.Principal{},
			Provider:  probeProvider,
			Operation: probeOperation,
			Params:    params,
		})
		if err != nil {
			return nil, status.Errorf(codes.Unknown, "probe invoke: %v", err)
		}
		record["probe_status"] = resp.GetStatus()
		record["probe_body"] = resp.GetBody()
	}

	if outputFile, _ := cfg["output_file"].(string); outputFile != "" {
		if err := writeJSON(outputFile, record); err != nil {
			return nil, status.Errorf(codes.Internal, "write output: %v", err)
		}
	}

	p.name = req.GetName()
	log.Printf("echo runtime %q started with %d capabilities", p.name, len(capsResp.GetCapabilities()))
	return &emptypb.Empty{}, nil
}

func (p *echoRuntimePlugin) Stop(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	if p.hostConn != nil {
		_ = p.hostConn.Close()
		p.hostConn = nil
		p.host = nil
	}
	log.Printf("echo runtime %q stopped", p.name)
	return &emptypb.Empty{}, nil
}

func capabilityNames(caps []*pluginapiv1.Capability) []string {
	names := make([]string, 0, len(caps))
	for _, cap := range caps {
		names = append(names, cap.GetProvider()+"."+cap.GetOperation())
	}
	return names
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
