package echo

import (
	"context"
	"log"

	"github.com/valon-technologies/toolshed/core"
)

var _ core.Runtime = (*Runtime)(nil)

type Runtime struct {
	name   string
	broker core.Broker
}

func New(name string, broker core.Broker) *Runtime {
	return &Runtime{name: name, broker: broker}
}

func (r *Runtime) Name() string { return r.name }

func (r *Runtime) Start(_ context.Context) error {
	caps := r.broker.ListCapabilities()
	log.Printf("echo runtime %q started with %d capabilities", r.name, len(caps))
	return nil
}

func (r *Runtime) Stop(_ context.Context) error {
	log.Printf("echo runtime %q stopped", r.name)
	return nil
}

func (r *Runtime) Broker() core.Broker { return r.broker }
