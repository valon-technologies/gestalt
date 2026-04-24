package gestalt_test

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

var (
	testClient      *gestalt.IndexedDBClient
	testCacheClient *gestalt.CacheClient
	testS3Client    *gestalt.S3Client
)

func TestMain(m *testing.M) {
	if runtime.GOOS == "windows" {
		os.Exit(0)
	}

	idbBin, idbSock, idbCmd := buildAndStartHarness("indexeddbtransportd", "go-sdk-idb-test.sock")
	cacheBin, cacheSock, cacheCmd := buildAndStartHarness("cachetransportd", "go-sdk-cache-test.sock")
	s3Bin, s3Sock, s3Cmd := buildAndStartHarness("s3transportd", "go-sdk-s3-test.sock")

	os.Setenv(gestalt.EnvIndexedDBSocket, idbSock)
	os.Setenv(gestalt.IndexedDBSocketEnv("test"), "unix://"+idbSock)
	client, err := gestalt.IndexedDB()
	if err != nil {
		_ = idbCmd.Process.Kill()
		_ = cacheCmd.Process.Kill()
		_ = s3Cmd.Process.Kill()
		panic("connect: " + err.Error())
	}
	testClient = client

	os.Setenv(gestalt.EnvCacheSocket, cacheSock)
	os.Setenv(gestalt.CacheSocketEnv("test"), cacheSock)
	cacheClient, err := gestalt.Cache()
	if err != nil {
		_ = client.Close()
		_ = idbCmd.Process.Kill()
		_ = cacheCmd.Process.Kill()
		_ = s3Cmd.Process.Kill()
		panic("connect cache: " + err.Error())
	}
	testCacheClient = cacheClient

	os.Setenv(gestalt.EnvS3Socket, s3Sock)
	os.Setenv(gestalt.S3SocketEnv("test"), s3Sock)
	s3Client, err := gestalt.S3()
	if err != nil {
		_ = client.Close()
		_ = cacheClient.Close()
		_ = idbCmd.Process.Kill()
		_ = cacheCmd.Process.Kill()
		_ = s3Cmd.Process.Kill()
		panic("connect s3: " + err.Error())
	}
	testS3Client = s3Client

	code := m.Run()

	_ = client.Close()
	_ = cacheClient.Close()
	_ = s3Client.Close()
	_ = idbCmd.Process.Kill()
	_ = cacheCmd.Process.Kill()
	_ = s3Cmd.Process.Kill()
	_ = idbCmd.Wait()
	_ = cacheCmd.Wait()
	_ = s3Cmd.Wait()
	_ = os.Remove(idbSock)
	_ = os.Remove(cacheSock)
	_ = os.Remove(s3Sock)
	_ = os.Remove(idbBin)
	_ = os.Remove(cacheBin)
	_ = os.Remove(s3Bin)
	os.Exit(code)
}

func buildAndStartHarness(binaryName, socketName string) (string, string, *exec.Cmd) {
	harnessDir := filepath.Join("..", "..", "gestaltd", "internal", "testutil", "cmd", binaryName)
	bin := filepath.Join(os.TempDir(), binaryName)
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = harnessDir
	if out, err := build.CombinedOutput(); err != nil {
		panic("build harness: " + string(out))
	}

	sock := filepath.Join(os.TempDir(), socketName)
	_ = os.Remove(sock)

	cmd := exec.Command(bin, "--socket", sock)
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		panic("start harness: " + err.Error())
	}

	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "READY" {
		_ = cmd.Process.Kill()
		panic("harness did not print READY")
	}
	return bin, sock, cmd
}

func buildAndStartTCPHarness(binaryName, expectToken string) (string, string, *exec.Cmd) {
	harnessDir := filepath.Join("..", "..", "gestaltd", "internal", "testutil", "cmd", binaryName)
	bin := filepath.Join(os.TempDir(), binaryName+"-tcp")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = harnessDir
	if out, err := build.CombinedOutput(); err != nil {
		panic("build harness: " + string(out))
	}

	address := reserveTCPAddress()
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		panic("split tcp address: " + err.Error())
	}
	args := []string{"--tcp", net.JoinHostPort(host, port)}
	if expectToken != "" {
		args = append(args, "--expect-token", expectToken)
	}
	cmd := exec.Command(bin, args...)
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		panic("start tcp harness: " + err.Error())
	}

	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "READY" {
		_ = cmd.Process.Kill()
		panic("tcp harness did not print READY")
	}
	return bin, "tcp://" + address, cmd
}

func reserveTCPAddress() string {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic("reserve tcp address: " + err.Error())
	}
	defer func() { _ = lis.Close() }()
	return lis.Addr().String()
}

func TestTransport_NamedSocketEnv(t *testing.T) {
	client, err := gestalt.IndexedDB("test")
	if err != nil {
		t.Fatalf("connect named indexeddb: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	store := "named_socket_" + t.Name()
	if err := client.CreateObjectStore(ctx, store, gestalt.ObjectStoreSchema{}); err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}
	s := client.ObjectStore(store)
	if err := s.Put(ctx, gestalt.Record{"id": "named", "value": "ok"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Get(ctx, "named")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got["value"] != "ok" {
		t.Fatalf("value = %v, want ok", got["value"])
	}
}

func TestTransport_TCPTargetEnv(t *testing.T) {
	bin, target, cmd := buildAndStartTCPHarness("indexeddbtransportd", "")
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = os.Remove(bin)
	})

	t.Setenv(gestalt.EnvIndexedDBSocket, target)
	client, err := gestalt.IndexedDB()
	if err != nil {
		t.Fatalf("connect tcp indexeddb: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	store := "tcp_target_" + t.Name()
	if err := client.CreateObjectStore(ctx, store, gestalt.ObjectStoreSchema{}); err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}
	if err := client.ObjectStore(store).Put(ctx, gestalt.Record{"id": "tcp", "value": "ok"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := client.ObjectStore(store).Get(ctx, "tcp")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got["value"] != "ok" {
		t.Fatalf("value = %v, want ok", got["value"])
	}
}

func TestTransport_TCPTargetTokenEnv(t *testing.T) {
	const token = "relay-token-go"
	bin, target, cmd := buildAndStartTCPHarness("indexeddbtransportd", token)
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = os.Remove(bin)
	})

	t.Setenv(gestalt.EnvIndexedDBSocket, target)
	t.Setenv(gestalt.IndexedDBSocketTokenEnv(""), token)
	client, err := gestalt.IndexedDB()
	if err != nil {
		t.Fatalf("connect tcp indexeddb with token: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	store := "tcp_target_token_" + t.Name()
	if err := client.CreateObjectStore(ctx, store, gestalt.ObjectStoreSchema{}); err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}
	if err := client.ObjectStore(store).Put(ctx, gestalt.Record{"id": "tcp", "value": "token"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := client.ObjectStore(store).Get(ctx, "tcp")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got["value"] != "token" {
		t.Fatalf("value = %v, want token", got["value"])
	}
}

type indexedDBSchemaTransportHarness struct {
	proto.UnimplementedIndexedDBServer

	mu       sync.Mutex
	requests []*proto.CreateObjectStoreRequest
}

func (h *indexedDBSchemaTransportHarness) CreateObjectStore(_ context.Context, req *proto.CreateObjectStoreRequest) (*emptypb.Empty, error) {
	h.mu.Lock()
	h.requests = append(h.requests, gproto.Clone(req).(*proto.CreateObjectStoreRequest))
	h.mu.Unlock()
	return &emptypb.Empty{}, nil
}

func TestTransport_CreateObjectStorePreservesColumns(t *testing.T) {
	address := reserveTCPAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &indexedDBSchemaTransportHarness{}
	srv := grpc.NewServer()
	proto.RegisterIndexedDBServer(srv, harness)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvIndexedDBSocket, "tcp://"+address)
	client, err := gestalt.IndexedDB()
	if err != nil {
		t.Fatalf("IndexedDB: %v", err)
	}
	defer func() { _ = client.Close() }()

	err = client.CreateObjectStore(context.Background(), "agent_session_metadata", gestalt.ObjectStoreSchema{
		Indexes: []gestalt.IndexSchema{
			{Name: "by_subject", KeyPath: []string{"subject_id"}},
			{Name: "by_subject_session", KeyPath: []string{"subject_id", "session_id"}, Unique: true},
		},
		Columns: []gestalt.ColumnDef{
			{Name: "id", Type: gestalt.TypeString, PrimaryKey: true},
			{Name: "subject_id", Type: gestalt.TypeString, NotNull: true},
			{Name: "session_id", Type: gestalt.TypeString, NotNull: true, Unique: true},
			{Name: "metadata_json", Type: gestalt.TypeJSON},
		},
	})
	if err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.requests) != 1 {
		t.Fatalf("create object store requests len = %d, want 1", len(harness.requests))
	}
	req := harness.requests[0]
	if req.GetName() != "agent_session_metadata" {
		t.Fatalf("request name = %q, want agent_session_metadata", req.GetName())
	}
	if len(req.GetSchema().GetIndexes()) != 2 {
		t.Fatalf("index count = %d, want 2", len(req.GetSchema().GetIndexes()))
	}
	if len(req.GetSchema().GetColumns()) != 4 {
		t.Fatalf("column count = %d, want 4", len(req.GetSchema().GetColumns()))
	}
	gotID := req.GetSchema().GetColumns()[0]
	if gotID.GetName() != "id" || gotID.GetType() != int32(gestalt.TypeString) || !gotID.GetPrimaryKey() {
		t.Fatalf("id column = %#v, want primary string id column", gotID)
	}
	gotSession := req.GetSchema().GetColumns()[2]
	if gotSession.GetName() != "session_id" || !gotSession.GetNotNull() || !gotSession.GetUnique() {
		t.Fatalf("session column = %#v, want non-null unique session_id column", gotSession)
	}
	gotMetadata := req.GetSchema().GetColumns()[3]
	if gotMetadata.GetName() != "metadata_json" || gotMetadata.GetType() != int32(gestalt.TypeJSON) {
		t.Fatalf("metadata column = %#v, want json metadata_json column", gotMetadata)
	}
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
