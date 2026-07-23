package runtimehost

import (
	"context"
	"errors"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const DefaultProviderRPCRetryInterval = 25 * time.Millisecond

// IsTransientProviderRPCError reports whether a provider startup RPC should be
// retried while the hosted runtime or host-service relay is still coming up.
func IsTransientProviderRPCError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
		return true
	default:
		msg := strings.ToLower(err.Error())
		return strings.Contains(msg, "connection refused") ||
			strings.Contains(msg, "connection reset")
	}
}

func RetryWhileTransient[T any](ctx context.Context, interval time.Duration, op func(context.Context) (T, error)) (T, error) {
	var zero T
	if interval <= 0 {
		interval = DefaultProviderRPCRetryInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		result, err := op(ctx)
		if err == nil || !IsTransientProviderRPCError(err) {
			return result, err
		}
		select {
		case <-ctx.Done():
			return zero, err
		case <-ticker.C:
		}
	}
}

func RetryWhileTransientNoResult(ctx context.Context, interval time.Duration, op func(context.Context) error) error {
	_, err := RetryWhileTransient(ctx, interval, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, op(ctx)
	})
	return err
}
