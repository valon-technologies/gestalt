package gestalt

import (
	"context"
	"fmt"
	"net"
	"os"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
)

const envWriteCatalog = "GESTALT_PLUGIN_WRITE_CATALOG"

type executableProvider interface {
	Provider
	execute(ctx context.Context, operation string, params map[string]any, token string) (*OperationResult, error)
	sessionCatalogProvider() (SessionCatalogProvider, bool)
}

type routedProvider[P any, PP interface {
	*P
	Provider
}] struct {
	provider PP
	router   *Router[P]
}

func (p *routedProvider[P, PP]) Configure(ctx context.Context, name string, config map[string]any) error {
	return p.provider.Configure(ctx, name, config)
}

func (p *routedProvider[P, PP]) execute(ctx context.Context, operation string, params map[string]any, token string) (*OperationResult, error) {
	if p.router == nil {
		return nil, fmt.Errorf("router is nil")
	}
	return p.router.Execute(ctx, (*P)(p.provider), operation, params, token)
}

func (p *routedProvider[P, PP]) sessionCatalogProvider() (SessionCatalogProvider, bool) {
	scp, ok := any(p.provider).(SessionCatalogProvider)
	return scp, ok
}

// ServeProvider starts a gRPC server for the given [Provider] and typed
// router on the Unix socket specified by the GESTALT_PLUGIN_SOCKET environment
// variable. It blocks until ctx is cancelled, at which point it drains
// in-flight requests and returns nil. This is the main entry point for
// provider plugins.
func ServeProvider[P any, PP interface {
	*P
	Provider
}](ctx context.Context, provider PP, router *Router[P]) error {
	if catalogPath := os.Getenv(envWriteCatalog); catalogPath != "" {
		if router == nil {
			return writeCatalogYAML(nil, catalogPath)
		}
		return writeCatalogYAML(router.Catalog(), catalogPath)
	}
	return servePlugin(ctx, func(srv *grpc.Server) {
		proto.RegisterProviderPluginServer(srv, NewProviderServer(provider, router))
	})
}

func servePlugin(ctx context.Context, register func(*grpc.Server)) error {
	socket := os.Getenv(proto.EnvPluginSocket)
	if socket == "" {
		return fmt.Errorf("%s is required", proto.EnvPluginSocket)
	}
	if err := os.RemoveAll(socket); err != nil {
		return fmt.Errorf("remove stale socket %q: %w", socket, err)
	}

	lis, err := net.Listen("unix", socket)
	if err != nil {
		return fmt.Errorf("listen on plugin socket %q: %w", socket, err)
	}
	defer func() {
		_ = lis.Close()
		_ = os.Remove(socket)
	}()

	srv := grpc.NewServer()
	register(srv)

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		srv.GracefulStop()
	}()

	err = srv.Serve(lis)
	if ctx.Err() != nil {
		<-stopped
		return nil
	}
	return err
}
