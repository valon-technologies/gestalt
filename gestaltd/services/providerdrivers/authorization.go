package providerdrivers

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	authorizationservice "github.com/valon-technologies/gestalt/server/services/authorization"
	"github.com/valon-technologies/gestalt/server/services/providerdrivers/componentprovider"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"gopkg.in/yaml.v3"
)

const CallerTokenPublicKeyEnv = "GESTALTD_CALLER_TOKEN_ED25519_PUBLIC_KEY"

type AuthorizationDeps struct {
	Transport            providergateway.Transport
	CallerTokenPublicKey string
}

type AuthorizationBuildResult struct {
	Raw     core.AuthorizationProvider
	Guarded core.AuthorizationProvider
}

func AuthorizationFactory(ctx context.Context, name string, node yaml.Node, hostServices []runtimehost.HostService, deps AuthorizationDeps) (AuthorizationBuildResult, error) {
	var cfg componentprovider.YAMLConfig
	if err := node.Decode(&cfg); err != nil {
		return AuthorizationBuildResult{}, fmt.Errorf("authorization provider: parsing config: %w", err)
	}
	cfg.Env = authorizationEnvWithCallerTokenPublicKey(cfg.Env, deps.CallerTokenPublicKey)
	prepared, err := componentprovider.PrepareExecution(componentprovider.PrepareParams{
		Kind:                 providermanifestv1.KindAuthorization,
		Subject:              "authorization provider",
		SourceMissingMessage: "no Go authorization provider source package found",
		Config:               cfg,
	})
	if err != nil {
		return AuthorizationBuildResult{}, err
	}
	cfg = prepared.YAMLConfig

	transport := deps.Transport
	if transport == nil {
		transport = providergateway.DirectTransport{}
	}
	execCfg := authorizationservice.ExecConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Workdir:      cfg.Workdir,
		Env:          cfg.Env,
		Config:       cfg.Config,
		Egress:       cfg.EgressPolicy(""),
		HostBinary:   cfg.HostBinary,
		Cleanup:      prepared.Cleanup,
		HostServices: hostServices,
		Name:         name,
		Transport:    transport,
	}

	exec, err := authorizationservice.StartExecutable(ctx, execCfg)
	if err != nil {
		return AuthorizationBuildResult{}, err
	}

	raw, err := authorizationservice.NewFromExecutable(exec, execCfg)
	if err != nil {
		_ = exec.Close()
		return AuthorizationBuildResult{}, err
	}

	gatewayTransport := providergateway.NewProviderGatewayTransport()
	gatewayTransport.SetAuthorizationProvider(raw)
	gatewayTransport.SetCallerTokenPublicKey(deps.CallerTokenPublicKey)

	execCfg.Transport = gatewayTransport
	guarded, err := authorizationservice.NewFromExecutable(exec, execCfg)
	if err != nil {
		_ = exec.Close()
		return AuthorizationBuildResult{}, err
	}

	return AuthorizationBuildResult{
		Raw:     raw,
		Guarded: guarded,
	}, nil
}

func authorizationEnvWithCallerTokenPublicKey(env map[string]string, publicKey string) map[string]string {
	if publicKey == "" || env[CallerTokenPublicKeyEnv] != "" {
		return env
	}
	next := make(map[string]string, len(env)+1)
	for key, value := range env {
		next[key] = value
	}
	next[CallerTokenPublicKeyEnv] = publicKey
	return next
}
