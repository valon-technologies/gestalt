package plugins

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeGestaltAppClient struct {
	invoke        func(context.Context, *proto.AppInvokeRequest) (*proto.OperationResult, error)
	invokeGraphQL func(context.Context, *proto.AppInvokeGraphQLRequest) (*proto.OperationResult, error)
}

func (f *fakeGestaltAppClient) Invoke(ctx context.Context, req *proto.AppInvokeRequest, _ ...grpc.CallOption) (*proto.OperationResult, error) {
	if f.invoke != nil {
		return f.invoke(ctx, req)
	}
	return nil, errors.New("unexpected Invoke")
}

func (f *fakeGestaltAppClient) InvokeGraphQL(ctx context.Context, req *proto.AppInvokeGraphQLRequest, _ ...grpc.CallOption) (*proto.OperationResult, error) {
	if f.invokeGraphQL != nil {
		return f.invokeGraphQL(ctx, req)
	}
	return nil, errors.New("unexpected InvokeGraphQL")
}

func TestGestaltRemoteProviderDelegatesInvoke(t *testing.T) {
	t.Parallel()

	var gotApp, gotOperation string
	client := &fakeGestaltAppClient{
		invoke: func(_ context.Context, req *proto.AppInvokeRequest) (*proto.OperationResult, error) {
			gotApp = req.GetApp()
			gotOperation = req.GetOperation()
			return &proto.OperationResult{Status: 200, Body: []byte(`{"ok":true}`)}, nil
		},
	}
	provider := NewGestaltRemoteProvider(client, StaticProviderSpec{Name: "linear", DisplayName: "Linear"})
	result, err := provider.Execute(context.Background(), "issues.get", map[string]any{"id": "issue-1"}, "")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotApp != "linear" || gotOperation != "issues.get" {
		t.Fatalf("remote invoke = %q.%q, want linear.issues.get", gotApp, gotOperation)
	}
	if result.Status != 200 {
		t.Fatalf("status = %d, want 200", result.Status)
	}
}

func TestGestaltRemoteProviderDelegatesGraphQL(t *testing.T) {
	t.Parallel()

	client := &fakeGestaltAppClient{
		invokeGraphQL: func(_ context.Context, req *proto.AppInvokeGraphQLRequest) (*proto.OperationResult, error) {
			if req.GetApp() != "linear" || req.GetDocument() != "{ searchIssues { nodes { id } } }" {
				t.Fatalf("remote graphql = %#v", req)
			}
			return &proto.OperationResult{Status: 200}, nil
		},
	}
	provider := NewGestaltRemoteProvider(client, StaticProviderSpec{Name: "linear"})
	graphQL, ok := provider.(core.GraphQLSurfaceInvoker)
	if !ok {
		t.Fatal("expected GraphQLSurfaceInvoker")
	}
	if _, err := graphQL.InvokeGraphQL(context.Background(), core.GraphQLRequest{
		Document: "{ searchIssues { nodes { id } } }",
	}, ""); err != nil {
		t.Fatalf("InvokeGraphQL: %v", err)
	}
}

func TestGestaltRemoteProviderMapsRemoteAuthErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		code    codes.Code
		wantErr error
	}{
		{"unauthenticated", codes.Unauthenticated, invocation.ErrNotAuthenticated},
		{"permission denied", codes.PermissionDenied, invocation.ErrAuthorizationDenied},
		{"not found", codes.NotFound, invocation.ErrProviderNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := &fakeGestaltAppClient{
				invoke: func(context.Context, *proto.AppInvokeRequest) (*proto.OperationResult, error) {
					return nil, status.Error(tc.code, tc.name)
				},
			}
			provider := NewGestaltRemoteProvider(client, StaticProviderSpec{Name: "linear"})
			_, err := provider.Execute(context.Background(), "issues.get", nil, "")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Execute err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
