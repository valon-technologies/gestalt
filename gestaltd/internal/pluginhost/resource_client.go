package pluginhost

import (
	"context"
	"fmt"
	"io"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ResourceExecConfig holds the parameters for launching a resource provider
// plugin as a child process.
type ResourceExecConfig struct {
	Command      string
	Args         []string
	Env          map[string]string
	Config       map[string]any
	AllowedHosts []string
	HostBinary   string
	Cleanup      func()
	Name         string
}

// RemoteResourceProvider is the host-side representation of a resource plugin
// process. It implements [core.ResourceProvider] and exposes typed gRPC clients
// for each capability.
type RemoteResourceProvider struct {
	runtime      proto.ResourceRuntimeClient
	kvClient     proto.KeyValueResourceClient
	sqlClient    proto.SQLResourceClient
	blobClient   proto.BlobStoreResourceClient
	name         string
	capabilities []core.ResourceCapability
	closer       io.Closer
}

// NewExecutableResource launches a resource provider plugin process, configures
// it, and returns a [core.ResourceProvider].
func NewExecutableResource(ctx context.Context, cfg ResourceExecConfig) (core.ResourceProvider, error) {
	proc, err := startPluginProcess(ctx, ExecConfig{
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

	runtimeClient := proto.NewResourceRuntimeClient(proc.conn)
	res, err := newRemoteResource(ctx, proc.conn, runtimeClient, cfg.Name, cfg.Config)
	if err != nil {
		_ = proc.Close()
		return nil, err
	}
	res.closer = proc
	return res, nil
}

func newRemoteResource(ctx context.Context, conn *grpc.ClientConn, runtimeClient proto.ResourceRuntimeClient, name string, config map[string]any) (*RemoteResourceProvider, error) {
	res := &RemoteResourceProvider{
		runtime: runtimeClient,
	}
	if err := res.configure(ctx, runtimeClient, name, config); err != nil {
		return nil, err
	}
	for _, cap := range res.capabilities {
		switch cap {
		case core.ResourceCapabilityKeyValue:
			res.kvClient = proto.NewKeyValueResourceClient(conn)
		case core.ResourceCapabilitySQL:
			res.sqlClient = proto.NewSQLResourceClient(conn)
		case core.ResourceCapabilityBlobStore:
			res.blobClient = proto.NewBlobStoreResourceClient(conn)
		}
	}
	return res, nil
}

func (r *RemoteResourceProvider) configure(ctx context.Context, runtimeClient proto.ResourceRuntimeClient, name string, config map[string]any) error {
	metaCtx, cancel := pluginConfigureContext(ctx)
	defer cancel()
	meta, err := runtimeClient.GetResourceMetadata(metaCtx, &emptypb.Empty{})
	if err != nil {
		return fmt.Errorf("get resource metadata: %w", err)
	}

	if err := validateResourceProtocol(meta); err != nil {
		return err
	}

	cfgStruct, err := structFromMap(config)
	if err != nil {
		return fmt.Errorf("encode resource config: %w", err)
	}
	cfgCtx, cfgCancel := pluginConfigureContext(ctx)
	defer cfgCancel()
	cfgResp, err := runtimeClient.ConfigureResource(cfgCtx, &proto.ConfigureResourceRequest{
		Name:            name,
		Config:          cfgStruct,
		ProtocolVersion: proto.CurrentProtocolVersion,
	})
	if err != nil {
		return fmt.Errorf("configure resource %q: %w", name, err)
	}
	if cfgResp.GetProtocolVersion() != proto.CurrentProtocolVersion {
		return fmt.Errorf("resource %q: protocol version mismatch: got %d, want %d",
			name, cfgResp.GetProtocolVersion(), proto.CurrentProtocolVersion)
	}

	r.name = name
	if meta.GetName() != "" {
		r.name = meta.GetName()
	}
	for _, cap := range meta.GetCapabilities() {
		switch cap {
		case proto.ResourceCapability_RESOURCE_CAPABILITY_KEY_VALUE:
			r.capabilities = append(r.capabilities, core.ResourceCapabilityKeyValue)
		case proto.ResourceCapability_RESOURCE_CAPABILITY_SQL:
			r.capabilities = append(r.capabilities, core.ResourceCapabilitySQL)
		case proto.ResourceCapability_RESOURCE_CAPABILITY_BLOB_STORE:
			r.capabilities = append(r.capabilities, core.ResourceCapabilityBlobStore)
		}
	}
	return nil
}

func validateResourceProtocol(meta *proto.ResourceMetadata) error {
	min := meta.GetMinProtocolVersion()
	max := meta.GetMaxProtocolVersion()
	if min == 0 && max == 0 {
		return nil
	}
	if min > 0 && proto.CurrentProtocolVersion < min {
		return fmt.Errorf("resource protocol version %d below minimum %d", proto.CurrentProtocolVersion, min)
	}
	if max > 0 && proto.CurrentProtocolVersion > max {
		return fmt.Errorf("resource protocol version %d above maximum %d", proto.CurrentProtocolVersion, max)
	}
	return nil
}

func (r *RemoteResourceProvider) Name() string                            { return r.name }
func (r *RemoteResourceProvider) Capabilities() []core.ResourceCapability { return r.capabilities }

func (r *RemoteResourceProvider) Ping(ctx context.Context) error {
	rpcCtx, cancel := context.WithTimeout(ctx, pluginRPCTimeout)
	defer cancel()
	resp, err := r.runtime.HealthCheck(rpcCtx, &emptypb.Empty{})
	if err != nil {
		return fmt.Errorf("resource health check: %w", err)
	}
	if !resp.GetReady() {
		return fmt.Errorf("resource not ready: %s", resp.GetMessage())
	}
	return nil
}

func (r *RemoteResourceProvider) Close() error {
	if r.closer != nil {
		return r.closer.Close()
	}
	return nil
}

// HasCapability reports whether the resource provider supports the given
// capability.
func (r *RemoteResourceProvider) HasCapability(cap core.ResourceCapability) bool {
	for _, c := range r.capabilities {
		if c == cap {
			return true
		}
	}
	return false
}

// KVClient returns the KeyValue gRPC client, or nil if not supported.
func (r *RemoteResourceProvider) KVClient() proto.KeyValueResourceClient { return r.kvClient }

// SQLClient returns the SQL gRPC client, or nil if not supported.
func (r *RemoteResourceProvider) SQLClient() proto.SQLResourceClient { return r.sqlClient }

// BlobClient returns the BlobStore gRPC client, or nil if not supported.
func (r *RemoteResourceProvider) BlobClient() proto.BlobStoreResourceClient { return r.blobClient }

// --- Namespace-injecting consumer wrappers ---

// NewNamespacedKVStore wraps a KeyValue gRPC client with a fixed namespace.
func NewNamespacedKVStore(client proto.KeyValueResourceClient, namespace string) core.KeyValueStore {
	return &namespacedKVStore{client: client, namespace: namespace}
}

type namespacedKVStore struct {
	client    proto.KeyValueResourceClient
	namespace string
}

func (s *namespacedKVStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	resp, err := s.client.Get(ctx, &proto.KVGetRequest{Namespace: s.namespace, Key: key})
	if err != nil {
		return nil, false, err
	}
	return resp.GetValue(), resp.GetFound(), nil
}

func (s *namespacedKVStore) Put(ctx context.Context, key string, value []byte) error {
	_, err := s.client.Put(ctx, &proto.KVPutRequest{Namespace: s.namespace, Key: key, Value: value})
	return err
}

func (s *namespacedKVStore) PutWithTTL(ctx context.Context, key string, value []byte, ttlSeconds int64) error {
	_, err := s.client.Put(ctx, &proto.KVPutRequest{Namespace: s.namespace, Key: key, Value: value, TtlSeconds: ttlSeconds})
	return err
}

func (s *namespacedKVStore) Delete(ctx context.Context, key string) (bool, error) {
	resp, err := s.client.Delete(ctx, &proto.KVDeleteRequest{Namespace: s.namespace, Key: key})
	if err != nil {
		return false, err
	}
	return resp.GetDeleted(), nil
}

func (s *namespacedKVStore) List(ctx context.Context, prefix string, cursor string, limit int32) ([]core.KVEntry, string, error) {
	resp, err := s.client.List(ctx, &proto.KVListRequest{Namespace: s.namespace, Prefix: prefix, Cursor: cursor, Limit: limit})
	if err != nil {
		return nil, "", err
	}
	entries := make([]core.KVEntry, len(resp.GetEntries()))
	for i, e := range resp.GetEntries() {
		entries[i] = core.KVEntry{Key: e.GetKey(), Value: e.GetValue()}
	}
	return entries, resp.GetNextCursor(), nil
}

// NewNamespacedSQLStore wraps a SQL gRPC client with a fixed namespace.
func NewNamespacedSQLStore(client proto.SQLResourceClient, namespace string) core.SQLStore {
	return &namespacedSQLStore{client: client, namespace: namespace}
}

type namespacedSQLStore struct {
	client    proto.SQLResourceClient
	namespace string
}

func (s *namespacedSQLStore) Query(ctx context.Context, query string, params ...any) (*core.SQLRows, error) {
	protoParams, err := anySliceToProtoSQLValues(params)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Query(ctx, &proto.SQLQueryRequest{Namespace: s.namespace, Query: query, Params: protoParams})
	if err != nil {
		return nil, err
	}
	return protoSQLRowsToCore(resp), nil
}

func (s *namespacedSQLStore) Exec(ctx context.Context, query string, params ...any) (core.SQLExecResult, error) {
	protoParams, err := anySliceToProtoSQLValues(params)
	if err != nil {
		return core.SQLExecResult{}, err
	}
	resp, err := s.client.Exec(ctx, &proto.SQLExecRequest{Namespace: s.namespace, Query: query, Params: protoParams})
	if err != nil {
		return core.SQLExecResult{}, err
	}
	return core.SQLExecResult{RowsAffected: resp.GetRowsAffected(), LastInsertID: resp.GetLastInsertId()}, nil
}

func (s *namespacedSQLStore) Migrate(ctx context.Context, migrations []core.SQLMigration) error {
	protoMigrations := make([]*proto.SQLMigration, len(migrations))
	for i, m := range migrations {
		protoMigrations[i] = &proto.SQLMigration{Version: m.Version, Description: m.Description, UpSql: m.UpSQL}
	}
	_, err := s.client.Migrate(ctx, &proto.SQLMigrateRequest{Namespace: s.namespace, Migrations: protoMigrations})
	return err
}

// NewNamespacedBlobStore wraps a BlobStore gRPC client with a fixed namespace.
func NewNamespacedBlobStore(client proto.BlobStoreResourceClient, namespace string) core.BlobStore {
	return &namespacedBlobStore{client: client, namespace: namespace}
}

type namespacedBlobStore struct {
	client    proto.BlobStoreResourceClient
	namespace string
}

func (s *namespacedBlobStore) Get(ctx context.Context, key string) ([]byte, string, error) {
	resp, err := s.client.Get(ctx, &proto.BlobGetRequest{Namespace: s.namespace, Key: key})
	if err != nil {
		return nil, "", err
	}
	return resp.GetData(), resp.GetContentType(), nil
}

func (s *namespacedBlobStore) Put(ctx context.Context, key string, data []byte, contentType string) error {
	_, err := s.client.Put(ctx, &proto.BlobPutRequest{Namespace: s.namespace, Key: key, Data: data, ContentType: contentType})
	return err
}

func (s *namespacedBlobStore) Delete(ctx context.Context, key string) error {
	_, err := s.client.Delete(ctx, &proto.BlobDeleteRequest{Namespace: s.namespace, Key: key})
	return err
}

func (s *namespacedBlobStore) List(ctx context.Context, prefix string, cursor string, limit int32) ([]core.BlobEntry, string, error) {
	resp, err := s.client.List(ctx, &proto.BlobListRequest{Namespace: s.namespace, Prefix: prefix, Cursor: cursor, Limit: limit})
	if err != nil {
		return nil, "", err
	}
	entries := make([]core.BlobEntry, len(resp.GetEntries()))
	for i, e := range resp.GetEntries() {
		entries[i] = core.BlobEntry{Key: e.GetKey(), Size: e.GetSize(), ContentType: e.GetContentType()}
	}
	return entries, resp.GetNextCursor(), nil
}

// --- Proto conversion helpers ---

func anySliceToProtoSQLValues(params []any) ([]*proto.SQLValue, error) {
	if len(params) == 0 {
		return nil, nil
	}
	out := make([]*proto.SQLValue, len(params))
	for i, p := range params {
		out[i] = anyToProtoSQLValue(p)
	}
	return out, nil
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
	case int64:
		return &proto.SQLValue{Kind: &proto.SQLValue_IntValue{IntValue: val}}
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

func protoSQLRowsToCore(resp *proto.SQLQueryResponse) *core.SQLRows {
	if resp == nil {
		return &core.SQLRows{}
	}
	rows := make([][]any, len(resp.GetRows()))
	for i, row := range resp.GetRows() {
		vals := make([]any, len(row.GetValues()))
		for j, v := range row.GetValues() {
			vals[j] = protoSQLValueToAny(v)
		}
		rows[i] = vals
	}
	return &core.SQLRows{Columns: resp.GetColumns(), Rows: rows}
}

func protoSQLValueToAny(v *proto.SQLValue) any {
	if v == nil {
		return nil
	}
	switch k := v.GetKind().(type) {
	case *proto.SQLValue_StringValue:
		return k.StringValue
	case *proto.SQLValue_IntValue:
		return k.IntValue
	case *proto.SQLValue_DoubleValue:
		return k.DoubleValue
	case *proto.SQLValue_BoolValue:
		return k.BoolValue
	case *proto.SQLValue_BytesValue:
		return k.BytesValue
	case *proto.SQLValue_IsNull:
		return nil
	default:
		return nil
	}
}
