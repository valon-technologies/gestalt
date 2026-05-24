package indexeddb

import (
	"context"
	"fmt"
	"io"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	coreindexeddb "github.com/valon-technologies/gestalt/server/core/indexeddb"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ExecConfig struct {
	Command    string
	Args       []string
	Workdir    string
	Env        map[string]string
	Config     map[string]any
	Egress     egress.Policy
	HostBinary string
	Cleanup    func()
	Name       string
}

type remoteIndexedDB struct {
	client  proto.IndexedDBClient
	runtime proto.ProviderLifecycleClient
	closer  io.Closer
}

func NewExecutable(ctx context.Context, cfg ExecConfig) (coreindexeddb.IndexedDB, error) {
	proc, err := runtimehost.StartAppProcess(ctx, runtimehost.ProcessConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Workdir:      cfg.Workdir,
		Env:          cfg.Env,
		Egress:       cfg.Egress,
		HostBinary:   cfg.HostBinary,
		Cleanup:      cfg.Cleanup,
		ProviderName: cfg.Name,
	})
	if err != nil {
		return nil, err
	}

	runtimeClient := proc.Lifecycle()
	dsClient := proto.NewIndexedDBClient(proc.Conn())

	_, err = runtimehost.ConfigureRuntimeProvider(ctx, runtimeClient, proto.ProviderKind_PROVIDER_KIND_INDEXEDDB, cfg.Name, cfg.Config)
	if err != nil {
		_ = proc.Close()
		return nil, err
	}

	return &remoteIndexedDB{client: dsClient, runtime: runtimeClient, closer: proc}, nil
}

func (r *remoteIndexedDB) ObjectStore(name string) idb.ObjectStore {
	return &remoteObjectStore{client: r.client, store: name}
}

func (r *remoteIndexedDB) Transaction(ctx context.Context, stores []string, mode idb.TransactionMode, opts idb.TransactionOptions) (idb.Transaction, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	streamCtx, cancel := context.WithCancel(ctx)
	stream, err := r.client.Transaction(streamCtx)
	if err != nil {
		cancel()
		return nil, idb.RPCError(err)
	}
	if err := stream.Send(&proto.TransactionClientMessage{
		Msg: &proto.TransactionClientMessage_Begin{Begin: &proto.BeginTransactionRequest{
			Stores:         stores,
			Mode:           transactionModeToProto(mode),
			DurabilityHint: durabilityHintToProto(opts.DurabilityHint),
		}},
	}); err != nil {
		_ = stream.CloseSend()
		cancel()
		return nil, idb.RPCError(err)
	}
	resp, err := stream.Recv()
	if err != nil {
		_ = stream.CloseSend()
		cancel()
		return nil, idb.RPCError(err)
	}
	if resp.GetBegin() == nil {
		_ = stream.CloseSend()
		cancel()
		return nil, fmt.Errorf("indexeddb transaction: expected begin response")
	}
	return &remoteTransaction{stream: stream, cancel: cancel}, nil
}

func (r *remoteIndexedDB) CreateObjectStore(ctx context.Context, name string, schema idb.ObjectStoreSchema) (idb.ObjectStore, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
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
	if err != nil {
		return nil, idb.RPCError(err)
	}
	return r.ObjectStore(name), nil
}

func (r *remoteIndexedDB) DeleteObjectStore(ctx context.Context, name string) error {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	_, err := r.client.DeleteObjectStore(ctx, &proto.DeleteObjectStoreRequest{Name: name})
	return idb.RPCError(err)
}

func (r *remoteIndexedDB) Ping(ctx context.Context) error {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
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
