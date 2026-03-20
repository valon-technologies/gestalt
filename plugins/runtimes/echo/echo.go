package echo

import (
	"context"
	"log"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
)

var _ core.Runtime = (*Runtime)(nil)

type Runtime struct {
	name string
	deps bootstrap.RuntimeDeps
}

func New(name string, deps bootstrap.RuntimeDeps) *Runtime {
	return &Runtime{name: name, deps: deps}
}

func (r *Runtime) Name() string { return r.name }

func (r *Runtime) Start(_ context.Context) error {
	caps := []core.Capability(nil)
	if r.deps.CapabilityLister != nil {
		caps = r.deps.CapabilityLister.ListCapabilities()
	}
	log.Printf("echo runtime %q started with %d capabilities", r.name, len(caps))
	return nil
}

func (r *Runtime) Stop(_ context.Context) error {
	log.Printf("echo runtime %q stopped", r.name)
	return nil
}
