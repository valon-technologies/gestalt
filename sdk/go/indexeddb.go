package gestalt

import (
	"context"
	"fmt"
	"os"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

const EnvIndexedDBSocket = "GESTALT_INDEXEDDB_SOCKET"

var (
	ErrNotFound      = fmt.Errorf("indexeddb: not found")
	ErrAlreadyExists = fmt.Errorf("indexeddb: already exists")
	ErrKeysOnly      = fmt.Errorf("indexeddb: value not available on key-only cursor")
)

type CursorDirection string

const (
	CursorNext       CursorDirection = "next"
	CursorNextUnique CursorDirection = "nextunique"
	CursorPrev       CursorDirection = "prev"
	CursorPrevUnique CursorDirection = "prevunique"
)

type Record = map[string]any

type KeyRange struct {
	Lower     any
	Upper     any
	LowerOpen bool
	UpperOpen bool
}

type IndexSchema struct {
	Name    string
	KeyPath []string
	Unique  bool
}

type ObjectStoreSchema struct {
	Indexes []IndexSchema
}

type IndexedDBClient struct {
	client proto.IndexedDBClient
	conn   *grpc.ClientConn
}

func IndexedDB() (*IndexedDBClient, error) {
	socketPath := os.Getenv(EnvIndexedDBSocket)
	if socketPath == "" {
		return nil, fmt.Errorf("indexeddb: %s is not set", EnvIndexedDBSocket)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, "unix:"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("indexeddb: connect to host: %w", err)
	}
	return &IndexedDBClient{
		client: proto.NewIndexedDBClient(conn),
		conn:   conn,
	}, nil
}

func (db *IndexedDBClient) Close() error {
	return db.conn.Close()
}

func (db *IndexedDBClient) CreateObjectStore(ctx context.Context, name string, schema ObjectStoreSchema) error {
	indexes := make([]*proto.IndexSchema, len(schema.Indexes))
	for i, idx := range schema.Indexes {
		indexes[i] = &proto.IndexSchema{Name: idx.Name, KeyPath: idx.KeyPath, Unique: idx.Unique}
	}
	_, err := db.client.CreateObjectStore(ctx, &proto.CreateObjectStoreRequest{
		Name: name, Schema: &proto.ObjectStoreSchema{Indexes: indexes},
	})
	return grpcErr(err)
}

func (db *IndexedDBClient) DeleteObjectStore(ctx context.Context, name string) error {
	_, err := db.client.DeleteObjectStore(ctx, &proto.DeleteObjectStoreRequest{Name: name})
	return grpcErr(err)
}

func (db *IndexedDBClient) ObjectStore(name string) *ObjectStoreClient {
	return &ObjectStoreClient{client: db.client, store: name}
}

type ObjectStoreClient struct {
	client proto.IndexedDBClient
	store  string
}

func (o *ObjectStoreClient) Get(ctx context.Context, id string) (Record, error) {
	resp, err := o.client.Get(ctx, &proto.ObjectStoreRequest{Store: o.store, Id: id})
	if err != nil {
		return nil, grpcErr(err)
	}
	record, err := RecordFromProto(resp.GetRecord())
	if err != nil {
		return nil, fmt.Errorf("unmarshal record: %w", err)
	}
	return record, nil
}

func (o *ObjectStoreClient) GetKey(ctx context.Context, id string) (string, error) {
	resp, err := o.client.GetKey(ctx, &proto.ObjectStoreRequest{Store: o.store, Id: id})
	if err != nil {
		return "", grpcErr(err)
	}
	return resp.GetKey(), nil
}

func (o *ObjectStoreClient) Add(ctx context.Context, record Record) error {
	pbRecord, err := RecordToProto(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = o.client.Add(ctx, &proto.RecordRequest{Store: o.store, Record: pbRecord})
	return grpcErr(err)
}

func (o *ObjectStoreClient) Put(ctx context.Context, record Record) error {
	pbRecord, err := RecordToProto(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = o.client.Put(ctx, &proto.RecordRequest{Store: o.store, Record: pbRecord})
	return grpcErr(err)
}

func (o *ObjectStoreClient) Delete(ctx context.Context, id string) error {
	_, err := o.client.Delete(ctx, &proto.ObjectStoreRequest{Store: o.store, Id: id})
	return grpcErr(err)
}

func (o *ObjectStoreClient) Clear(ctx context.Context) error {
	_, err := o.client.Clear(ctx, &proto.ObjectStoreNameRequest{Store: o.store})
	return grpcErr(err)
}

func (o *ObjectStoreClient) GetAll(ctx context.Context, r *KeyRange) ([]Record, error) {
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := o.client.GetAll(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return nil, grpcErr(err)
	}
	records, err := RecordsFromProto(resp.GetRecords())
	if err != nil {
		return nil, fmt.Errorf("unmarshal records: %w", err)
	}
	return records, nil
}

func (o *ObjectStoreClient) GetAllKeys(ctx context.Context, r *KeyRange) ([]string, error) {
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := o.client.GetAllKeys(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return nil, grpcErr(err)
	}
	return resp.GetKeys(), nil
}

func (o *ObjectStoreClient) Count(ctx context.Context, r *KeyRange) (int64, error) {
	kr, err := krToProto(r)
	if err != nil {
		return 0, err
	}
	resp, err := o.client.Count(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return 0, grpcErr(err)
	}
	return resp.GetCount(), nil
}

func (o *ObjectStoreClient) DeleteRange(ctx context.Context, r KeyRange) (int64, error) {
	kr, err := krToProto(&r)
	if err != nil {
		return 0, err
	}
	resp, err := o.client.DeleteRange(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return 0, grpcErr(err)
	}
	return resp.GetDeleted(), nil
}

func (o *ObjectStoreClient) OpenCursor(ctx context.Context, r *KeyRange, dir CursorDirection) (*Cursor, error) {
	return openCursor(ctx, o.client, o.store, "", r, dir, false, nil)
}

func (o *ObjectStoreClient) OpenKeyCursor(ctx context.Context, r *KeyRange, dir CursorDirection) (*Cursor, error) {
	return openCursor(ctx, o.client, o.store, "", r, dir, true, nil)
}

func (o *ObjectStoreClient) Index(name string) *IndexClient {
	return &IndexClient{client: o.client, store: o.store, index: name}
}

type IndexClient struct {
	client proto.IndexedDBClient
	store  string
	index  string
}

func (idx *IndexClient) Get(ctx context.Context, values ...any) (Record, error) {
	vals, err := anyToProtoValues(values)
	if err != nil {
		return nil, err
	}
	resp, err := idx.client.IndexGet(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: vals,
	})
	if err != nil {
		return nil, grpcErr(err)
	}
	record, err := RecordFromProto(resp.GetRecord())
	if err != nil {
		return nil, fmt.Errorf("unmarshal record: %w", err)
	}
	return record, nil
}

func (idx *IndexClient) GetKey(ctx context.Context, values ...any) (string, error) {
	vals, err := anyToProtoValues(values)
	if err != nil {
		return "", err
	}
	resp, err := idx.client.IndexGetKey(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: vals,
	})
	if err != nil {
		return "", grpcErr(err)
	}
	return resp.GetKey(), nil
}

func (idx *IndexClient) GetAll(ctx context.Context, r *KeyRange, values ...any) ([]Record, error) {
	vals, err := anyToProtoValues(values)
	if err != nil {
		return nil, err
	}
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := idx.client.IndexGetAll(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: vals, Range: kr,
	})
	if err != nil {
		return nil, grpcErr(err)
	}
	records, err := RecordsFromProto(resp.GetRecords())
	if err != nil {
		return nil, fmt.Errorf("unmarshal records: %w", err)
	}
	return records, nil
}

func (idx *IndexClient) GetAllKeys(ctx context.Context, r *KeyRange, values ...any) ([]string, error) {
	vals, err := anyToProtoValues(values)
	if err != nil {
		return nil, err
	}
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := idx.client.IndexGetAllKeys(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: vals, Range: kr,
	})
	if err != nil {
		return nil, grpcErr(err)
	}
	return resp.GetKeys(), nil
}

func (idx *IndexClient) Count(ctx context.Context, r *KeyRange, values ...any) (int64, error) {
	vals, err := anyToProtoValues(values)
	if err != nil {
		return 0, err
	}
	kr, err := krToProto(r)
	if err != nil {
		return 0, err
	}
	resp, err := idx.client.IndexCount(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: vals, Range: kr,
	})
	if err != nil {
		return 0, grpcErr(err)
	}
	return resp.GetCount(), nil
}

func (idx *IndexClient) Delete(ctx context.Context, values ...any) (int64, error) {
	vals, err := anyToProtoValues(values)
	if err != nil {
		return 0, err
	}
	resp, err := idx.client.IndexDelete(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: vals,
	})
	if err != nil {
		return 0, grpcErr(err)
	}
	return resp.GetDeleted(), nil
}

func (idx *IndexClient) OpenCursor(ctx context.Context, r *KeyRange, dir CursorDirection, values ...any) (*Cursor, error) {
	return openCursor(ctx, idx.client, idx.store, idx.index, r, dir, false, values)
}

func (idx *IndexClient) OpenKeyCursor(ctx context.Context, r *KeyRange, dir CursorDirection, values ...any) (*Cursor, error) {
	return openCursor(ctx, idx.client, idx.store, idx.index, r, dir, true, values)
}

type Cursor struct {
	stream      proto.IndexedDB_OpenCursorClient
	keysOnly    bool
	indexCursor bool
	entry       *proto.CursorEntry
	err         error
	done        bool
}

func (c *Cursor) Continue() bool {
	return c.sendAndRecv(&proto.CursorCommand{
		Command: &proto.CursorCommand_Next{Next: true},
	})
}

func (c *Cursor) ContinueToKey(key any) bool {
	kvs := cursorKeyToProtoSDK(key, c.indexCursor)
	return c.sendAndRecv(&proto.CursorCommand{
		Command: &proto.CursorCommand_ContinueToKey{ContinueToKey: &proto.CursorKeyTarget{Key: kvs}},
	})
}

func (c *Cursor) Advance(count int) bool {
	return c.sendAndRecv(&proto.CursorCommand{
		Command: &proto.CursorCommand_Advance{Advance: int32(count)},
	})
}

func (c *Cursor) Key() any {
	if c.entry == nil || len(c.entry.GetKey()) == 0 {
		return nil
	}
	parts := make([]any, len(c.entry.GetKey()))
	for i, kv := range c.entry.GetKey() {
		parts[i] = keyValueToAny(kv)
	}
	if !c.indexCursor && len(parts) == 1 {
		return parts[0]
	}
	return parts
}

func keyValueToAny(kv *proto.KeyValue) any {
	switch v := kv.GetKind().(type) {
	case *proto.KeyValue_Scalar:
		val, _ := AnyFromTypedValue(v.Scalar)
		return val
	case *proto.KeyValue_Array:
		parts := make([]any, len(v.Array.GetElements()))
		for i, elem := range v.Array.GetElements() {
			parts[i] = keyValueToAny(elem)
		}
		return parts
	default:
		return nil
	}
}

func anyToKeyValueSDK(v any) *proto.KeyValue {
	if arr, ok := v.([]any); ok {
		elems := make([]*proto.KeyValue, len(arr))
		for i, elem := range arr {
			elems[i] = anyToKeyValueSDK(elem)
		}
		return &proto.KeyValue{Kind: &proto.KeyValue_Array{Array: &proto.KeyValueArray{Elements: elems}}}
	}
	tv, _ := TypedValueFromAny(v)
	return &proto.KeyValue{Kind: &proto.KeyValue_Scalar{Scalar: tv}}
}

func cursorKeyToProtoSDK(key any, indexCursor bool) []*proto.KeyValue {
	if indexCursor {
		if parts, ok := key.([]any); ok {
			kvs := make([]*proto.KeyValue, len(parts))
			for i, part := range parts {
				kvs[i] = anyToKeyValueSDK(part)
			}
			return kvs
		}
	}
	return []*proto.KeyValue{anyToKeyValueSDK(key)}
}

func (c *Cursor) PrimaryKey() string {
	if c.entry == nil {
		return ""
	}
	return c.entry.GetPrimaryKey()
}

func (c *Cursor) Value() (Record, error) {
	if c.keysOnly {
		return nil, ErrKeysOnly
	}
	if c.entry == nil {
		return nil, ErrNotFound
	}
	return RecordFromProto(c.entry.GetRecord())
}

func (c *Cursor) Delete() error {
	if c.err != nil {
		return c.err
	}
	if c.done {
		return ErrNotFound
	}
	err := c.stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{
			Command: &proto.CursorCommand{
				Command: &proto.CursorCommand_Delete{Delete: true},
			},
		},
	})
	if err != nil {
		c.err = grpcErr(err)
		return c.err
	}
	resp, err := c.stream.Recv()
	if err != nil {
		c.err = grpcErr(err)
		return c.err
	}
	if entry := resp.GetEntry(); entry != nil {
		c.entry = entry
	}
	return nil
}

func (c *Cursor) Update(value Record) error {
	if c.err != nil {
		return c.err
	}
	if c.done {
		return ErrNotFound
	}
	pbRecord, err := RecordToProto(value)
	if err != nil {
		return fmt.Errorf("indexeddb: marshal cursor update: %w", err)
	}
	err = c.stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{
			Command: &proto.CursorCommand{
				Command: &proto.CursorCommand_Update{Update: pbRecord},
			},
		},
	})
	if err != nil {
		c.err = grpcErr(err)
		return c.err
	}
	resp, err := c.stream.Recv()
	if err != nil {
		c.err = grpcErr(err)
		return c.err
	}
	if entry := resp.GetEntry(); entry != nil {
		c.entry = entry
	}
	return nil
}

func (c *Cursor) Err() error {
	return c.err
}

func (c *Cursor) Close() error {
	if !c.done {
		c.done = true
		_ = c.stream.Send(&proto.CursorClientMessage{
			Msg: &proto.CursorClientMessage_Command{
				Command: &proto.CursorCommand{
					Command: &proto.CursorCommand_Close{Close: true},
				},
			},
		})
	}
	return grpcErr(c.stream.CloseSend())
}

func (c *Cursor) sendAndRecv(cmd *proto.CursorCommand) bool {
	if c.done || c.err != nil {
		return false
	}
	err := c.stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{Command: cmd},
	})
	if err != nil {
		c.err = grpcErr(err)
		return false
	}
	resp, err := c.stream.Recv()
	if err != nil {
		c.err = grpcErr(err)
		return false
	}
	switch v := resp.GetResult().(type) {
	case *proto.CursorResponse_Entry:
		c.entry = v.Entry
		return true
	case *proto.CursorResponse_Done:
		c.done = true
		c.entry = nil
		return false
	default:
		c.err = fmt.Errorf("indexeddb: unexpected cursor response")
		c.entry = nil
		return false
	}
}

func cursorDirectionToProto(dir CursorDirection) proto.CursorDirection {
	switch dir {
	case CursorNextUnique:
		return proto.CursorDirection_CURSOR_NEXT_UNIQUE
	case CursorPrev:
		return proto.CursorDirection_CURSOR_PREV
	case CursorPrevUnique:
		return proto.CursorDirection_CURSOR_PREV_UNIQUE
	default:
		return proto.CursorDirection_CURSOR_NEXT
	}
}

func openCursor(ctx context.Context, client proto.IndexedDBClient, store, index string, r *KeyRange, dir CursorDirection, keysOnly bool, values []any) (*Cursor, error) {
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	vals, err := TypedValuesFromAny(values)
	if err != nil {
		return nil, err
	}
	stream, err := client.OpenCursor(ctx)
	if err != nil {
		return nil, grpcErr(err)
	}
	err = stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Open{
			Open: &proto.OpenCursorRequest{
				Store:     store,
				Range:     kr,
				Direction: cursorDirectionToProto(dir),
				KeysOnly:  keysOnly,
				Index:     index,
				Values:    vals,
			},
		},
	})
	if err != nil {
		_ = stream.CloseSend()
		return nil, grpcErr(err)
	}
	// Read the open ack to surface creation errors synchronously.
	if _, err := stream.Recv(); err != nil {
		_ = stream.CloseSend()
		return nil, grpcErr(err)
	}
	return &Cursor{stream: stream, keysOnly: keysOnly, indexCursor: index != ""}, nil
}

func krToProto(r *KeyRange) (*proto.KeyRange, error) {
	if r == nil {
		return nil, nil
	}
	kr := &proto.KeyRange{LowerOpen: r.LowerOpen, UpperOpen: r.UpperOpen}
	if r.Lower != nil {
		v, err := TypedValueFromAny(r.Lower)
		if err != nil {
			return nil, fmt.Errorf("marshal key range lower: %w", err)
		}
		kr.Lower = v
	}
	if r.Upper != nil {
		v, err := TypedValueFromAny(r.Upper)
		if err != nil {
			return nil, fmt.Errorf("marshal key range upper: %w", err)
		}
		kr.Upper = v
	}
	return kr, nil
}

func anyToProtoValues(values []any) ([]*proto.TypedValue, error) {
	return TypedValuesFromAny(values)
}

func grpcErr(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	switch st.Code() {
	case codes.NotFound:
		return ErrNotFound
	case codes.AlreadyExists:
		return ErrAlreadyExists
	default:
		return err
	}
}
