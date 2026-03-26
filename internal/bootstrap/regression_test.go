package bootstrap_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	coretesting "github.com/valon-technologies/gestalt/core/testing"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/egress"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/principal"
	"gopkg.in/yaml.v3"
)

func TestGatewayMode_NoRuntimesOrBindingsRequired(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	cfg.Runtimes = nil
	cfg.Bindings = nil

	result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if result.Invoker == nil {
		t.Fatal("expected Invoker to be non-nil")
	}
	if result.CapabilityLister == nil {
		t.Fatal("expected CapabilityLister to be non-nil")
	}
	invoker, ok := result.Invoker.(*invocation.Broker)
	if !ok {
		t.Fatalf("expected Invoker to be *invocation.Broker, got %T", result.Invoker)
	}
	lister, ok := result.CapabilityLister.(*invocation.Broker)
	if !ok {
		t.Fatalf("expected CapabilityLister to be *invocation.Broker, got %T", result.CapabilityLister)
	}
	if invoker != lister {
		t.Fatal("expected Invoker and CapabilityLister to reference the same shared instance")
	}
	<-result.ProvidersReady
	names := result.Providers.List()
	if len(names) != 1 || names[0] != "alpha" {
		t.Errorf("Providers.List: got %v, want [alpha]", names)
	}
	if result.Runtimes != nil {
		t.Error("expected Runtimes to be nil")
	}
	if result.Bindings != nil {
		t.Error("expected Bindings to be nil")
	}
}

func TestPlatformMode_BindingsAndRuntimesWithSafetyLayer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	cfg.Integrations["beta"] = config.IntegrationDef{}

	cfg.Runtimes = map[string]config.RuntimeDef{
		"my-runtime": {
			Type:      "echo",
			Providers: []string{"alpha"},
		},
	}
	cfg.Bindings = map[string]config.BindingDef{
		"my-binding": {
			Type:      "test-binding",
			Providers: []string{"beta"},
		},
	}

	factories := validFactories()
	factories.Providers["alpha"] = func(_ context.Context, _ string, _ config.IntegrationDef, _ bootstrap.Deps) (core.Provider, error) {
		return &stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "alpha",
				ConnMode: core.ConnectionModeNone,
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
					return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
				},
			},
			ops: []core.Operation{{Name: "do", Method: http.MethodPost}},
		}, nil
	}
	factories.Providers["beta"] = func(_ context.Context, _ string, _ config.IntegrationDef, _ bootstrap.Deps) (core.Provider, error) {
		return &stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "beta",
				ConnMode: core.ConnectionModeNone,
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
					return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
				},
			},
			ops: []core.Operation{{Name: "do", Method: http.MethodPost}},
		}, nil
	}

	var runtimeDeps bootstrap.RuntimeDeps
	factories.Runtimes["echo"] = func(_ context.Context, name string, _ config.RuntimeDef, deps bootstrap.RuntimeDeps) (core.Runtime, error) {
		runtimeDeps = deps
		return &coretesting.StubRuntime{N: name}, nil
	}

	var bindingDeps bootstrap.BindingDeps
	factories.Bindings["test-binding"] = func(_ context.Context, name string, _ config.BindingDef, deps bootstrap.BindingDeps) (core.Binding, error) {
		bindingDeps = deps
		return &coretesting.StubBinding{N: name}, nil
	}

	result, err := bootstrap.Bootstrap(ctx, cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady
	if result.AuditSink == nil {
		t.Fatal("expected AuditSink to be non-nil")
	}

	if runtimeDeps.Invoker == nil {
		t.Fatal("expected runtime invoker to be non-nil")
	}
	if runtimeDeps.CapabilityLister == nil {
		t.Fatal("expected runtime capability lister to be non-nil")
	}
	if any(runtimeDeps.Invoker) != any(runtimeDeps.CapabilityLister) {
		t.Fatal("expected runtime invoker and capability lister to be the same scoped instance")
	}

	rtCaps := runtimeDeps.CapabilityLister.ListCapabilities()
	for _, cap := range rtCaps {
		if cap.Provider != "alpha" {
			t.Errorf("runtime broker should only see alpha, got %q", cap.Provider)
		}
	}

	if bindingDeps.Invoker == nil {
		t.Fatal("expected binding deps to carry an invoker")
	}
	if bindingDeps.Invoker == result.Invoker {
		t.Fatal("expected binding deps to be scoped, not the shared invoker")
	}

	_, err = bindingDeps.Invoker.Invoke(ctx, &principal.Principal{}, "alpha", "", "do", nil)
	if err == nil || !strings.Contains(err.Error(), "not available in this scope") {
		t.Fatalf("expected scoped binding invoker to reject alpha, got %v", err)
	}

	resultOp, err := bindingDeps.Invoker.Invoke(ctx, &principal.Principal{}, "beta", "", "do", nil)
	if err != nil {
		t.Fatalf("expected scoped binding invoker to allow beta: %v", err)
	}
	if resultOp.Status != http.StatusOK {
		t.Fatalf("expected binding invoke status 200, got %d", resultOp.Status)
	}
}

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
			Target: egress.Target{Provider: "alpha", Method: "GET", Host: "api.test", Path: path},
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

func TestEgressCredentialGrantWiredThroughBootstrap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	cfg.Egress = config.EgressConfig{
		Credentials: []config.EgressCredentialGrant{
			{Provider: "alpha", Host: "api.test"},
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
		t.Fatal("expected egress resolver to be wired")
	}
	if receivedEgress.Resolver.Credentials == nil {
		t.Fatal("expected credential resolver to be wired when grants are configured")
	}
}

func TestSecretBackedCredentialGrant_ConfigGrant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const secretName = "vendor-api-key"
	const secretValue = "sk-test-secret-abc123"

	cfg := validConfig()
	cfg.Egress = config.EgressConfig{
		Credentials: []config.EgressCredentialGrant{
			{
				Host:      "api.vendor.test",
				SecretRef: secretName,
				AuthStyle: "bearer",
			},
		},
	}
	cfg.Bindings = map[string]config.BindingDef{
		"my-binding": {Type: "test-binding"},
	}

	factories := validFactories()
	factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
		return &coretesting.StubSecretManager{
			Secrets: map[string]string{secretName: secretValue},
		}, nil
	}

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

	if receivedEgress.Resolver == nil || receivedEgress.Resolver.Credentials == nil {
		t.Fatal("expected credential resolver to be wired")
	}

	agentCtx := egress.WithSubject(ctx, egress.Subject{Kind: egress.SubjectAgent, ID: "ec-agent-1"})
	resolution, err := receivedEgress.Resolver.Resolve(agentCtx, egress.ResolutionInput{
		Target: egress.Target{Method: "GET", Host: "api.vendor.test", Path: "/v1/data"},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	const wantAuth = "Bearer " + secretValue
	if resolution.Credential.Authorization != wantAuth {
		t.Fatalf("got Authorization %q, want %q", resolution.Credential.Authorization, wantAuth)
	}
}

func TestSecretBackedCredentialGrant_MultiTenantHostMatching(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const (
		secretNameShop1 = "shop-1-key"
		secretNameShop2 = "shop-2-key"
		secretShop1     = "sk-shop-one"
		secretShop2     = "sk-shop-two"
		hostShop1       = "shop-1.example.com"
		hostShop2       = "shop-2.example.com"
	)

	cfg := validConfig()
	cfg.Egress = config.EgressConfig{
		Credentials: []config.EgressCredentialGrant{
			{Host: hostShop1, SecretRef: secretNameShop1, AuthStyle: "raw"},
			{Host: hostShop2, SecretRef: secretNameShop2, AuthStyle: "raw"},
		},
	}
	cfg.Bindings = map[string]config.BindingDef{
		"my-binding": {Type: "test-binding"},
	}

	factories := validFactories()
	factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
		return &coretesting.StubSecretManager{
			Secrets: map[string]string{
				secretNameShop1: secretShop1,
				secretNameShop2: secretShop2,
			},
		}, nil
	}

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

	agentCtx := egress.WithSubject(ctx, egress.Subject{Kind: egress.SubjectAgent, ID: "ec-agent-1"})

	resolve := func(host string) string {
		t.Helper()
		res, err := receivedEgress.Resolver.Resolve(agentCtx, egress.ResolutionInput{
			Target: egress.Target{Method: "GET", Host: host, Path: "/api/orders"},
		})
		if err != nil {
			t.Fatalf("Resolve(%s): %v", host, err)
		}
		return res.Credential.Authorization
	}

	if auth := resolve(hostShop1); auth != secretShop1 {
		t.Fatalf("shop-1: got %q, want %q", auth, secretShop1)
	}
	if auth := resolve(hostShop2); auth != secretShop2 {
		t.Fatalf("shop-2: got %q, want %q", auth, secretShop2)
	}
}

func TestSecretBackedGrant_CoexistsWithProviderTokenGrant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const (
		secretName  = "ext-api-key"
		secretValue = "sk-external-key"
		tokenValue  = "tok-provider-token"
	)

	cfg := validConfig()
	cfg.Egress = config.EgressConfig{
		Credentials: []config.EgressCredentialGrant{
			{Host: "api.external.test", SecretRef: secretName, AuthStyle: "bearer"},
			{Provider: "alpha", Host: "api.provider.test"},
		},
	}
	cfg.Bindings = map[string]config.BindingDef{
		"my-binding": {Type: "test-binding", Providers: []string{"alpha"}},
	}

	factories := validFactories()
	factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
		return &coretesting.StubSecretManager{
			Secrets: map[string]string{secretName: secretValue},
		}, nil
	}
	factories.Datastores["test-store"] = func(_ yaml.Node, _ bootstrap.Deps) (core.Datastore, error) {
		return &coretesting.StubDatastore{
			TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{AccessToken: tokenValue}, nil
			},
		}, nil
	}

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

	agentCtx := egress.WithSubject(ctx, egress.Subject{Kind: egress.SubjectAgent, ID: "ec-agent-1"})
	secretRes, err := receivedEgress.Resolver.Resolve(agentCtx, egress.ResolutionInput{
		Target: egress.Target{Method: "GET", Host: "api.external.test", Path: "/v1/data"},
	})
	if err != nil {
		t.Fatalf("Resolve secret grant: %v", err)
	}
	if secretRes.Credential.Authorization != "Bearer "+secretValue {
		t.Fatalf("secret grant: got %q, want %q", secretRes.Credential.Authorization, "Bearer "+secretValue)
	}

	userCtx := egress.WithSubject(ctx, egress.Subject{Kind: egress.SubjectUser, ID: "user-456"})
	providerRes, err := receivedEgress.Resolver.Resolve(userCtx, egress.ResolutionInput{
		Target: egress.Target{Provider: "alpha", Method: "GET", Host: "api.provider.test", Path: "/v1/data"},
	})
	if err != nil {
		t.Fatalf("Resolve provider grant: %v", err)
	}
	if !strings.Contains(providerRes.Credential.Authorization, tokenValue) {
		t.Fatalf("provider grant: got %q, want to contain %q", providerRes.Credential.Authorization, tokenValue)
	}
}

func TestSecretRefNotEagerlyResolvedByBootstrap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const secretRefValue = "my-key"

	cfg := validConfig()
	cfg.Egress = config.EgressConfig{
		Credentials: []config.EgressCredentialGrant{
			{Host: "api.test", SecretRef: secretRefValue},
		},
	}

	factories := validFactories()

	result, err := bootstrap.Bootstrap(ctx, cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady

	if len(result.Egress.CredentialGrants) != 1 {
		t.Fatalf("expected 1 credential grant, got %d", len(result.Egress.CredentialGrants))
	}
	if result.Egress.CredentialGrants[0].SecretRef != secretRefValue {
		t.Fatalf("secret_ref was modified by bootstrap: got %q, want %q",
			result.Egress.CredentialGrants[0].SecretRef, secretRefValue)
	}
}

func TestSecretBackedGrant_SecretURIPrefixStripped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const (
		secretName  = "prefixed-key"
		secretValue = "sk-with-prefix"
	)

	cfg := validConfig()
	cfg.Egress = config.EgressConfig{
		Credentials: []config.EgressCredentialGrant{
			{Host: "api.prefix.test", SecretRef: "secret://" + secretName, AuthStyle: "bearer"},
		},
	}
	cfg.Bindings = map[string]config.BindingDef{
		"my-binding": {Type: "test-binding"},
	}

	factories := validFactories()
	factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
		return &coretesting.StubSecretManager{
			Secrets: map[string]string{secretName: secretValue},
		}, nil
	}

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

	agentCtx := egress.WithSubject(ctx, egress.Subject{Kind: egress.SubjectAgent, ID: "ec-agent-1"})
	resolution, err := receivedEgress.Resolver.Resolve(agentCtx, egress.ResolutionInput{
		Target: egress.Target{Method: "GET", Host: "api.prefix.test", Path: "/v1"},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	const wantAuth = "Bearer " + secretValue
	if resolution.Credential.Authorization != wantAuth {
		t.Fatalf("got Authorization %q, want %q", resolution.Credential.Authorization, wantAuth)
	}
}
