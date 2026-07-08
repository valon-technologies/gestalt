package server_test

import (
	"context"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/remotetest"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	grpcstatus "google.golang.org/grpc/status"
)

func TestPlan6FakeRemoteRejectsMissingBearer(t *testing.T) {
	t.Parallel()

	fake := remotetest.New(t, remotetest.DefaultToken)
	conn, err := grpc.NewClient(
		fake.URL()[7:],
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = proto.NewAppClient(conn).Invoke(ctx, &proto.AppInvokeRequest{
		App:       "linear",
		Operation: "issues.list",
	})
	if grpcstatus.Code(err) != codes.Unauthenticated {
		t.Fatalf("status = %v, want Unauthenticated (%v)", grpcstatus.Code(err), err)
	}
	if fake.Recorder.AuthFailureCount() == 0 {
		t.Fatal("expected auth failure to be recorded")
	}
}
