package gestalt

import "testing"

// TestHostedGRPCServerOptionsRaisesDefaults guards against silent regression
// of the constant and helper that raise grpc-go's 4 MiB default for the
// provider and host-service gRPC servers. Keep in sync with the matching
// guard in gestaltd/services/runtimehost/runtimeprovider/dial_test.go.
func TestHostedGRPCServerOptionsRaisesDefaults(t *testing.T) {
	t.Parallel()

	const grpcGoDefault = 4 * 1024 * 1024
	if hostedGRPCMaxMessageBytes <= grpcGoDefault {
		t.Fatalf("hostedGRPCMaxMessageBytes = %d, want > grpc-go default of %d", hostedGRPCMaxMessageBytes, grpcGoDefault)
	}
	if hostedGRPCMaxMessageBytes < 8*1024*1024 {
		t.Fatalf("hostedGRPCMaxMessageBytes = %d, want >= 8 MiB", hostedGRPCMaxMessageBytes)
	}
	opts := hostedGRPCServerOptions()
	if got, want := len(opts), 2; got != want {
		t.Fatalf("hostedGRPCServerOptions() returned %d options, want %d (MaxRecvMsgSize + MaxSendMsgSize)", got, want)
	}
}
