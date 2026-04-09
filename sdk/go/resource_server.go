package gestalt

import (
	"context"
	"fmt"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// --- Resource Runtime Server ---

type resourceRuntimeServer struct {
	proto.UnimplementedResourceRuntimeServer
	provider DatastoreProvider
}

func newResourceRuntimeServer(provider DatastoreProvider) *resourceRuntimeServer {
	return &resourceRuntimeServer{provider: provider}
}

func (s *resourceRuntimeServer) GetResourceMetadata(_ context.Context, _ *emptypb.Empty) (*proto.ResourceMetadata, error) {
	caps := s.provider.Capabilities()
	protoCaps := make([]proto.ResourceCapability, len(caps))
	for i, cap := range caps {
		protoCaps[i] = resourceCapabilityToProto(cap)
	}
	meta := &proto.ResourceMetadata{
		Capabilities:       protoCaps,
		MinProtocolVersion: proto.CurrentProtocolVersion,
		MaxProtocolVersion: proto.CurrentProtocolVersion,
	}
	if mp, ok := s.provider.(MetadataProvider); ok {
		pm := mp.Metadata()
		meta.Name = pm.Name
		meta.DisplayName = pm.DisplayName
		meta.Description = pm.Description
	}
	return meta, nil
}

func (s *resourceRuntimeServer) ConfigureResource(ctx context.Context, req *proto.ConfigureResourceRequest) (*proto.ConfigureResourceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.GetProtocolVersion() != proto.CurrentProtocolVersion {
		return nil, status.Errorf(
			codes.FailedPrecondition,
			"host requested protocol version %d, resource provider requires %d",
			req.GetProtocolVersion(),
			proto.CurrentProtocolVersion,
		)
	}
	config := req.GetConfig().AsMap()
	if config == nil {
		config = map[string]any{}
	}
	if err := s.provider.Configure(ctx, req.GetName(), config); err != nil {
		return nil, status.Errorf(codes.Unknown, "configure resource: %v", err)
	}
	return &proto.ConfigureResourceResponse{ProtocolVersion: proto.CurrentProtocolVersion}, nil
}

func (s *resourceRuntimeServer) HealthCheck(ctx context.Context, _ *emptypb.Empty) (*proto.ResourceHealthCheckResponse, error) {
	if err := s.provider.HealthCheck(ctx); err != nil {
		return &proto.ResourceHealthCheckResponse{
			Ready:   false,
			Message: err.Error(),
		}, nil
	}
	return &proto.ResourceHealthCheckResponse{Ready: true}, nil
}

// --- KeyValue Server ---

type kvResourceServer struct {
	proto.UnimplementedKeyValueResourceServer
	provider KeyValueDatastoreProvider
}

func newKVResourceServer(provider KeyValueDatastoreProvider) *kvResourceServer {
	return &kvResourceServer{provider: provider}
}

func (s *kvResourceServer) Get(ctx context.Context, req *proto.KVGetRequest) (*proto.KVGetResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	value, found, err := s.provider.KVGet(ctx, req.GetNamespace(), req.GetKey())
	if err != nil {
		return nil, providerRPCError("kv get", err)
	}
	return &proto.KVGetResponse{Value: value, Found: found}, nil
}

func (s *kvResourceServer) Put(ctx context.Context, req *proto.KVPutRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := s.provider.KVPut(ctx, req.GetNamespace(), req.GetKey(), req.GetValue(), req.GetTtlSeconds()); err != nil {
		return nil, providerRPCError("kv put", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *kvResourceServer) Delete(ctx context.Context, req *proto.KVDeleteRequest) (*proto.KVDeleteResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	deleted, err := s.provider.KVDelete(ctx, req.GetNamespace(), req.GetKey())
	if err != nil {
		return nil, providerRPCError("kv delete", err)
	}
	return &proto.KVDeleteResponse{Deleted: deleted}, nil
}

func (s *kvResourceServer) List(ctx context.Context, req *proto.KVListRequest) (*proto.KVListResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	entries, nextCursor, err := s.provider.KVList(ctx, req.GetNamespace(), req.GetPrefix(), req.GetCursor(), req.GetLimit())
	if err != nil {
		return nil, providerRPCError("kv list", err)
	}
	protoEntries := make([]*proto.KVEntry, len(entries))
	for i, e := range entries {
		protoEntries[i] = &proto.KVEntry{Key: e.Key, Value: e.Value}
	}
	return &proto.KVListResponse{Entries: protoEntries, NextCursor: nextCursor}, nil
}

func (s *kvResourceServer) Migrate(ctx context.Context, req *proto.KVMigrateRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := s.provider.KVMigrate(ctx, req.GetNamespace()); err != nil {
		return nil, providerRPCError("kv migrate", err)
	}
	return &emptypb.Empty{}, nil
}

// --- SQL Server ---

type sqlResourceServer struct {
	proto.UnimplementedSQLResourceServer
	provider SQLDatastoreProvider
}

func newSQLResourceServer(provider SQLDatastoreProvider) *sqlResourceServer {
	return &sqlResourceServer{provider: provider}
}

func (s *sqlResourceServer) Query(ctx context.Context, req *proto.SQLQueryRequest) (*proto.SQLQueryResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	params := sqlValuesFromProto(req.GetParams())
	rows, err := s.provider.SQLQuery(ctx, req.GetNamespace(), req.GetQuery(), params)
	if err != nil {
		return nil, providerRPCError("sql query", err)
	}
	return sqlRowsToProto(rows), nil
}

func (s *sqlResourceServer) Exec(ctx context.Context, req *proto.SQLExecRequest) (*proto.SQLExecResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	params := sqlValuesFromProto(req.GetParams())
	result, err := s.provider.SQLExec(ctx, req.GetNamespace(), req.GetQuery(), params)
	if err != nil {
		return nil, providerRPCError("sql exec", err)
	}
	return &proto.SQLExecResponse{RowsAffected: result.RowsAffected, LastInsertId: result.LastInsertID}, nil
}

func (s *sqlResourceServer) Migrate(ctx context.Context, req *proto.SQLMigrateRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	migrations := make([]SQLMigration, len(req.GetMigrations()))
	for i, m := range req.GetMigrations() {
		migrations[i] = SQLMigration{
			Version:     m.GetVersion(),
			Description: m.GetDescription(),
			UpSQL:       m.GetUpSql(),
		}
	}
	if err := s.provider.SQLMigrate(ctx, req.GetNamespace(), migrations); err != nil {
		return nil, providerRPCError("sql migrate", err)
	}
	return &emptypb.Empty{}, nil
}

// --- BlobStore Server ---

type blobResourceServer struct {
	proto.UnimplementedBlobStoreResourceServer
	provider BlobStoreDatastoreProvider
}

func newBlobResourceServer(provider BlobStoreDatastoreProvider) *blobResourceServer {
	return &blobResourceServer{provider: provider}
}

func (s *blobResourceServer) Get(ctx context.Context, req *proto.BlobGetRequest) (*proto.BlobGetResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	data, contentType, metadata, err := s.provider.BlobGet(ctx, req.GetNamespace(), req.GetKey())
	if err != nil {
		return nil, providerRPCError("blob get", err)
	}
	return &proto.BlobGetResponse{Data: data, ContentType: contentType, Metadata: metadata}, nil
}

func (s *blobResourceServer) Put(ctx context.Context, req *proto.BlobPutRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := s.provider.BlobPut(ctx, req.GetNamespace(), req.GetKey(), req.GetData(), req.GetContentType(), req.GetMetadata()); err != nil {
		return nil, providerRPCError("blob put", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *blobResourceServer) Delete(ctx context.Context, req *proto.BlobDeleteRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := s.provider.BlobDelete(ctx, req.GetNamespace(), req.GetKey()); err != nil {
		return nil, providerRPCError("blob delete", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *blobResourceServer) List(ctx context.Context, req *proto.BlobListRequest) (*proto.BlobListResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	entries, nextCursor, err := s.provider.BlobList(ctx, req.GetNamespace(), req.GetPrefix(), req.GetCursor(), req.GetLimit())
	if err != nil {
		return nil, providerRPCError("blob list", err)
	}
	protoEntries := make([]*proto.BlobEntry, len(entries))
	for i, e := range entries {
		pe := &proto.BlobEntry{
			Key:         e.Key,
			Size:        e.Size,
			ContentType: e.ContentType,
		}
		if !e.LastModified.IsZero() {
			pe.LastModified = timestamppb.New(e.LastModified)
		}
		protoEntries[i] = pe
	}
	return &proto.BlobListResponse{Entries: protoEntries, NextCursor: nextCursor}, nil
}

// --- Conversion helpers ---

func resourceCapabilityToProto(cap DatastoreCapability) proto.ResourceCapability {
	switch cap {
	case CapabilityKeyValue:
		return proto.ResourceCapability_RESOURCE_CAPABILITY_KEY_VALUE
	case CapabilitySQL:
		return proto.ResourceCapability_RESOURCE_CAPABILITY_SQL
	case CapabilityBlobStore:
		return proto.ResourceCapability_RESOURCE_CAPABILITY_BLOB_STORE
	default:
		return proto.ResourceCapability_RESOURCE_CAPABILITY_UNSPECIFIED
	}
}

func sqlValuesFromProto(values []*proto.SQLValue) []SQLValue {
	out := make([]SQLValue, len(values))
	for i, v := range values {
		out[i] = sqlValueFromProto(v)
	}
	return out
}

func sqlValueFromProto(v *proto.SQLValue) SQLValue {
	if v == nil {
		return SQLValue{Kind: SQLValueNull}
	}
	switch k := v.GetKind().(type) {
	case *proto.SQLValue_StringValue:
		return SQLValue{Kind: SQLValueString, Value: k.StringValue}
	case *proto.SQLValue_IntValue:
		return SQLValue{Kind: SQLValueInt, Value: k.IntValue}
	case *proto.SQLValue_DoubleValue:
		return SQLValue{Kind: SQLValueDouble, Value: k.DoubleValue}
	case *proto.SQLValue_BoolValue:
		return SQLValue{Kind: SQLValueBool, Value: k.BoolValue}
	case *proto.SQLValue_BytesValue:
		return SQLValue{Kind: SQLValueBytes, Value: k.BytesValue}
	case *proto.SQLValue_IsNull:
		return SQLValue{Kind: SQLValueNull}
	default:
		return SQLValue{Kind: SQLValueNull}
	}
}

func sqlRowsToProto(rows *SQLRows) *proto.SQLQueryResponse {
	if rows == nil {
		return &proto.SQLQueryResponse{}
	}
	protoRows := make([]*proto.SQLRow, len(rows.Rows))
	for i, row := range rows.Rows {
		vals := make([]*proto.SQLValue, len(row))
		for j, v := range row {
			vals[j] = anyToProtoSQLValue(v)
		}
		protoRows[i] = &proto.SQLRow{Values: vals}
	}
	return &proto.SQLQueryResponse{Columns: rows.Columns, Rows: protoRows}
}

func anyToProtoSQLValue(v any) *proto.SQLValue {
	if v == nil {
		return &proto.SQLValue{Kind: &proto.SQLValue_IsNull{IsNull: true}}
	}
	switch val := v.(type) {
	case string:
		return &proto.SQLValue{Kind: &proto.SQLValue_StringValue{StringValue: val}}
	case int:
		return &proto.SQLValue{Kind: &proto.SQLValue_IntValue{IntValue: int64(val)}}
	case int32:
		return &proto.SQLValue{Kind: &proto.SQLValue_IntValue{IntValue: int64(val)}}
	case int64:
		return &proto.SQLValue{Kind: &proto.SQLValue_IntValue{IntValue: val}}
	case float32:
		return &proto.SQLValue{Kind: &proto.SQLValue_DoubleValue{DoubleValue: float64(val)}}
	case float64:
		return &proto.SQLValue{Kind: &proto.SQLValue_DoubleValue{DoubleValue: val}}
	case bool:
		return &proto.SQLValue{Kind: &proto.SQLValue_BoolValue{BoolValue: val}}
	case []byte:
		return &proto.SQLValue{Kind: &proto.SQLValue_BytesValue{BytesValue: val}}
	default:
		return &proto.SQLValue{Kind: &proto.SQLValue_StringValue{StringValue: fmt.Sprintf("%v", val)}}
	}
}

