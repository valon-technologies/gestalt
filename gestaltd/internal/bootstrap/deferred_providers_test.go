package bootstrap

import (
	"testing"
	"time"
)

func TestDeferredProvidersUntriggered(t *testing.T) {
	t.Parallel()

	var nilD *deferredProviders
	nilD.waitReady()

	d := &deferredProviders{}
	if got := d.connectionAuth(); got != nil {
		t.Fatalf("connectionAuth before set = %v, want nil", got)
	}
	if got := d.manualConnectionAuth(); got != nil {
		t.Fatalf("manualConnectionAuth before set = %v, want nil", got)
	}

	done := make(chan struct{})
	go func() { d.waitReady(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("waitReady blocked while untriggered")
	}
}

func TestDeferredProvidersSetPopulatesAndWaits(t *testing.T) {
	t.Parallel()

	ready := make(chan struct{})
	d := &deferredProviders{}
	d.set(
		ready,
		func() map[string]map[string]OAuthHandler {
			return map[string]map[string]OAuthHandler{"gh": nil}
		},
		func() map[string]map[string]ManualTokenExchanger {
			return map[string]map[string]ManualTokenExchanger{"sl": nil}
		},
	)

	if _, ok := d.connectionAuth()["gh"]; !ok {
		t.Fatal("connectionAuth not populated after set")
	}
	if _, ok := d.manualConnectionAuth()["sl"]; !ok {
		t.Fatal("manualConnectionAuth not populated after set")
	}

	done := make(chan struct{})
	go func() { d.waitReady(); close(done) }()
	select {
	case <-done:
		t.Fatal("waitReady returned before its ready channel closed")
	case <-time.After(100 * time.Millisecond):
	}
	close(ready)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("waitReady did not return after ready closed")
	}
}
