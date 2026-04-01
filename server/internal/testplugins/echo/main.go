package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/pluginhost"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

func main() {
	if len(os.Args) < 2 {
		slog.Error("usage", "command", "gestalt-plugin-echo <provider|runtime>")
		os.Exit(2)
	}
	if err := run(); err != nil {
		slog.Error("echo plugin failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch os.Args[1] {
	case "provider":
		return pluginhost.ServeProvider(ctx, newProxyProvider(&echoProvider{}))
	case "runtime":
		return pluginhost.ServeRuntime(ctx, &echoRuntimePlugin{})
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
func (p *echoProvider) Catalog() *catalog.Catalog {
	return &catalog.Catalog{
		Name:        p.Name(),
		DisplayName: p.DisplayName(),
		Description: p.Description(),
		Operations: []catalog.CatalogOperation{
			{
				ID:          "echo",
				Description: "Echo back input params as JSON",
				Method:      http.MethodPost,
				Transport:   catalog.TransportPlugin,
			},
		},
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
	proto.UnimplementedRuntimePluginServer
	hostConn *grpc.ClientConn
	host     proto.RuntimeHostClient
	name     string
}

func (p *echoRuntimePlugin) Start(ctx context.Context, req *proto.StartRuntimeRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	if p.hostConn == nil {
		conn, host, err := pluginhost.DialRuntimeHost(ctx)
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
		resp, err := p.host.Invoke(ctx, &proto.InvokeRequest{
			Principal: &proto.Principal{},
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
	slog.Info("echo runtime started", "runtime", p.name, "capability_count", len(capsResp.GetCapabilities()))
	return &emptypb.Empty{}, nil
}

func (p *echoRuntimePlugin) Stop(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	if p.hostConn != nil {
		_ = p.hostConn.Close()
		p.hostConn = nil
		p.host = nil
	}
	slog.Info("echo runtime stopped", "runtime", p.name)
	return &emptypb.Empty{}, nil
}

func capabilityNames(caps []*proto.Capability) []string {
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
