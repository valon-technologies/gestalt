package bootstrap

import (
	"context"
	"errors"
	"testing"
	"time"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
)

// stubRetryProvider satisfies coreworkflow.Provider for retry tests. Its methods
// are never called; only its non-nil identity is asserted.
type stubRetryProvider struct{ coreworkflow.Provider }

func TestBuildWorkflowProviderWithRetry(t *testing.T) {
	orig := workflowProviderBuildBackoff
	workflowProviderBuildBackoff = time.Millisecond
	t.Cleanup(func() { workflowProviderBuildBackoff = orig })

	t.Run("recovers after transient failures", func(t *testing.T) {
		stub := &stubRetryProvider{}
		calls := 0
		provider, err := buildWorkflowProviderWithRetry(context.Background(), func() (coreworkflow.Provider, error) {
			calls++
			if calls < 3 {
				return nil, errors.New("connect temporal: context deadline exceeded")
			}
			return stub, nil
		})
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if provider != coreworkflow.Provider(stub) {
			t.Fatalf("provider = %v, want stub", provider)
		}
		if calls != 3 {
			t.Fatalf("calls = %d, want 3", calls)
		}
	})

	t.Run("surfaces last error after exhausting attempts", func(t *testing.T) {
		sentinel := errors.New("permanent failure")
		calls := 0
		_, err := buildWorkflowProviderWithRetry(context.Background(), func() (coreworkflow.Provider, error) {
			calls++
			return nil, sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("err = %v, want sentinel", err)
		}
		if calls != workflowProviderBuildAttempts {
			t.Fatalf("calls = %d, want %d", calls, workflowProviderBuildAttempts)
		}
	})

	t.Run("stops promptly on context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		calls := 0
		_, err := buildWorkflowProviderWithRetry(ctx, func() (coreworkflow.Provider, error) {
			calls++
			return nil, errors.New("transient")
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if calls != 1 {
			t.Fatalf("calls = %d, want 1", calls)
		}
	})
}
