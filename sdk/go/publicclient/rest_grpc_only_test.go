package publicclient

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/go/publicclient/generated"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestRESTUnaryTransportRejectsGRPCOnlyMethod(t *testing.T) {
	rest := &restUnaryTransport{
		baseURL: "http://example.com",
		auth:    Unauthenticated(),
		client:  nil,
	}

	err := rest.Unary(
		context.Background(),
		generated.MethodIndexedDBGet,
		&emptypb.Empty{},
		&proto.RecordResponse{},
	)
	var gerr *generated.GestaltError
	if !errors.As(err, &gerr) {
		t.Fatalf("error = %v, want *generated.GestaltError", err)
	}
	if gerr.Message == "" {
		t.Fatal("expected HTTP binding error message")
	}
}
