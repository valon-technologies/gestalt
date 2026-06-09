package secrets

import (
	"context"
	"fmt"
	"io"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
)

type ExecConfig struct {
	Command    string
	Args       []string
	Workdir    string
	Env        map[string]string
	Config     map[string]any
	Egress     egress.Policy
	HostBinary string
	Cleanup    func()
	Name       string
}

type remoteSecretManager struct {
	client proto.SecretsClient
	closer io.Closer
}

func NewExecutable(ctx context.Context, cfg ExecConfig) (core.SecretManager, error) {
	proc, err := runtimehost.StartAppProcess(ctx, runtimehost.ProcessConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Workdir:      cfg.Workdir,
		Env:          cfg.Env,
		Egress:       cfg.Egress,
		HostBinary:   cfg.HostBinary,
		Cleanup:      cfg.Cleanup,
		ProviderName: cfg.Name,
	})
	if err != nil {
		return nil, err
	}

	runtimeClient := proc.Lifecycle()
	secretsClient := proto.NewSecretsClient(proc.Conn())

	_, err = runtimehost.ConfigureRuntimeProvider(ctx, runtimeClient, proto.ProviderKind_PROVIDER_KIND_SECRETS, cfg.Name, cfg.Config)
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
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
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
