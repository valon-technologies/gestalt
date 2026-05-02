package gestalt

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"google.golang.org/grpc"
)

const envWriteCatalog = "GESTALT_PLUGIN_WRITE_CATALOG"

type providerCloserContextKey struct{}

// ServeProvider starts a gRPC server for the given [Provider] and typed
// router on the Unix socket specified by the GESTALT_PLUGIN_SOCKET environment
// variable. It blocks until ctx is cancelled, at which point it drains
// in-flight requests and returns nil. This is the main entry point for
// integration providers.
func ServeProvider[P any, PP interface {
	*P
	Provider
}](ctx context.Context, provider PP, router *Router[P]) error {
	catalogPath := os.Getenv(envWriteCatalog)
	if catalogPath != "" {
		cat := router.Catalog()
		if cat == nil {
			cat = &proto.Catalog{}
		}
		if err := ensureOutputDir("catalog", catalogPath); err != nil {
			return err
		}
		if err := writeCatalogYAML(cat, catalogPath); err != nil {
			return err
		}
		return nil
	}
	ctx = withProviderCloser(ctx, provider)
	return serveProvider(ctx, func(srv *grpc.Server) {
		proto.RegisterIntegrationProviderServer(srv, NewProviderServer(provider, router))
	})
}

func ensureOutputDir(label, path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s directory %q: %w", label, dir, err)
	}
	return nil
}

func withProviderCloser(ctx context.Context, provider any) context.Context {
	if closer, ok := provider.(Closer); ok {
		return context.WithValue(ctx, providerCloserContextKey{}, closer)
	}
	return ctx
}

func serveProvider(ctx context.Context, register func(*grpc.Server)) error {
	socket := os.Getenv(proto.EnvProviderSocket)
	if socket == "" {
		return fmt.Errorf("%s is required", proto.EnvProviderSocket)
	}
	if err := os.Remove(socket); err != nil && !os.IsNotExist(err) {
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

	closer, _ := ctx.Value(providerCloserContextKey{}).(Closer)
	var closeOnce sync.Once
	closeProvider := func() {
		if closer != nil {
			_ = closer.Close()
		}
	}
	defer closeOnce.Do(closeProvider)

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		srv.GracefulStop()
		closeOnce.Do(closeProvider)
	}()
	if parentPID := providerParentPID(); parentPID > 0 {
		go watchProviderParent(parentPID, srv)
	}

	err = srv.Serve(lis)
	if ctx.Err() != nil {
		<-stopped
		return nil
	}
	return err
}

func providerParentPID() int {
	raw := os.Getenv(proto.EnvProviderParentPID)
	if raw == "" {
		return 0
	}
	pid, err := strconv.Atoi(raw)
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

func watchProviderParent(parentPID int, srv *grpc.Server) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		if os.Getppid() == parentPID {
			continue
		}
		srv.GracefulStop()
		return
	}
}
