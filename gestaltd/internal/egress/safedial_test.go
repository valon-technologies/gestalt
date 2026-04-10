package egress_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/egress"
)

func TestSafeClientBlocksLoopbackServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := egress.SafeClient(&egress.PrivateNetworkPolicy{AllowPrivateNetworks: false}, 5*time.Second)
	_, err := client.Get(srv.URL)
	if err == nil {
		t.Fatal("expected request to loopback server to be blocked")
	}
	if !errors.Is(err, egress.ErrEgressDenied) {
		t.Fatalf("expected ErrEgressDenied, got: %v", err)
	}
}

func TestSafeClientAllowsLoopbackWhenPermitted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := egress.SafeClient(&egress.PrivateNetworkPolicy{AllowPrivateNetworks: true}, 5*time.Second)
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("expected request to succeed with AllowPrivateNetworks=true, got: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestSafeClientNilPolicyAllowsLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := egress.SafeClient(nil, 5*time.Second)
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("expected request to succeed with nil policy, got: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}
