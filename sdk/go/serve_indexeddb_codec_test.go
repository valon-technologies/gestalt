package gestalt

import (
	"context"
	"net"
	"reflect"
	"testing"

	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestIndexedDBProviderNativeCodecTransport(t *testing.T) {
	t.Parallel()

	provider := &codecTransportProvider{
		indexRecords: []Record{{
			"id":    "r1",
			"blob":  []byte{1, 2, 3},
			"count": int64(2),
			"json":  map[string]any{"ok": true},
		}},
		cursor: &codecTransportCursor{entries: []*IndexedDBCursorEntry{{
			Key:        []any{[]any{"x", "y"}},
			PrimaryKey: "r1",
			Record:     Record{"id": "r1", "status": "active"},
		}}},
	}
	client, closeClient := newIndexedDBCodecBufconnClient(t, provider)
	defer closeClient()
	ctx := context.Background()

	pbRecord, err := recordToProto(Record{
		"id":     "r1",
		"blob":   []byte{9, 8},
		"count":  int64(4),
		"active": true,
	})
	if err != nil {
		t.Fatalf("recordToProto: %v", err)
	}
	if _, err := client.Put(ctx, &proto.RecordRequest{Store: "records", Record: pbRecord}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	wantPut := Record{"id": "r1", "blob": []byte{9, 8}, "count": int64(4), "active": true}
	if !reflect.DeepEqual(provider.putReq.Record, wantPut) {
		t.Fatalf("provider Put record = %#v, want %#v", provider.putReq.Record, wantPut)
	}

	values, err := typedValuesFromAny([]any{"active", int64(2)})
	if err != nil {
		t.Fatalf("typedValuesFromAny: %v", err)
	}
	lower, err := typedValueFromAny([]any{"a", float64(1)})
	if err != nil {
		t.Fatalf("typedValueFromAny lower: %v", err)
	}
	upper, err := typedValueFromAny("z")
	if err != nil {
		t.Fatalf("typedValueFromAny upper: %v", err)
	}
	indexResp, err := client.IndexGetAll(ctx, &proto.IndexQueryRequest{
		Store:  "records",
		Index:  "by_status",
		Values: values,
		Range: &proto.KeyRange{
			Lower:     lower,
			Upper:     upper,
			LowerOpen: true,
		},
	})
	if err != nil {
		t.Fatalf("IndexGetAll: %v", err)
	}
	wantIndexValues := []any{"active", int64(2)}
	if !reflect.DeepEqual(provider.indexReq.Values, wantIndexValues) {
		t.Fatalf("provider index values = %#v, want %#v", provider.indexReq.Values, wantIndexValues)
	}
	wantRange := &KeyRange{Lower: []any{"a", float64(1)}, Upper: "z", LowerOpen: true}
	if !reflect.DeepEqual(provider.indexReq.Range, wantRange) {
		t.Fatalf("provider index range = %#v, want %#v", provider.indexReq.Range, wantRange)
	}
	indexRecords, err := recordsFromProto(indexResp.GetRecords())
	if err != nil {
		t.Fatalf("recordsFromProto: %v", err)
	}
	if !reflect.DeepEqual(indexRecords, provider.indexRecords) {
		t.Fatalf("IndexGetAll records = %#v, want %#v", indexRecords, provider.indexRecords)
	}

	for _, tt := range []struct {
		name  string
		lower *proto.TypedValue
	}{
		{
			name:  "missing kind",
			lower: &proto.TypedValue{},
		},
		{
			name:  "null",
			lower: &proto.TypedValue{Kind: &proto.TypedValue_NullValue{NullValue: structpb.NullValue_NULL_VALUE}},
		},
		{
			name: "invalid timestamp",
			lower: &proto.TypedValue{
				Kind: &proto.TypedValue_TimeValue{TimeValue: &timestamppb.Timestamp{Seconds: 253402300800}},
			},
		},
	} {
		_, err = client.GetAll(ctx, &proto.ObjectStoreRangeRequest{
			Store: "records",
			Range: &proto.KeyRange{Lower: tt.lower},
		})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("GetAll malformed range %s error = %v, want InvalidArgument", tt.name, err)
		}
	}

	txStream, err := client.Transaction(ctx)
	if err != nil {
		t.Fatalf("Transaction stream: %v", err)
	}
	if err := txStream.Send(&proto.TransactionClientMessage{Msg: &proto.TransactionClientMessage_Begin{Begin: &proto.BeginTransactionRequest{
		Stores: []string{"records"},
		Mode:   proto.TransactionMode_TRANSACTION_READWRITE,
	}}}); err != nil {
		t.Fatalf("send transaction begin: %v", err)
	}
	beginResp, err := txStream.Recv()
	if err != nil {
		t.Fatalf("recv transaction begin: %v", err)
	}
	if beginResp.GetBegin() == nil {
		t.Fatalf("transaction begin response = %#v, want begin", beginResp)
	}
	if err := txStream.Send(&proto.TransactionClientMessage{Msg: &proto.TransactionClientMessage_Operation{Operation: &proto.TransactionOperation{
		RequestId: 1,
		Operation: &proto.TransactionOperation_Count{Count: &proto.ObjectStoreRangeRequest{
			Store: "records",
			Range: &proto.KeyRange{Lower: &proto.TypedValue{}},
		}},
	}}}); err != nil {
		t.Fatalf("send transaction malformed range count: %v", err)
	}
	if err := txStream.CloseSend(); err != nil {
		t.Fatalf("close transaction send: %v", err)
	}
	opResp, err := txStream.Recv()
	if err != nil {
		t.Fatalf("recv transaction operation error: %v", err)
	}
	if got := codes.Code(opResp.GetOperation().GetError().GetCode()); got != codes.InvalidArgument {
		t.Fatalf("transaction malformed range code = %v, want InvalidArgument; response=%#v", got, opResp)
	}
	abortResp, err := txStream.Recv()
	if err != nil {
		t.Fatalf("recv transaction abort: %v", err)
	}
	if abortResp.GetAbort() == nil {
		t.Fatalf("transaction abort response = %#v, want abort", abortResp)
	}

	stream, err := client.OpenCursor(ctx)
	if err != nil {
		t.Fatalf("OpenCursor stream: %v", err)
	}
	if err := stream.Send(&proto.CursorClientMessage{Msg: &proto.CursorClientMessage_Open{Open: &proto.OpenCursorRequest{
		Store: "records",
		Index: "by_compound",
	}}}); err != nil {
		t.Fatalf("send open cursor: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("recv cursor open ack: %v", err)
	}
	if err := stream.Send(&proto.CursorClientMessage{Msg: &proto.CursorClientMessage_Command{Command: &proto.CursorCommand{
		Command: &proto.CursorCommand_Next{Next: true},
	}}}); err != nil {
		t.Fatalf("send cursor next: %v", err)
	}
	cursorResp, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv cursor entry: %v", err)
	}
	entry := cursorResp.GetEntry()
	if entry == nil {
		t.Fatalf("cursor response = %#v, want entry", cursorResp)
	}
	gotKey, err := keyValuesToAny(entry.GetKey())
	if err != nil {
		t.Fatalf("keyValuesToAny: %v", err)
	}
	wantKey := []any{[]any{"x", "y"}}
	if !reflect.DeepEqual(gotKey, wantKey) {
		t.Fatalf("cursor key = %#v, want %#v", gotKey, wantKey)
	}
}

func newIndexedDBCodecBufconnClient(t *testing.T, provider IndexedDBProvider) (proto.IndexedDBClient, func()) {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	proto.RegisterIndexedDBServer(srv, indexedDBProviderServer{provider: provider})
	go func() { _ = srv.Serve(lis) }()

	conn, err := grpc.NewClient(
		"passthrough:///bufconn",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}

	return proto.NewIndexedDBClient(conn), func() {
		_ = conn.Close()
		srv.Stop()
		_ = lis.Close()
	}
}

type codecTransportProvider struct {
	putReq       IndexedDBRecordRequest
	indexReq     IndexedDBIndexQueryRequest
	openReq      IndexedDBOpenCursorRequest
	indexRecords []Record
	cursor       IndexedDBCursor
}

func (p *codecTransportProvider) Configure(context.Context, string, map[string]any) error { return nil }
func (p *codecTransportProvider) CreateObjectStore(context.Context, string, ObjectStoreSchema) error {
	return nil
}
func (p *codecTransportProvider) DeleteObjectStore(context.Context, string) error { return nil }
func (p *codecTransportProvider) Get(context.Context, IndexedDBObjectStoreRequest) (Record, error) {
	return nil, ErrNotFound
}
func (p *codecTransportProvider) GetKey(context.Context, IndexedDBObjectStoreRequest) (string, error) {
	return "", ErrNotFound
}
func (p *codecTransportProvider) Add(_ context.Context, req IndexedDBRecordRequest) error {
	p.putReq = req
	return nil
}
func (p *codecTransportProvider) Put(_ context.Context, req IndexedDBRecordRequest) error {
	p.putReq = req
	return nil
}
func (p *codecTransportProvider) Delete(context.Context, IndexedDBObjectStoreRequest) error {
	return nil
}
func (p *codecTransportProvider) Clear(context.Context, string) error { return nil }
func (p *codecTransportProvider) GetAll(context.Context, IndexedDBObjectStoreRangeRequest) ([]Record, error) {
	return nil, nil
}
func (p *codecTransportProvider) GetAllKeys(context.Context, IndexedDBObjectStoreRangeRequest) ([]string, error) {
	return nil, nil
}
func (p *codecTransportProvider) Count(context.Context, IndexedDBObjectStoreRangeRequest) (int64, error) {
	return 0, nil
}
func (p *codecTransportProvider) DeleteRange(context.Context, IndexedDBObjectStoreRangeRequest) (int64, error) {
	return 0, nil
}
func (p *codecTransportProvider) IndexGet(context.Context, IndexedDBIndexQueryRequest) (Record, error) {
	return nil, ErrNotFound
}
func (p *codecTransportProvider) IndexGetKey(context.Context, IndexedDBIndexQueryRequest) (string, error) {
	return "", ErrNotFound
}
func (p *codecTransportProvider) IndexGetAll(_ context.Context, req IndexedDBIndexQueryRequest) ([]Record, error) {
	p.indexReq = req
	return p.indexRecords, nil
}
func (p *codecTransportProvider) IndexGetAllKeys(context.Context, IndexedDBIndexQueryRequest) ([]string, error) {
	return nil, nil
}
func (p *codecTransportProvider) IndexCount(context.Context, IndexedDBIndexQueryRequest) (int64, error) {
	return 0, nil
}
func (p *codecTransportProvider) IndexDelete(context.Context, IndexedDBIndexQueryRequest) (int64, error) {
	return 0, nil
}
func (p *codecTransportProvider) OpenCursor(_ context.Context, req IndexedDBOpenCursorRequest) (IndexedDBCursor, error) {
	p.openReq = req
	return p.cursor, nil
}
func (p *codecTransportProvider) BeginTransaction(context.Context, IndexedDBBeginTransactionRequest) (IndexedDBTransaction, error) {
	return &codecTransportTransaction{}, nil
}

type codecTransportCursor struct {
	entries []*IndexedDBCursorEntry
	offset  int
}

func (c *codecTransportCursor) Next(context.Context) (*IndexedDBCursorEntry, error) {
	if c.offset >= len(c.entries) {
		return nil, nil
	}
	entry := c.entries[c.offset]
	c.offset++
	return entry, nil
}

func (c *codecTransportCursor) ContinueToKey(context.Context, any) (*IndexedDBCursorEntry, error) {
	return c.Next(context.Background())
}

func (c *codecTransportCursor) Advance(_ context.Context, count int) (*IndexedDBCursorEntry, error) {
	for count > 1 {
		if _, err := c.Next(context.Background()); err != nil {
			return nil, err
		}
		count--
	}
	return c.Next(context.Background())
}

func (c *codecTransportCursor) Delete(context.Context) error { return nil }

func (c *codecTransportCursor) Update(_ context.Context, record Record) (*IndexedDBCursorEntry, error) {
	return &IndexedDBCursorEntry{Key: []any{[]any{"x", "y"}}, PrimaryKey: "r1", Record: record}, nil
}

func (c *codecTransportCursor) Close() error { return nil }

type codecTransportTransaction struct{}

func (tx *codecTransportTransaction) Commit(context.Context) error { return nil }
func (tx *codecTransportTransaction) Abort(context.Context) error  { return nil }
func (tx *codecTransportTransaction) Get(context.Context, IndexedDBObjectStoreRequest) (Record, error) {
	return nil, ErrNotFound
}
func (tx *codecTransportTransaction) GetKey(context.Context, IndexedDBObjectStoreRequest) (string, error) {
	return "", ErrNotFound
}
func (tx *codecTransportTransaction) Add(context.Context, IndexedDBRecordRequest) error { return nil }
func (tx *codecTransportTransaction) Put(context.Context, IndexedDBRecordRequest) error { return nil }
func (tx *codecTransportTransaction) Delete(context.Context, IndexedDBObjectStoreRequest) error {
	return nil
}
func (tx *codecTransportTransaction) Clear(context.Context, string) error { return nil }
func (tx *codecTransportTransaction) GetAll(context.Context, IndexedDBObjectStoreRangeRequest) ([]Record, error) {
	return nil, nil
}
func (tx *codecTransportTransaction) GetAllKeys(context.Context, IndexedDBObjectStoreRangeRequest) ([]string, error) {
	return nil, nil
}
func (tx *codecTransportTransaction) Count(context.Context, IndexedDBObjectStoreRangeRequest) (int64, error) {
	return 0, nil
}
func (tx *codecTransportTransaction) DeleteRange(context.Context, IndexedDBObjectStoreRangeRequest) (int64, error) {
	return 0, nil
}
func (tx *codecTransportTransaction) IndexGet(context.Context, IndexedDBIndexQueryRequest) (Record, error) {
	return nil, ErrNotFound
}
func (tx *codecTransportTransaction) IndexGetKey(context.Context, IndexedDBIndexQueryRequest) (string, error) {
	return "", ErrNotFound
}
func (tx *codecTransportTransaction) IndexGetAll(context.Context, IndexedDBIndexQueryRequest) ([]Record, error) {
	return nil, nil
}
func (tx *codecTransportTransaction) IndexGetAllKeys(context.Context, IndexedDBIndexQueryRequest) ([]string, error) {
	return nil, nil
}
func (tx *codecTransportTransaction) IndexCount(context.Context, IndexedDBIndexQueryRequest) (int64, error) {
	return 0, nil
}
func (tx *codecTransportTransaction) IndexDelete(context.Context, IndexedDBIndexQueryRequest) (int64, error) {
	return 0, nil
}
