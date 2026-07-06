package indexeddb

import (
	"context"
	"io"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	coreindexeddb "github.com/valon-technologies/gestalt/server/core/indexeddb"
	rpcidb "github.com/valon-technologies/gestalt/server/rpc/indexeddb"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ExecConfig configures a provider-backed IndexedDB executable.
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
	idb.Database
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
	_, err = runtimehost.ConfigureRuntimeProvider(ctx, runtimeClient, proto.ProviderKind_PROVIDER_KIND_INDEXEDDB, cfg.Name, cfg.Config)
	if err != nil {
		_ = proc.Close()
		return nil, err
	}

	db := rpcidb.NewConn(proc.Conn(), rpcidb.Options{
		UnaryTimeout: runtimehost.ProviderRPCTimeout,
	})
	return &remoteIndexedDB{
		Database: db,
		runtime:  runtimeClient,
		closer:   proc,
	}, nil
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

func (r *remoteIndexedDB) AcquireLock(ctx context.Context, key, holder string, ttl time.Duration) (idb.LockLease, error) {
	locker, ok := r.Database.(idb.Locker)
	if !ok {
		return idb.LockLease{}, status.Error(codes.Unimplemented, "indexeddb: advisory locks not supported")
	}
	return locker.AcquireLock(ctx, key, holder, ttl)
}

func (r *remoteIndexedDB) ReleaseLock(ctx context.Context, key, holder string) error {
	locker, ok := r.Database.(idb.Locker)
	if !ok {
		return status.Error(codes.Unimplemented, "indexeddb: advisory locks not supported")
	}
	return locker.ReleaseLock(ctx, key, holder)
}

func (r *remoteIndexedDB) CreateIndex(ctx context.Context, store string, index idb.IndexDefinition) error {
	manager, ok := r.Database.(idb.IndexManager)
	if !ok {
		return status.Error(codes.Unimplemented, "indexeddb: index management not supported")
	}
	return manager.CreateIndex(ctx, store, index)
}

func (r *remoteIndexedDB) DeleteIndex(ctx context.Context, store, name string) error {
	manager, ok := r.Database.(idb.IndexManager)
	if !ok {
		return status.Error(codes.Unimplemented, "indexeddb: index management not supported")
	}
	return manager.DeleteIndex(ctx, store, name)
}
