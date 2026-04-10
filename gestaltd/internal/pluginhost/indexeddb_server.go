package pluginhost

import (
	"context"
	"errors"
	"fmt"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core/datastore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

type indexedDBServer struct {
	proto.UnimplementedIndexedDBServer
	ds     datastore.Datastore
	prefix string
}

func NewIndexedDBServer(ds datastore.Datastore, pluginName string) proto.IndexedDBServer {
	return &indexedDBServer{ds: ds, prefix: "plugin_" + pluginName + "_"}
}

func (s *indexedDBServer) storeName(name string) string {
	return s.prefix + name
}

func (s *indexedDBServer) CreateObjectStore(ctx context.Context, req *proto.CreateObjectStoreRequest) (*emptypb.Empty, error) {
	schema := protoToSchema(req.GetSchema())
	if err := s.ds.CreateObjectStore(ctx, s.storeName(req.GetName()), schema); err != nil {
		return nil, datastoreToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) DeleteObjectStore(ctx context.Context, req *proto.DeleteObjectStoreRequest) (*emptypb.Empty, error) {
	if err := s.ds.DeleteObjectStore(ctx, s.storeName(req.GetName())); err != nil {
		return nil, datastoreToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) Get(ctx context.Context, req *proto.ObjectStoreRequest) (*proto.RecordResponse, error) {
	rec, err := s.ds.ObjectStore(s.storeName(req.GetStore())).Get(ctx, req.GetId())
	if err != nil {
		return nil, datastoreToGRPCErr(err)
	}
	return recordToProto(rec)
}

func (s *indexedDBServer) GetKey(ctx context.Context, req *proto.ObjectStoreRequest) (*proto.KeyResponse, error) {
	key, err := s.ds.ObjectStore(s.storeName(req.GetStore())).GetKey(ctx, req.GetId())
	if err != nil {
		return nil, datastoreToGRPCErr(err)
	}
	return &proto.KeyResponse{Key: key}, nil
}

func (s *indexedDBServer) Add(ctx context.Context, req *proto.RecordRequest) (*emptypb.Empty, error) {
	if err := s.ds.ObjectStore(s.storeName(req.GetStore())).Add(ctx, req.GetRecord().AsMap()); err != nil {
		return nil, datastoreToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) Put(ctx context.Context, req *proto.RecordRequest) (*emptypb.Empty, error) {
	if err := s.ds.ObjectStore(s.storeName(req.GetStore())).Put(ctx, req.GetRecord().AsMap()); err != nil {
		return nil, datastoreToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) Delete(ctx context.Context, req *proto.ObjectStoreRequest) (*emptypb.Empty, error) {
	if err := s.ds.ObjectStore(s.storeName(req.GetStore())).Delete(ctx, req.GetId()); err != nil {
		return nil, datastoreToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) Clear(ctx context.Context, req *proto.ObjectStoreNameRequest) (*emptypb.Empty, error) {
	if err := s.ds.ObjectStore(s.storeName(req.GetStore())).Clear(ctx); err != nil {
		return nil, datastoreToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) GetAll(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.RecordsResponse, error) {
	recs, err := s.ds.ObjectStore(s.storeName(req.GetStore())).GetAll(ctx, protoToKeyRange(req.Range))
	if err != nil {
		return nil, datastoreToGRPCErr(err)
	}
	return recordsToProto(recs)
}

func (s *indexedDBServer) GetAllKeys(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.KeysResponse, error) {
	keys, err := s.ds.ObjectStore(s.storeName(req.GetStore())).GetAllKeys(ctx, protoToKeyRange(req.Range))
	if err != nil {
		return nil, datastoreToGRPCErr(err)
	}
	return &proto.KeysResponse{Keys: keys}, nil
}

func (s *indexedDBServer) Count(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.CountResponse, error) {
	count, err := s.ds.ObjectStore(s.storeName(req.GetStore())).Count(ctx, protoToKeyRange(req.Range))
	if err != nil {
		return nil, datastoreToGRPCErr(err)
	}
	return &proto.CountResponse{Count: count}, nil
}

func (s *indexedDBServer) DeleteRange(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.DeleteResponse, error) {
	kr := protoToKeyRange(req.Range)
	if kr == nil {
		return nil, status.Error(codes.InvalidArgument, "key range is required for DeleteRange")
	}
	deleted, err := s.ds.ObjectStore(s.storeName(req.GetStore())).DeleteRange(ctx, *kr)
	if err != nil {
		return nil, datastoreToGRPCErr(err)
	}
	return &proto.DeleteResponse{Deleted: deleted}, nil
}

func (s *indexedDBServer) IndexGet(ctx context.Context, req *proto.IndexQueryRequest) (*proto.RecordResponse, error) {
	rec, err := s.ds.ObjectStore(s.storeName(req.GetStore())).Index(req.GetIndex()).Get(ctx, protoValuesToAny(req.GetValues())...)
	if err != nil {
		return nil, datastoreToGRPCErr(err)
	}
	return recordToProto(rec)
}

func (s *indexedDBServer) IndexGetKey(ctx context.Context, req *proto.IndexQueryRequest) (*proto.KeyResponse, error) {
	key, err := s.ds.ObjectStore(s.storeName(req.GetStore())).Index(req.GetIndex()).GetKey(ctx, protoValuesToAny(req.GetValues())...)
	if err != nil {
		return nil, datastoreToGRPCErr(err)
	}
	return &proto.KeyResponse{Key: key}, nil
}

func (s *indexedDBServer) IndexGetAll(ctx context.Context, req *proto.IndexQueryRequest) (*proto.RecordsResponse, error) {
	recs, err := s.ds.ObjectStore(s.storeName(req.GetStore())).Index(req.GetIndex()).GetAll(ctx, protoToKeyRange(req.Range), protoValuesToAny(req.GetValues())...)
	if err != nil {
		return nil, datastoreToGRPCErr(err)
	}
	return recordsToProto(recs)
}

func (s *indexedDBServer) IndexGetAllKeys(ctx context.Context, req *proto.IndexQueryRequest) (*proto.KeysResponse, error) {
	keys, err := s.ds.ObjectStore(s.storeName(req.GetStore())).Index(req.GetIndex()).GetAllKeys(ctx, protoToKeyRange(req.Range), protoValuesToAny(req.GetValues())...)
	if err != nil {
		return nil, datastoreToGRPCErr(err)
	}
	return &proto.KeysResponse{Keys: keys}, nil
}

func (s *indexedDBServer) IndexCount(ctx context.Context, req *proto.IndexQueryRequest) (*proto.CountResponse, error) {
	count, err := s.ds.ObjectStore(s.storeName(req.GetStore())).Index(req.GetIndex()).Count(ctx, protoToKeyRange(req.Range), protoValuesToAny(req.GetValues())...)
	if err != nil {
		return nil, datastoreToGRPCErr(err)
	}
	return &proto.CountResponse{Count: count}, nil
}

func (s *indexedDBServer) IndexDelete(ctx context.Context, req *proto.IndexQueryRequest) (*proto.DeleteResponse, error) {
	deleted, err := s.ds.ObjectStore(s.storeName(req.GetStore())).Index(req.GetIndex()).Delete(ctx, protoValuesToAny(req.GetValues())...)
	if err != nil {
		return nil, datastoreToGRPCErr(err)
	}
	return &proto.DeleteResponse{Deleted: deleted}, nil
}

func recordToProto(rec datastore.Record) (*proto.RecordResponse, error) {
	s, err := structpb.NewStruct(rec)
	if err != nil {
		return nil, fmt.Errorf("marshal record: %w", err)
	}
	return &proto.RecordResponse{Record: s}, nil
}

func recordsToProto(recs []datastore.Record) (*proto.RecordsResponse, error) {
	structs := make([]*structpb.Struct, len(recs))
	for i, rec := range recs {
		s, err := structpb.NewStruct(rec)
		if err != nil {
			return nil, fmt.Errorf("marshal record %d: %w", i, err)
		}
		structs[i] = s
	}
	return &proto.RecordsResponse{Records: structs}, nil
}

func protoToSchema(ps *proto.ObjectStoreSchema) datastore.ObjectStoreSchema {
	if ps == nil {
		return datastore.ObjectStoreSchema{}
	}
	schema := datastore.ObjectStoreSchema{
		Indexes: make([]datastore.IndexSchema, len(ps.GetIndexes())),
		Columns: make([]datastore.ColumnDef, len(ps.GetColumns())),
	}
	for i, idx := range ps.GetIndexes() {
		schema.Indexes[i] = datastore.IndexSchema{
			Name: idx.GetName(), KeyPath: idx.GetKeyPath(), Unique: idx.GetUnique(),
		}
	}
	for i, col := range ps.GetColumns() {
		schema.Columns[i] = datastore.ColumnDef{
			Name: col.GetName(), Type: datastore.ColumnType(col.GetType()),
			PrimaryKey: col.GetPrimaryKey(), NotNull: col.GetNotNull(), Unique: col.GetUnique(),
		}
	}
	return schema
}

func protoToKeyRange(kr *proto.KeyRange) *datastore.KeyRange {
	if kr == nil {
		return nil
	}
	r := &datastore.KeyRange{
		LowerOpen: kr.GetLowerOpen(),
		UpperOpen: kr.GetUpperOpen(),
	}
	if kr.GetLower() != nil {
		r.Lower = kr.GetLower().AsInterface()
	}
	if kr.GetUpper() != nil {
		r.Upper = kr.GetUpper().AsInterface()
	}
	return r
}

func protoValuesToAny(vals []*structpb.Value) []any {
	out := make([]any, len(vals))
	for i, v := range vals {
		out[i] = v.AsInterface()
	}
	return out
}

func datastoreToGRPCErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, datastore.ErrNotFound) {
		return status.Error(codes.NotFound, err.Error())
	}
	if errors.Is(err, datastore.ErrAlreadyExists) {
		return status.Error(codes.AlreadyExists, err.Error())
	}
	return status.Error(codes.Internal, err.Error())
}
