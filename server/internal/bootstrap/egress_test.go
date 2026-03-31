package bootstrap_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/egress"
)

func TestEgressPolicyWiredThroughBootstrap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	cfg.Egress = config.EgressConfig{
		DefaultAction: "deny",
		Policies: []config.EgressPolicyRule{
			{Action: "allow", Provider: "alpha", PathPrefix: "/v1/public"},
		},
	}
	cfg.Bindings = map[string]config.BindingDef{
		"my-binding": {
			Type:      "test-binding",
			Providers: []string{"alpha"},
		},
	}

	factories := validFactories()

	var receivedEgress bootstrap.EgressDeps
	factories.Bindings["test-binding"] = func(_ context.Context, name string, _ config.BindingDef, deps bootstrap.BindingDeps) (core.Binding, error) {
		receivedEgress = deps.Egress
		return &coretesting.StubBinding{N: name}, nil
	}

	result, err := bootstrap.Bootstrap(ctx, cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady

	if receivedEgress.Resolver == nil {
		t.Fatal("expected egress resolver to be wired into binding deps")
	}
	if receivedEgress.Resolver.Policy == nil {
		t.Fatal("expected policy enforcer to be set")
	}

	resolve := func(path string) error {
		_, err := receivedEgress.Resolver.Resolve(ctx, egress.ResolutionInput{
			Target: egress.Target{Provider: "alpha", Method: http.MethodGet, Host: "api.test", Path: path},
		})
		return err
	}

	if err := resolve("/v1/public/items"); err != nil {
		t.Fatalf("allowed path should pass: %v", err)
	}
	if err := resolve("/v1/admin/users"); err == nil {
		t.Fatal("default-deny should block unmatched path")
	}
}
