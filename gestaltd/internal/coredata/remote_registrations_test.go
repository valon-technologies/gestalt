package coredata

import (
	"context"
	"errors"
	"testing"
	"time"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestRemoteRegistrationService(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	start := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	lease := 30 * time.Second

	newService := func(now time.Time) *RemoteRegistrationService {
		t.Helper()
		services, err := New(&coretesting.StubIndexedDB{})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		services.RemoteRegistrations.now = func() time.Time { return now }
		return services.RemoteRegistrations
	}

	newRegistration := func(id string) *RemoteRegistration {
		return &RemoteRegistration{
			ID:                id,
			TunnelHost:        "tunnel.example.test",
			TunnelCertificate: []byte("cert-bytes"),
			ServerSPKISHA256:  "spki-sha256",
			LeaseExpiresAt:    start.Add(lease),
		}
	}

	newProvider := func(kind, name string) *RemoteProvider {
		return &RemoteProvider{
			ProviderKind: kind,
			ProviderName: name,
			Definition: map[string]any{
				"displayName": kind + "/" + name,
			},
		}
	}

	t.Run("replace_and_resolve", func(t *testing.T) {
		t.Parallel()
		svc := newService(start)
		got, err := svc.Replace(ctx, "subject:alice", newRegistration("reg-1"), []*RemoteProvider{
			newProvider("app", "test-app"),
		}, 0)
		if err != nil {
			t.Fatalf("Replace: %v", err)
		}
		if got.Generation != 1 {
			t.Fatalf("generation = %d, want 1", got.Generation)
		}

		got, err = svc.Replace(ctx, "subject:alice", newRegistration("reg-1"), []*RemoteProvider{
			newProvider("app", "test-app"),
			newProvider("workflow", "billing"),
		}, 1)
		if err != nil {
			t.Fatalf("Replace: %v", err)
		}
		if got.Generation != 2 {
			t.Fatalf("generation = %d, want 2", got.Generation)
		}

		_, providers, err := svc.ListByOwner(ctx, "subject:alice")
		if err != nil {
			t.Fatalf("ListByOwner: %v", err)
		}
		if len(providers) != 2 {
			t.Fatalf("providers = %d, want 2", len(providers))
		}

		provider, reg, err := svc.ResolveProvider(ctx, "app", "test-app")
		if err != nil {
			t.Fatalf("ResolveProvider: %v", err)
		}
		if provider.Generation != 2 || reg.Generation != 2 {
			t.Fatalf("resolved generations = (%d, %d), want 2", provider.Generation, reg.Generation)
		}
	})

	t.Run("stale_generation_and_cross_owner_conflict", func(t *testing.T) {
		t.Parallel()
		svc := newService(start)
		if _, err := svc.Replace(ctx, "subject:alice", newRegistration("reg-1"), []*RemoteProvider{
			newProvider("app", "test-app"),
		}, 0); err != nil {
			t.Fatalf("alice Replace: %v", err)
		}

		_, err := svc.Replace(ctx, "subject:alice", newRegistration("reg-1"), []*RemoteProvider{
			newProvider("app", "test-app"),
		}, 0)
		if !errors.Is(err, ErrGenerationMismatch) {
			t.Fatalf("stale Replace error = %v, want %v", err, ErrGenerationMismatch)
		}

		_, err = svc.Replace(ctx, "subject:bob", newRegistration("reg-2"), []*RemoteProvider{
			newProvider("app", "test-app"),
		}, 0)
		if !errors.Is(err, ErrProviderOwnedElsewhere) {
			t.Fatalf("bob Replace error = %v, want %v", err, ErrProviderOwnedElsewhere)
		}
		// A failed Replace must leave both stores unchanged.
		if _, err := svc.Get(ctx, "reg-2"); !errors.Is(err, ErrNotRegistered) {
			t.Fatalf("bob registration after conflict = %v, want %v", err, ErrNotRegistered)
		}
		provider, reg, err := svc.ResolveProvider(ctx, "app", "test-app")
		if err != nil {
			t.Fatalf("ResolveProvider after conflict: %v", err)
		}
		if reg.ID != "reg-1" || reg.Generation != 1 || provider.RegistrationID != "reg-1" {
			t.Fatalf("alice state after conflict = (%s, gen %d, provider %s), want reg-1, gen 1, reg-1",
				reg.ID, reg.Generation, provider.RegistrationID)
		}
	})

	t.Run("lease_renewal_delete_and_expire", func(t *testing.T) {
		t.Parallel()
		svc := newService(start)
		got, err := svc.Replace(ctx, "subject:alice", newRegistration("reg-1"), []*RemoteProvider{
			newProvider("app", "test-app"),
		}, 0)
		if err != nil {
			t.Fatalf("Replace: %v", err)
		}

		next := start.Add(10 * time.Second)
		svc.now = func() time.Time { return next }
		if err := svc.RenewLease(ctx, "reg-1", got.Generation, lease); err != nil {
			t.Fatalf("RenewLease: %v", err)
		}
		updated, err := svc.Get(ctx, "reg-1")
		if err != nil {
			t.Fatalf("Get after renew: %v", err)
		}
		if !updated.LeaseExpiresAt.Equal(next.Add(lease)) {
			t.Fatalf("lease_expires_at = %v, want %v", updated.LeaseExpiresAt, next.Add(lease))
		}

		if err := svc.Delete(ctx, "reg-1", updated.Generation); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := svc.Get(ctx, "reg-1"); !errors.Is(err, ErrNotRegistered) {
			t.Fatalf("Get after delete error = %v, want %v", err, ErrNotRegistered)
		}

		got, err = svc.Replace(ctx, "subject:alice", newRegistration("reg-1"), []*RemoteProvider{
			newProvider("app", "test-app"),
		}, 0)
		if err != nil {
			t.Fatalf("second Replace: %v", err)
		}
		svc.now = func() time.Time { return start.Add(lease) }
		if err := svc.Expire(ctx, "reg-1", got.Generation, got.LeaseExpiresAt); err != nil {
			t.Fatalf("Expire: %v", err)
		}
		if _, err := svc.Get(ctx, "reg-1"); !errors.Is(err, ErrNotRegistered) {
			t.Fatalf("Get after expire error = %v, want %v", err, ErrNotRegistered)
		}
	})

	t.Run("resolve_treats_expired_lease_as_unregistered", func(t *testing.T) {
		t.Parallel()
		svc := newService(start)
		if _, err := svc.Replace(ctx, "subject:alice", newRegistration("reg-1"), []*RemoteProvider{
			newProvider("app", "test-app"),
		}, 0); err != nil {
			t.Fatalf("Replace: %v", err)
		}
		svc.now = func() time.Time { return start.Add(lease) }
		_, _, err := svc.ResolveProvider(ctx, "app", "test-app")
		if !errors.Is(err, ErrNotRegistered) {
			t.Fatalf("ResolveProvider error = %v, want %v", err, ErrNotRegistered)
		}
	})

	t.Run("expired_lease_allows_generation_zero_create_and_takeover", func(t *testing.T) {
		t.Parallel()
		svc := newService(start)
		// freshRegistration builds a registration whose lease is alive at the current svc.now.
		freshRegistration := func(id string) *RemoteRegistration {
			reg := newRegistration(id)
			reg.LeaseExpiresAt = svc.now().Add(lease)
			return reg
		}
		if _, err := svc.Replace(ctx, "subject:alice", freshRegistration("reg-1"), []*RemoteProvider{
			newProvider("app", "test-app"),
		}, 0); err != nil {
			t.Fatalf("alice Replace: %v", err)
		}
		// Advance past the lease deadline without running Expire. The durable row and
		// provider rows are still present, but reads resolve unregistered, so a write at
		// expectedGeneration 0 must succeed for the same owner and for a different owner.
		svc.now = func() time.Time { return start.Add(lease + time.Second) }

		// A stale nonzero generation against the expired row still mismatches: the row
		// resolves unregistered, so only expectedGeneration 0 is valid.
		if _, err := svc.Replace(ctx, "subject:alice", freshRegistration("reg-1"), []*RemoteProvider{
			newProvider("app", "test-app"),
		}, 1); !errors.Is(err, ErrGenerationMismatch) {
			t.Fatalf("expired stale-generation Replace error = %v, want %v", err, ErrGenerationMismatch)
		}

		alice2, err := svc.Replace(ctx, "subject:alice", freshRegistration("reg-1"), []*RemoteProvider{
			newProvider("app", "test-app"),
		}, 0)
		if err != nil {
			t.Fatalf("alice re-Replace after expiry: %v", err)
		}
		if alice2.Generation != 1 {
			t.Fatalf("alice generation after expiry re-create = %d, want 1", alice2.Generation)
		}

		// Expire alice's new lease, then a different owner takes over the same (kind, name).
		svc.now = func() time.Time { return alice2.LeaseExpiresAt.Add(time.Second) }
		bob, err := svc.Replace(ctx, "subject:bob", freshRegistration("reg-2"), []*RemoteProvider{
			newProvider("app", "test-app"),
		}, 0)
		if err != nil {
			t.Fatalf("bob takeover after expiry: %v", err)
		}
		if bob.OwnerSubjectID != "subject:bob" {
			t.Fatalf("bob owner = %q, want subject:bob", bob.OwnerSubjectID)
		}
		provider, reg, err := svc.ResolveProvider(ctx, "app", "test-app")
		if err != nil {
			t.Fatalf("ResolveProvider after takeover: %v", err)
		}
		if reg.ID != "reg-2" || provider.RegistrationID != "reg-2" {
			t.Fatalf("after takeover = (reg %s, provider %s), want reg-2, reg-2", reg.ID, provider.RegistrationID)
		}
		if _, err := svc.Get(ctx, "reg-1"); !errors.Is(err, ErrNotRegistered) {
			t.Fatalf("alice reg after takeover = %v, want %v", err, ErrNotRegistered)
		}
	})

	t.Run("expired_lease_rejects_late_heartbeat_and_check_failure", func(t *testing.T) {
		t.Parallel()
		svc := newService(start)
		freshRegistration := func(id string) *RemoteRegistration {
			reg := newRegistration(id)
			reg.LeaseExpiresAt = svc.now().Add(lease)
			return reg
		}
		got, err := svc.Replace(ctx, "subject:alice", freshRegistration("reg-1"), []*RemoteProvider{
			newProvider("app", "test-app"),
		}, 0)
		if err != nil {
			t.Fatalf("Replace: %v", err)
		}
		// Advance past the lease deadline. A late heartbeat at the correct generation must
		// not resurrect the registration, and a check-failure report must not write to it.
		expired := got.LeaseExpiresAt.Add(time.Second)
		svc.now = func() time.Time { return expired }

		if err := svc.RenewLease(ctx, "reg-1", got.Generation, lease); !errors.Is(err, ErrNotRegistered) {
			t.Fatalf("RenewLease after expiry error = %v, want %v", err, ErrNotRegistered)
		}
		if err := svc.RecordCheckFailure(ctx, "reg-1", got.Generation, "dial timeout"); !errors.Is(err, ErrNotRegistered) {
			t.Fatalf("RecordCheckFailure after expiry error = %v, want %v", err, ErrNotRegistered)
		}
		stored, err := svc.Get(ctx, "reg-1")
		if err != nil {
			t.Fatalf("Get after late heartbeat: %v", err)
		}
		if !stored.LeaseExpiresAt.Equal(got.LeaseExpiresAt) {
			t.Fatalf("lease_expires_at after late heartbeat = %v, want %v (unchanged)", stored.LeaseExpiresAt, got.LeaseExpiresAt)
		}
		if stored.LastError != "" {
			t.Fatalf("last_error after late check failure = %q, want empty", stored.LastError)
		}
		// The late heartbeat must not block a generation-0 takeover by another owner.
		svc.now = func() time.Time { return expired }
		if _, err := svc.Replace(ctx, "subject:bob", freshRegistration("reg-2"), []*RemoteProvider{
			newProvider("app", "test-app"),
		}, 0); err != nil {
			t.Fatalf("bob takeover after late heartbeat: %v", err)
		}
	})

	t.Run("rejects_forbidden_provider_kinds", func(t *testing.T) {
		t.Parallel()
		svc := newService(start)
		for _, kind := range []string{providermanifestv1.KindIdentity, providermanifestv1.KindAuthorization} {
			_, err := svc.Replace(ctx, "subject:alice", newRegistration("reg-1"), []*RemoteProvider{
				newProvider(kind, "local"),
			}, 0)
			if err == nil {
				t.Fatalf("Replace(%q) succeeded, want error", kind)
			}
		}
	})
}
