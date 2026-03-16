package registry

import (
	"errors"
	"strconv"
	"sync"
	"testing"

	"github.com/valon-technologies/toolshed/core"
	coretesting "github.com/valon-technologies/toolshed/core/testing"
)

func TestPluginMapRegisterAndGet(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		register func(r *Registry) error
		get      func(r *Registry) (any, error)
	}{
		{
			name:     "datastore",
			register: func(r *Registry) error { return r.Datastores.Register("sqlite", &coretesting.StubDatastore{}) },
			get:      func(r *Registry) (any, error) { return r.Datastores.Get("sqlite") },
		},
		{
			name: "auth provider",
			register: func(r *Registry) error {
				return r.AuthProviders.Register("google", &coretesting.StubAuthProvider{N: "google"})
			},
			get: func(r *Registry) (any, error) { return r.AuthProviders.Get("google") },
		},
		{
			name: "integration",
			register: func(r *Registry) error {
				return r.Integrations.Register("slack", &coretesting.StubIntegration{N: "slack"})
			},
			get: func(r *Registry) (any, error) { return r.Integrations.Get("slack") },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := New()
			if err := tc.register(r); err != nil {
				t.Fatalf("Register: %v", err)
			}
			got, err := tc.get(r)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got == nil {
				t.Fatal("Get returned nil")
			}
		})
	}
}

func TestPluginMapDuplicateRegistration(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		register func(r *Registry) error
	}{
		{
			name:     "datastore",
			register: func(r *Registry) error { return r.Datastores.Register("sqlite", &coretesting.StubDatastore{}) },
		},
		{
			name: "auth provider",
			register: func(r *Registry) error {
				return r.AuthProviders.Register("google", &coretesting.StubAuthProvider{N: "google"})
			},
		},
		{
			name: "integration",
			register: func(r *Registry) error {
				return r.Integrations.Register("slack", &coretesting.StubIntegration{N: "slack"})
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := New()
			if err := tc.register(r); err != nil {
				t.Fatalf("first Register: %v", err)
			}
			err := tc.register(r)
			if err == nil {
				t.Fatal("second Register: expected error, got nil")
			}
			if !errors.Is(err, core.ErrAlreadyRegistered) {
				t.Errorf("expected ErrAlreadyRegistered, got: %v", err)
			}
		})
	}
}

func TestPluginMapNotFound(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		get  func(r *Registry) (any, error)
	}{
		{name: "datastore", get: func(r *Registry) (any, error) { return r.Datastores.Get("nope") }},
		{name: "auth provider", get: func(r *Registry) (any, error) { return r.AuthProviders.Get("nope") }},
		{name: "integration", get: func(r *Registry) (any, error) { return r.Integrations.Get("nope") }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := New()
			_, err := tc.get(r)
			if err == nil {
				t.Fatal("Get: expected error, got nil")
			}
			if !errors.Is(err, core.ErrNotFound) {
				t.Errorf("expected ErrNotFound, got: %v", err)
			}
		})
	}
}

func TestPluginMapList(t *testing.T) {
	t.Parallel()

	r := New()

	if got := r.Integrations.List(); len(got) != 0 {
		t.Fatalf("List on empty: got %v, want empty", got)
	}

	_ = r.Integrations.Register("slack", &coretesting.StubIntegration{N: "slack"})
	_ = r.Integrations.Register("pagerduty", &coretesting.StubIntegration{N: "pagerduty"})
	_ = r.Integrations.Register("datadog", &coretesting.StubIntegration{N: "datadog"})

	got := r.Integrations.List()
	want := []string{"datadog", "pagerduty", "slack"}

	if len(got) != len(want) {
		t.Fatalf("List: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("List[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRegistryIsolation(t *testing.T) {
	t.Parallel()

	r1 := New()
	r2 := New()

	_ = r1.Integrations.Register("slack", &coretesting.StubIntegration{N: "slack"})

	if _, err := r2.Integrations.Get("slack"); err == nil {
		t.Fatal("registries are not isolated: r2 found r1's integration")
	}
}

func TestConcurrentAccess(t *testing.T) {
	t.Parallel()

	r := New()
	const goroutines = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			name := "integration-" + strconv.Itoa(n)
			_ = r.Integrations.Register(name, &coretesting.StubIntegration{N: name})
			_, _ = r.Integrations.Get(name)
			_ = r.Integrations.List()
		}(i)
	}

	wg.Wait()

	if got := r.Integrations.List(); len(got) != goroutines {
		t.Fatalf("expected %d integrations, got %d", goroutines, len(got))
	}
}
