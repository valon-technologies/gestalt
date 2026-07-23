package bootstrap

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRetryProviderBuildDoesNotRetryPermanentFailure(t *testing.T) {
	t.Parallel()

	attempts := 0
	_, err := retryProviderBuild(context.Background(), func(context.Context) (*ProviderBuildResult, error) {
		attempts++
		return nil, status.Error(codes.InvalidArgument, "invalid config")
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("error = %v, want InvalidArgument", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestRetryProviderBuildStopsWhenContextCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	_, err := retryProviderBuild(ctx, func(context.Context) (*ProviderBuildResult, error) {
		attempts++
		cancel()
		return nil, status.Error(codes.Unavailable, "relay unavailable")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestTransientProviderBuildErrorRejectsOperationDeadline(t *testing.T) {
	t.Parallel()

	if isTransientProviderBuildError(context.DeadlineExceeded) {
		t.Fatal("context deadline exceeded must not be retried")
	}
}
