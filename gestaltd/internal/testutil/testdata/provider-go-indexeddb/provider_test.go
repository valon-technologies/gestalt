package provider

import (
	"context"
	"errors"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

func TestOpenDatabaseUpgradeRollsBackOnError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	p := New()
	version := uint64(1)
	_, err := p.OpenDatabase(ctx, "app", gestalt.OpenOptions{
		Version: &version,
		Upgrade: func(ctx context.Context, upgrade gestalt.UpgradeContext) error {
			return upgrade.CreateObjectStore(ctx, "users", gestalt.ObjectStoreSchema{
				Indexes: []gestalt.IndexSchema{{Name: "by_email", KeyPath: []string{"email"}}},
			})
		},
	})
	if err != nil {
		t.Fatalf("Open successful v1: %v", err)
	}

	upgradeErr := errors.New("upgrade failed")
	version = 2
	_, err = p.OpenDatabase(ctx, "app", gestalt.OpenOptions{
		Version: &version,
		Upgrade: func(ctx context.Context, upgrade gestalt.UpgradeContext) error {
			if err := upgrade.DeleteIndex(ctx, "users", "by_email"); err != nil {
				return err
			}
			if err := upgrade.CreateObjectStore(ctx, "sessions", gestalt.ObjectStoreSchema{}); err != nil {
				return err
			}
			return upgradeErr
		},
	})
	if !errors.Is(err, upgradeErr) {
		t.Fatalf("Open v2 error = %v, want upgrade error", err)
	}
	if p.Version() != 1 {
		t.Fatalf("Version after failed v2 upgrade = %d, want 1", p.Version())
	}
	if providerHasStore(p, "sessions") {
		t.Fatal("failed v2 upgrade left created sessions store")
	}
	if err := p.Put(ctx, gestalt.IndexedDBRecordRequest{
		Store:  "users",
		Record: gestalt.Record{"id": "user-1", "email": "kept@example.com"},
	}); err != nil {
		t.Fatalf("Put after failed v2 upgrade: %v", err)
	}
	record, err := p.IndexGet(ctx, gestalt.IndexedDBIndexQueryRequest{
		Store:  "users",
		Index:  "by_email",
		Values: []any{"kept@example.com"},
	})
	if err != nil {
		t.Fatalf("users.by_email after failed v2 upgrade: %v", err)
	}
	if record["id"] != "user-1" {
		t.Fatalf("record id after failed v2 upgrade = %v, want user-1", record["id"])
	}
}

func TestUpgradeCreateIndexRequiresExistingObjectStore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	p := New()
	version := uint64(1)
	_, err := p.OpenDatabase(ctx, "app", gestalt.OpenOptions{
		Version: &version,
		Upgrade: func(ctx context.Context, upgrade gestalt.UpgradeContext) error {
			return upgrade.CreateIndex(ctx, "missing", gestalt.IndexSchema{
				Name:    "by_email",
				KeyPath: []string{"email"},
			})
		},
	})
	if err == nil {
		t.Fatal("Open succeeded, want missing object store error")
	}
	if providerHasStore(p, "missing") {
		t.Fatal("CreateIndex created missing object store")
	}
}

func TestUpgradeDeleteIndexRequiresExistingObjectStore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	p := New()
	version := uint64(1)
	_, err := p.OpenDatabase(ctx, "app", gestalt.OpenOptions{
		Version: &version,
		Upgrade: func(ctx context.Context, upgrade gestalt.UpgradeContext) error {
			return upgrade.DeleteIndex(ctx, "missing", "by_email")
		},
	})
	if err == nil {
		t.Fatal("Open succeeded, want missing object store error")
	}
	if providerHasStore(p, "missing") {
		t.Fatal("DeleteIndex created missing object store")
	}
}

func providerHasStore(p *Provider, name string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, ok := p.stores[name]
	return ok
}
