package providerhost

import (
	"context"
	"errors"
	"fmt"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type indexedDBServer struct {
	proto.UnimplementedIndexedDBServer
	ds      indexeddb.IndexedDB
	db      string
	plugin  string
	allowed map[string]struct{}
}

type IndexedDBServerOptions struct {
	AllowedStores []string
}

func NewIndexedDBServer(ds indexeddb.IndexedDB, pluginName string, opts IndexedDBServerOptions) proto.IndexedDBServer {
	allowed := make(map[string]struct{}, len(opts.AllowedStores))
	for _, store := range opts.AllowedStores {
		allowed[store] = struct{}{}
	}
	if len(allowed) == 0 {
		allowed = nil
	}
	return &indexedDBServer{
		ds:      ds,
		db:      metricutil.IndexedDBName(ds),
		plugin:  pluginName,
		allowed: allowed,
	}
}

func (s *indexedDBServer) storeName(name string) string {
	return name
}

func (s *indexedDBServer) ensureAllowedStore(name string) error {
	if len(s.allowed) == 0 {
		return nil
	}
	if _, ok := s.allowed[name]; ok {
		return nil
	}
	return indexeddb.ErrNotFound
}

func (s *indexedDBServer) objectStore(name string) (indexeddb.ObjectStore, error) {
	if err := s.ensureAllowedStore(name); err != nil {
		return nil, err
	}
	return metricutil.InstrumentObjectStore(
		metricutil.UnwrapIndexedDB(s.ds).ObjectStore(s.storeName(name)),
		metricutil.IndexedDBMetricLabels{
			DB:          s.db,
			Plugin:      s.plugin,
			ObjectStore: name,
		},
	), nil
}

func (s *indexedDBServer) CreateObjectStore(ctx context.Context, req *proto.CreateObjectStoreRequest) (*emptypb.Empty, error) {
	if err := s.ensureAllowedStore(req.GetName()); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	schema := protoToSchema(req.GetSchema())
	if err := s.ds.CreateObjectStore(ctx, s.storeName(req.GetName()), schema); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) DeleteObjectStore(ctx context.Context, req *proto.DeleteObjectStoreRequest) (*emptypb.Empty, error) {
	if err := s.ensureAllowedStore(req.GetName()); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	if err := s.ds.DeleteObjectStore(ctx, s.storeName(req.GetName())); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) Get(ctx context.Context, req *proto.ObjectStoreRequest) (*proto.RecordResponse, error) {
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	rec, err := store.Get(ctx, req.GetId())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return recordToProto(rec)
}

func (s *indexedDBServer) GetKey(ctx context.Context, req *proto.ObjectStoreRequest) (*proto.KeyResponse, error) {
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	key, err := store.GetKey(ctx, req.GetId())
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
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	if err := store.Add(ctx, record); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) Put(ctx context.Context, req *proto.RecordRequest) (*emptypb.Empty, error) {
	record, err := gestalt.RecordFromProto(req.GetRecord())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal record: %v", err)
	}
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	if err := store.Put(ctx, record); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) Delete(ctx context.Context, req *proto.ObjectStoreRequest) (*emptypb.Empty, error) {
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	if err := store.Delete(ctx, req.GetId()); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) Clear(ctx context.Context, req *proto.ObjectStoreNameRequest) (*emptypb.Empty, error) {
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	if err := store.Clear(ctx); err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *indexedDBServer) GetAll(ctx context.Context, req *proto.ObjectStoreRangeRequest) (*proto.RecordsResponse, error) {
	keyRange, err := protoToKeyRange(req.Range)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal key range: %v", err)
	}
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	recs, err := store.GetAll(ctx, keyRange)
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
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	keys, err := store.GetAllKeys(ctx, keyRange)
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
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	count, err := store.Count(ctx, keyRange)
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
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	deleted, err := store.DeleteRange(ctx, *kr)
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
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	rec, err := store.Index(req.GetIndex()).Get(ctx, values...)
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
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	key, err := store.Index(req.GetIndex()).GetKey(ctx, values...)
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
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	recs, err := store.Index(req.GetIndex()).GetAll(ctx, keyRange, values...)
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
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	keys, err := store.Index(req.GetIndex()).GetAllKeys(ctx, keyRange, values...)
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
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	count, err := store.Index(req.GetIndex()).Count(ctx, keyRange, values...)
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
	store, err := s.objectStore(req.GetStore())
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	deleted, err := store.Index(req.GetIndex()).Delete(ctx, values...)
	if err != nil {
		return nil, indexeddbToGRPCErr(err)
	}
	return &proto.DeleteResponse{Deleted: deleted}, nil
}

func protoCursorDirection(d proto.CursorDirection) indexeddb.CursorDirection {
	switch d {
	case proto.CursorDirection_CURSOR_NEXT_UNIQUE:
		return indexeddb.CursorNextUnique
	case proto.CursorDirection_CURSOR_PREV:
		return indexeddb.CursorPrev
	case proto.CursorDirection_CURSOR_PREV_UNIQUE:
		return indexeddb.CursorPrevUnique
	default:
		return indexeddb.CursorNext
	}
}

func (s *indexedDBServer) OpenCursor(stream proto.IndexedDB_OpenCursorServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	openReq := first.GetOpen()
	if openReq == nil {
		return status.Error(codes.InvalidArgument, "first message must be OpenCursorRequest")
	}

	keyRange, err := protoToKeyRange(openReq.Range)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "unmarshal key range: %v", err)
	}
	dir := protoCursorDirection(openReq.GetDirection())
	ctx := stream.Context()

	var cursor indexeddb.Cursor
	store, err := s.objectStore(openReq.GetStore())
	if err != nil {
		return indexeddbToGRPCErr(err)
	}

	if openReq.GetIndex() != "" {
		values, vErr := protoValuesToAny(openReq.GetValues())
		if vErr != nil {
			return status.Errorf(codes.InvalidArgument, "unmarshal index values: %v", vErr)
		}
		idx := store.Index(openReq.GetIndex())
		if openReq.GetKeysOnly() {
			cursor, err = idx.OpenKeyCursor(ctx, keyRange, dir, values...)
		} else {
			cursor, err = idx.OpenCursor(ctx, keyRange, dir, values...)
		}
	} else {
		if openReq.GetKeysOnly() {
			cursor, err = store.OpenKeyCursor(ctx, keyRange, dir)
		} else {
			cursor, err = store.OpenCursor(ctx, keyRange, dir)
		}
	}
	if err != nil {
		return indexeddbToGRPCErr(err)
	}
	defer func() { _ = cursor.Close() }()

	// Send an open ack so clients can detect failures synchronously.
	if sErr := stream.Send(&proto.CursorResponse{Result: &proto.CursorResponse_Done{}}); sErr != nil {
		return sErr
	}

	for {
		msg, recvErr := stream.Recv()
		if recvErr != nil {
			return recvErr
		}
		cmd := msg.GetCommand()
		if cmd == nil {
			return status.Error(codes.InvalidArgument, "expected CursorCommand after open")
		}

		switch v := cmd.GetCommand().(type) {
		case *proto.CursorCommand_Next:
			if !cursor.Continue() {
				if cErr := cursor.Err(); cErr != nil {
					return indexeddbToGRPCErr(cErr)
				}
				if sErr := stream.Send(&proto.CursorResponse{Result: &proto.CursorResponse_Done{Done: true}}); sErr != nil {
					return sErr
				}
				continue
			}
			entry, eErr := cursorEntryToProto(cursor, openReq.GetKeysOnly())
			if eErr != nil {
				return eErr
			}
			if sErr := stream.Send(&proto.CursorResponse{Result: &proto.CursorResponse_Entry{Entry: entry}}); sErr != nil {
				return sErr
			}

		case *proto.CursorCommand_ContinueToKey:
			target := v.ContinueToKey.GetKey()
			if len(target) == 0 {
				return status.Error(codes.InvalidArgument, "continue key is required")
			}
			parts, kErr := gestalt.KeyValuesToAny(target)
			if kErr != nil {
				return status.Errorf(codes.InvalidArgument, "unmarshal continue key: %v", kErr)
			}
			var key any
			switch {
			case openReq.GetIndex() != "":
				key = parts
			case len(parts) == 1:
				key = parts[0]
			default:
				key = parts
			}
			if !cursor.ContinueToKey(key) {
				if cErr := cursor.Err(); cErr != nil {
					return indexeddbToGRPCErr(cErr)
				}
				if sErr := stream.Send(&proto.CursorResponse{Result: &proto.CursorResponse_Done{Done: true}}); sErr != nil {
					return sErr
				}
				continue
			}
			entry, eErr := cursorEntryToProto(cursor, openReq.GetKeysOnly())
			if eErr != nil {
				return eErr
			}
			if sErr := stream.Send(&proto.CursorResponse{Result: &proto.CursorResponse_Entry{Entry: entry}}); sErr != nil {
				return sErr
			}

		case *proto.CursorCommand_Advance:
			if v.Advance <= 0 {
				return status.Error(codes.InvalidArgument, "advance count must be positive")
			}
			if !cursor.Advance(int(v.Advance)) {
				if cErr := cursor.Err(); cErr != nil {
					return indexeddbToGRPCErr(cErr)
				}
				if sErr := stream.Send(&proto.CursorResponse{Result: &proto.CursorResponse_Done{Done: true}}); sErr != nil {
					return sErr
				}
				continue
			}
			entry, eErr := cursorEntryToProto(cursor, openReq.GetKeysOnly())
			if eErr != nil {
				return eErr
			}
			if sErr := stream.Send(&proto.CursorResponse{Result: &proto.CursorResponse_Entry{Entry: entry}}); sErr != nil {
				return sErr
			}

		case *proto.CursorCommand_Delete:
			if dErr := cursor.Delete(); dErr != nil {
				return indexeddbToGRPCErr(dErr)
			}
			if sErr := stream.Send(&proto.CursorResponse{Result: &proto.CursorResponse_Done{}}); sErr != nil {
				return sErr
			}

		case *proto.CursorCommand_Update:
			rec, rErr := gestalt.RecordFromProto(v.Update)
			if rErr != nil {
				return status.Errorf(codes.InvalidArgument, "unmarshal update record: %v", rErr)
			}
			if uErr := cursor.Update(rec); uErr != nil {
				return indexeddbToGRPCErr(uErr)
			}
			entry, eErr := cursorEntryToProto(cursor, openReq.GetKeysOnly())
			if eErr != nil {
				return eErr
			}
			if sErr := stream.Send(&proto.CursorResponse{Result: &proto.CursorResponse_Entry{Entry: entry}}); sErr != nil {
				return sErr
			}

		case *proto.CursorCommand_Close:
			return nil

		default:
			return status.Error(codes.InvalidArgument, "unknown cursor command")
		}
	}
}

func cursorEntryToProto(c indexeddb.Cursor, keysOnly bool) (*proto.CursorEntry, error) {
	entry := &proto.CursorEntry{PrimaryKey: c.PrimaryKey()}
	key := c.Key()
	if key != nil {
		if parts, ok := key.([]any); ok {
			kvs := make([]*proto.KeyValue, len(parts))
			for i, p := range parts {
				kv, err := gestalt.AnyToKeyValue(p)
				if err != nil {
					return nil, fmt.Errorf("marshal cursor key[%d]: %w", i, err)
				}
				kvs[i] = kv
			}
			entry.Key = kvs
		} else {
			kv, err := gestalt.AnyToKeyValue(key)
			if err != nil {
				return nil, fmt.Errorf("marshal cursor key: %w", err)
			}
			entry.Key = []*proto.KeyValue{kv}
		}
	}
	if !keysOnly {
		rec, err := c.Value()
		if err != nil {
			return nil, fmt.Errorf("cursor value: %w", err)
		}
		pbRec, err := gestalt.RecordToProto(rec)
		if err != nil {
			return nil, fmt.Errorf("marshal cursor record: %w", err)
		}
		entry.Record = pbRec
	}
	return entry, nil
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
