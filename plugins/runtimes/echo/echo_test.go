package echo_test

import (
	"context"
	"testing"

	"github.com/valon-technologies/toolshed/core"
	echoruntime "github.com/valon-technologies/toolshed/plugins/runtimes/echo"
)

type stubBroker struct {
	caps []core.Capability
}

func (b *stubBroker) Invoke(_ context.Context, _ core.InvocationRequest) (*core.OperationResult, error) {
	return nil, nil
}

func (b *stubBroker) ListCapabilities() []core.Capability {
	return b.caps
}

func TestRuntime(t *testing.T) {
	t.Parallel()

	broker := &stubBroker{
		caps: []core.Capability{
			{Provider: "alpha", Operation: "op1"},
			{Provider: "alpha", Operation: "op2"},
		},
	}

	rt := echoruntime.New("test-echo", broker)

	if rt.Name() != "test-echo" {
		t.Fatalf("expected name test-echo, got %q", rt.Name())
	}

	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := rt.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestRuntime_BrokerAccessible(t *testing.T) {
	t.Parallel()

	caps := []core.Capability{
		{Provider: "alpha", Operation: "op1"},
	}
	broker := &stubBroker{caps: caps}
	rt := echoruntime.New("broker-test", broker)

	got := rt.Broker().ListCapabilities()
	if len(got) != 1 {
		t.Fatalf("expected 1 capability, got %d", len(got))
	}
	if got[0].Provider != "alpha" || got[0].Operation != "op1" {
		t.Fatalf("unexpected capability: %+v", got[0])
	}
}
