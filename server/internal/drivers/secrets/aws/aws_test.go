package aws

import (
	"context"
	"errors"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"

	"github.com/valon-technologies/gestalt/server/core"
)

type stubClient struct {
	gotInput *secretsmanager.GetSecretValueInput
	value    *secretsmanager.GetSecretValueOutput
	err      error
}

func (s *stubClient) GetSecretValue(_ context.Context, input *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	s.gotInput = input
	return s.value, s.err
}

func TestProvider(t *testing.T) {
	t.Parallel()

	t.Run("resolves secret with correct parameters", func(t *testing.T) {
		t.Parallel()
		stub := &stubClient{
			value: &secretsmanager.GetSecretValueOutput{
				SecretString: awssdk.String("hunter2"),
			},
		}
		p := &Provider{client: stub, versionStage: "AWSCURRENT"}

		val, err := p.GetSecret(context.Background(), "db-password")
		if err != nil {
			t.Fatalf("GetSecret: %v", err)
		}
		if val != "hunter2" {
			t.Errorf("got %q, want %q", val, "hunter2")
		}
		if awssdk.ToString(stub.gotInput.SecretId) != "db-password" {
			t.Errorf("SecretId = %q, want %q", awssdk.ToString(stub.gotInput.SecretId), "db-password")
		}
		if awssdk.ToString(stub.gotInput.VersionStage) != "AWSCURRENT" {
			t.Errorf("VersionStage = %q, want %q", awssdk.ToString(stub.gotInput.VersionStage), "AWSCURRENT")
		}
	})

	t.Run("passes custom version stage", func(t *testing.T) {
		t.Parallel()
		stub := &stubClient{
			value: &secretsmanager.GetSecretValueOutput{
				SecretString: awssdk.String("old-value"),
			},
		}
		p := &Provider{client: stub, versionStage: "AWSPREVIOUS"}

		_, err := p.GetSecret(context.Background(), "db-password")
		if err != nil {
			t.Fatalf("GetSecret: %v", err)
		}
		if awssdk.ToString(stub.gotInput.VersionStage) != "AWSPREVIOUS" {
			t.Errorf("VersionStage = %q, want %q", awssdk.ToString(stub.gotInput.VersionStage), "AWSPREVIOUS")
		}
	})

	t.Run("returns ErrSecretNotFound for missing secret", func(t *testing.T) {
		t.Parallel()
		p := &Provider{
			client:       &stubClient{err: &types.ResourceNotFoundException{Message: awssdk.String("not found")}},
			versionStage: "AWSCURRENT",
		}
		_, err := p.GetSecret(context.Background(), "missing")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, core.ErrSecretNotFound) {
			t.Errorf("expected ErrSecretNotFound, got: %v", err)
		}
	})

	t.Run("returns error for binary secret", func(t *testing.T) {
		t.Parallel()
		p := &Provider{
			client: &stubClient{
				value: &secretsmanager.GetSecretValueOutput{SecretBinary: []byte("binary-data")},
			},
			versionStage: "AWSCURRENT",
		}
		_, err := p.GetSecret(context.Background(), "binary-secret")
		if err == nil {
			t.Fatal("expected error for binary secret, got nil")
		}
	})

	t.Run("wraps unexpected errors", func(t *testing.T) {
		t.Parallel()
		p := &Provider{
			client:       &stubClient{err: errors.New("network timeout")},
			versionStage: "AWSCURRENT",
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
