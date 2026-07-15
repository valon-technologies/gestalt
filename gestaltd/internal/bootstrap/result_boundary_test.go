package bootstrap_test

import (
	"context"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
)

type closableProvider struct {
	coretesting.StubIntegration
	closeFn func() error
}

func (p *closableProvider) Close() error {
	if p.closeFn != nil {
		return p.closeFn()
	}
	return nil
}

type closableSecretManager struct {
	coretesting.StubSecretManager
	closeFn func() error
}

func (s *closableSecretManager) Close() error {
	if s.closeFn != nil {
		return s.closeFn()
	}
	return nil
}

type closableWorkflowProvider struct {
	coreworkflow.Provider
	closeFn func() error
}

func (p closableWorkflowProvider) Close() error { return p.closeFn() }

type closableAgentProvider struct {
	coreagent.Provider
	closeFn func() error
}

func (p closableAgentProvider) Close() error { return p.closeFn() }

func TestResultClose_ShutsDownConstructedResources(t *testing.T) {
	t.Parallel()

	var closed []string

	result := &bootstrap.Result{
		Providers: registryWithProvider(t, "acme", &closableProvider{
			StubIntegration: coretesting.StubIntegration{N: "acme"},
			closeFn: func() error {
				closed = append(closed, "app")
				return nil
			},
		}),
		ExtraWorkflows: []coreworkflow.Provider{closableWorkflowProvider{closeFn: func() error {
			closed = append(closed, "workflow")
			return nil
		}}},
		ExtraAgents: []coreagent.Provider{closableAgentProvider{closeFn: func() error {
			closed = append(closed, "agent")
			return nil
		}}},
		Services: testutil.NewStubServices(t),
		SecretManager: &closableSecretManager{
			closeFn: func() error {
				closed = append(closed, "secret manager")
				return nil
			},
		},
	}

	if err := result.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := result.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got, want := strings.Join(closed, ", "), "workflow, agent, app, secret manager"; got != want {
		t.Errorf("shutdown order = %q, want %q", got, want)
	}
}

func registryWithProvider(t *testing.T, name string, p *closableProvider) *registry.ProviderMap[core.Provider] {
	t.Helper()
	r := registry.New()
	_ = r.Providers.Register(name, p)
	return &r.Providers
}
