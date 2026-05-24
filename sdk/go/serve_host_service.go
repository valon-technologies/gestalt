package gestalt

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"

	proto "github.com/valon-technologies/gestalt/sdk/go/protov1/v1"
	"google.golang.org/grpc"
)

// RegisterIndexedDBHostService registers an IndexedDB host service on srv.
func RegisterIndexedDBHostService(srv *grpc.Server, datastore IndexedDBProvider) {
	proto.RegisterIndexedDBServer(srv, indexedDBProviderServer{provider: datastore})
}

// RegisterExternalCredentialHostService registers an external credential host
// service on srv.
func RegisterExternalCredentialHostService(srv *grpc.Server, provider ExternalCredentialProvider) {
	proto.RegisterExternalCredentialProviderServer(srv, newExternalCredentialProviderServer(provider))
}

// ServeHostServiceGRPC serves host-service gRPC handlers on a unix socket until
// ctx is cancelled. The register callback should register one or more host
// services on the server.
func ServeHostServiceGRPC(ctx context.Context, socketPath string, register func(*grpc.Server)) error {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return fmt.Errorf("host service socket path is required")
	}
	if register == nil {
		return fmt.Errorf("host service register callback is required")
	}
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale socket %q: %w", socketPath, err)
	}

	lis, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen on host service socket %q: %w", socketPath, err)
	}
	defer func() {
		_ = lis.Close()
		_ = os.Remove(socketPath)
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
