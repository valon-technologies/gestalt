package runtimehost

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIsStartupTransient(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "canceled", err: context.Canceled, want: false},
		{name: "deadline exceeded", err: context.DeadlineExceeded, want: true},
		{name: "grpc deadline exceeded", err: status.Error(codes.DeadlineExceeded, "timed out"), want: true},
		{name: "unavailable", err: status.Error(codes.Unavailable, "relay warming up"), want: true},
		{name: "connection refused message", err: fmt.Errorf("configure provider: dial tcp 10.10.0.5:8080: connection refused"), want: true},
		{name: "unknown", err: status.Error(codes.Unknown, "provider metadata exploded"), want: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isStartupTransient(tc.err); got != tc.want {
				t.Fatalf("isStartupTransient(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestCallWhileStartingEventuallySucceeds(t *testing.T) {
	t.Parallel()

	attempts := 0
	result, err := CallWhileStarting(context.Background(), 50*time.Millisecond, func(context.Context) (string, error) {
		attempts++
		if attempts < 3 {
			return "", status.Error(codes.Unavailable, "warming up")
		}
		return "ready", nil
	})
	if err != nil {
		t.Fatalf("CallWhileStarting: %v", err)
	}
	if result != "ready" {
		t.Fatalf("result = %q, want ready", result)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestCallWhileStartingStopsOnPermanentError(t *testing.T) {
	t.Parallel()

	attempts := 0
	_, err := CallWhileStarting(context.Background(), 50*time.Millisecond, func(context.Context) (string, error) {
		attempts++
		return "", status.Error(codes.InvalidArgument, "bad config")
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("error = %v, want InvalidArgument", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestCallWhileStartingRespectsContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	attempts := 0
	_, err := CallWhileStarting(ctx, 50*time.Millisecond, func(context.Context) (string, error) {
		attempts++
		return "", status.Error(codes.Unavailable, "warming up")
	})
	if err == nil {
		t.Fatal("expected retry to fail")
	}
	if !errors.Is(err, context.Canceled) && status.Code(err) != codes.Unavailable {
		t.Fatalf("error = %v, want canceled or unavailable", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestCallWhileStartingRetriesAttemptDeadlineWithinBudget(t *testing.T) {
	t.Parallel()

	attempts := 0
	_, err := CallWhileStarting(context.Background(), 20*time.Millisecond, func(ctx context.Context) (struct{}, error) {
		attempts++
		if attempts < 2 {
			<-ctx.Done()
			return struct{}{}, context.DeadlineExceeded
		}
		return struct{}{}, nil
	})
	if err != nil {
		t.Fatalf("CallWhileStarting: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestWithStartupBudgetAddsTimeoutWhenMissing(t *testing.T) {
	t.Parallel()

	ctx, cancel := withStartupBudget(context.Background())
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected deadline")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > ProviderStartupRetryTimeout {
		t.Fatalf("remaining deadline = %s, want (0, %s]", remaining, ProviderStartupRetryTimeout)
	}
}

func TestWithStartupBudgetPreservesParentDeadline(t *testing.T) {
	t.Parallel()

	parentDeadline := time.Now().Add(2 * time.Second)
	parent, parentCancel := context.WithDeadline(context.Background(), parentDeadline)
	defer parentCancel()

	ctx, cancel := withStartupBudget(parent)
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok || !deadline.Equal(parentDeadline) {
		t.Fatalf("deadline = %v, want parent deadline %v", deadline, parentDeadline)
	}
}
