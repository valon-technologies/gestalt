package pluginhost

import (
	"context"
	"errors"
	"fmt"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type indexedDBServer struct {
	proto.UnimplementedIndexedDBServer
	ds     indexeddb.IndexedDB
	prefix string
}

func NewIndexedDBServer(ds indexeddb.IndexedDB, pluginName string) proto.IndexedDBServer {
	return &indexedDBServer{ds: ds, prefix: "plugin_" + pluginName + "_"}
}

func (s *indexedDBServer) storeName(name string) string {
	return s.prefix + name
}

func (s *indexedDBServer) CreateObjectStore(ctx context.Context, req *proto.CreateObjectStoreRequest) (*emptypb.Empty, error) {
	schema := protoToSchema(req.GetSchema())
	if err := s.ds.CreateObjectStore(ctx, s.storeName(req.GetName()), schema); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) DeleteObjectStore(ctx context.Context, req *proto.DeleteObjectStoreRequest) (*emptypb.Empty, error) {
	if err := s.ds.DeleteObjectStore(ctx, s.storeName(req.GetName())); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) Get(ctx context.Context, req *proto.ObjectStoreRequest) (*proto.RecordResponse, error) {
	rec, err := s.ds.ObjectStore(s.storeName(req.GetStore())).Get(ctx, req.GetId())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return recordToProto(rec)
}

func (s *indexedDBServer) GetKey(ctx context.Context, req *proto.ObjectStoreRequest) (*proto.KeyResponse, error) {
	key, err := s.ds.ObjectStore(s.storeName(req.GetStore())).GetKey(ctx, req.GetId())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.KeyResponse{Key: key}, nil
}

func (s *indexedDBServer) Add(ctx context.Context, req *proto.RecordRequest) (*emptypb.Empty, error) {
	record, err := gestalt.RecordFromProto(req.GetRecord())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal record: %v", err)
	}
	if err := s.ds.ObjectStore(s.storeName(req.GetStore())).Add(ctx, record); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) Put(ctx context.Context, req *proto.RecordRequest) (*emptypb.Empty, error) {
	record, err := gestalt.RecordFromProto(req.GetRecord())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal record: %v", err)
	}
	if err := s.ds.ObjectStore(s.storeName(req.GetStore())).Put(ctx, record); err != nil {
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
	keyRange, err := protoToKeyRange(req.Range)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal key range: %v", err)
	}
	recs, err := s.ds.ObjectStore(s.storeName(req.GetStore())).GetAll(ctx, keyRange)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return recordsToProto(recs)
}

func (s *indexedDBServer) GetAllKeys(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.KeysResponse, error) {
	keyRange, err := protoToKeyRange(req.Range)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal key range: %v", err)
	}
	keys, err := s.ds.ObjectStore(s.storeName(req.GetStore())).GetAllKeys(ctx, keyRange)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.KeysResponse{Keys: keys}, nil
}

func (s *indexedDBServer) Count(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.CountResponse, error) {
	keyRange, err := protoToKeyRange(req.Range)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal key range: %v", err)
	}
	count, err := s.ds.ObjectStore(s.storeName(req.GetStore())).Count(ctx, keyRange)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.CountResponse{Count: count}, nil
}

func (s *indexedDBServer) DeleteRange(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.DeleteResponse, error) {
	kr, err := protoToKeyRange(req.Range)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal key range: %v", err)
	}
	if kr == nil {
		return nil, status.Error(codes.InvalidArgument, "key range is required for DeleteRange")
	}
	deleted, err := s.ds.ObjectStore(s.storeName(req.GetStore())).DeleteRange(ctx, *kr)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.DeleteResponse{Deleted: deleted}, nil
}

func (s *indexedDBServer) IndexGet(ctx context.Context, req *proto.IndexQueryRequest) (*proto.RecordResponse, error) {
	values, err := protoValuesToAny(req.GetValues())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal index values: %v", err)
	}
	rec, err := s.ds.ObjectStore(s.storeName(req.GetStore())).Index(req.GetIndex()).Get(ctx, values...)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return recordToProto(rec)
}

func (s *indexedDBServer) IndexGetKey(ctx context.Context, req *proto.IndexQueryRequest) (*proto.KeyResponse, error) {
	values, err := protoValuesToAny(req.GetValues())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal index values: %v", err)
	}
	key, err := s.ds.ObjectStore(s.storeName(req.GetStore())).Index(req.GetIndex()).GetKey(ctx, values...)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.KeyResponse{Key: key}, nil
}

func (s *indexedDBServer) IndexGetAll(ctx context.Context, req *proto.IndexQueryRequest) (*proto.RecordsResponse, error) {
	keyRange, err := protoToKeyRange(req.Range)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal key range: %v", err)
	}
	values, err := protoValuesToAny(req.GetValues())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal index values: %v", err)
	}
	recs, err := s.ds.ObjectStore(s.storeName(req.GetStore())).Index(req.GetIndex()).GetAll(ctx, keyRange, values...)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return recordsToProto(recs)
}

func (s *indexedDBServer) IndexGetAllKeys(ctx context.Context, req *proto.IndexQueryRequest) (*proto.KeysResponse, error) {
	keyRange, err := protoToKeyRange(req.Range)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal key range: %v", err)
	}
	values, err := protoValuesToAny(req.GetValues())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal index values: %v", err)
	}
	keys, err := s.ds.ObjectStore(s.storeName(req.GetStore())).Index(req.GetIndex()).GetAllKeys(ctx, keyRange, values...)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.KeysResponse{Keys: keys}, nil
}

func (s *indexedDBServer) IndexCount(ctx context.Context, req *proto.IndexQueryRequest) (*proto.CountResponse, error) {
	keyRange, err := protoToKeyRange(req.Range)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal key range: %v", err)
	}
	values, err := protoValuesToAny(req.GetValues())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal index values: %v", err)
	}
	count, err := s.ds.ObjectStore(s.storeName(req.GetStore())).Index(req.GetIndex()).Count(ctx, keyRange, values...)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.CountResponse{Count: count}, nil
}

func (s *indexedDBServer) IndexDelete(ctx context.Context, req *proto.IndexQueryRequest) (*proto.DeleteResponse, error) {
	values, err := protoValuesToAny(req.GetValues())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal index values: %v", err)
	}
	deleted, err := s.ds.ObjectStore(s.storeName(req.GetStore())).Index(req.GetIndex()).Delete(ctx, values...)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.DeleteResponse{Deleted: deleted}, nil
}

func recordToProto(rec indexeddb.Record) (*proto.RecordResponse, error) {
	pbRecord, err := gestalt.RecordToProto(rec)
	if err != nil {
		return nil, fmt.Errorf("marshal record: %w", err)
	}
	return &proto.RecordResponse{Record: pbRecord}, nil
}

func recordsToProto(recs []indexeddb.Record) (*proto.RecordsResponse, error) {
	pbRecords, err := gestalt.RecordsToProto(recs)
	if err != nil {
		return nil, err
	}
	return &proto.RecordsResponse{Records: pbRecords}, nil
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

func protoToKeyRange(kr *proto.KeyRange) (*indexeddb.KeyRange, error) {
	if kr == nil {
		return nil, nil
	}
	r := &indexeddb.KeyRange{
		LowerOpen: kr.GetLowerOpen(),
		UpperOpen: kr.GetUpperOpen(),
	}
	if kr.GetLower() != nil {
		value, err := gestalt.AnyFromTypedValue(kr.GetLower())
		if err != nil {
			return nil, fmt.Errorf("key range lower: %w", err)
		}
		r.Lower = value
	}
	if kr.GetUpper() != nil {
		value, err := gestalt.AnyFromTypedValue(kr.GetUpper())
		if err != nil {
			return nil, fmt.Errorf("key range upper: %w", err)
		}
		r.Upper = value
	}
	return r, nil
}

func protoValuesToAny(vals []*proto.TypedValue) ([]any, error) {
	return gestalt.AnyFromTypedValues(vals)
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
