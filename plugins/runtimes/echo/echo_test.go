package echo_test

import (
	"context"
	"testing"

	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/internal/bootstrap"
	"github.com/valon-technologies/toolshed/internal/principal"
	echoruntime "github.com/valon-technologies/toolshed/plugins/runtimes/echo"
)

type stubInvoker struct{}

func (b *stubInvoker) Invoke(_ context.Context, _ *principal.Principal, _ string, _ string, _ map[string]any) (*core.OperationResult, error) {
	return nil, nil
}

type stubCapabilityLister struct {
	caps  []core.Capability
	calls int
}

func (b *stubCapabilityLister) ListCapabilities() []core.Capability {
	b.calls++
	return b.caps
}

func TestRuntime(t *testing.T) {
	t.Parallel()

	lister := &stubCapabilityLister{
		caps: []core.Capability{
			{Provider: "alpha", Operation: "op1"},
			{Provider: "alpha", Operation: "op2"},
		},
	}
	deps := bootstrap.RuntimeDeps{
		Invoker:          &stubInvoker{},
		CapabilityLister: lister,
	}

	rt := echoruntime.New("test-echo", deps)

	if rt.Name() != "test-echo" {
		t.Fatalf("expected name test-echo, got %q", rt.Name())
	}

	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if lister.calls != 1 {
		t.Fatalf("expected capability lister to be called once, got %d", lister.calls)
	}

	if err := rt.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestRuntime_UsesExplicitDeps(t *testing.T) {
	t.Parallel()

	caps := []core.Capability{
		{Provider: "alpha", Operation: "op1"},
	}
	lister := &stubCapabilityLister{caps: caps}
	rt := echoruntime.New("deps-test", bootstrap.RuntimeDeps{
		Invoker:          &stubInvoker{},
		CapabilityLister: lister,
	})

	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if lister.calls != 1 {
		t.Fatalf("expected capability lister to be called once, got %d", lister.calls)
	}
	if got := lister.ListCapabilities(); len(got) != 1 {
		t.Fatalf("expected 1 capability, got %d", len(got))
	} else if got[0].Provider != "alpha" || got[0].Operation != "op1" {
		t.Fatalf("unexpected capability: %+v", got[0])
	}
}
