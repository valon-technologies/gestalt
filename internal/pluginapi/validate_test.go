package pluginapi

import (
	"context"
	"net"
	"strings"
	"testing"

	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginapi/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
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

type schemaFixtureServer struct {
	pluginapiv1.UnimplementedProviderPluginServer
	metadata *pluginapiv1.ProviderMetadata
	started  int
}

func (s *schemaFixtureServer) GetMetadata(context.Context, *emptypb.Empty) (*pluginapiv1.ProviderMetadata, error) {
	return s.metadata, nil
}

func (s *schemaFixtureServer) StartProvider(_ context.Context, req *pluginapiv1.StartProviderRequest) (*pluginapiv1.StartProviderResponse, error) {
	s.started++
	return &pluginapiv1.StartProviderResponse{
		ProtocolVersion: req.GetProtocolVersion(),
	}, nil
}

func (s *schemaFixtureServer) ListOperations(context.Context, *emptypb.Empty) (*pluginapiv1.ListOperationsResponse, error) {
	return &pluginapiv1.ListOperationsResponse{}, nil
}

func TestNewRemoteProviderConfigValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		schema      string
		config      map[string]any
		wantErr     string
		wantStarted int
	}{
		{
			name:        "no schema",
			config:      map[string]any{"anything": "goes"},
			wantStarted: 1,
		},
		{
			name:        "valid config",
			schema:      testConfigSchema,
			config:      map[string]any{"api_key": "token", "retries": 2},
			wantStarted: 1,
		},
		{
			name:    "missing required field",
			schema:  testConfigSchema,
			config:  map[string]any{"retries": 2},
			wantErr: "config validation failed",
		},
		{
			name:    "invalid schema document",
			schema:  `{not valid json`,
			config:  map[string]any{"api_key": "token"},
			wantErr: "invalid config schema",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := &schemaFixtureServer{
				metadata: &pluginapiv1.ProviderMetadata{
					Name:             "fixture",
					DisplayName:      "Fixture",
					ConfigSchemaJson: tc.schema,
				},
			}

			client := newSchemaFixtureClient(t, server)
			prov, err := NewRemoteProvider(context.Background(), client, "fixture-instance", tc.config, "")

			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want substring %q", err, tc.wantErr)
				}
				if server.started != 0 {
					t.Fatalf("StartProvider should not be called on validation failure, got %d", server.started)
				}
				return
			}

			if err != nil {
				t.Fatalf("NewRemoteProvider: %v", err)
			}
			if prov.Name() != "fixture" {
				t.Fatalf("unexpected provider name: %q", prov.Name())
			}
			if server.started != tc.wantStarted {
				t.Fatalf("StartProvider calls = %d, want %d", server.started, tc.wantStarted)
			}
		})
	}
}

func newSchemaFixtureClient(t *testing.T, server pluginapiv1.ProviderPluginServer) pluginapiv1.ProviderPluginClient {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	pluginapiv1.RegisterProviderPluginServer(srv, server)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return pluginapiv1.NewProviderPluginClient(conn)
}
