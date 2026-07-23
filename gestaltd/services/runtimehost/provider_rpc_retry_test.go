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

func TestIsTransientProviderRPCError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "canceled", err: context.Canceled, want: false},
		{name: "deadline exceeded", err: context.DeadlineExceeded, want: true},
		{name: "unavailable", err: status.Error(codes.Unavailable, "relay warming up"), want: true},
		{name: "connection refused message", err: fmt.Errorf("configure provider: dial tcp 10.10.0.5:8080: connection refused"), want: true},
		{name: "unknown", err: status.Error(codes.Unknown, "provider metadata exploded"), want: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsTransientProviderRPCError(tc.err); got != tc.want {
				t.Fatalf("IsTransientProviderRPCError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestRetryWhileTransientEventuallySucceeds(t *testing.T) {
	t.Parallel()

	attempts := 0
	result, err := RetryWhileTransient(context.Background(), time.Millisecond, func(context.Context) (string, error) {
		attempts++
		if attempts < 3 {
			return "", status.Error(codes.Unavailable, "warming up")
		}
		return "ready", nil
	})
	if err != nil {
		t.Fatalf("RetryWhileTransient: %v", err)
	}
	if result != "ready" {
		t.Fatalf("result = %q, want ready", result)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestRetryWhileTransientStopsOnPermanentError(t *testing.T) {
	t.Parallel()

	attempts := 0
	_, err := RetryWhileTransient(context.Background(), time.Millisecond, func(context.Context) (string, error) {
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

func TestRetryWhileTransientRespectsContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	attempts := 0
	_, err := RetryWhileTransient(ctx, time.Millisecond, func(context.Context) (string, error) {
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
