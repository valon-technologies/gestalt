package models

import (
	"context"
	"fmt"
	"io"
	"time"

	coremodel "github.com/valon-technologies/gestalt/server/core/model"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const modelGenerateRPCTimeout = 5 * time.Minute

type ExecConfig struct {
	Command      string
	Args         []string
	Env          map[string]string
	Config       map[string]any
	Egress       egress.Policy
	HostBinary   string
	Cleanup      func()
	HostServices []runtimehost.HostService
	Name         string
	Telemetry    metricutil.TelemetryProviders
}

type RemoteConfig struct {
	Client  proto.ModelProviderClient
	Runtime proto.ProviderLifecycleClient
	Closer  io.Closer
	Config  map[string]any
	Name    string
}

type remoteModel struct {
	client  proto.ModelProviderClient
	runtime proto.ProviderLifecycleClient
	closer  io.Closer
}

func NewExecutable(ctx context.Context, cfg ExecConfig) (coremodel.Provider, error) {
	proc, err := runtimehost.StartPluginProcess(ctx, runtimehost.ProcessConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Env:          cfg.Env,
		Egress:       cfg.Egress,
		HostBinary:   cfg.HostBinary,
		Cleanup:      cfg.Cleanup,
		HostServices: cfg.HostServices,
		ProviderName: cfg.Name,
		Telemetry:    cfg.Telemetry,
	})
	if err != nil {
		return nil, err
	}
	return NewRemote(ctx, RemoteConfig{
		Client:  proto.NewModelProviderClient(proc.Conn()),
		Runtime: proc.Lifecycle(),
		Closer:  proc,
		Config:  cfg.Config,
		Name:    cfg.Name,
	})
}

func NewRemote(ctx context.Context, cfg RemoteConfig) (coremodel.Provider, error) {
	if cfg.Client == nil {
		return nil, fmt.Errorf("model provider client is required")
	}
	if cfg.Runtime == nil {
		return nil, fmt.Errorf("model provider lifecycle client is required")
	}
	if _, err := runtimehost.ConfigureRuntimeProvider(ctx, cfg.Runtime, proto.ProviderKind_PROVIDER_KIND_MODEL, cfg.Name, cfg.Config); err != nil {
		if cfg.Closer != nil {
			_ = cfg.Closer.Close()
		}
		return nil, err
	}
	return &remoteModel{client: cfg.Client, runtime: cfg.Runtime, closer: cfg.Closer}, nil
}

func (r *remoteModel) Generate(ctx context.Context, req coremodel.GenerateRequest) (*coremodel.GenerateResponse, error) {
	ctx, cancel := modelGenerateContext(ctx)
	defer cancel()
	messages, err := modelMessagesToProto(req.Messages)
	if err != nil {
		return nil, err
	}
	responseSchema, err := structFromMap(req.ResponseSchema)
	if err != nil {
		return nil, err
	}
	modelOptions, err := structFromMap(req.ModelOptions)
	if err != nil {
		return nil, err
	}
	metadata, err := structFromMap(req.Metadata)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.Generate(ctx, &proto.GenerateModelRequest{
		ProviderName:     req.ProviderName,
		Model:            req.Model,
		Messages:         messages,
		ResponseSchema:   responseSchema,
		ModelOptions:     modelOptions,
		Metadata:         metadata,
		Subject:          modelSubjectContextToProto(req.Subject),
		CallerPluginName: req.CallerPluginName,
	})
	if err != nil {
		return nil, err
	}
	return modelGenerateResponseFromProto(resp)
}

func (r *remoteModel) GetCapabilities(ctx context.Context, req coremodel.GetCapabilitiesRequest) (*coremodel.ProviderCapabilities, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.client.GetCapabilities(ctx, &proto.GetModelProviderCapabilitiesRequest{})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return &coremodel.ProviderCapabilities{}, nil
		}
		return nil, err
	}
	return modelProviderCapabilitiesFromProto(resp), nil
}

func (r *remoteModel) Ping(ctx context.Context) error {
	if err := runtimehost.CheckRuntimeProviderHealth(ctx, r.runtime); err != nil {
		return err
	}
	resp, err := r.GetCapabilities(ctx, coremodel.GetCapabilitiesRequest{})
	if err != nil {
		return fmt.Errorf("model provider capabilities check failed: %w", err)
	}
	if resp == nil {
		return fmt.Errorf("model provider capabilities check returned nil response")
	}
	return nil
}

func (r *remoteModel) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}
	return r.closer.Close()
}

var _ coremodel.Provider = (*remoteModel)(nil)

func modelGenerateContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, modelGenerateRPCTimeout)
}
