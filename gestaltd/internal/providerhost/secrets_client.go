package providerhost

import (
	"context"
	"fmt"
	"io"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
)

type SecretsExecConfig struct {
	Command      string
	Args         []string
	Env          map[string]string
	Config       map[string]any
	AllowedHosts []string
	HostBinary   string
	Cleanup      func()
	Name         string
}

type remoteSecretManager struct {
	client proto.SecretsProviderClient
	closer io.Closer
}

func NewExecutableSecretManager(ctx context.Context, cfg SecretsExecConfig) (core.SecretManager, error) {
	execCfg := ExecConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Env:          cfg.Env,
		Config:       cfg.Config,
		AllowedHosts: cfg.AllowedHosts,
		HostBinary:   cfg.HostBinary,
		Cleanup:      cfg.Cleanup,
		ProviderName: cfg.Name,
	}
	proc, err := startProviderProcess(ctx, execCfg.processConfig())
	if err != nil {
		return nil, err
	}

	runtimeClient := proto.NewProviderLifecycleClient(proc.conn)
	secretsClient := proto.NewSecretsProviderClient(proc.conn)

	_, err = ConfigureRuntimeProvider(ctx, runtimeClient, proto.ProviderKind_PROVIDER_KIND_SECRETS, cfg.Name, cfg.Config)
	if err != nil {
		_ = proc.Close()
		return nil, err
	}

	return &remoteSecretManager{
		client: secretsClient,
		closer: proc,
	}, nil
}

func (r *remoteSecretManager) GetSecret(ctx context.Context, name string) (string, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()

	resp, err := r.client.GetSecret(ctx, &proto.GetSecretRequest{Name: name})
	if err != nil {
		return "", fmt.Errorf("get secret: %w", err)
	}
	return resp.GetValue(), nil
}

func (r *remoteSecretManager) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}
	return r.closer.Close()
}

var _ core.SecretManager = (*remoteSecretManager)(nil)
