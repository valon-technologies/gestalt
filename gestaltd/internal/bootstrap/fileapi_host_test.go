package bootstrap

import (
	"context"
	"net"
	"testing"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core/fileapi"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func newHostFileAPIClient(t *testing.T, register func(*grpc.Server)) proto.FileAPIClient {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	register(srv)
	go func() {
		_ = srv.Serve(lis)
	}()
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
	return proto.NewFileAPIClient(conn)
}

func TestPluginFileAPIHostServicesShareObjectURLsAcrossAliasSockets(t *testing.T) {
	t.Parallel()

	services, err := buildPluginFileAPIHostServices("docs", &config.ProviderEntry{FileAPIs: []string{"main"}}, Deps{
		FileAPIs: map[string]fileapi.FileAPI{
			"main": &coretesting.StubFileAPI{},
		},
	})
	if err != nil {
		t.Fatalf("buildPluginFileAPIHostServices: %v", err)
	}
	if len(services) != 2 {
		t.Fatalf("len(services) = %d, want 2", len(services))
	}

	clients := make(map[string]proto.FileAPIClient, len(services))
	for _, service := range services {
		clients[service.EnvVar] = newHostFileAPIClient(t, service.Register)
	}
	defaultClient := clients[providerhost.DefaultFileAPISocketEnv]
	namedClient := clients[providerhost.FileAPISocketEnv("main")]
	if defaultClient == nil || namedClient == nil {
		t.Fatal("expected both default and named FileAPI host services")
	}

	ctx := context.Background()
	blob, err := defaultClient.CreateBlob(ctx, &proto.CreateBlobRequest{
		Parts: []*proto.BlobPart{{Kind: &proto.BlobPart_StringData{StringData: "hello"}}},
	})
	if err != nil {
		t.Fatalf("CreateBlob: %v", err)
	}
	urlResp, err := defaultClient.CreateObjectURL(ctx, &proto.CreateObjectURLRequest{Id: blob.GetObject().GetId()})
	if err != nil {
		t.Fatalf("CreateObjectURL: %v", err)
	}

	if _, err := namedClient.ResolveObjectURL(ctx, &proto.ObjectURLRequest{Url: urlResp.GetUrl()}); err != nil {
		t.Fatalf("ResolveObjectURL via named socket: %v", err)
	}
	if _, err := defaultClient.ResolveObjectURL(ctx, &proto.ObjectURLRequest{Url: urlResp.GetUrl()}); err != nil {
		t.Fatalf("ResolveObjectURL via default socket after named resolve: %v", err)
	}
	if _, err := namedClient.RevokeObjectURL(ctx, &proto.ObjectURLRequest{Url: urlResp.GetUrl()}); err != nil {
		t.Fatalf("RevokeObjectURL: %v", err)
	}
	if _, err := defaultClient.ResolveObjectURL(ctx, &proto.ObjectURLRequest{Url: urlResp.GetUrl()}); status.Code(err) != codes.NotFound {
		t.Fatalf("ResolveObjectURL after revoke error = %v, want NotFound", err)
	}
}

func TestPluginFileAPIHostServicesNamespaceObjectURLsPerBinding(t *testing.T) {
	t.Parallel()

	services, err := buildPluginFileAPIHostServices("docs", &config.ProviderEntry{FileAPIs: []string{"main", "archive"}}, Deps{
		FileAPIs: map[string]fileapi.FileAPI{
			"main":    &coretesting.StubFileAPI{},
			"archive": &coretesting.StubFileAPI{},
		},
	})
	if err != nil {
		t.Fatalf("buildPluginFileAPIHostServices: %v", err)
	}
	if len(services) != 2 {
		t.Fatalf("len(services) = %d, want 2", len(services))
	}

	clients := make(map[string]proto.FileAPIClient, len(services))
	for _, service := range services {
		clients[service.EnvVar] = newHostFileAPIClient(t, service.Register)
	}

	ctx := context.Background()
	makeURL := func(t *testing.T, envVar string) string {
		t.Helper()
		client := clients[envVar]
		blob, err := client.CreateBlob(ctx, &proto.CreateBlobRequest{
			Parts: []*proto.BlobPart{{Kind: &proto.BlobPart_StringData{StringData: envVar}}},
		})
		if err != nil {
			t.Fatalf("CreateBlob(%s): %v", envVar, err)
		}
		urlResp, err := client.CreateObjectURL(ctx, &proto.CreateObjectURLRequest{Id: blob.GetObject().GetId()})
		if err != nil {
			t.Fatalf("CreateObjectURL(%s): %v", envVar, err)
		}
		return urlResp.GetUrl()
	}

	mainURL := makeURL(t, providerhost.FileAPISocketEnv("main"))
	archiveURL := makeURL(t, providerhost.FileAPISocketEnv("archive"))
	if mainURL == archiveURL {
		t.Fatalf("mainURL = archiveURL = %q, want distinct namespaced URLs", mainURL)
	}
	if _, err := clients[providerhost.FileAPISocketEnv("main")].ResolveObjectURL(ctx, &proto.ObjectURLRequest{Url: archiveURL}); status.Code(err) != codes.NotFound {
		t.Fatalf("main binding resolving archive URL error = %v, want NotFound", err)
	}
	if _, err := clients[providerhost.FileAPISocketEnv("archive")].ResolveObjectURL(ctx, &proto.ObjectURLRequest{Url: mainURL}); status.Code(err) != codes.NotFound {
		t.Fatalf("archive binding resolving main URL error = %v, want NotFound", err)
	}
}
