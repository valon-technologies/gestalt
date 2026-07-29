package remotepublish

import (
	"context"
	"testing"

	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
)

// stubProvider is a minimal core.Provider for testing.
type stubProvider struct {
	name string
}

func (s *stubProvider) Name() string                                   { return s.name }
func (s *stubProvider) DisplayName() string                            { return s.name }
func (s *stubProvider) Description() string                            { return "" }
func (s *stubProvider) ConnectionMode() core.ConnectionMode            { return core.ConnectionModeNone }
func (s *stubProvider) AuthTypes() []string                            { return nil }
func (s *stubProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef { return nil }
func (s *stubProvider) CredentialFields() []core.CredentialFieldDef   { return nil }
func (s *stubProvider) DiscoveryConfig() *core.DiscoveryConfig         { return nil }
func (s *stubProvider) ConnectionForOperation(string) string           { return "" }
func (s *stubProvider) Catalog() *catalog.Catalog                      { return nil }
func (s *stubProvider) Execute(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
	return &core.OperationResult{Status: 200, Body: []byte(`{"app":"` + s.name + `"}`)}, nil
}

func TestTunnelAppProviderServerDispatchByMetadata(t *testing.T) {
	reg := registry.New()
	providers := &reg.Providers
	if err := providers.Register("ci-cd", &stubProvider{name: "ci-cd"}); err != nil {
		t.Fatal(err)
	}
	if err := providers.Register("other-app", &stubProvider{name: "other-app"}); err != nil {
		t.Fatal(err)
	}

	server := NewTunnelAppProviderServer(providers)

	// GetMetadata with app metadata for ci-cd should resolve to the ci-cd provider.
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(tunnelAppMetadataKey, "ci-cd"))
	meta, err := server.GetMetadata(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetMetadata failed: %v", err)
	}
	if meta == nil {
		t.Fatal("GetMetadata returned nil")
	}

	// Execute with app metadata for ci-cd should dispatch to ci-cd.
	resp, err := server.Execute(ctx, &proto.ExecuteRequest{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if string(resp.Body) != `{"app":"ci-cd"}` {
		t.Fatalf("Execute returned wrong body: %s", resp.Body)
	}
}

func TestTunnelAppProviderServerMissingMetadata(t *testing.T) {
	reg := registry.New()
	providers := &reg.Providers
	server := NewTunnelAppProviderServer(providers)

	// Execute without app metadata should fail.
	_, err := server.Execute(context.Background(), &proto.ExecuteRequest{})
	if err == nil {
		t.Fatal("Execute without metadata should fail")
	}
}

func TestTunnelAppProviderServerUnknownApp(t *testing.T) {
	reg := registry.New()
	providers := &reg.Providers
	server := NewTunnelAppProviderServer(providers)

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(tunnelAppMetadataKey, "no-such-app"))
	_, err := server.Execute(ctx, &proto.ExecuteRequest{})
	if err == nil {
		t.Fatal("Execute for unknown app should fail")
	}
}
