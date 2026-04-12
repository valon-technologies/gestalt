package pluginhost

import (
	"context"
	"errors"
	"fmt"
	"sync"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

type indexedDBServer struct {
	proto.UnimplementedIndexedDBServer
	ds       indexeddb.IndexedDB
	prefix   string
	schemaMu sync.RWMutex
	schemas  map[string]indexeddb.ObjectStoreSchema
}

func NewIndexedDBServer(ds indexeddb.IndexedDB, pluginName string) proto.IndexedDBServer {
	return &indexedDBServer{
		ds:      ds,
		prefix:  "plugin_" + pluginName + "_",
		schemas: make(map[string]indexeddb.ObjectStoreSchema),
	}
}

func (s *indexedDBServer) storeName(name string) string {
	return s.prefix + name
}

func (s *indexedDBServer) CreateObjectStore(ctx context.Context, req *proto.CreateObjectStoreRequest) (*emptypb.Empty, error) {
	schema := protoToSchema(req.GetSchema())
	storeName := s.storeName(req.GetName())
	if err := s.ds.CreateObjectStore(ctx, storeName, schema); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	s.setSchema(storeName, schema)
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) DeleteObjectStore(ctx context.Context, req *proto.DeleteObjectStoreRequest) (*emptypb.Empty, error) {
	storeName := s.storeName(req.GetName())
	if err := s.ds.DeleteObjectStore(ctx, storeName); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	s.deleteSchema(storeName)
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) Get(ctx context.Context, req *proto.ObjectStoreRequest) (*proto.RecordResponse, error) {
	storeName := s.storeName(req.GetStore())
	rec, err := s.ds.ObjectStore(storeName).Get(ctx, req.GetId())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return recordToProto(rec, s.schema(storeName))
}

func (s *indexedDBServer) GetKey(ctx context.Context, req *proto.ObjectStoreRequest) (*proto.KeyResponse, error) {
	key, err := s.ds.ObjectStore(s.storeName(req.GetStore())).GetKey(ctx, req.GetId())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.KeyResponse{Key: key}, nil
}

func (s *indexedDBServer) Add(ctx context.Context, req *proto.RecordRequest) (*emptypb.Empty, error) {
	storeName := s.storeName(req.GetStore())
	record, err := recordFromStruct(req.GetRecord(), s.schema(storeName))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := s.ds.ObjectStore(storeName).Add(ctx, record); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) Put(ctx context.Context, req *proto.RecordRequest) (*emptypb.Empty, error) {
	storeName := s.storeName(req.GetStore())
	record, err := recordFromStruct(req.GetRecord(), s.schema(storeName))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := s.ds.ObjectStore(storeName).Put(ctx, record); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) Delete(ctx context.Context, req *proto.ObjectStoreRequest) (*emptypb.Empty, error) {
	if err := s.ds.ObjectStore(s.storeName(req.GetStore())).Delete(ctx, req.GetId()); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) Clear(ctx context.Context, req *proto.ObjectStoreNameRequest) (*emptypb.Empty, error) {
	if err := s.ds.ObjectStore(s.storeName(req.GetStore())).Clear(ctx); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) GetAll(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.RecordsResponse, error) {
	storeName := s.storeName(req.GetStore())
	keyRange, err := protoToKeyRange(req.Range, schemaPrimaryKeyCodec(s.schema(storeName)))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	recs, err := s.ds.ObjectStore(storeName).GetAll(ctx, keyRange)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return recordsToProto(recs, s.schema(storeName))
}

func (s *indexedDBServer) GetAllKeys(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.KeysResponse, error) {
	storeName := s.storeName(req.GetStore())
	keyRange, err := protoToKeyRange(req.Range, schemaPrimaryKeyCodec(s.schema(storeName)))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	keys, err := s.ds.ObjectStore(storeName).GetAllKeys(ctx, keyRange)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.KeysResponse{Keys: keys}, nil
}

func (s *indexedDBServer) Count(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.CountResponse, error) {
	storeName := s.storeName(req.GetStore())
	keyRange, err := protoToKeyRange(req.Range, schemaPrimaryKeyCodec(s.schema(storeName)))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	count, err := s.ds.ObjectStore(storeName).Count(ctx, keyRange)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.CountResponse{Count: count}, nil
}

func (s *indexedDBServer) DeleteRange(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.DeleteResponse, error) {
	storeName := s.storeName(req.GetStore())
	kr, err := protoToKeyRange(req.Range, schemaPrimaryKeyCodec(s.schema(storeName)))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if kr == nil {
		return nil, status.Error(codes.InvalidArgument, "key range is required for DeleteRange")
	}
	deleted, err := s.ds.ObjectStore(storeName).DeleteRange(ctx, *kr)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.DeleteResponse{Deleted: deleted}, nil
}

func (s *indexedDBServer) IndexGet(ctx context.Context, req *proto.IndexQueryRequest) (*proto.RecordResponse, error) {
	storeName := s.storeName(req.GetStore())
	values, err := protoValuesToAny(req.GetValues(), schemaIndexCodecs(s.schema(storeName), req.GetIndex()))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	rec, err := s.ds.ObjectStore(storeName).Index(req.GetIndex()).Get(ctx, values...)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return recordToProto(rec, s.schema(storeName))
}

func (s *indexedDBServer) IndexGetKey(ctx context.Context, req *proto.IndexQueryRequest) (*proto.KeyResponse, error) {
	storeName := s.storeName(req.GetStore())
	values, err := protoValuesToAny(req.GetValues(), schemaIndexCodecs(s.schema(storeName), req.GetIndex()))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	key, err := s.ds.ObjectStore(storeName).Index(req.GetIndex()).GetKey(ctx, values...)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.KeyResponse{Key: key}, nil
}

func (s *indexedDBServer) IndexGetAll(ctx context.Context, req *proto.IndexQueryRequest) (*proto.RecordsResponse, error) {
	storeName := s.storeName(req.GetStore())
	keyRange, err := protoToKeyRange(req.Range, schemaPrimaryKeyCodec(s.schema(storeName)))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	values, err := protoValuesToAny(req.GetValues(), schemaIndexCodecs(s.schema(storeName), req.GetIndex()))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	recs, err := s.ds.ObjectStore(storeName).Index(req.GetIndex()).GetAll(ctx, keyRange, values...)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return recordsToProto(recs, s.schema(storeName))
}

func (s *indexedDBServer) IndexGetAllKeys(ctx context.Context, req *proto.IndexQueryRequest) (*proto.KeysResponse, error) {
	storeName := s.storeName(req.GetStore())
	keyRange, err := protoToKeyRange(req.Range, schemaPrimaryKeyCodec(s.schema(storeName)))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	values, err := protoValuesToAny(req.GetValues(), schemaIndexCodecs(s.schema(storeName), req.GetIndex()))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	keys, err := s.ds.ObjectStore(storeName).Index(req.GetIndex()).GetAllKeys(ctx, keyRange, values...)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.KeysResponse{Keys: keys}, nil
}

func (s *indexedDBServer) IndexCount(ctx context.Context, req *proto.IndexQueryRequest) (*proto.CountResponse, error) {
	storeName := s.storeName(req.GetStore())
	keyRange, err := protoToKeyRange(req.Range, schemaPrimaryKeyCodec(s.schema(storeName)))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	values, err := protoValuesToAny(req.GetValues(), schemaIndexCodecs(s.schema(storeName), req.GetIndex()))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	count, err := s.ds.ObjectStore(storeName).Index(req.GetIndex()).Count(ctx, keyRange, values...)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.CountResponse{Count: count}, nil
}

func (s *indexedDBServer) IndexDelete(ctx context.Context, req *proto.IndexQueryRequest) (*proto.DeleteResponse, error) {
	storeName := s.storeName(req.GetStore())
	values, err := protoValuesToAny(req.GetValues(), schemaIndexCodecs(s.schema(storeName), req.GetIndex()))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	deleted, err := s.ds.ObjectStore(storeName).Index(req.GetIndex()).Delete(ctx, values...)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.DeleteResponse{Deleted: deleted}, nil
}

func (s *indexedDBServer) schema(name string) *indexeddb.ObjectStoreSchema {
	s.schemaMu.RLock()
	defer s.schemaMu.RUnlock()
	schema, ok := s.schemas[name]
	if !ok {
		return nil
	}
	copy := schema
	return &copy
}

func (s *indexedDBServer) setSchema(name string, schema indexeddb.ObjectStoreSchema) {
	s.schemaMu.Lock()
	defer s.schemaMu.Unlock()
	s.schemas[name] = schema
}

func (s *indexedDBServer) deleteSchema(name string) {
	s.schemaMu.Lock()
	defer s.schemaMu.Unlock()
	delete(s.schemas, name)
}

func recordToProto(rec indexeddb.Record, schema *indexeddb.ObjectStoreSchema) (*proto.RecordResponse, error) {
	s, err := structFromRecord(rec, schema)
	if err != nil {
		return nil, fmt.Errorf("marshal record: %w", err)
	}
	return &proto.RecordResponse{Record: s}, nil
}

func recordsToProto(recs []indexeddb.Record, schema *indexeddb.ObjectStoreSchema) (*proto.RecordsResponse, error) {
	structs := make([]*structpb.Struct, len(recs))
	for i, rec := range recs {
		s, err := structFromRecord(rec, schema)
		if err != nil {
			return nil, fmt.Errorf("marshal record %d: %w", i, err)
		}
		structs[i] = s
	}
	return &proto.RecordsResponse{Records: structs}, nil
}

func protoToSchema(ps *proto.ObjectStoreSchema) indexeddb.ObjectStoreSchema {
	if ps == nil {
		return indexeddb.ObjectStoreSchema{}
	}
	schema := indexeddb.ObjectStoreSchema{
		Indexes: make([]indexeddb.IndexSchema, len(ps.GetIndexes())),
		Columns: make([]indexeddb.ColumnDef, len(ps.GetColumns())),
	}
	for i, idx := range ps.GetIndexes() {
		schema.Indexes[i] = indexeddb.IndexSchema{
			Name: idx.GetName(), KeyPath: idx.GetKeyPath(), Unique: idx.GetUnique(),
		}
	}
	for i, col := range ps.GetColumns() {
		schema.Columns[i] = indexeddb.ColumnDef{
			Name: col.GetName(), Type: indexeddb.ColumnType(col.GetType()),
			PrimaryKey: col.GetPrimaryKey(), NotNull: col.GetNotNull(), Unique: col.GetUnique(),
		}
	}
	return schema
}

func protoToKeyRange(kr *proto.KeyRange, codec valueCodec) (*indexeddb.KeyRange, error) {
	return keyRangeFromProto(kr, codec)
}

func protoValuesToAny(vals []*structpb.Value, codecs []valueCodec) ([]any, error) {
	return anyFromProtoValues(vals, codecs)
}

func indexeddbToGRPCErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, indexeddb.ErrNotFound) {
		return status.Error(codes.NotFound, err.Error())
	}
	if errors.Is(err, indexeddb.ErrAlreadyExists) {
		return status.Error(codes.AlreadyExists, err.Error())
	}
	return status.Error(codes.Internal, err.Error())
}
