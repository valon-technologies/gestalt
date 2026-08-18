package server

import (
	"context"
	"testing"
	"time"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coredata "github.com/valon-technologies/gestalt/server/internal/coredata"
)

func TestTenantAppDirectoryEpochIncludesRemoteTopology(t *testing.T) {
	t.Parallel()

	services, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	s := &Server{
		tunnelResolver: newTunnelProviderResolver(TunnelResolverConfig{
			RemoteRegistrations: services.RemoteRegistrations,
			ConnectAddr:         "127.0.0.1:1",
		}),
	}
	first := s.tenantAppDirectoryEpoch()
	if first.remote != 0 {
		t.Fatalf("remote topology = %d, want 0", first.remote)
	}
	_, err = services.RemoteRegistrations.Replace(context.Background(), "subject:alice", &coredata.RemoteRegistration{
		ID:                "reg-1",
		TunnelHost:        "tunnel.example.test",
		TunnelCertificate: []byte("cert"),
		ServerSPKISHA256:  "spki",
		LeaseExpiresAt:    time.Now().Add(time.Minute),
	}, []*coredata.RemoteProvider{{
		ProviderKind: "app",
		ProviderName: "slack",
		Definition:   map[string]any{"displayName": "Slack"},
	}}, 0)
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}
	second := s.tenantAppDirectoryEpoch()
	if second.remote == first.remote {
		t.Fatal("tunnel register left the tenant directory epoch unchanged")
	}
}
