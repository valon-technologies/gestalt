package azure

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"

	"github.com/valon-technologies/gestalt/server/core"
)

type stubClient struct {
	gotName    string
	gotVersion string
	resp       azsecrets.GetSecretResponse
	err        error
}

func (s *stubClient) GetSecret(_ context.Context, name string, version string, _ *azsecrets.GetSecretOptions) (azsecrets.GetSecretResponse, error) {
	s.gotName = name
	s.gotVersion = version
	return s.resp, s.err
}

func strPtr(s string) *string { return &s }

func TestProvider(t *testing.T) {
	t.Parallel()

	t.Run("resolves secret with correct parameters", func(t *testing.T) {
		t.Parallel()
		stub := &stubClient{
			resp: azsecrets.GetSecretResponse{
				Secret: azsecrets.Secret{Value: strPtr("hunter2")},
			},
		}
		p := &Provider{client: stub, version: "abc123"}

		val, err := p.GetSecret(context.Background(), "db-password")
		if err != nil {
			t.Fatalf("GetSecret: %v", err)
		}
		if val != "hunter2" {
			t.Errorf("got %q, want %q", val, "hunter2")
		}
		if stub.gotName != "db-password" {
			t.Errorf("name = %q, want %q", stub.gotName, "db-password")
		}
		if stub.gotVersion != "abc123" {
			t.Errorf("version = %q, want %q", stub.gotVersion, "abc123")
		}
	})

	t.Run("passes empty version for latest", func(t *testing.T) {
		t.Parallel()
		stub := &stubClient{
			resp: azsecrets.GetSecretResponse{
				Secret: azsecrets.Secret{Value: strPtr("latest-value")},
			},
		}
		p := &Provider{client: stub, version: ""}

		_, err := p.GetSecret(context.Background(), "my-secret")
		if err != nil {
			t.Fatalf("GetSecret: %v", err)
		}
		if stub.gotVersion != "" {
			t.Errorf("version = %q, want empty string for latest", stub.gotVersion)
		}
	})

	t.Run("returns ErrSecretNotFound for 404", func(t *testing.T) {
		t.Parallel()
		p := &Provider{
			client: &stubClient{err: &azcore.ResponseError{StatusCode: http.StatusNotFound}},
		}
		_, err := p.GetSecret(context.Background(), "missing")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, core.ErrSecretNotFound) {
			t.Errorf("expected ErrSecretNotFound, got: %v", err)
		}
	})

	t.Run("returns error for nil value", func(t *testing.T) {
		t.Parallel()
		p := &Provider{
			client: &stubClient{
				resp: azsecrets.GetSecretResponse{
					Secret: azsecrets.Secret{Value: nil},
				},
			},
		}
		_, err := p.GetSecret(context.Background(), "nil-secret")
		if err == nil {
			t.Fatal("expected error for nil value, got nil")
		}
	})

	t.Run("wraps unexpected errors", func(t *testing.T) {
		t.Parallel()
		p := &Provider{
			client: &stubClient{err: errors.New("network timeout")},
		}
		_, err := p.GetSecret(context.Background(), "any-secret")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if errors.Is(err, core.ErrSecretNotFound) {
			t.Error("unexpected ErrSecretNotFound for network error")
		}
	})
}
