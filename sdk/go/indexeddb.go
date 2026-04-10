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
	"google.golang.org/protobuf/types/known/structpb"
)

const EnvIndexedDBSocket = "GESTALT_INDEXEDDB_SOCKET"

var (
	ErrNotFound      = fmt.Errorf("indexeddb: not found")
	ErrAlreadyExists = fmt.Errorf("indexeddb: already exists")
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
	return resp.GetRecord().AsMap(), nil
}

func (o *ObjectStoreClient) GetKey(ctx context.Context, id string) (string, error) {
	resp, err := o.client.GetKey(ctx, &proto.ObjectStoreRequest{Store: o.store, Id: id})
	if err != nil {
		return "", grpcErr(err)
	}
	return resp.GetKey(), nil
}

func (o *ObjectStoreClient) Add(ctx context.Context, record Record) error {
	s, err := structpb.NewStruct(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = o.client.Add(ctx, &proto.RecordRequest{Store: o.store, Record: s})
	return grpcErr(err)
}

func (o *ObjectStoreClient) Put(ctx context.Context, record Record) error {
	s, err := structpb.NewStruct(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = o.client.Put(ctx, &proto.RecordRequest{Store: o.store, Record: s})
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
	return structsToMaps(resp.GetRecords()), nil
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
	return resp.GetRecord().AsMap(), nil
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
	return structsToMaps(resp.GetRecords()), nil
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

func krToProto(r *KeyRange) (*proto.KeyRange, error) {
	if r == nil {
		return nil, nil
	}
	kr := &proto.KeyRange{LowerOpen: r.LowerOpen, UpperOpen: r.UpperOpen}
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

func anyToProtoValues(values []any) ([]*structpb.Value, error) {
	out := make([]*structpb.Value, len(values))
	for i, v := range values {
		pv, err := structpb.NewValue(v)
		if err != nil {
			return nil, fmt.Errorf("marshal value %d: %w", i, err)
		}
		out[i] = pv
	}
	return out, nil
}

func structsToMaps(ss []*structpb.Struct) []Record {
	out := make([]Record, len(ss))
	for i, s := range ss {
		out[i] = s.AsMap()
	}
	return out
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
