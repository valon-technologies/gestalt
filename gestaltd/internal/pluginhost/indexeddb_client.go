package pluginhost

import (
	"context"
	"fmt"
	"io"
	"sync"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
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
	mu      sync.RWMutex
	schemas map[string]indexeddb.ObjectStoreSchema
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

	return &remoteIndexedDB{
		client:  dsClient,
		runtime: runtimeClient,
		closer:  proc,
		schemas: make(map[string]indexeddb.ObjectStoreSchema),
	}, nil
}

func (r *remoteIndexedDB) ObjectStore(name string) indexeddb.ObjectStore {
	return &remoteObjectStore{db: r, store: name}
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
	if err == nil {
		r.setSchema(name, schema)
	}
	return grpcToDatastoreErr(err)
}

func (r *remoteIndexedDB) DeleteObjectStore(ctx context.Context, name string) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	_, err := r.client.DeleteObjectStore(ctx, &proto.DeleteObjectStoreRequest{Name: name})
	if err == nil {
		r.deleteSchema(name)
	}
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

func (r *remoteIndexedDB) schema(name string) *indexeddb.ObjectStoreSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()
	schema, ok := r.schemas[name]
	if !ok {
		return nil
	}
	copy := schema
	return &copy
}

func (r *remoteIndexedDB) setSchema(name string, schema indexeddb.ObjectStoreSchema) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.schemas[name] = schema
}

func (r *remoteIndexedDB) deleteSchema(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.schemas, name)
}

// --- ObjectStore ---

type remoteObjectStore struct {
	db    *remoteIndexedDB
	store string
}

func (o *remoteObjectStore) Get(ctx context.Context, id string) (indexeddb.Record, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := o.db.client.Get(ctx, &proto.ObjectStoreRequest{Store: o.store, Id: id})
	if err != nil {
		return nil, grpcToDatastoreErr(err)
	}
	return structToRecord(resp.GetRecord(), o.db.schema(o.store))
}

func (o *remoteObjectStore) GetKey(ctx context.Context, id string) (string, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := o.db.client.GetKey(ctx, &proto.ObjectStoreRequest{Store: o.store, Id: id})
	if err != nil {
		return "", grpcToDatastoreErr(err)
	}
	return resp.GetKey(), nil
}

func (o *remoteObjectStore) Add(ctx context.Context, record indexeddb.Record) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	s, err := structFromRecord(record, o.db.schema(o.store))
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = o.db.client.Add(ctx, &proto.RecordRequest{Store: o.store, Record: s})
	return grpcToDatastoreErr(err)
}

func (o *remoteObjectStore) Put(ctx context.Context, record indexeddb.Record) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	s, err := structFromRecord(record, o.db.schema(o.store))
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = o.db.client.Put(ctx, &proto.RecordRequest{Store: o.store, Record: s})
	return grpcToDatastoreErr(err)
}

func (o *remoteObjectStore) Delete(ctx context.Context, id string) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	_, err := o.db.client.Delete(ctx, &proto.ObjectStoreRequest{Store: o.store, Id: id})
	return grpcToDatastoreErr(err)
}

func (o *remoteObjectStore) Clear(ctx context.Context) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	_, err := o.db.client.Clear(ctx, &proto.ObjectStoreNameRequest{Store: o.store})
	return grpcToDatastoreErr(err)
}

func (o *remoteObjectStore) GetAll(ctx context.Context, r *indexeddb.KeyRange) ([]indexeddb.Record, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	kr, err := keyRangeToProto(r, schemaPrimaryKeyCodec(o.db.schema(o.store)))
	if err != nil {
		return nil, err
	}
	resp, err := o.db.client.GetAll(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return nil, grpcToDatastoreErr(err)
	}
	return structsToRecords(resp.GetRecords(), o.db.schema(o.store))
}

func (o *remoteObjectStore) GetAllKeys(ctx context.Context, r *indexeddb.KeyRange) ([]string, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	kr, err := keyRangeToProto(r, schemaPrimaryKeyCodec(o.db.schema(o.store)))
	if err != nil {
		return nil, err
	}
	resp, err := o.db.client.GetAllKeys(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return nil, grpcToDatastoreErr(err)
	}
	return resp.GetKeys(), nil
}

func (o *remoteObjectStore) Count(ctx context.Context, r *indexeddb.KeyRange) (int64, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	kr, err := keyRangeToProto(r, schemaPrimaryKeyCodec(o.db.schema(o.store)))
	if err != nil {
		return 0, err
	}
	resp, err := o.db.client.Count(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return 0, grpcToDatastoreErr(err)
	}
	return resp.GetCount(), nil
}

func (o *remoteObjectStore) DeleteRange(ctx context.Context, r indexeddb.KeyRange) (int64, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	kr, err := keyRangeToProto(&r, schemaPrimaryKeyCodec(o.db.schema(o.store)))
	if err != nil {
		return 0, err
	}
	resp, err := o.db.client.DeleteRange(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return 0, grpcToDatastoreErr(err)
	}
	return resp.GetDeleted(), nil
}

func (o *remoteObjectStore) Index(name string) indexeddb.Index {
	return &remoteIndex{db: o.db, store: o.store, index: name}
}

// --- Index ---

type remoteIndex struct {
	db    *remoteIndexedDB
	store string
	index string
}

func (idx *remoteIndex) Get(ctx context.Context, values ...any) (indexeddb.Record, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	pbValues, err := toProtoValues(values, schemaIndexCodecs(idx.db.schema(idx.store), idx.index))
	if err != nil {
		return nil, err
	}
	resp, err := idx.db.client.IndexGet(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: pbValues,
	})
	if err != nil {
		return nil, grpcToDatastoreErr(err)
	}
	return structToRecord(resp.GetRecord(), idx.db.schema(idx.store))
}

func (idx *remoteIndex) GetKey(ctx context.Context, values ...any) (string, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	pbValues, err := toProtoValues(values, schemaIndexCodecs(idx.db.schema(idx.store), idx.index))
	if err != nil {
		return "", err
	}
	resp, err := idx.db.client.IndexGetKey(ctx, &proto.IndexQueryRequest{
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
	pbValues, err := toProtoValues(values, schemaIndexCodecs(idx.db.schema(idx.store), idx.index))
	if err != nil {
		return nil, err
	}
	kr, err := keyRangeToProto(r, schemaPrimaryKeyCodec(idx.db.schema(idx.store)))
	if err != nil {
		return nil, err
	}
	resp, err := idx.db.client.IndexGetAll(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: pbValues, Range: kr,
	})
	if err != nil {
		return nil, grpcToDatastoreErr(err)
	}
	return structsToRecords(resp.GetRecords(), idx.db.schema(idx.store))
}

func (idx *remoteIndex) GetAllKeys(ctx context.Context, r *indexeddb.KeyRange, values ...any) ([]string, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	pbValues, err := toProtoValues(values, schemaIndexCodecs(idx.db.schema(idx.store), idx.index))
	if err != nil {
		return nil, err
	}
	kr, err := keyRangeToProto(r, schemaPrimaryKeyCodec(idx.db.schema(idx.store)))
	if err != nil {
		return nil, err
	}
	resp, err := idx.db.client.IndexGetAllKeys(ctx, &proto.IndexQueryRequest{
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
	pbValues, err := toProtoValues(values, schemaIndexCodecs(idx.db.schema(idx.store), idx.index))
	if err != nil {
		return 0, err
	}
	kr, err := keyRangeToProto(r, schemaPrimaryKeyCodec(idx.db.schema(idx.store)))
	if err != nil {
		return 0, err
	}
	resp, err := idx.db.client.IndexCount(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: pbValues, Range: kr,
	})
	if err != nil {
		return 0, grpcToDatastoreErr(err)
	}
	return resp.GetCount(), nil
}

func (idx *remoteIndex) Delete(ctx context.Context, values ...any) (int64, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	pbValues, err := toProtoValues(values, schemaIndexCodecs(idx.db.schema(idx.store), idx.index))
	if err != nil {
		return 0, err
	}
	resp, err := idx.db.client.IndexDelete(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: pbValues,
	})
	if err != nil {
		return 0, grpcToDatastoreErr(err)
	}
	return resp.GetDeleted(), nil
}

// --- Helpers ---

func structToRecord(s *structpb.Struct, schema *indexeddb.ObjectStoreSchema) (indexeddb.Record, error) {
	return recordFromStruct(s, schema)
}

func structsToRecords(ss []*structpb.Struct, schema *indexeddb.ObjectStoreSchema) ([]indexeddb.Record, error) {
	records := make([]indexeddb.Record, len(ss))
	for i, s := range ss {
		record, err := structToRecord(s, schema)
		if err != nil {
			return nil, fmt.Errorf("decode record %d: %w", i, err)
		}
		records[i] = record
	}
	return records, nil
}

func toProtoValues(values []any, codecs []valueCodec) ([]*structpb.Value, error) {
	return protoValuesFromAny(values, codecs)
}

func keyRangeToProto(r *indexeddb.KeyRange, codec valueCodec) (*proto.KeyRange, error) {
	return protoKeyRangeFromRange(r, codec)
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

var _ indexeddb.IndexedDB = (*remoteIndexedDB)(nil)
