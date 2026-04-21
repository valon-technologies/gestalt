package gestalt

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
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
	if catalogPath := os.Getenv(envWriteCatalog); catalogPath != "" {
		cat := router.Catalog()
		if cat == nil {
			cat = &proto.Catalog{}
		}
		if dir := filepath.Dir(catalogPath); dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("create catalog directory %q: %w", dir, err)
			}
		}
		return writeCatalogYAML(cat, catalogPath)
	}
	ctx = withProviderCloser(ctx, provider)
	return serveProvider(ctx, func(srv *grpc.Server) {
		proto.RegisterIntegrationProviderServer(srv, NewProviderServer(provider, router))
	})
}

func withProviderCloser(ctx context.Context, provider any) context.Context {
	if closer, ok := provider.(Closer); ok {
		return context.WithValue(ctx, providerCloserContextKey{}, closer)
	}
	return ctx
}

func serveProvider(ctx context.Context, register func(*grpc.Server)) error {
	listenTarget := os.Getenv(proto.EnvProviderSocket)
	if listenTarget == "" {
		return fmt.Errorf("%s is required", proto.EnvProviderSocket)
	}

	network, address, err := providerListenTarget(listenTarget)
	if err != nil {
		return err
	}
	if network == "unix" {
		if err := os.Remove(address); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove stale socket %q: %w", address, err)
		}
	}
	lis, err := net.Listen(network, address)
	if err != nil {
		return fmt.Errorf("listen on plugin %s %q: %w", network, address, err)
	}
	defer func() {
		_ = lis.Close()
		if network == "unix" {
			_ = os.Remove(address)
		}
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

func providerListenTarget(raw string) (network string, address string, err error) {
	if strings.HasPrefix(raw, "tcp://") {
		address = strings.TrimSpace(strings.TrimPrefix(raw, "tcp://"))
		if address == "" {
			return "", "", fmt.Errorf("provider tcp listen target %q is missing host:port", raw)
		}
		return "tcp", address, nil
	}
	if strings.HasPrefix(raw, "unix://") {
		address = strings.TrimSpace(strings.TrimPrefix(raw, "unix://"))
		if address == "" {
			return "", "", fmt.Errorf("provider unix listen target %q is missing a socket path", raw)
		}
		return "unix", address, nil
	}
	if strings.Contains(raw, "://") {
		return "", "", fmt.Errorf("unsupported provider listen target %q", raw)
	}
	return "unix", filepath.Clean(raw), nil
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
