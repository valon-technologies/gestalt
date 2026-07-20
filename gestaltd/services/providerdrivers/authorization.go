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

type AuthorizationDeps struct {
	Gateway *providergateway.ProviderGatewayTransport
}

type AuthorizationBuildResult struct {
	Raw core.AuthorizationProvider
}

func AuthorizationFactory(ctx context.Context, name string, node yaml.Node, hostServices []runtimehost.HostService, deps AuthorizationDeps) (AuthorizationBuildResult, error) {
	var cfg componentprovider.YAMLConfig
	if err := node.Decode(&cfg); err != nil {
		return AuthorizationBuildResult{}, fmt.Errorf("authorization provider: parsing config: %w", err)
	}
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
	}

	exec, err := authorizationservice.StartExecutable(ctx, execCfg)
	if err != nil {
		return AuthorizationBuildResult{}, err
	}

	if deps.Gateway != nil {
		target := providergateway.ProviderTarget{Kind: providergateway.ProviderKindAuthorization, Name: name}
		if err := deps.Gateway.RegisterDirect(target, providergateway.DirectEndpoint{Conn: exec.Conn()}); err != nil {
			_ = exec.Close()
			return AuthorizationBuildResult{}, err
		}
		raw, err := authorizationservice.NewFromExecutable(exec, deps.Gateway.Conn(target))
		if err != nil {
			_ = exec.Close()
			return AuthorizationBuildResult{}, err
		}
		return AuthorizationBuildResult{Raw: raw}, nil
	}

	raw, err := authorizationservice.NewFromExecutable(exec, exec.Conn())
	if err != nil {
		_ = exec.Close()
		return AuthorizationBuildResult{}, err
	}

	return AuthorizationBuildResult{Raw: raw}, nil
}
