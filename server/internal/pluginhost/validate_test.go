package pluginhost

import (
	"context"
	"strings"
	"testing"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/protobuf/types/known/emptypb"
)

const testConfigSchema = `{
	"type": "object",
	"properties": {
		"api_key": {"type": "string"},
		"retries": {"type": "integer"}
	},
	"required": ["api_key"]
}`

func TestValidateConfigSchema_Valid(t *testing.T) {
	t.Parallel()
	config := map[string]any{"api_key": "sk-123", "retries": 3}
	if err := validateConfigSchema(config, testConfigSchema); err != nil {
		t.Fatalf("expected valid config to pass: %v", err)
	}
}

func TestValidateConfigSchema_MissingRequired(t *testing.T) {
	t.Parallel()
	config := map[string]any{"retries": 3}
	err := validateConfigSchema(config, testConfigSchema)
	if err == nil {
		t.Fatal("expected error for missing required field")
	}
	if !strings.Contains(err.Error(), "config validation failed") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidateConfigSchema_EmptyConfigWithRequired(t *testing.T) {
	t.Parallel()
	config := map[string]any{}
	err := validateConfigSchema(config, testConfigSchema)
	if err == nil {
		t.Fatal("expected error for empty config with required fields")
	}
	if !strings.Contains(err.Error(), "config validation failed") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidateConfigSchema_InvalidSchema(t *testing.T) {
	t.Parallel()
	config := map[string]any{"key": "val"}
	err := validateConfigSchema(config, `{not valid json`)
	if err == nil {
		t.Fatal("expected error for invalid schema JSON")
	}
}

type stubProviderPluginServer struct {
	proto.UnimplementedProviderPluginServer
	metadata *proto.ProviderMetadata
}

func (s *stubProviderPluginServer) GetMetadata(context.Context, *emptypb.Empty) (*proto.ProviderMetadata, error) {
	return s.metadata, nil
}

func (s *stubProviderPluginServer) StartProvider(_ context.Context, req *proto.StartProviderRequest) (*proto.StartProviderResponse, error) {
	return &proto.StartProviderResponse{
		ProtocolVersion: req.GetProtocolVersion(),
	}, nil
}

func TestNewRemoteProvider_NoSchema(t *testing.T) {
	t.Parallel()

	stub := &stubProviderPluginServer{
		metadata: &proto.ProviderMetadata{
			Name:        "test-plugin",
			DisplayName: "Test Plugin",
		},
	}
	client := newProviderPluginClient(t, stub)
	prov, err := NewRemoteProvider(context.Background(), client, "test-plugin", map[string]any{"anything": "goes"})
	if err != nil {
		t.Fatalf("expected no error without schema: %v", err)
	}
	if prov.Name() != "test-plugin" {
		t.Fatalf("unexpected name: %q", prov.Name())
	}
}

func TestNewRemoteProvider_SchemaRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	stub := &stubProviderPluginServer{
		metadata: &proto.ProviderMetadata{
			Name:             "test-plugin",
			DisplayName:      "Test Plugin",
			ConfigSchemaJson: testConfigSchema,
		},
	}
	client := newProviderPluginClient(t, stub)
	_, err := NewRemoteProvider(context.Background(), client, "test-plugin", map[string]any{"retries": 3})
	if err == nil {
		t.Fatal("expected error for config missing required field")
	}
	if !strings.Contains(err.Error(), "config validation failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewRemoteProvider_SchemaAcceptsValidConfig(t *testing.T) {
	t.Parallel()

	stub := &stubProviderPluginServer{
		metadata: &proto.ProviderMetadata{
			Name:             "test-plugin",
			DisplayName:      "Test Plugin",
			ConfigSchemaJson: testConfigSchema,
		},
	}
	client := newProviderPluginClient(t, stub)
	prov, err := NewRemoteProvider(context.Background(), client, "test-plugin", map[string]any{"api_key": "sk-test"})
	if err != nil {
		t.Fatalf("expected valid config to pass: %v", err)
	}
	if prov.Name() != "test-plugin" {
		t.Fatalf("unexpected name: %q", prov.Name())
	}
}

func TestNewRemoteProvider_SchemaRejectsNilConfig(t *testing.T) {
	t.Parallel()

	stub := &stubProviderPluginServer{
		metadata: &proto.ProviderMetadata{
			Name:             "test-plugin",
			DisplayName:      "Test Plugin",
			ConfigSchemaJson: testConfigSchema,
		},
	}
	client := newProviderPluginClient(t, stub)
	_, err := NewRemoteProvider(context.Background(), client, "test-plugin", nil)
	if err == nil {
		t.Fatal("expected nil config to fail validation against schema with required fields")
	}
}
