package registry

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
)

// stubRemoteResolver returns a fixed provider for a given name.
type stubRemoteResolver[T any] struct {
	provider T
	err      error
}

func (r *stubRemoteResolver[T]) ResolveProvider(_ context.Context, _ string) (T, error) {
	return r.provider, r.err
}

func TestProviderMapRemoteResolverPrecedence(t *testing.T) {
	t.Parallel()
	m := newProviderMap[int]("test")
	if err := m.Register("foo", 42); err != nil {
		t.Fatal(err)
	}

	// Without a remote resolver, Get returns the local value.
	val, err := m.Get("foo")
	if err != nil || val != 42 {
		t.Fatalf("Get without resolver: got %d, err %v", val, err)
	}

	// With a remote resolver that returns a value, the remote takes precedence.
	m.SetRemoteResolver(&stubRemoteResolver[int]{provider: 99})
	val, err = m.Get("foo")
	if err != nil || val != 99 {
		t.Fatalf("Get with resolver: got %d, err %v (want 99)", val, err)
	}

	// When the resolver returns ErrNotFound, fall back to local.
	m.SetRemoteResolver(&stubRemoteResolver[int]{err: core.ErrNotFound})
	val, err = m.Get("foo")
	if err != nil || val != 42 {
		t.Fatalf("Get with ErrNotFound resolver: got %d, err %v (want 42)", val, err)
	}
}

func TestProviderMapRemoteResolverOnlyRemote(t *testing.T) {
	t.Parallel()
	m := newProviderMap[int]("test")
	m.SetRemoteResolver(&stubRemoteResolver[int]{provider: 77})

	// A name not in the local map should resolve via the remote resolver.
	val, err := m.Get("remote-only")
	if err != nil || val != 77 {
		t.Fatalf("Get remote-only: got %d, err %v (want 77)", val, err)
	}
}

func TestProviderMapNoResolverStillWorks(t *testing.T) {
	t.Parallel()
	m := newProviderMap[int]("test")
	if err := m.Register("local", 1); err != nil {
		t.Fatal(err)
	}
	val, err := m.Get("local")
	if err != nil || val != 1 {
		t.Fatalf("Get without resolver: got %d, err %v", val, err)
	}
	_, err = m.Get("missing")
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("Get missing: expected ErrNotFound, got %v", err)
	}
}

// TestProviderMapRemoteResolverNonErrNotFoundErrorPropagates verifies that
// when the resolver returns a non-ErrNotFound error (e.g. tunnel registration
// exists but proxy construction failed), the error is propagated to the caller
// rather than silently falling back to the local provider.
func TestProviderMapRemoteResolverNonErrNotFoundErrorPropagates(t *testing.T) {
	t.Parallel()
	m := newProviderMap[int]("test")
	if err := m.Register("foo", 42); err != nil {
		t.Fatal(err)
	}

	// A non-ErrNotFound error must propagate, not fall back to local.
	tunnelErr := errors.New("tunnel provider is registered but unavailable")
	m.SetRemoteResolver(&stubRemoteResolver[int]{err: tunnelErr})
	_, err := m.Get("foo")
	if err == nil {
		t.Fatal("Get with non-ErrNotFound error should propagate, not fall back")
	}
	if !errors.Is(err, tunnelErr) {
		t.Fatalf("Get should propagate the resolver error, got: %v", err)
	}
}
