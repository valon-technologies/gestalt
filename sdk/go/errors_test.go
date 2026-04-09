package gestalt

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestProviderRPCErrorMapsSecretNotFoundToNotFound(t *testing.T) {
	t.Parallel()

	err := providerRPCError("get secret", ErrSecretNotFound)
	if err == nil {
		t.Fatal("providerRPCError returned nil")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("providerRPCError() = %v, want gRPC status error", err)
	}
	if st.Code() != codes.NotFound {
		t.Fatalf("providerRPCError() code = %v, want %v", st.Code(), codes.NotFound)
	}
	if st.Message() != ErrSecretNotFound.Error() {
		t.Fatalf("providerRPCError() message = %q, want %q", st.Message(), ErrSecretNotFound.Error())
	}
}
