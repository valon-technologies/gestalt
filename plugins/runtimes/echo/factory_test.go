package echo_test

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/testutil"
	echoruntime "github.com/valon-technologies/gestalt/plugins/runtimes/echo"
)

func TestFactoryUsesExplicitDeps(t *testing.T) {
	t.Parallel()

	lister := &stubCapabilityLister{
		caps: []core.Capability{
			{Provider: "alpha", Operation: "echo"},
		},
	}

	rt, err := echoruntime.Factory(context.Background(), "factory-echo", config.RuntimeDef{}, bootstrap.RuntimeDeps{
		Invoker:          &testutil.StubInvoker{},
		CapabilityLister: lister,
	})
	if err != nil {
		t.Fatalf("Factory: %v", err)
	}
	if rt.Name() != "factory-echo" {
		t.Fatalf("runtime name = %q, want factory-echo", rt.Name())
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if lister.calls != 1 {
		t.Fatalf("expected capability lister to be called once, got %d", lister.calls)
	}
}
