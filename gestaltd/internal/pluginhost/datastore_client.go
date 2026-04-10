package pluginhost

import (
	"context"
	"fmt"
	"io"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core/datastore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

type DatastoreExecConfig struct {
	Command      string
	Args         []string
	Env          map[string]string
	Config       map[string]any
	AllowedHosts []string
	HostBinary   string
	Cleanup      func()
	Name         string
}

type remoteDatastore struct {
	client  proto.IndexedDBClient
	runtime proto.ProviderLifecycleClient
	closer  io.Closer
}

func NewExecutableDatastore(ctx context.Context, cfg DatastoreExecConfig) (datastore.Datastore, error) {
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

	_, err = configureRuntimeProvider(ctx, runtimeClient, proto.ProviderKind_PROVIDER_KIND_DATASTORE, cfg.Name, cfg.Config)
	if err != nil {
		_ = proc.Close()
		return nil, err
	}

	return &remoteDatastore{client: dsClient, runtime: runtimeClient, closer: proc}, nil
}

func (r *remoteDatastore) ObjectStore(name string) datastore.ObjectStore {
	return &remoteObjectStore{client: r.client, store: name}
}

func (r *remoteDatastore) CreateObjectStore(ctx context.Context, name string, schema datastore.ObjectStoreSchema) error {
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

func (r *remoteDatastore) DeleteObjectStore(ctx context.Context, name string) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	_, err := r.client.DeleteObjectStore(ctx, &proto.DeleteObjectStoreRequest{Name: name})
	return grpcToDatastoreErr(err)
}

func (r *remoteDatastore) Ping(ctx context.Context) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	_, err := r.runtime.HealthCheck(ctx, &emptypb.Empty{})
	return err
}

func (r *remoteDatastore) Close() error {
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

func (o *remoteObjectStore) Get(ctx context.Context, id string) (datastore.Record, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := o.client.Get(ctx, &proto.ObjectStoreRequest{Store: o.store, Id: id})
	if err != nil {
		return nil, grpcToDatastoreErr(err)
	}
	return structToRecord(resp.GetRecord()), nil
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

func (o *remoteObjectStore) Add(ctx context.Context, record datastore.Record) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	s, err := structpb.NewStruct(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = o.client.Add(ctx, &proto.RecordRequest{Store: o.store, Record: s})
	return grpcToDatastoreErr(err)
}

func (o *remoteObjectStore) Put(ctx context.Context, record datastore.Record) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	s, err := structpb.NewStruct(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = o.client.Put(ctx, &proto.RecordRequest{Store: o.store, Record: s})
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

func (o *remoteObjectStore) GetAll(ctx context.Context, r *datastore.KeyRange) ([]datastore.Record, error) {
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
	return structsToRecords(resp.GetRecords()), nil
}

func (o *remoteObjectStore) GetAllKeys(ctx context.Context, r *datastore.KeyRange) ([]string, error) {
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

func (o *remoteObjectStore) Count(ctx context.Context, r *datastore.KeyRange) (int64, error) {
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

func (o *remoteObjectStore) DeleteRange(ctx context.Context, r datastore.KeyRange) (int64, error) {
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

func (o *remoteObjectStore) Index(name string) datastore.Index {
	return &remoteIndex{client: o.client, store: o.store, index: name}
}

// --- Index ---

type remoteIndex struct {
	client proto.IndexedDBClient
	store  string
	index  string
}

func (idx *remoteIndex) Get(ctx context.Context, values ...any) (datastore.Record, error) {
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
	return structToRecord(resp.GetRecord()), nil
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

func (idx *remoteIndex) GetAll(ctx context.Context, r *datastore.KeyRange, values ...any) ([]datastore.Record, error) {
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
	return structsToRecords(resp.GetRecords()), nil
}

func (idx *remoteIndex) GetAllKeys(ctx context.Context, r *datastore.KeyRange, values ...any) ([]string, error) {
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

func (idx *remoteIndex) Count(ctx context.Context, r *datastore.KeyRange, values ...any) (int64, error) {
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

func structToRecord(s *structpb.Struct) datastore.Record {
	if s == nil {
		return nil
	}
	return s.AsMap()
}

func structsToRecords(ss []*structpb.Struct) []datastore.Record {
	records := make([]datastore.Record, len(ss))
	for i, s := range ss {
		records[i] = structToRecord(s)
	}
	return records
}

func toProtoValues(values []any) ([]*structpb.Value, error) {
	pbValues := make([]*structpb.Value, len(values))
	for i, v := range values {
		pv, err := structpb.NewValue(v)
		if err != nil {
			return nil, fmt.Errorf("marshal index value %d: %w", i, err)
		}
		pbValues[i] = pv
	}
	return pbValues, nil
}

func keyRangeToProto(r *datastore.KeyRange) (*proto.KeyRange, error) {
	if r == nil {
		return nil, nil
	}
	kr := &proto.KeyRange{
		LowerOpen: r.LowerOpen,
		UpperOpen: r.UpperOpen,
	}
	if r.Lower != nil {
		v, err := structpb.NewValue(r.Lower)
		if err != nil {
			return nil, fmt.Errorf("marshal key range lower: %w", err)
		}
		kr.Lower = v
	}
	if r.Upper != nil {
		v, err := structpb.NewValue(r.Upper)
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
		return datastore.ErrNotFound
	case codes.AlreadyExists:
		return datastore.ErrAlreadyExists
	default:
		return err
	}
}

var _ datastore.Datastore = (*remoteDatastore)(nil)
