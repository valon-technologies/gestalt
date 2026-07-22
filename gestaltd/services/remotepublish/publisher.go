package remotepublish

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"

	gestaltclient "github.com/valon-technologies/gestalt/sdk/go/client"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/remote"
	"github.com/valon-technologies/gestalt/server/internal/tunnel"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
)

type RemoteClient interface {
	ListRemotes(ctx context.Context, req *gestaltclient.ListRemotesRequest) (*gestaltclient.ListRemotesResponse, error)
	CreateRemote(ctx context.Context, req *gestaltclient.CreateRemoteRequest) (*gestaltclient.Remote, error)
	DeleteRemote(ctx context.Context, req *gestaltclient.DeleteRemoteRequest) error
	Close() error
}

type remoteClient struct {
	rm   *gestaltclient.RemoteManagement
	conn *grpc.ClientConn
}

func newRemoteClient(ctx context.Context, remoteCfg *config.RemoteConfig) (*remoteClient, error) {
	conn, err := remote.Dial(ctx, remote.Config{
		URL:   remoteCfg.URL,
		Token: remoteCfg.Token,
	})
	if err != nil {
		return nil, err
	}
	return &remoteClient{rm: gestaltclient.NewRemoteManagement(conn), conn: conn}, nil
}

func (c *remoteClient) ListRemotes(ctx context.Context, req *gestaltclient.ListRemotesRequest) (*gestaltclient.ListRemotesResponse, error) {
	return c.rm.ListRemotes(ctx, req)
}

func (c *remoteClient) CreateRemote(ctx context.Context, req *gestaltclient.CreateRemoteRequest) (*gestaltclient.Remote, error) {
	return c.rm.CreateRemote(ctx, req)
}

func (c *remoteClient) DeleteRemote(ctx context.Context, req *gestaltclient.DeleteRemoteRequest) error {
	return c.rm.DeleteRemote(ctx, req)
}

func (c *remoteClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

type PublicationGroup struct {
	RemoteName string
	Remote     *config.RemoteConfig
	Providers  []ProviderPublication
}

type ProviderPublication struct {
	Kind       string
	Name       string
	Definition map[string]any
}

type PublisherConfig struct {
	Groups    []PublicationGroup
	Providers *registry.ProviderMap[core.Provider]
	Logger    *slog.Logger
}

type Publisher struct {
	groups    []*groupState
	providers *registry.ProviderMap[core.Provider]
	logger    *slog.Logger
	mu        sync.Mutex
	started   bool
}

type groupState struct {
	group      PublicationGroup
	client     RemoteClient
	host       *tunnel.Host
	grpcServer *grpc.Server
	remote     *gestaltclient.Remote
	published  bool
	cancel     context.CancelFunc
	done       chan struct{}
}

func NewPublisher(cfg PublisherConfig) *Publisher {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	groups := make([]*groupState, 0, len(cfg.Groups))
	for _, g := range cfg.Groups {
		groups = append(groups, &groupState{group: g})
	}
	return &Publisher{groups: groups, providers: cfg.Providers, logger: logger}
}

func (p *Publisher) ReadinessReason() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var pending []string
	for _, gs := range p.groups {
		if !gs.published {
			pending = append(pending, gs.group.RemoteName)
		}
	}
	if len(pending) == 0 {
		return ""
	}
	slices.Sort(pending)
	return "pending reverse publication: " + strings.Join(pending, ", ")
}

func (p *Publisher) Start(ctx context.Context) error {
	p.mu.Lock()
	if p.started {
		p.mu.Unlock()
		return fmt.Errorf("publisher already started")
	}
	p.started = true
	p.mu.Unlock()
	for _, gs := range p.groups {
		gs := gs
		gCtx, cancel := context.WithCancel(ctx)
		gs.cancel = cancel
		gs.done = make(chan struct{})
		go p.runGroup(gCtx, gs)
	}
	return nil
}

func (p *Publisher) runGroup(ctx context.Context, gs *groupState) {
	defer close(gs.done)
	backoff := initialBackoff
	for {
		if ctx.Err() != nil {
			return
		}
		err := p.publishOnce(ctx, gs)
		if err == nil {
			p.mu.Lock()
			gs.published = true
			p.mu.Unlock()
			p.logger.Info("reverse publication succeeded",
				"remote", gs.group.RemoteName,
				"generation", gs.remote.Generation,
				"providers", len(gs.group.Providers),
			)
			<-ctx.Done()
			p.shutdownGroup(context.Background(), gs)
			return
		}
		if ctx.Err() != nil {
			return
		}
		p.logger.Warn("reverse publication failed, retrying",
			"remote", gs.group.RemoteName,
			"error", err,
			"backoff", backoff,
		)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}
		backoff = nextBackoff(backoff)
	}
}

func (p *Publisher) publishOnce(ctx context.Context, gs *groupState) (err error) {
	client, cerr := newRemoteClient(ctx, gs.group.Remote)
	if cerr != nil {
		return fmt.Errorf("connect to remote %s: %w", gs.group.RemoteName, cerr)
	}
	gs.client = client
	defer func() {
		if err != nil {
			_ = client.Close()
			gs.client = nil
		}
	}()

	listResp, lerr := client.ListRemotes(ctx, &gestaltclient.ListRemotesRequest{})
	if lerr != nil {
		return fmt.Errorf("list remotes from %s: %w", gs.group.RemoteName, lerr)
	}

	var expectedGeneration uint64
	if listResp != nil && len(listResp.Remotes) > 0 {
		expectedGeneration = listResp.Remotes[0].Generation
	}

	identity, ierr := tunnel.NewIdentity()
	if ierr != nil {
		return fmt.Errorf("tunnel identity: %w", ierr)
	}

	var upstreamSPKI, frpsAddress string
	if listResp != nil {
		if listResp.ServerIdentity != nil {
			upstreamSPKI = listResp.ServerIdentity.ClientSpkiSha256
		}
		if listResp.Tunnel != nil {
			frpsAddress = listResp.Tunnel.FrpsAddress
		}
	}

	host, herr := tunnel.StartHost(ctx, tunnel.HostConfig{
		ServerURL:    frpsAddress,
		Identity:     identity,
		UpstreamSPKI: upstreamSPKI,
	})
	if herr != nil {
		return fmt.Errorf("tunnel host: %w", herr)
	}
	gs.host = host
	defer func() {
		if err != nil {
			_ = host.Close()
			gs.host = nil
		}
	}()

	// Wait for the frpc proxy to register with frps before triggering the
	// upstream's dial-back validation.
	waitCtx, waitCancel := context.WithTimeout(ctx, 15*time.Second)
	if werr := host.WaitReady(waitCtx); werr != nil {
		waitCancel()
		return fmt.Errorf("tunnel host readiness: %w", werr)
	}
	waitCancel()

	grpcServer := grpc.NewServer()
	proto.RegisterRegistrationLifecycleServer(grpcServer,
		newRegistrationLifecycleServer(gs.group.Providers, &providerLookup{providers: p.providers}))
	go func() { _ = grpcServer.Serve(host.Listener()) }()
	gs.grpcServer = grpcServer
	defer func() {
		if err != nil {
			stopGRPC(grpcServer)
			gs.grpcServer = nil
		}
	}()

	providerDefs := make([]*gestaltclient.RemoteProviderDefinition, 0, len(gs.group.Providers))
	for _, pub := range gs.group.Providers {
		providerDefs = append(providerDefs, &gestaltclient.RemoteProviderDefinition{
			Kind:       pub.Kind,
			Name:       pub.Name,
			Definition: pub.Definition,
		})
	}

	remote, cerr2 := client.CreateRemote(ctx, &gestaltclient.CreateRemoteRequest{
		Tunnel: &gestaltclient.TunnelEndpoint{
			Host:             identity.TunnelHost,
			Certificate:      identity.Certificate.Certificate[0],
			ServerSpkiSha256: identity.SPKISHA256,
		},
		Providers:          providerDefs,
		ExpectedGeneration: expectedGeneration,
	})
	if cerr2 != nil {
		return fmt.Errorf("create remote on %s: %w", gs.group.RemoteName, cerr2)
	}
	gs.remote = remote
	return nil
}

func (p *Publisher) shutdownGroup(ctx context.Context, gs *groupState) {
	if gs.client != nil && gs.remote != nil {
		deleteCtx, cancel := context.WithTimeout(ctx, deleteTimeout)
		if err := gs.client.DeleteRemote(deleteCtx, &gestaltclient.DeleteRemoteRequest{
			Id:                 gs.remote.Id,
			ExpectedGeneration: gs.remote.Generation,
		}); err != nil {
			p.logger.Warn("failed to delete remote on shutdown",
				"remote", gs.group.RemoteName, "error", err)
		}
		cancel()
	}
	if gs.grpcServer != nil {
		stopGRPC(gs.grpcServer)
	}
	if gs.host != nil {
		_ = gs.host.Close()
	}
	if gs.client != nil {
		_ = gs.client.Close()
	}
}

func (p *Publisher) Shutdown(ctx context.Context) {
	p.mu.Lock()
	for _, gs := range p.groups {
		if gs.cancel != nil {
			gs.cancel()
		}
	}
	p.mu.Unlock()
	for _, gs := range p.groups {
		if gs.done != nil {
			select {
			case <-gs.done:
			case <-time.After(groupShutdownTimeout):
			}
		}
	}
}

// providerLookup adapts the provider registry to the ProviderLookup interface.
type providerLookup struct {
	providers *registry.ProviderMap[core.Provider]
}

func (l *providerLookup) Has(name string) bool {
	_, err := l.providers.Get(name)
	return err == nil
}

const (
	initialBackoff = 2 * time.Second
	maxBackoff     = 60 * time.Second
	// deleteTimeout bounds the DeleteRemote call during shutdown.
	deleteTimeout = 10 * time.Second
	// grpcStopTimeout bounds GracefulStop before falling back to Stop.
	grpcStopTimeout = 5 * time.Second
	// groupShutdownTimeout covers deleteTimeout + grpcStopTimeout plus margin.
	groupShutdownTimeout = deleteTimeout + grpcStopTimeout + 5*time.Second
)

func stopGRPC(s *grpc.Server) {
	stopped := make(chan struct{})
	go func() { s.GracefulStop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(grpcStopTimeout):
		s.Stop()
	}
}

func nextBackoff(d time.Duration) time.Duration {
	d *= 2
	if d > maxBackoff {
		return maxBackoff
	}
	return d
}
