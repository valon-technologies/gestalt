package indexeddb

import (
	"context"
	"testing"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/indexeddbcodec"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/hostserviceingress"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
)

func TestRemoteDevelopmentStoreNameResolverMapsLogicalNames(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := &coretesting.StubIndexedDB{}
	namespaces := coredata.NewRemoteIndexedDBNamespaceService(db)

	ns, err := namespaces.Prepare(ctx, "reg-1", 1, "session-1", "owner-1", "support", &coredata.AppIndexedDBBinding{
		ProviderName:  "main",
		DatabaseName:  "support",
		AllowedStores: []string{"tasks"},
	}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if err := namespaces.ActivateRegistration(ctx, "reg-1", 1); err != nil {
		t.Fatalf("ActivateRegistration: %v", err)
	}

	physicalName, err := namespaces.ResolvePhysicalName(ctx, ns.ID, "tasks")
	if err != nil {
		t.Fatalf("ResolvePhysicalName: %v", err)
	}
	if physicalName == "tasks" {
		t.Fatalf("expected physical name to differ from logical name, got %q", physicalName)
	}

	resolver := &RemoteDevelopmentStoreNameResolver{
		Namespaces:   namespaces,
		AppName:      "support",
		ProviderName: "main",
		DatabaseName: "support",
	}

	noNamespace, scope, err := resolver.ResolveStoreName(ctx, "tasks")
	if err != nil {
		t.Fatalf("ResolveStoreName without namespace claim: %v", err)
	}
	if noNamespace != "tasks" || scope != nil {
		t.Fatalf("expected identity mapping without namespace claim, got %q scope=%v", noNamespace, scope)
	}

	capCtx := hostserviceingress.ApplyCapability(ctx, runtimehost.HostServiceRelayTarget{
		AppName:      "support",
		SessionID:    "session-1",
		Service:      "indexeddb",
		MethodPrefix: "/gestalt.provider.v1.IndexedDB/",
		IndexedDBNamespace: &runtimehost.IndexedDBNamespaceClaims{
			NamespaceID:    ns.ID,
			RegistrationID: "reg-1",
			Generation:     1,
			ProviderName:   "main",
			DatabaseName:   "support",
			SessionID:      "session-1",
			AppName:        "support",
		},
	})

	mapped, scope, err := resolver.ResolveStoreName(capCtx, "tasks")
	if err != nil {
		t.Fatalf("ResolveStoreName with namespace claim: %v", err)
	}
	if mapped != physicalName {
		t.Fatalf("expected physical name %q, got %q", physicalName, mapped)
	}
	if scope == nil || scope.NamespaceID != ns.ID || scope.LogicalName != "tasks" || scope.PhysicalName != physicalName {
		t.Fatalf("unexpected scope: %+v", scope)
	}

	wrongBindingCtx := hostserviceingress.ApplyCapability(ctx, runtimehost.HostServiceRelayTarget{
		AppName:      "support",
		SessionID:    "session-1",
		Service:      "indexeddb",
		MethodPrefix: "/gestalt.provider.v1.IndexedDB/",
		IndexedDBNamespace: &runtimehost.IndexedDBNamespaceClaims{
			NamespaceID:    ns.ID,
			RegistrationID: "reg-1",
			Generation:     1,
			ProviderName:   "main",
			DatabaseName:   "wrong-db",
			SessionID:      "session-1",
			AppName:        "support",
		},
	})

	if _, _, err := resolver.ResolveStoreName(wrongBindingCtx, "tasks"); err == nil {
		t.Fatalf("expected error for namespace database mismatch")
	}
}

func TestIndexedDBServerIsolatesRemoteDevelopmentNamespace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := &coretesting.StubIndexedDB{}
	namespaces := coredata.NewRemoteIndexedDBNamespaceService(db)

	ns, err := namespaces.Prepare(ctx, "reg-1", 1, "session-1", "owner-1", "support", &coredata.AppIndexedDBBinding{
		ProviderName:  "main",
		DatabaseName:  "support",
		AllowedStores: []string{"tasks"},
	}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if err := namespaces.ActivateRegistration(ctx, "reg-1", 1); err != nil {
		t.Fatalf("ActivateRegistration: %v", err)
	}

	physicalName, err := namespaces.ResolvePhysicalName(ctx, ns.ID, "tasks")
	if err != nil {
		t.Fatalf("ResolvePhysicalName: %v", err)
	}

	// Seed a production logical "tasks" store that the local app must not touch.
	prodDB := &coretesting.StubIndexedDB{}
	if _, err := prodDB.CreateObjectStore(ctx, "tasks", idb.ObjectStoreOptions{}); err != nil {
		t.Fatalf("CreateObjectStore production tasks: %v", err)
	}

	resolver := &RemoteDevelopmentStoreNameResolver{
		Namespaces:   namespaces,
		AppName:      "support",
		ProviderName: "main",
		DatabaseName: "support",
	}
	tracker := &RemoteDevelopmentNamespaceTracker{Namespaces: namespaces}
	srv := NewServer(prodDB, "main", ServerOptions{
		AllowedStores: []string{"tasks"},
		StoreNames:    resolver,
		StoreTracker:  tracker,
	}).(*indexedDBServer)

	capCtx := hostserviceingress.ApplyCapability(ctx, runtimehost.HostServiceRelayTarget{
		AppName:      "support",
		SessionID:    "session-1",
		Service:      "indexeddb",
		MethodPrefix: "/gestalt.provider.v1.IndexedDB/",
		IndexedDBNamespace: &runtimehost.IndexedDBNamespaceClaims{
			NamespaceID:    ns.ID,
			RegistrationID: "reg-1",
			Generation:     1,
			ProviderName:   "main",
			DatabaseName:   "support",
			SessionID:      "session-1",
			AppName:        "support",
		},
	})

	record, err := indexeddbcodec.RecordToProto(map[string]any{"id": "task-local", "title": "local task"})
	if err != nil {
		t.Fatalf("RecordToProto: %v", err)
	}
	if _, err := srv.Put(capCtx, &proto.RecordRequest{Store: "tasks", Record: record}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Local writes should land in the physical namespace store, not the
	// production logical "tasks" store.
	if !prodDB.HasObjectStore(physicalName) {
		t.Fatalf("expected physical store %q to exist", physicalName)
	}
	if _, err := prodDB.ObjectStore(physicalName).Get(capCtx, "task-local"); err != nil {
		t.Fatalf("expected local record in physical store: %v", err)
	}
	if _, err := prodDB.ObjectStore("tasks").Get(capCtx, "task-local"); err == nil {
		t.Fatalf("expected local record not to leak into production logical store")
	}

	// The production row should not be visible to the local session.
	_, err = srv.Get(capCtx, &proto.ObjectStoreRequest{Store: "tasks", Id: "task-prod"})
	if err == nil {
		t.Fatalf("expected local session not to see production task-prod")
	}
}
