package pluginhost

import (
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
)

func TestStructFromMap_NormalizesTimeValues(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 11, 20, 31, 30, 840502000, time.UTC)
	record := map[string]any{
		"created_at": now,
		"expires_at": &now,
		"nested": map[string]any{
			"updated_at": now,
		},
		"values": []any{now, &now},
	}

	s, err := structFromMap(record)
	if err != nil {
		t.Fatalf("structFromMap: %v", err)
	}

	got := s.AsMap()
	want := now.Format(transportTimeLayout)
	if got["created_at"] != want {
		t.Fatalf("created_at = %#v, want %#v", got["created_at"], want)
	}
	if got["expires_at"] != want {
		t.Fatalf("expires_at = %#v, want %#v", got["expires_at"], want)
	}
	nested, ok := got["nested"].(map[string]any)
	if !ok || nested["updated_at"] != want {
		t.Fatalf("nested = %#v, want updated_at=%#v", got["nested"], want)
	}
	values, ok := got["values"].([]any)
	if !ok || len(values) != 2 {
		t.Fatalf("values = %#v, want 2 normalized entries", got["values"])
	}
	if values[0] != want || values[1] != want {
		t.Fatalf("values = %#v, want both %#v", values, want)
	}
}

func TestRecordTransportRoundTrip_UsesSchemaTypes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 11, 20, 31, 30, 840502000, time.FixedZone("EDT", -4*60*60))
	schema := &indexeddb.ObjectStoreSchema{
		Columns: []indexeddb.ColumnDef{
			{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
			{Name: "created_at", Type: indexeddb.TypeTime},
			{Name: "refresh_error_count", Type: indexeddb.TypeInt},
			{Name: "payload", Type: indexeddb.TypeBytes},
			{Name: "enabled", Type: indexeddb.TypeBool},
			{Name: "score", Type: indexeddb.TypeFloat},
		},
	}
	record := indexeddb.Record{
		"id":                  "tok_123",
		"created_at":          now,
		"refresh_error_count": 7,
		"payload":             []byte("secret"),
		"enabled":             true,
		"score":               1.5,
	}

	s, err := structFromRecord(record, schema)
	if err != nil {
		t.Fatalf("structFromRecord: %v", err)
	}

	got := s.AsMap()
	if got["created_at"] != now.UTC().Format(transportTimeLayout) {
		t.Fatalf("created_at = %#v, want %#v", got["created_at"], now.UTC().Format(transportTimeLayout))
	}
	if got["refresh_error_count"] != "7" {
		t.Fatalf("refresh_error_count = %#v, want %#v", got["refresh_error_count"], "7")
	}
	if got["payload"] != "b64:c2VjcmV0" {
		t.Fatalf("payload = %#v, want %#v", got["payload"], "b64:c2VjcmV0")
	}

	decoded, err := recordFromStruct(s, schema)
	if err != nil {
		t.Fatalf("recordFromStruct: %v", err)
	}

	createdAt, ok := decoded["created_at"].(time.Time)
	if !ok || !createdAt.Equal(now.UTC()) {
		t.Fatalf("created_at = %#v, want %v", decoded["created_at"], now.UTC())
	}
	if decoded["refresh_error_count"] != int64(7) {
		t.Fatalf("refresh_error_count = %#v, want %#v", decoded["refresh_error_count"], int64(7))
	}
	payload, ok := decoded["payload"].([]byte)
	if !ok || string(payload) != "secret" {
		t.Fatalf("payload = %#v, want []byte(\"secret\")", decoded["payload"])
	}
	if decoded["enabled"] != true {
		t.Fatalf("enabled = %#v, want true", decoded["enabled"])
	}
	if decoded["score"] != 1.5 {
		t.Fatalf("score = %#v, want 1.5", decoded["score"])
	}
}

func TestRecordFromStruct_AcceptsLegacyTimeLayout(t *testing.T) {
	t.Parallel()

	schema := &indexeddb.ObjectStoreSchema{
		Columns: []indexeddb.ColumnDef{
			{Name: "created_at", Type: indexeddb.TypeTime},
		},
	}
	s, err := structFromMap(map[string]any{"created_at": "2026-04-11 23:52:45"})
	if err != nil {
		t.Fatalf("structFromMap: %v", err)
	}

	record, err := recordFromStruct(s, schema)
	if err != nil {
		t.Fatalf("recordFromStruct: %v", err)
	}

	got, ok := record["created_at"].(time.Time)
	if !ok {
		t.Fatalf("created_at = %#v, want time.Time", record["created_at"])
	}
	want := time.Date(2026, time.April, 11, 23, 52, 45, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("created_at = %v, want %v", got, want)
	}
}

func TestIndexedDBServer_AddRestoresTypedValuesFromSchema(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	srv := NewIndexedDBServer(db, "test")
	ctx := t.Context()
	now := time.Date(2026, time.April, 11, 23, 52, 45, 123000000, time.UTC)

	_, err := srv.CreateObjectStore(ctx, &proto.CreateObjectStoreRequest{
		Name: "tokens",
		Schema: &proto.ObjectStoreSchema{
			Columns: []*proto.ColumnDef{
				{Name: "id", Type: int32(indexeddb.TypeString), PrimaryKey: true},
				{Name: "last_refreshed_at", Type: int32(indexeddb.TypeTime)},
				{Name: "refresh_error_count", Type: int32(indexeddb.TypeInt)},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}

	record, err := structFromRecord(indexeddb.Record{
		"id":                  "tok_123",
		"last_refreshed_at":   now,
		"refresh_error_count": 2,
	}, &indexeddb.ObjectStoreSchema{
		Columns: []indexeddb.ColumnDef{
			{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
			{Name: "last_refreshed_at", Type: indexeddb.TypeTime},
			{Name: "refresh_error_count", Type: indexeddb.TypeInt},
		},
	})
	if err != nil {
		t.Fatalf("structFromRecord: %v", err)
	}

	if _, err := srv.Add(ctx, &proto.RecordRequest{Store: "tokens", Record: record}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	stored, err := db.ObjectStore("plugin_test_tokens").Get(ctx, "tok_123")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	gotTime, ok := stored["last_refreshed_at"].(time.Time)
	if !ok || !gotTime.Equal(now.UTC()) {
		t.Fatalf("last_refreshed_at = %#v, want %v", stored["last_refreshed_at"], now.UTC())
	}
	if stored["refresh_error_count"] != int64(2) {
		t.Fatalf("refresh_error_count = %#v, want %#v", stored["refresh_error_count"], int64(2))
	}
}
