package bootstrap

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
)

type countedAuthProvider struct {
	*coretesting.StubAuthProvider
	closeCount *atomic.Int32
}

func (p *countedAuthProvider) Close() error {
	if p.closeCount != nil {
		p.closeCount.Add(1)
	}
	return nil
}

func TestPreparedCoreCloseClosesEachAuthProviderOnce(t *testing.T) {
	t.Parallel()

	var selectedClosed atomic.Int32
	var extraClosed atomic.Int32
	selected := &countedAuthProvider{
		StubAuthProvider: &coretesting.StubAuthProvider{N: "selected"},
		closeCount:       &selectedClosed,
	}
	extra := &countedAuthProvider{
		StubAuthProvider: &coretesting.StubAuthProvider{N: "extra"},
		closeCount:       &extraClosed,
	}

	prepared := &preparedCore{
		Auth: selected,
		AuthProviders: map[string]core.AuthenticationProvider{
			"selected": selected,
			"extra":    extra,
		},
		Services: coretesting.NewStubServices(t),
	}

	if err := prepared.Close(context.Background()); err != nil {
		t.Fatalf("preparedCore.Close: %v", err)
	}
	if got := selectedClosed.Load(); got != 1 {
		t.Fatalf("selected auth close count = %d, want 1", got)
	}
	if got := extraClosed.Load(); got != 1 {
		t.Fatalf("extra auth close count = %d, want 1", got)
	}
}
