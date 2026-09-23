package invocation

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

// An mcp_oauth connection's AuthConfigResolver discovers the upstream live.
// The broker must not pay for that when the subject has nothing stored for the
// connection, and must still pay for it when a grant exists so the credential
// provider can refresh it.
func TestBrokerResolveSubjectTokenSkipsAuthConfigDiscoveryWithoutStoredCredential(t *testing.T) {
	t.Parallel()

	const providerName = "linear"
	svc := testutil.NewStubServices(t)
	provider := &coretesting.StubIntegration{N: providerName, ConnMode: core.ConnectionModeSubject}
	providers := testutil.NewProviderRegistry(t, provider)

	var resolverCalls atomic.Int32
	runtime := ConnectionRuntimeMap{
		providerName: {
			core.AppConnectionName: ConnectionRuntimeInfo{
				ConnectionID: providerName + ":" + core.AppConnectionName,
				Mode:         core.ConnectionModeSubject,
				AuthConfigResolver: func(context.Context) (core.ExternalCredentialAuthConfig, error) {
					resolverCalls.Add(1)
					return core.ExternalCredentialAuthConfig{Type: "oauth2", TokenURL: "https://upstream.example/token", ClientID: "client-001"}, nil
				},
			},
		},
	}
	broker := NewBroker(providers, svc.Users, svc.ExternalCredentials, WithConnectionRuntime(runtime.Resolve))

	t.Run("NothingStored", func(t *testing.T) {
		_, _, err := broker.ResolveSubjectToken(context.Background(), provider, "user:never-connected", providerName, "", "")
		if !errors.Is(err, ErrNoCredential) {
			t.Fatalf("ResolveSubjectToken with nothing stored: err = %v, want ErrNoCredential", err)
		}
		if got := resolverCalls.Load(); got != 0 {
			t.Fatalf("AuthConfigResolver calls = %d, want 0 (no grant to refresh, so no discovery)", got)
		}
	})

	t.Run("ExplicitInstanceIsNotGated", func(t *testing.T) {
		// With an instance named the store is not listed, so the existing
		// order stands: resolve the auth config, then let ResolveCredential
		// report the missing grant.
		before := resolverCalls.Load()
		_, _, err := broker.ResolveSubjectToken(context.Background(), provider, "user:never-connected", providerName, "", "team-a")
		if !errors.Is(err, ErrNoCredential) {
			t.Fatalf("ResolveSubjectToken with explicit instance: err = %v, want ErrNoCredential", err)
		}
		if got := resolverCalls.Load(); got != before+1 {
			t.Fatalf("AuthConfigResolver calls = %d, want %d (explicit instance keeps the current path)", got, before+1)
		}
	})

	t.Run("StoredGrantStillResolvesAuthConfig", func(t *testing.T) {
		const subjectID = "user:connected"
		if err := svc.ExternalCredentials.UpsertCredential(context.Background(), &core.ExternalCredential{
			ID:        "linear-grant",
			Subject:   subjectID,
			Audience:  providerName + ":" + core.AppConnectionName,
			Qualifier: "default",
			Grant:     &core.ExternalCredentialGrant{AccessToken: "linear-token"},
		}); err != nil {
			t.Fatalf("UpsertCredential: %v", err)
		}
		before := resolverCalls.Load()
		_, token, err := broker.ResolveSubjectToken(context.Background(), provider, subjectID, providerName, "", "")
		if err != nil {
			t.Fatalf("ResolveSubjectToken with a stored grant: %v", err)
		}
		if token != "linear-token" {
			t.Fatalf("token = %q, want linear-token", token)
		}
		if got := resolverCalls.Load(); got != before+1 {
			t.Fatalf("AuthConfigResolver calls = %d, want %d (a stored grant needs the refresh config)", got, before+1)
		}
	})
}
