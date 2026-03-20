package grpc

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"google.golang.org/grpc"
)

const gracefulStopTimeout = 10 * time.Second

var _ core.Binding = (*Binding)(nil)

type grpcConfig struct {
	Port int `yaml:"port"`
}

type Binding struct {
	name           string
	cfg            grpcConfig
	invoker        invocation.Invoker
	capLister      invocation.CapabilityLister
	providerLister invocation.ProviderLister
	server         *grpc.Server
}

func New(name string, cfg grpcConfig, invoker invocation.Invoker, capLister invocation.CapabilityLister, providerLister invocation.ProviderLister) *Binding {
	return &Binding{
		name:           name,
		cfg:            cfg,
		invoker:        invoker,
		capLister:      capLister,
		providerLister: providerLister,
	}
}

func (b *Binding) Name() string           { return b.name }
func (b *Binding) Kind() core.BindingKind { return core.BindingSurface }
func (b *Binding) Routes() []core.Route   { return nil }

func (b *Binding) Start(_ context.Context) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", b.cfg.Port))
	if err != nil {
		return fmt.Errorf("grpc binding %q: listen on port %d: %w", b.name, b.cfg.Port, err)
	}

	b.server = grpc.NewServer()
	registerService(b.server, b.invoker, b.capLister, b.providerLister)

	go func() {
		log.Printf("grpc binding %q listening on :%d", b.name, b.cfg.Port)
		if err := b.server.Serve(lis); err != nil {
			log.Printf("grpc binding %q: serve: %v", b.name, err)
		}
	}()

	return nil
}

func (b *Binding) Close() error {
	if b.server != nil {
		done := make(chan struct{})
		go func() {
			b.server.GracefulStop()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(gracefulStopTimeout):
			b.server.Stop()
		}
	}
	return nil
}
