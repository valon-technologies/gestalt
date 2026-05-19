package indexeddb

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/internal/indexeddbcodec"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIndexedDBServerUsesStoreNamesAsProvided(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	srv := NewServer(db, "roadmap", ServerOptions{})
	ctx := context.Background()
	record, err := indexeddbcodec.RecordToProto(map[string]any{"id": "snap-1"})
	if err != nil {
		t.Fatalf("RecordToProto: %v", err)
	}

	if _, err := srv.(*indexedDBServer).Put(ctx, &proto.RecordRequest{
		Store:  "snapshots",
		Record: record,
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := db.ObjectStore("snapshots").Get(ctx, "snap-1"); err != nil {
		t.Fatalf("expected object store record to exist: %v", err)
	}
}

func TestIndexedDBServerRecordsPluginMetricAttributes(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)

	db := metricutil.InstrumentIndexedDB(&coretesting.StubIndexedDB{}, "system")
	srv := NewServer(db, "roadmap", ServerOptions{})
	if err := metricutil.UnwrapIndexedDB(db).CreateObjectStore(ctx, "snapshots", indexeddb.ObjectStoreSchema{
		Indexes: []indexeddb.IndexSchema{{Name: "by_type", KeyPath: []string{"type"}}},
	}); err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}

	if _, err := srv.(*indexedDBServer).Put(ctx, &proto.RecordRequest{
		Store: "snapshots",
		Record: func() *proto.Record {
			rec, err := indexeddbcodec.RecordToProto(map[string]any{"id": "snap-1", "type": "daily"})
			if err != nil {
				t.Fatalf("RecordToProto with type: %v", err)
			}
			return rec
		}(),
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	value, err := indexeddbcodec.TypedValueFromAny("daily")
	if err != nil {
		t.Fatalf("TypedValueFromAny: %v", err)
	}
	if _, err := srv.(*indexedDBServer).IndexGet(ctx, &proto.IndexQueryRequest{
		Store:  "snapshots",
		Index:  "by_type",
		Values: []*proto.TypedValue{value},
	}); err != nil {
		t.Fatalf("IndexGet: %v", err)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	dbPutAttrs := map[string]string{
		"db.system.name":         "gestaltd.indexeddb",
		"db.namespace":           "system",
		"db.collection.name":     "snapshots",
		"db.operation.name":      "put",
		"gestaltd.provider.name": "roadmap",
	}
	metrictest.RequireFloat64Histogram(t, rm, "db.client.operation.duration", dbPutAttrs)
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "db.client.operation.duration", dbPutAttrs, "gestalt.db")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "db.client.operation.duration", dbPutAttrs, "gestalt.plugin")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "db.client.operation.duration", dbPutAttrs, "gestalt.object_store")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "db.client.operation.duration", dbPutAttrs, "gestalt.method")

	dbIndexAttrs := map[string]string{
		"db.system.name":                "gestaltd.indexeddb",
		"db.namespace":                  "system",
		"db.collection.name":            "snapshots",
		"db.operation.name":             "index_get",
		"gestaltd.provider.name":        "roadmap",
		"gestaltd.indexeddb.index.name": "by_type",
	}
	metrictest.RequireFloat64Histogram(t, rm, "db.client.operation.duration", dbIndexAttrs)
	metrictest.RequireNoMetric(t, rm, "gestaltd.indexeddb.count")
	metrictest.RequireNoMetric(t, rm, "gestaltd.indexeddb.error_count")
	metrictest.RequireNoMetric(t, rm, "gestaltd.indexeddb.duration")
}

func TestIndexedDBServerRejectsStoresOutsideAllowlist(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	ctx := context.Background()
	schema := indexeddb.ObjectStoreSchema{
		Indexes: []indexeddb.IndexSchema{{Name: "by_type", KeyPath: []string{"type"}}},
	}
	if err := db.CreateObjectStore(ctx, "events", schema); err != nil {
		t.Fatalf("CreateObjectStore events: %v", err)
	}
	if err := db.ObjectStore("events").Put(ctx, indexeddb.Record{"id": "evt-1", "type": "daily"}); err != nil {
		t.Fatalf("seed events record: %v", err)
	}

	srv := NewServer(db, "roadmap", ServerOptions{
		AllowedStores: []string{"tasks"},
	})
	record, err := indexeddbcodec.RecordToProto(map[string]any{"id": "evt-1"})
	if err != nil {
		t.Fatalf("RecordToProto: %v", err)
	}
	indexValue, err := indexeddbcodec.TypedValueFromAny("daily")
	if err != nil {
		t.Fatalf("TypedValueFromAny: %v", err)
	}
	eventsRange, err := keyRangeToProto(indexeddb.Only("evt-1"))
	if err != nil {
		t.Fatalf("keyRangeToProto: %v", err)
	}

	if _, err := srv.(*indexedDBServer).Put(ctx, &proto.RecordRequest{
		Store:  "events",
		Record: record,
	}); err == nil {
		t.Fatal("Put should reject stores outside the configured allowlist")
	}
	if _, err := srv.(*indexedDBServer).CreateObjectStore(ctx, &proto.CreateObjectStoreRequest{
		Name: "events",
	}); err == nil {
		t.Fatal("CreateObjectStore should reject stores outside the configured allowlist")
	}
	if _, err := srv.(*indexedDBServer).DeleteObjectStore(ctx, &proto.DeleteObjectStoreRequest{
		Name: "events",
	}); err == nil {
		t.Fatal("DeleteObjectStore should reject stores outside the configured allowlist")
	}
	if _, err := srv.(*indexedDBServer).Get(ctx, &proto.ObjectStoreRequest{
		Store: "events",
		Id:    "evt-1",
	}); err == nil {
		t.Fatal("Get should reject stores outside the configured allowlist")
	}
	if _, err := srv.(*indexedDBServer).DeleteRange(ctx, &proto.ObjectStoreRangeRequest{
		Store: "events",
		Range: eventsRange,
	}); err == nil {
		t.Fatal("DeleteRange should reject stores outside the configured allowlist")
	}
	if _, err := srv.(*indexedDBServer).IndexGet(ctx, &proto.IndexQueryRequest{
		Store:  "events",
		Index:  "by_type",
		Values: []*proto.TypedValue{indexValue},
	}); err == nil {
		t.Fatal("IndexGet should reject stores outside the configured allowlist")
	}

	conn := newBufconnConn(t, func(server *grpc.Server) {
		proto.RegisterIndexedDBServer(server, srv)
	})
	remote := &remoteIndexedDB{client: proto.NewIndexedDBClient(conn)}

	if _, err := remote.ObjectStore("events").Get(ctx, "evt-1"); !errors.Is(err, indexeddb.ErrNotFound) {
		t.Fatalf("remote Get error = %v, want indexeddb.ErrNotFound", err)
	}
	if _, err := remote.Transaction(ctx, []string{"events"}, indexeddb.TransactionReadwrite, indexeddb.TransactionOptions{}); !errors.Is(err, indexeddb.ErrNotFound) {
		t.Fatalf("remote Transaction error = %v, want indexeddb.ErrNotFound", err)
	}
	if _, err := remote.ObjectStore("events").DeleteRange(ctx, *indexeddb.Only("evt-1")); !errors.Is(err, indexeddb.ErrNotFound) {
		t.Fatalf("remote DeleteRange error = %v, want indexeddb.ErrNotFound", err)
	}
	if _, err := remote.ObjectStore("events").Index("by_type").Get(ctx, "daily"); !errors.Is(err, indexeddb.ErrNotFound) {
		t.Fatalf("remote IndexGet error = %v, want indexeddb.ErrNotFound", err)
	}
	if cursor, err := remote.ObjectStore("events").OpenCursor(ctx, nil, indexeddb.CursorNext); !errors.Is(err, indexeddb.ErrNotFound) {
		if cursor != nil {
			_ = cursor.Close()
		}
		t.Fatalf("remote OpenCursor error = %v, want indexeddb.ErrNotFound", err)
	}
}

func TestIndexedDBServerRejectsDeleteDatabaseWhenStoreScoped(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := &coretesting.StubIndexedDB{}
	version := uint64(1)
	if opened, err := db.Open(ctx, "gestalt", indexeddb.OpenOptions{Version: &version}); err != nil {
		t.Fatalf("Open seed database: %v", err)
	} else {
		defer func() { _ = opened.Close() }()
	}
	srv := NewServer(db, "external_credentials", ServerOptions{
		AllowedStores:    []string{"external_credentials"},
		AllowedDatabases: []string{"gestalt"},
	})
	conn := newBufconnConn(t, func(server *grpc.Server) {
		proto.RegisterIndexedDBServer(server, srv)
	})
	remote := &remoteIndexedDB{client: proto.NewIndexedDBClient(conn)}

	if _, err := remote.DeleteDatabase(ctx, "gestalt", indexeddb.DeleteOptions{}); status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), "store-scoped") {
		t.Fatalf("DeleteDatabase error = %v, want FailedPrecondition store-scoped denial", err)
	}
	infos, err := db.Databases(ctx)
	if err != nil {
		t.Fatalf("Databases after denied delete: %v", err)
	}
	if len(infos) != 1 || infos[0].Name != "gestalt" || infos[0].Version != 1 {
		t.Fatalf("databases after denied delete = %#v, want gestalt v1 still present", infos)
	}
}

func TestIndexedDBServerSharesConnectionRegistryAcrossServerInstances(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := &coretesting.StubIndexedDB{}
	registry := NewConnectionRegistry()
	openConn := newBufconnConn(t, func(server *grpc.Server) {
		proto.RegisterIndexedDBServer(server, NewServer(db, "roadmap", ServerOptions{
			ConnectionRegistry: registry,
		}))
	})
	opsConn := newBufconnConn(t, func(server *grpc.Server) {
		proto.RegisterIndexedDBServer(server, NewServer(db, "roadmap", ServerOptions{
			ConnectionRegistry: registry,
		}))
	})
	opener := &remoteIndexedDB{client: proto.NewIndexedDBClient(openConn)}
	opsClient := proto.NewIndexedDBClient(opsConn)

	version := uint64(1)
	opened, err := opener.Open(ctx, "default", indexeddb.OpenOptions{
		Version: &version,
		Upgrade: func(ctx context.Context, upgrade indexeddb.UpgradeContext) error {
			return upgrade.CreateObjectStore(ctx, "events", indexeddb.ObjectStoreSchema{})
		},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = opened.Close() }()
	remoteDB, ok := opened.(*remoteDatabase)
	if !ok {
		t.Fatalf("opened database type = %T, want *remoteDatabase", opened)
	}

	record, err := indexeddbcodec.RecordToProto(map[string]any{"id": "evt-1", "value": "ok"})
	if err != nil {
		t.Fatalf("RecordToProto: %v", err)
	}
	if _, err := opsClient.Put(ctx, &proto.RecordRequest{
		ConnectionId: remoteDB.connectionID,
		Store:        "events",
		Record:       record,
	}); err != nil {
		t.Fatalf("Put through second server: %v", err)
	}
	if got, err := db.ObjectStore("events").Get(ctx, "evt-1"); err != nil {
		t.Fatalf("backing Get: %v", err)
	} else if got["value"] != "ok" {
		t.Fatalf("backing value = %v, want ok", got["value"])
	}
}

func TestIndexedDBServerUpgradeOperationErrorsAreRecoverable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := &coretesting.StubIndexedDB{}
	version := uint64(1)
	if _, err := db.Open(ctx, "app", indexeddb.OpenOptions{
		Version: &version,
		Upgrade: func(ctx context.Context, upgrade indexeddb.UpgradeContext) error {
			return upgrade.CreateObjectStore(ctx, "items", indexeddb.ObjectStoreSchema{})
		},
	}); err != nil {
		t.Fatalf("initial Open: %v", err)
	}

	srv := NewServer(db, "roadmap", ServerOptions{AllowedStores: []string{"items"}}).(*indexedDBServer)
	version = 2
	_, err := db.Open(ctx, "app", indexeddb.OpenOptions{
		Version: &version,
		Upgrade: func(ctx context.Context, upgrade indexeddb.UpgradeContext) error {
			resp, terminal, opErr := srv.executeUpgradeOperation(ctx, upgrade, &proto.UpgradeOperation{
				RequestId: 1,
				Op: &proto.UpgradeOperation_CreateObjectStore{CreateObjectStore: &proto.UpgradeCreateObjectStoreRequest{
					Name: "not-allowed",
				}},
			})
			if opErr != nil {
				t.Fatalf("execute invalid CreateObjectStore opErr = %v, want nil", opErr)
			}
			if terminal {
				t.Fatal("invalid CreateObjectStore terminal = true, want false")
			}
			if resp.GetError() == nil {
				t.Fatal("invalid CreateObjectStore response error is nil")
			}

			resp, terminal, opErr = srv.executeUpgradeOperation(ctx, upgrade, &proto.UpgradeOperation{
				RequestId: 2,
				Op:        &proto.UpgradeOperation_FinishUpgrade{FinishUpgrade: &proto.FinishUpgradeRequest{}},
			})
			if opErr != nil {
				t.Fatalf("finish opErr = %v, want nil", opErr)
			}
			if !terminal {
				t.Fatal("finish terminal = false, want true")
			}
			if resp.GetError() != nil {
				t.Fatalf("finish response error = %v, want nil", resp.GetError())
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("upgrade Open after recoverable op error: %v", err)
	}
}

func TestIndexedDBServerPutRejectsUniqueIndexConflict(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	ctx := context.Background()
	if err := db.CreateObjectStore(ctx, "users", indexeddb.ObjectStoreSchema{
		Indexes: []indexeddb.IndexSchema{{Name: "by_email", KeyPath: []string{"email"}, Unique: true}},
	}); err != nil {
		t.Fatalf("CreateObjectStore users: %v", err)
	}
	store := db.ObjectStore("users")
	if err := store.Put(ctx, indexeddb.Record{"id": "user-1", "email": "same@example.com"}); err != nil {
		t.Fatalf("seed user-1: %v", err)
	}
	if err := store.Put(ctx, indexeddb.Record{"id": "user-2", "email": "other@example.com"}); err != nil {
		t.Fatalf("seed user-2: %v", err)
	}

	srv := NewServer(db, "roadmap", ServerOptions{})
	conn := newBufconnConn(t, func(server *grpc.Server) {
		proto.RegisterIndexedDBServer(server, srv)
	})
	remote := &remoteIndexedDB{client: proto.NewIndexedDBClient(conn)}

	if err := remote.ObjectStore("users").Put(ctx, indexeddb.Record{"id": "user-2", "email": "same@example.com"}); !errors.Is(err, indexeddb.ErrAlreadyExists) {
		t.Fatalf("conflicting remote Put error = %v, want indexeddb.ErrAlreadyExists", err)
	}
	got, err := remote.ObjectStore("users").Get(ctx, "user-2")
	if err != nil {
		t.Fatalf("Get user-2: %v", err)
	}
	if got["email"] != "other@example.com" {
		t.Fatalf("user-2 email = %v, want unchanged value", got["email"])
	}
}

func TestIndexedDBTransactionPreservesSentinelErrors(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	ctx := context.Background()
	if err := db.CreateObjectStore(ctx, "events", indexeddb.ObjectStoreSchema{}); err != nil {
		t.Fatalf("CreateObjectStore events: %v", err)
	}
	srv := NewServer(db, "roadmap", ServerOptions{})
	conn := newBufconnConn(t, func(server *grpc.Server) {
		proto.RegisterIndexedDBServer(server, srv)
	})
	remote := &remoteIndexedDB{client: proto.NewIndexedDBClient(conn)}

	readonly, err := remote.Transaction(ctx, []string{"events"}, indexeddb.TransactionReadonly, indexeddb.TransactionOptions{})
	if err != nil {
		t.Fatalf("readonly Transaction: %v", err)
	}
	if err := readonly.ObjectStore("events").Put(ctx, indexeddb.Record{"id": "evt-1"}); !errors.Is(err, indexeddb.ErrReadOnly) {
		t.Fatalf("readonly Put error = %v, want indexeddb.ErrReadOnly", err)
	}
	if err := readonly.Commit(ctx); !errors.Is(err, indexeddb.ErrReadOnly) {
		t.Fatalf("readonly Commit error = %v, want indexeddb.ErrReadOnly", err)
	}

	readwrite, err := remote.Transaction(ctx, []string{"events"}, indexeddb.TransactionReadwrite, indexeddb.TransactionOptions{})
	if err != nil {
		t.Fatalf("readwrite Transaction: %v", err)
	}
	if err := readwrite.Abort(ctx); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if err := readwrite.Commit(ctx); !errors.Is(err, indexeddb.ErrTransactionDone) {
		t.Fatalf("Commit after Abort error = %v, want indexeddb.ErrTransactionDone", err)
	}
}
