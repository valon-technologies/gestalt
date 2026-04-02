package pluginhost

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

const testConfigSchemaJSON = `{
	"type": "object",
	"properties": {
		"api_key": {"type": "string"},
		"retries": {"type": "integer"}
	},
	"required": ["api_key"]
}`

const testConfigSchemaYAML = `type: object
properties:
  api_key:
    type: string
  retries:
    type: integer
required:
  - api_key
`

func TestValidateConfigSchema_Valid(t *testing.T) {
	t.Parallel()
	config := map[string]any{"api_key": "sk-123", "retries": 3}
	if err := validateConfigSchema(config, testConfigSchemaJSON); err != nil {
		t.Fatalf("expected valid config to pass: %v", err)
	}
}

func TestValidateConfigSchemaJSON_MissingRequired(t *testing.T) {
	t.Parallel()
	config := map[string]any{"retries": 3}
	err := validateConfigSchema(config, testConfigSchemaJSON)
	if err == nil {
		t.Fatal("expected error for missing required field")
	}
	if !strings.Contains(err.Error(), "config validation failed") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidateConfigSchemaYAML_Valid(t *testing.T) {
	t.Parallel()
	config := map[string]any{"api_key": "sk-123", "retries": 3}
	if err := validateConfigSchema(config, testConfigSchemaYAML); err != nil {
		t.Fatalf("expected valid config to pass: %v", err)
	}
}

func TestValidateConfigSchemaJSON_EmptyConfigWithRequired(t *testing.T) {
	t.Parallel()
	config := map[string]any{}
	err := validateConfigSchema(config, testConfigSchemaJSON)
	if err == nil {
		t.Fatal("expected error for empty config with required fields")
	}
	if !strings.Contains(err.Error(), "config validation failed") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidateConfigSchema_InvalidSchemaDocument(t *testing.T) {
	t.Parallel()
	config := map[string]any{"key": "val"}
	err := validateConfigSchema(config, `{not valid json`)
	if err == nil {
		t.Fatal("expected error for invalid schema document")
	}
}

type stubProviderPluginServer struct {
	proto.UnimplementedProviderPluginServer
	metadata *proto.ProviderMetadata
}

type flakyMetadataProviderPluginServer struct {
	stubProviderPluginServer
	failures int32
	calls    int32
}

func (s *stubProviderPluginServer) GetMetadata(context.Context, *emptypb.Empty) (*proto.ProviderMetadata, error) {
	return s.metadata, nil
}

func (s *flakyMetadataProviderPluginServer) GetMetadata(context.Context, *emptypb.Empty) (*proto.ProviderMetadata, error) {
	atomic.AddInt32(&s.calls, 1)
	if atomic.AddInt32(&s.failures, -1) >= 0 {
		return nil, status.Error(codes.Unavailable, "transport: Error while dialing: dial unix /tmp/plugin.sock: connect: connection refused")
	}
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
			ConfigSchemaJson: testConfigSchemaJSON,
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
			ConfigSchemaJson: testConfigSchemaJSON,
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

func TestNewRemoteProvider_YAMLSchemaAcceptsValidConfig(t *testing.T) {
	t.Parallel()

	stub := &stubProviderPluginServer{
		metadata: &proto.ProviderMetadata{
			Name:             "test-plugin",
			DisplayName:      "Test Plugin",
			ConfigSchemaJson: testConfigSchemaYAML,
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
			ConfigSchemaJson: testConfigSchemaJSON,
		},
	}
	client := newProviderPluginClient(t, stub)
	_, err := NewRemoteProvider(context.Background(), client, "test-plugin", nil)
	if err == nil {
		t.Fatal("expected nil config to fail validation against schema with required fields")
	}
}

func TestNewRemoteProvider_RetriesTransientMetadataUnavailable(t *testing.T) {
	t.Parallel()

	stub := &flakyMetadataProviderPluginServer{
		stubProviderPluginServer: stubProviderPluginServer{
			metadata: &proto.ProviderMetadata{
				Name:        "test-plugin",
				DisplayName: "Test Plugin",
			},
		},
		failures: 2,
	}
	client := newProviderPluginClient(t, stub)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	prov, err := NewRemoteProvider(ctx, client, "test-plugin", nil)
	if err != nil {
		t.Fatalf("expected transient startup failure to recover: %v", err)
	}
	if prov.Name() != "test-plugin" {
		t.Fatalf("unexpected name: %q", prov.Name())
	}
	if got := atomic.LoadInt32(&stub.calls); got < 3 {
		t.Fatalf("GetMetadata calls = %d, want at least 3", got)
	}
}
