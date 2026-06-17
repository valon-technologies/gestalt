package indexeddb

import (
	"context"
	"errors"
	"testing"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/indexeddbcodec"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	rpcidb "github.com/valon-technologies/gestalt/server/rpc/indexeddb"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"google.golang.org/grpc"
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
	if _, err := metricutil.UnwrapIndexedDB(db).CreateObjectStore(ctx, "snapshots", idb.ObjectStoreOptions{
		Indexes: []idb.IndexSchema{{Name: "by_type", KeyPath: []string{"type"}}},
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
		Store: "snapshots",
		Index: "by_type",
		Query: &proto.IndexedDBQuery{Query: &proto.IndexedDBQuery_Key{Key: &proto.KeyValue{Kind: &proto.KeyValue_Scalar{Scalar: value}}}},
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
		"db.system.name":          "gestaltd.indexeddb",
		"db.namespace":            "system",
		"db.collection.name":      "snapshots",
		"db.operation.name":       "index_get",
		"gestaltd.provider.name":  "roadmap",
		"gestaltd.idb.index.name": "by_type",
	}
	metrictest.RequireFloat64Histogram(t, rm, "db.client.operation.duration", dbIndexAttrs)
	metrictest.RequireNoMetric(t, rm, "gestaltd.idb.count")
	metrictest.RequireNoMetric(t, rm, "gestaltd.idb.error_count")
	metrictest.RequireNoMetric(t, rm, "gestaltd.idb.duration")
}

func TestIndexedDBServerRejectsStoresOutsideAllowlist(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	ctx := context.Background()
	schema := idb.ObjectStoreOptions{
		Indexes: []idb.IndexSchema{{Name: "by_type", KeyPath: []string{"type"}}},
	}
	if _, err := db.CreateObjectStore(ctx, "events", schema); err != nil {
		t.Fatalf("CreateObjectStore events: %v", err)
	}
	if err := db.ObjectStore("events").Put(ctx, idb.Record{"id": "evt-1", "type": "daily"}); err != nil {
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
	eventsQuery := &proto.IndexedDBQuery{Query: &proto.IndexedDBQuery_Key{Key: &proto.KeyValue{Kind: &proto.KeyValue_Scalar{Scalar: indexValue}}}}
	evtKey, err := indexeddbcodec.AnyToKeyValue("evt-1")
	if err != nil {
		t.Fatalf("AnyToKeyValue: %v", err)
	}
	eventsDeleteQuery := &proto.IndexedDBQuery{Query: &proto.IndexedDBQuery_Key{Key: evtKey}}

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
		Query: eventsDeleteQuery,
	}); err == nil {
		t.Fatal("DeleteRange should reject stores outside the configured allowlist")
	}
	if _, err := srv.(*indexedDBServer).IndexGet(ctx, &proto.IndexQueryRequest{
		Store: "events",
		Index: "by_type",
		Query: eventsQuery,
	}); err == nil {
		t.Fatal("IndexGet should reject stores outside the configured allowlist")
	}

	conn := newBufconnConn(t, func(server *grpc.Server) {
		proto.RegisterIndexedDBServer(server, srv)
	})
	remote := &remoteIndexedDB{Database: rpcidb.NewClient(proto.NewIndexedDBClient(conn), rpcidb.Options{})}

	if _, err := remote.ObjectStore("events").Get(ctx, "evt-1"); !errors.Is(err, idb.ErrNotFound) {
		t.Fatalf("remote Get error = %v, want idb.ErrNotFound", err)
	}
	if _, err := remote.Transaction(ctx, []string{"events"}, idb.TransactionReadwrite, idb.TransactionOptions{}); !errors.Is(err, idb.ErrNotFound) {
		t.Fatalf("remote Transaction error = %v, want idb.ErrNotFound", err)
	}
	if _, err := remote.ObjectStore("events").DeleteRange(ctx, eventsDeleteQuery); !errors.Is(err, idb.ErrNotFound) {
		t.Fatalf("remote DeleteRange error = %v, want idb.ErrNotFound", err)
	}
	if _, err := remote.ObjectStore("events").Index("by_type").Get(ctx, eventsQuery); !errors.Is(err, idb.ErrNotFound) {
		t.Fatalf("remote IndexGet error = %v, want idb.ErrNotFound", err)
	}
	if cursor, err := remote.ObjectStore("events").OpenCursor(ctx, nil, idb.CursorNext); !errors.Is(err, idb.ErrNotFound) {
		if cursor != nil {
			_ = cursor.Close()
		}
		t.Fatalf("remote OpenCursor error = %v, want idb.ErrNotFound", err)
	}

	t.Run("transaction_aborts_on_disallowed_store_mid_flight", func(t *testing.T) {
		if _, err := db.CreateObjectStore(ctx, "tasks", idb.ObjectStoreOptions{}); err != nil {
			t.Fatalf("CreateObjectStore tasks: %v", err)
		}
		if _, err := db.CreateObjectStore(ctx, "notes", idb.ObjectStoreOptions{}); err != nil {
			t.Fatalf("CreateObjectStore notes: %v", err)
		}

		tx, err := remote.Transaction(ctx, []string{"tasks"}, idb.TransactionReadwrite, idb.TransactionOptions{})
		if err != nil {
			t.Fatalf("Transaction: %v", err)
		}
		if err := tx.ObjectStore("tasks").Put(ctx, idb.Record{"id": "task-1"}); err != nil {
			t.Fatalf("Put allowed task: %v", err)
		}
		if err := tx.ObjectStore("notes").Put(ctx, idb.Record{"id": "note-1"}); !errors.Is(err, idb.ErrNotFound) {
			t.Fatalf("Put disallowed store error = %v, want idb.ErrNotFound", err)
		}
		if err := tx.Commit(ctx); err == nil {
			t.Fatal("Commit should fail after disallowed store operation")
		}
		if _, err := db.ObjectStore("tasks").Get(ctx, "task-1"); !errors.Is(err, idb.ErrNotFound) {
			t.Fatalf("task-1 after aborted transaction error = %v, want idb.ErrNotFound", err)
		}
	})
}

func TestIndexedDBServerPutRejectsUniqueIndexConflict(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	ctx := context.Background()
	if _, err := db.CreateObjectStore(ctx, "users", idb.ObjectStoreOptions{
		Indexes: []idb.IndexSchema{{Name: "by_email", KeyPath: []string{"email"}, Unique: true}},
	}); err != nil {
		t.Fatalf("CreateObjectStore users: %v", err)
	}
	store := db.ObjectStore("users")
	if err := store.Put(ctx, idb.Record{"id": "user-1", "email": "same@example.com"}); err != nil {
		t.Fatalf("seed user-1: %v", err)
	}
	if err := store.Put(ctx, idb.Record{"id": "user-2", "email": "other@example.com"}); err != nil {
		t.Fatalf("seed user-2: %v", err)
	}

	srv := NewServer(db, "roadmap", ServerOptions{})
	conn := newBufconnConn(t, func(server *grpc.Server) {
		proto.RegisterIndexedDBServer(server, srv)
	})
	remote := &remoteIndexedDB{Database: rpcidb.NewClient(proto.NewIndexedDBClient(conn), rpcidb.Options{})}

	if err := remote.ObjectStore("users").Put(ctx, idb.Record{"id": "user-2", "email": "same@example.com"}); !errors.Is(err, idb.ErrAlreadyExists) {
		t.Fatalf("conflicting remote Put error = %v, want idb.ErrAlreadyExists", err)
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
	if _, err := db.CreateObjectStore(ctx, "events", idb.ObjectStoreOptions{}); err != nil {
		t.Fatalf("CreateObjectStore events: %v", err)
	}
	srv := NewServer(db, "roadmap", ServerOptions{})
	conn := newBufconnConn(t, func(server *grpc.Server) {
		proto.RegisterIndexedDBServer(server, srv)
	})
	remote := &remoteIndexedDB{Database: rpcidb.NewClient(proto.NewIndexedDBClient(conn), rpcidb.Options{})}

	readonly, err := remote.Transaction(ctx, []string{"events"}, idb.TransactionReadonly, idb.TransactionOptions{})
	if err != nil {
		t.Fatalf("readonly Transaction: %v", err)
	}
	if err := readonly.ObjectStore("events").Put(ctx, idb.Record{"id": "evt-1"}); !errors.Is(err, idb.ErrReadOnly) {
		t.Fatalf("readonly Put error = %v, want idb.ErrReadOnly", err)
	}
	if err := readonly.Commit(ctx); !errors.Is(err, idb.ErrReadOnly) {
		t.Fatalf("readonly Commit error = %v, want idb.ErrReadOnly", err)
	}

	readwrite, err := remote.Transaction(ctx, []string{"events"}, idb.TransactionReadwrite, idb.TransactionOptions{})
	if err != nil {
		t.Fatalf("readwrite Transaction: %v", err)
	}
	if err := readwrite.Abort(ctx); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if err := readwrite.Commit(ctx); !errors.Is(err, idb.ErrTransactionDone) {
		t.Fatalf("Commit after Abort error = %v, want idb.ErrTransactionDone", err)
	}
}
