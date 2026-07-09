package plugins

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type errorReturningAppClient struct {
	err error
}

func (c errorReturningAppClient) Invoke(context.Context, *proto.AppInvokeRequest, ...grpc.CallOption) (*proto.OperationResult, error) {
	return nil, c.err
}

func (c errorReturningAppClient) InvokeGraphQL(context.Context, *proto.AppInvokeGraphQLRequest, ...grpc.CallOption) (*proto.OperationResult, error) {
	return nil, c.err
}

func gestaltRemoteProviderForErrorsTest(t *testing.T, grpcErr error) core.Provider {
	t.Helper()
	provider := NewGestaltRemote(errorReturningAppClient{err: grpcErr}, StaticProviderSpec{
		Name: "linear",
		Catalog: &catalog.Catalog{
			Operations: []catalog.CatalogOperation{{ID: "issues.list"}},
		},
	})
	if provider == nil {
		t.Fatal("NewGestaltRemote returned nil")
	}
	return provider
}

func TestGestaltRemoteProviderMapsGRPCStatusToInvocationErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		grpcErr error
		wantErr error
	}{
		{
			name:    "unauthenticated",
			grpcErr: status.Error(codes.Unauthenticated, "invalid token"),
			wantErr: invocation.ErrNotAuthenticated,
		},
		{
			name:    "permission denied",
			grpcErr: status.Error(codes.PermissionDenied, "forbidden"),
			wantErr: invocation.ErrAuthorizationDenied,
		},
		{
			name:    "provider not found",
			grpcErr: status.Error(codes.NotFound, "provider missing"),
			wantErr: invocation.ErrProviderNotFound,
		},
		{
			name:    "operation not found",
			grpcErr: status.Error(codes.NotFound, "operation missing"),
			wantErr: invocation.ErrOperationNotFound,
		},
		{
			name:    "no credential",
			grpcErr: status.Error(codes.FailedPrecondition, invocation.ErrNoCredential.Error()),
			wantErr: invocation.ErrNoCredential,
		},
		{
			name:    "reconnect required",
			grpcErr: status.Error(codes.FailedPrecondition, invocation.ErrReconnectRequired.Error()),
			wantErr: invocation.ErrReconnectRequired,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			provider := gestaltRemoteProviderForErrorsTest(t, tc.grpcErr)
			_, err := provider.Execute(context.Background(), "issues.list", nil, "")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Execute err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
