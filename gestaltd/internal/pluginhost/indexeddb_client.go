package pluginhost

import (
	"context"
	"fmt"
	"io"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type IndexedDBExecConfig struct {
	Command      string
	Args         []string
	Env          map[string]string
	Config       map[string]any
	AllowedHosts []string
	HostBinary   string
	Cleanup      func()
	Name         string
}

type remoteIndexedDB struct {
	client  proto.IndexedDBClient
	runtime proto.ProviderLifecycleClient
	closer  io.Closer
}

func NewExecutableIndexedDB(ctx context.Context, cfg IndexedDBExecConfig) (indexeddb.IndexedDB, error) {
	proc, err := startProviderProcess(ctx, ExecConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Env:          cfg.Env,
		Config:       cfg.Config,
		AllowedHosts: cfg.AllowedHosts,
		HostBinary:   cfg.HostBinary,
		Cleanup:      cfg.Cleanup,
	}, nil, "")
	if err != nil {
		return nil, err
	}

	runtimeClient := proto.NewProviderLifecycleClient(proc.conn)
	dsClient := proto.NewIndexedDBClient(proc.conn)

	_, err = configureRuntimeProvider(ctx, runtimeClient, proto.ProviderKind_PROVIDER_KIND_INDEXEDDB, cfg.Name, cfg.Config)
	if err != nil {
		_ = proc.Close()
		return nil, err
	}

	return &remoteIndexedDB{client: dsClient, runtime: runtimeClient, closer: proc}, nil
}

func (r *remoteIndexedDB) ObjectStore(name string) indexeddb.ObjectStore {
	return &remoteObjectStore{client: r.client, store: name}
}

func (r *remoteIndexedDB) CreateObjectStore(ctx context.Context, name string, schema indexeddb.ObjectStoreSchema) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	indexes := make([]*proto.IndexSchema, len(schema.Indexes))
	for i, idx := range schema.Indexes {
		indexes[i] = &proto.IndexSchema{Name: idx.Name, KeyPath: idx.KeyPath, Unique: idx.Unique}
	}
	columns := make([]*proto.ColumnDef, len(schema.Columns))
	for i, col := range schema.Columns {
		columns[i] = &proto.ColumnDef{
			Name: col.Name, Type: int32(col.Type),
			PrimaryKey: col.PrimaryKey, NotNull: col.NotNull, Unique: col.Unique,
		}
	}
	_, err := r.client.CreateObjectStore(ctx, &proto.CreateObjectStoreRequest{
		Name: name, Schema: &proto.ObjectStoreSchema{Indexes: indexes, Columns: columns},
	})
	return grpcToDatastoreErr(err)
}

func (r *remoteIndexedDB) DeleteObjectStore(ctx context.Context, name string) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	_, err := r.client.DeleteObjectStore(ctx, &proto.DeleteObjectStoreRequest{Name: name})
	return grpcToDatastoreErr(err)
}

func (r *remoteIndexedDB) Ping(ctx context.Context) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	_, err := r.runtime.HealthCheck(ctx, &emptypb.Empty{})
	return err
}

func (r *remoteIndexedDB) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}
	return r.closer.Close()
}

// --- ObjectStore ---

type remoteObjectStore struct {
	client proto.IndexedDBClient
	store  string
}

func (o *remoteObjectStore) Get(ctx context.Context, id string) (indexeddb.Record, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := o.client.Get(ctx, &proto.ObjectStoreRequest{Store: o.store, Id: id})
	if err != nil {
		return nil, grpcToDatastoreErr(err)
	}
	record, err := gestalt.RecordFromProto(resp.GetRecord())
	if err != nil {
		return nil, fmt.Errorf("unmarshal record: %w", err)
	}
	return record, nil
}

func (o *remoteObjectStore) GetKey(ctx context.Context, id string) (string, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := o.client.GetKey(ctx, &proto.ObjectStoreRequest{Store: o.store, Id: id})
	if err != nil {
		return "", grpcToDatastoreErr(err)
	}
	return resp.GetKey(), nil
}

func (o *remoteObjectStore) Add(ctx context.Context, record indexeddb.Record) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	pbRecord, err := gestalt.RecordToProto(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = o.client.Add(ctx, &proto.RecordRequest{Store: o.store, Record: pbRecord})
	return grpcToDatastoreErr(err)
}

func (o *remoteObjectStore) Put(ctx context.Context, record indexeddb.Record) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	pbRecord, err := gestalt.RecordToProto(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = o.client.Put(ctx, &proto.RecordRequest{Store: o.store, Record: pbRecord})
	return grpcToDatastoreErr(err)
}

func (o *remoteObjectStore) Delete(ctx context.Context, id string) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	_, err := o.client.Delete(ctx, &proto.ObjectStoreRequest{Store: o.store, Id: id})
	return grpcToDatastoreErr(err)
}

func (o *remoteObjectStore) Clear(ctx context.Context) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	_, err := o.client.Clear(ctx, &proto.ObjectStoreNameRequest{Store: o.store})
	return grpcToDatastoreErr(err)
}

func (o *remoteObjectStore) GetAll(ctx context.Context, r *indexeddb.KeyRange) ([]indexeddb.Record, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	kr, err := keyRangeToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := o.client.GetAll(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return nil, grpcToDatastoreErr(err)
	}
	records, err := gestalt.RecordsFromProto(resp.GetRecords())
	if err != nil {
		return nil, fmt.Errorf("unmarshal records: %w", err)
	}
	return records, nil
}

func (o *remoteObjectStore) GetAllKeys(ctx context.Context, r *indexeddb.KeyRange) ([]string, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	kr, err := keyRangeToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := o.client.GetAllKeys(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return nil, grpcToDatastoreErr(err)
	}
	return resp.GetKeys(), nil
}

func (o *remoteObjectStore) Count(ctx context.Context, r *indexeddb.KeyRange) (int64, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	kr, err := keyRangeToProto(r)
	if err != nil {
		return 0, err
	}
	resp, err := o.client.Count(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return 0, grpcToDatastoreErr(err)
	}
	return resp.GetCount(), nil
}

func (o *remoteObjectStore) DeleteRange(ctx context.Context, r indexeddb.KeyRange) (int64, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	kr, err := keyRangeToProto(&r)
	if err != nil {
		return 0, err
	}
	resp, err := o.client.DeleteRange(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return 0, grpcToDatastoreErr(err)
	}
	return resp.GetDeleted(), nil
}

func (o *remoteObjectStore) Index(name string) indexeddb.Index {
	return &remoteIndex{client: o.client, store: o.store, index: name}
}

func (o *remoteObjectStore) OpenCursor(ctx context.Context, r *indexeddb.KeyRange, dir indexeddb.CursorDirection) (indexeddb.Cursor, error) {
	return openRemoteCursor(ctx, o.client, o.store, "", r, dir, false, nil)
}

func (o *remoteObjectStore) OpenKeyCursor(ctx context.Context, r *indexeddb.KeyRange, dir indexeddb.CursorDirection) (indexeddb.Cursor, error) {
	return openRemoteCursor(ctx, o.client, o.store, "", r, dir, true, nil)
}

// --- Index ---

type remoteIndex struct {
	client proto.IndexedDBClient
	store  string
	index  string
}

func (idx *remoteIndex) Get(ctx context.Context, values ...any) (indexeddb.Record, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	pbValues, err := toProtoValues(values)
	if err != nil {
		return nil, err
	}
	resp, err := idx.client.IndexGet(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: pbValues,
	})
	if err != nil {
		return nil, grpcToDatastoreErr(err)
	}
	record, err := gestalt.RecordFromProto(resp.GetRecord())
	if err != nil {
		return nil, fmt.Errorf("unmarshal record: %w", err)
	}
	return record, nil
}

func (idx *remoteIndex) GetKey(ctx context.Context, values ...any) (string, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	pbValues, err := toProtoValues(values)
	if err != nil {
		return "", err
	}
	resp, err := idx.client.IndexGetKey(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: pbValues,
	})
	if err != nil {
		return "", grpcToDatastoreErr(err)
	}
	return resp.GetKey(), nil
}

func (idx *remoteIndex) GetAll(ctx context.Context, r *indexeddb.KeyRange, values ...any) ([]indexeddb.Record, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	pbValues, err := toProtoValues(values)
	if err != nil {
		return nil, err
	}
	kr, err := keyRangeToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := idx.client.IndexGetAll(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: pbValues, Range: kr,
	})
	if err != nil {
		return nil, grpcToDatastoreErr(err)
	}
	records, err := gestalt.RecordsFromProto(resp.GetRecords())
	if err != nil {
		return nil, fmt.Errorf("unmarshal records: %w", err)
	}
	return records, nil
}

func (idx *remoteIndex) GetAllKeys(ctx context.Context, r *indexeddb.KeyRange, values ...any) ([]string, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	pbValues, err := toProtoValues(values)
	if err != nil {
		return nil, err
	}
	kr, err := keyRangeToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := idx.client.IndexGetAllKeys(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: pbValues, Range: kr,
	})
	if err != nil {
		return nil, grpcToDatastoreErr(err)
	}
	return resp.GetKeys(), nil
}

func (idx *remoteIndex) Count(ctx context.Context, r *indexeddb.KeyRange, values ...any) (int64, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	pbValues, err := toProtoValues(values)
	if err != nil {
		return 0, err
	}
	kr, err := keyRangeToProto(r)
	if err != nil {
		return 0, err
	}
	resp, err := idx.client.IndexCount(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: pbValues, Range: kr,
	})
	if err != nil {
		return 0, grpcToDatastoreErr(err)
	}
	return resp.GetCount(), nil
}

func (idx *remoteIndex) OpenCursor(ctx context.Context, r *indexeddb.KeyRange, dir indexeddb.CursorDirection, values ...any) (indexeddb.Cursor, error) {
	return openRemoteCursor(ctx, idx.client, idx.store, idx.index, r, dir, false, values)
}

func (idx *remoteIndex) OpenKeyCursor(ctx context.Context, r *indexeddb.KeyRange, dir indexeddb.CursorDirection, values ...any) (indexeddb.Cursor, error) {
	return openRemoteCursor(ctx, idx.client, idx.store, idx.index, r, dir, true, values)
}

func (idx *remoteIndex) Delete(ctx context.Context, values ...any) (int64, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	pbValues, err := toProtoValues(values)
	if err != nil {
		return 0, err
	}
	resp, err := idx.client.IndexDelete(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: pbValues,
	})
	if err != nil {
		return 0, grpcToDatastoreErr(err)
	}
	return resp.GetDeleted(), nil
}

// --- Helpers ---

func toProtoValues(values []any) ([]*proto.TypedValue, error) {
	return gestalt.TypedValuesFromAny(values)
}

func keyRangeToProto(r *indexeddb.KeyRange) (*proto.KeyRange, error) {
	if r == nil {
		return nil, nil
	}
	kr := &proto.KeyRange{
		LowerOpen: r.LowerOpen,
		UpperOpen: r.UpperOpen,
	}
	if r.Lower != nil {
		v, err := gestalt.TypedValueFromAny(r.Lower)
		if err != nil {
			return nil, fmt.Errorf("marshal key range lower: %w", err)
		}
		kr.Lower = v
	}
	if r.Upper != nil {
		v, err := gestalt.TypedValueFromAny(r.Upper)
		if err != nil {
			return nil, fmt.Errorf("marshal key range upper: %w", err)
		}
		kr.Upper = v
	}
	return kr, nil
}

func grpcToDatastoreErr(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	switch st.Code() {
	case codes.NotFound:
		return indexeddb.ErrNotFound
	case codes.AlreadyExists:
		return indexeddb.ErrAlreadyExists
	default:
		return err
	}
}

// --- Remote Cursor ---

func cursorDirectionToProto(dir indexeddb.CursorDirection) proto.CursorDirection {
	switch dir {
	case indexeddb.CursorNextUnique:
		return proto.CursorDirection_CURSOR_NEXT_UNIQUE
	case indexeddb.CursorPrev:
		return proto.CursorDirection_CURSOR_PREV
	case indexeddb.CursorPrevUnique:
		return proto.CursorDirection_CURSOR_PREV_UNIQUE
	default:
		return proto.CursorDirection_CURSOR_NEXT
	}
}

func openRemoteCursor(ctx context.Context, client proto.IndexedDBClient, store, index string, r *indexeddb.KeyRange, dir indexeddb.CursorDirection, keysOnly bool, values []any) (*remoteCursor, error) {
	kr, err := keyRangeToProto(r)
	if err != nil {
		return nil, err
	}
	var pbValues []*proto.TypedValue
	if len(values) > 0 {
		pbValues, err = toProtoValues(values)
		if err != nil {
			return nil, err
		}
	}
	stream, err := client.OpenCursor(ctx)
	if err != nil {
		return nil, grpcToDatastoreErr(err)
	}
	if err := stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Open{Open: &proto.OpenCursorRequest{
			Store:     store,
			Range:     kr,
			Direction: cursorDirectionToProto(dir),
			KeysOnly:  keysOnly,
			Index:     index,
			Values:    pbValues,
		}},
	}); err != nil {
		return nil, grpcToDatastoreErr(err)
	}
	return &remoteCursor{stream: stream, keysOnly: keysOnly}, nil
}

type remoteCursor struct {
	stream   proto.IndexedDB_OpenCursorClient
	keysOnly bool
	entry    *proto.CursorEntry
	err      error
	done     bool
}

func (c *remoteCursor) sendAndRecv(msg *proto.CursorClientMessage) bool {
	if c.done || c.err != nil {
		return false
	}
	if err := c.stream.Send(msg); err != nil {
		c.err = grpcToDatastoreErr(err)
		return false
	}
	resp, err := c.stream.Recv()
	if err != nil {
		c.err = grpcToDatastoreErr(err)
		return false
	}
	switch v := resp.GetResult().(type) {
	case *proto.CursorResponse_Entry:
		c.entry = v.Entry
		return true
	case *proto.CursorResponse_Done:
		if v.Done {
			c.done = true
		}
		return false
	default:
		c.err = fmt.Errorf("unexpected cursor response")
		return false
	}
}

func (c *remoteCursor) Continue(_ context.Context) bool {
	return c.sendAndRecv(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{Command: &proto.CursorCommand{
			Command: &proto.CursorCommand_Next{Next: true},
		}},
	})
}

func (c *remoteCursor) ContinueToKey(_ context.Context, key any) bool {
	tv, err := gestalt.TypedValueFromAny(key)
	if err != nil {
		c.err = err
		return false
	}
	return c.sendAndRecv(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{Command: &proto.CursorCommand{
			Command: &proto.CursorCommand_ContinueToKey{ContinueToKey: tv},
		}},
	})
}

func (c *remoteCursor) Advance(_ context.Context, count int) bool {
	return c.sendAndRecv(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{Command: &proto.CursorCommand{
			Command: &proto.CursorCommand_Advance{Advance: int32(count)},
		}},
	})
}

func (c *remoteCursor) Key() any {
	if c.entry == nil || c.entry.Key == nil {
		return nil
	}
	v, _ := gestalt.AnyFromTypedValue(c.entry.Key)
	return v
}

func (c *remoteCursor) PrimaryKey() string {
	if c.entry == nil {
		return ""
	}
	return c.entry.PrimaryKey
}

func (c *remoteCursor) Value() (indexeddb.Record, error) {
	if c.keysOnly {
		return nil, indexeddb.ErrKeysOnly
	}
	if c.entry == nil || c.entry.Record == nil {
		return nil, indexeddb.ErrNotFound
	}
	return gestalt.RecordFromProto(c.entry.Record)
}

func (c *remoteCursor) Delete(_ context.Context) error {
	if c.done || c.err != nil {
		return indexeddb.ErrNotFound
	}
	if err := c.stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{Command: &proto.CursorCommand{
			Command: &proto.CursorCommand_Delete{Delete: true},
		}},
	}); err != nil {
		return grpcToDatastoreErr(err)
	}
	_, err := c.stream.Recv()
	return grpcToDatastoreErr(err)
}

func (c *remoteCursor) Update(_ context.Context, value indexeddb.Record) error {
	if c.done || c.err != nil {
		return indexeddb.ErrNotFound
	}
	pbRec, err := gestalt.RecordToProto(value)
	if err != nil {
		return fmt.Errorf("marshal update record: %w", err)
	}
	if err := c.stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{Command: &proto.CursorCommand{
			Command: &proto.CursorCommand_Update{Update: pbRec},
		}},
	}); err != nil {
		return grpcToDatastoreErr(err)
	}
	_, err = c.stream.Recv()
	return grpcToDatastoreErr(err)
}

func (c *remoteCursor) Err() error { return c.err }

func (c *remoteCursor) Close() error {
	if c.stream == nil {
		return nil
	}
	_ = c.stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{Command: &proto.CursorCommand{
			Command: &proto.CursorCommand_Close{Close: true},
		}},
	})
	return c.stream.CloseSend()
}

var _ indexeddb.IndexedDB = (*remoteIndexedDB)(nil)
