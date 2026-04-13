package gestalt_test

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var testClient *gestalt.IndexedDBClient

func TestMain(m *testing.M) {
	if runtime.GOOS == "windows" {
		os.Exit(0)
	}

	// Build the test harness binary.
	harnessDir := filepath.Join("..", "..", "gestaltd", "internal", "testutil", "cmd", "indexeddbtransportd")
	bin := filepath.Join(os.TempDir(), "indexeddbtransportd")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = harnessDir
	if out, err := build.CombinedOutput(); err != nil {
		panic("build harness: " + string(out))
	}

	sock := filepath.Join(os.TempDir(), "go-sdk-idb-test.sock")
	_ = os.Remove(sock)

	cmd := exec.Command(bin, "--socket", sock)
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		panic("start harness: " + err.Error())
	}

	// Wait for READY.
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "READY" {
		cmd.Process.Kill()
		panic("harness did not print READY")
	}

	os.Setenv(gestalt.EnvIndexedDBSocket, sock)
	client, err := gestalt.IndexedDB()
	if err != nil {
		cmd.Process.Kill()
		panic("connect: " + err.Error())
	}
	testClient = client

	code := m.Run()

	_ = client.Close()
	cmd.Process.Kill()
	_ = cmd.Wait()
	_ = os.Remove(sock)
	_ = os.Remove(bin)
	os.Exit(code)
}

func seedStore(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	store := "items_" + t.Name()

	err := testClient.CreateObjectStore(ctx, store, gestalt.ObjectStoreSchema{
		Indexes: []gestalt.IndexSchema{
			{Name: "by_status", KeyPath: []string{"status"}, Unique: false},
			{Name: "by_email", KeyPath: []string{"email"}, Unique: true},
		},
	})
	if err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}

	s := testClient.ObjectStore(store)
	for _, r := range []gestalt.Record{
		{"id": "a", "name": "Alice", "status": "active", "email": "alice@test.com"},
		{"id": "b", "name": "Bob", "status": "active", "email": "bob@test.com"},
		{"id": "c", "name": "Carol", "status": "inactive", "email": "carol@test.com"},
		{"id": "d", "name": "Dave", "status": "active", "email": "dave@test.com"},
	} {
		if err := s.Add(ctx, r); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	return store
}

func TestTransport_NestedJSON(t *testing.T) {
	ctx := context.Background()
	store := "nested_" + t.Name()
	_ = testClient.CreateObjectStore(ctx, store, gestalt.ObjectStoreSchema{})
	s := testClient.ObjectStore(store)

	rec := gestalt.Record{
		"id":   "r1",
		"meta": map[string]any{"role": "admin", "level": float64(5)},
		"tags": []any{"rust", "go"},
	}
	if err := s.Put(ctx, rec); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Get(ctx, "r1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	meta, ok := got["meta"].(map[string]any)
	if !ok {
		t.Fatalf("meta type = %T, want map[string]any", got["meta"])
	}
	if meta["role"] != "admin" {
		t.Errorf("meta.role = %v, want admin", meta["role"])
	}
	tags, ok := got["tags"].([]any)
	if !ok {
		t.Fatalf("tags type = %T, want []any", got["tags"])
	}
	if len(tags) != 2 || tags[0] != "rust" {
		t.Errorf("tags = %v, want [rust go]", tags)
	}
}

func TestTransport_CursorHappyPath(t *testing.T) {
	store := seedStore(t)
	s := testClient.ObjectStore(store)
	ctx := context.Background()

	cursor, err := s.OpenCursor(ctx, nil, gestalt.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer cursor.Close()

	var keys []string
	for cursor.Continue() {
		keys = append(keys, cursor.PrimaryKey())
	}
	if err := cursor.Err(); err != nil {
		t.Fatalf("Err: %v", err)
	}
	if len(keys) != 4 {
		t.Fatalf("got %d keys, want 4: %v", len(keys), keys)
	}
	for i := 1; i < len(keys); i++ {
		if keys[i] <= keys[i-1] {
			t.Errorf("keys not sorted: %v", keys)
			break
		}
	}
}

func TestTransport_EmptyCursor(t *testing.T) {
	ctx := context.Background()
	store := "empty_" + t.Name()
	_ = testClient.CreateObjectStore(ctx, store, gestalt.ObjectStoreSchema{})

	cursor, err := testClient.ObjectStore(store).OpenCursor(ctx, nil, gestalt.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer cursor.Close()

	if cursor.Continue() {
		t.Fatal("Continue returned true on empty store")
	}
}

func TestTransport_KeysOnlyCursor(t *testing.T) {
	store := seedStore(t)
	s := testClient.ObjectStore(store)
	ctx := context.Background()

	cursor, err := s.OpenKeyCursor(ctx, nil, gestalt.CursorNext)
	if err != nil {
		t.Fatalf("OpenKeyCursor: %v", err)
	}
	defer cursor.Close()

	if !cursor.Continue() {
		t.Fatal("Continue returned false")
	}
	_, err = cursor.Value()
	if !errors.Is(err, gestalt.ErrKeysOnly) {
		t.Errorf("Value() = %v, want ErrKeysOnly", err)
	}
}

func TestTransport_CursorExhaustion(t *testing.T) {
	store := seedStore(t)
	s := testClient.ObjectStore(store)
	ctx := context.Background()

	cursor, err := s.OpenCursor(ctx, nil, gestalt.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer cursor.Close()

	for cursor.Continue() {
	}
	_, err = cursor.Value()
	if !errors.Is(err, gestalt.ErrNotFound) {
		t.Errorf("Value after exhaustion = %v, want ErrNotFound", err)
	}
}

func TestTransport_ContinueToKeyBeyondEnd(t *testing.T) {
	store := seedStore(t)
	ctx := context.Background()
	cursor, err := testClient.ObjectStore(store).OpenCursor(ctx, nil, gestalt.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer cursor.Close()

	if cursor.ContinueToKey("zzz") {
		t.Fatal("ContinueToKey beyond end returned true")
	}
	if cursor.Err() != nil {
		t.Fatalf("Err: %v", cursor.Err())
	}
}

func TestTransport_AdvancePastEnd(t *testing.T) {
	store := seedStore(t)
	ctx := context.Background()
	cursor, err := testClient.ObjectStore(store).OpenCursor(ctx, nil, gestalt.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer cursor.Close()

	if cursor.Advance(100) {
		t.Fatal("Advance(100) returned true")
	}
	if cursor.Err() != nil {
		t.Fatalf("Err: %v", cursor.Err())
	}
}

func TestTransport_AdvanceOnFreshCursorSkipsRequestedRows(t *testing.T) {
	store := seedStore(t)
	ctx := context.Background()
	cursor, err := testClient.ObjectStore(store).OpenCursor(ctx, nil, gestalt.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer cursor.Close()

	if !cursor.Advance(1) {
		t.Fatalf("Advance(1) returned false err=%v", cursor.Err())
	}
	if cursor.PrimaryKey() != "b" {
		t.Fatalf("PrimaryKey after Advance(1) = %q, want b", cursor.PrimaryKey())
	}
}

func TestTransport_AdvanceRejectsNonPositiveCounts(t *testing.T) {
	store := seedStore(t)
	ctx := context.Background()
	cursor, err := testClient.ObjectStore(store).OpenCursor(ctx, nil, gestalt.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer cursor.Close()

	if cursor.Advance(0) {
		t.Fatal("Advance(0) returned true")
	}
	if cursor.Err() == nil {
		t.Fatal("Err() = nil, want invalid argument")
	}
	st, ok := status.FromError(cursor.Err())
	if !ok {
		t.Fatalf("Err() = %T, want gRPC status", cursor.Err())
	}
	if st.Code() != codes.InvalidArgument {
		t.Fatalf("Err() code = %v, want %v", st.Code(), codes.InvalidArgument)
	}
}

func TestTransport_PostExhaustion(t *testing.T) {
	store := seedStore(t)
	ctx := context.Background()
	cursor, err := testClient.ObjectStore(store).OpenCursor(ctx, nil, gestalt.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer cursor.Close()

	for cursor.Continue() {
	}
	if cursor.Continue() {
		t.Fatal("Continue after exhaustion returned true")
	}
	if err := cursor.Delete(); !errors.Is(err, gestalt.ErrNotFound) {
		t.Errorf("Delete after exhaustion = %v, want ErrNotFound", err)
	}
}

func TestTransport_IndexCursor(t *testing.T) {
	store := seedStore(t)
	ctx := context.Background()
	cursor, err := testClient.ObjectStore(store).Index("by_status").OpenCursor(ctx, nil, gestalt.CursorNext, "active")
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer cursor.Close()

	count := 0
	for cursor.Continue() {
		rec, err := cursor.Value()
		if err != nil {
			t.Fatalf("Value: %v", err)
		}
		if rec["status"] != "active" {
			t.Errorf("status = %v, want active", rec["status"])
		}
		count++
	}
	if count != 3 {
		t.Errorf("got %d active records, want 3", count)
	}
}

func TestTransport_IndexContinueToKeyRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := "index_seek_" + t.Name()

	err := testClient.CreateObjectStore(ctx, store, gestalt.ObjectStoreSchema{
		Indexes: []gestalt.IndexSchema{{Name: "by_num", KeyPath: []string{"n"}}},
	})
	if err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}

	s := testClient.ObjectStore(store)
	for _, r := range []gestalt.Record{
		{"id": "a", "n": 1},
		{"id": "b", "n": 2},
		{"id": "c", "n": 3},
	} {
		if err := s.Add(ctx, r); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}

	cursor, err := s.Index("by_num").OpenCursor(ctx, nil, gestalt.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer cursor.Close()

	if !cursor.Continue() {
		t.Fatal("Continue returned false")
	}

	key, ok := cursor.Key().([]any)
	if !ok {
		t.Fatalf("Key() type = %T, want []any", cursor.Key())
	}
	if len(key) != 1 || key[0] != int64(1) {
		t.Fatalf("Key() = %#v, want []any{int64(1)}", key)
	}

	if !cursor.ContinueToKey(cursor.Key()) {
		t.Fatalf("ContinueToKey returned false err=%v", cursor.Err())
	}
	if cursor.PrimaryKey() != "b" {
		t.Fatalf("PrimaryKey = %q, want b", cursor.PrimaryKey())
	}
}

func TestTransport_IndexCursorOrdersBinaryKeysBytewise(t *testing.T) {
	ctx := context.Background()
	store := "index_binary_" + t.Name()

	err := testClient.CreateObjectStore(ctx, store, gestalt.ObjectStoreSchema{
		Indexes: []gestalt.IndexSchema{{Name: "by_blob", KeyPath: []string{"blob"}}},
	})
	if err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}

	s := testClient.ObjectStore(store)
	for _, r := range []gestalt.Record{
		{"id": "a", "blob": []byte{10}},
		{"id": "b", "blob": []byte{2}},
		{"id": "c", "blob": []byte{2, 0}},
	} {
		if err := s.Add(ctx, r); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}

	cursor, err := s.Index("by_blob").OpenCursor(ctx, nil, gestalt.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer cursor.Close()

	var keys []string
	for cursor.Continue() {
		keys = append(keys, cursor.PrimaryKey())
	}
	if err := cursor.Err(); err != nil {
		t.Fatalf("Err: %v", err)
	}
	want := []string{"b", "c", "a"}
	if len(keys) != len(want) {
		t.Fatalf("got %d keys, want %d: %v", len(keys), len(want), keys)
	}
	for i, key := range want {
		if keys[i] != key {
			t.Fatalf("keys[%d] = %q, want %q (full order %v)", i, keys[i], key, keys)
		}
	}
}

func TestTransport_CursorUpdateAcknowledgesMutation(t *testing.T) {
	ctx := context.Background()
	store := "update_ack_" + t.Name()
	_ = testClient.CreateObjectStore(ctx, store, gestalt.ObjectStoreSchema{})
	s := testClient.ObjectStore(store)

	if err := s.Put(ctx, gestalt.Record{"id": "u1", "status": "active"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	cursor, err := s.OpenCursor(ctx, nil, gestalt.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer cursor.Close()

	if !cursor.Continue() {
		t.Fatal("Continue returned false")
	}
	updated := gestalt.Record{"id": "u1", "status": "inactive"}
	if err := cursor.Update(updated); err != nil {
		t.Fatalf("Update: %v", err)
	}
	curRec, err := cursor.Value()
	if err != nil {
		t.Fatalf("Value after Update: %v", err)
	}
	if curRec["status"] != "inactive" {
		t.Fatalf("cursor.Value().status = %v, want inactive", curRec["status"])
	}
	got, err := s.Get(ctx, "u1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got["status"] != "inactive" {
		t.Fatalf("Get status = %v, want inactive", got["status"])
	}
}

func TestTransport_ErrorMapping(t *testing.T) {
	ctx := context.Background()
	store := "errmap_" + t.Name()
	_ = testClient.CreateObjectStore(ctx, store, gestalt.ObjectStoreSchema{})
	s := testClient.ObjectStore(store)

	_, err := s.Get(ctx, "nonexistent")
	if !errors.Is(err, gestalt.ErrNotFound) {
		t.Errorf("Get missing = %v, want ErrNotFound", err)
	}

	_ = s.Add(ctx, gestalt.Record{"id": "x"})
	err = s.Add(ctx, gestalt.Record{"id": "x"})
	if !errors.Is(err, gestalt.ErrAlreadyExists) {
		t.Errorf("duplicate Add = %v, want ErrAlreadyExists", err)
	}
}
