package indexeddb

import (
	"context"
	"io"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	idbhost "github.com/valon-technologies/gestalt/sdk/go/indexeddb/host"
	coreindexeddb "github.com/valon-technologies/gestalt/server/core/indexeddb"
	proto "github.com/valon-technologies/gestalt/sdk/go/protov1/v1"
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

type executableIndexedDB struct {
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

	return &executableIndexedDB{
		Database: idbhost.NewProviderConn(proc.Conn(), runtimehost.ProviderRPCTimeout),
		runtime:  runtimeClient,
		closer:   proc,
	}, nil
}

func (e *executableIndexedDB) Ping(ctx context.Context) error {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	_, err := e.runtime.HealthCheck(ctx, &emptypb.Empty{})
	return err
}

func (e *executableIndexedDB) Close() error {
	if e == nil || e.closer == nil {
		return nil
	}
	return e.closer.Close()
}

var _ coreindexeddb.IndexedDB = (*executableIndexedDB)(nil)
