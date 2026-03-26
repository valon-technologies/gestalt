package echo_test

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/testutil"
	echoruntime "github.com/valon-technologies/gestalt/plugins/runtimes/echo"
)

type stubCapabilityLister struct {
	caps  []core.Capability
	calls int
}

func (b *stubCapabilityLister) ListCapabilities() []core.Capability {
	b.calls++
	return b.caps
}

func TestRuntimeStartAndStopUseInjectedDeps(t *testing.T) {
	t.Parallel()

	lister := &stubCapabilityLister{
		caps: []core.Capability{
			{Provider: "alpha", Operation: "op1"},
			{Provider: "alpha", Operation: "op2"},
		},
	}
	rt := echoruntime.New("test-echo", bootstrap.RuntimeDeps{
		Invoker:          &testutil.StubInvoker{},
		CapabilityLister: lister,
	})

	if rt.Name() != "test-echo" {
		t.Fatalf("expected name test-echo, got %q", rt.Name())
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if lister.calls != 1 {
		t.Fatalf("expected capability lister to be called once, got %d", lister.calls)
	}

	got := lister.ListCapabilities()
	if len(got) != 2 {
		t.Fatalf("expected 2 capabilities, got %d", len(got))
	}
	if got[0].Provider != "alpha" || got[0].Operation != "op1" {
		t.Fatalf("unexpected first capability: %+v", got[0])
	}
	if got[1].Provider != "alpha" || got[1].Operation != "op2" {
		t.Fatalf("unexpected second capability: %+v", got[1])
	}

	if err := rt.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}
