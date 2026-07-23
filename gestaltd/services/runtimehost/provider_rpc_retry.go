package runtimehost

import (
	"context"
	"errors"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	DefaultProviderRPCRetryInterval = 25 * time.Millisecond
	// ProviderStartupRetryTimeout bounds total startup retry time when callers
	// pass an unbounded context.
	ProviderStartupRetryTimeout     = 2 * time.Minute
	providerConfigureAttemptTimeout = 90 * time.Second
	providerStartAttemptTimeout     = 30 * time.Second
)

func isStartupTransient(err error) bool {
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
	case codes.Unavailable, codes.ResourceExhausted:
		return true
	case codes.DeadlineExceeded:
		return true
	default:
		msg := strings.ToLower(err.Error())
		return strings.Contains(msg, "connection refused") ||
			strings.Contains(msg, "connection reset")
	}
}

func withStartupBudget(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	if _, ok := parent.Deadline(); ok {
		return parent, func() {}
	}
	return context.WithTimeout(parent, ProviderStartupRetryTimeout)
}

func attemptContext(parent context.Context, attemptTimeout time.Duration) (context.Context, context.CancelFunc) {
	if attemptTimeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, attemptTimeout)
}

// CallWhileStarting retries op while the hosted relay is still coming up.
// attemptTimeout caps each try; when zero, each try uses the remaining parent budget.
func CallWhileStarting[T any](parent context.Context, attemptTimeout time.Duration, op func(context.Context) (T, error)) (T, error) {
	var zero T
	parent, cancel := withStartupBudget(parent)
	defer cancel()

	interval := DefaultProviderRPCRetryInterval
	if interval <= 0 {
		interval = 25 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastErr error
	for {
		attemptCtx, attemptCancel := attemptContext(parent, attemptTimeout)
		result, err := op(attemptCtx)
		attemptCancel()
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !isStartupTransient(err) || parent.Err() != nil {
			return zero, err
		}

		select {
		case <-parent.Done():
			return zero, lastErr
		case <-ticker.C:
		}
	}
}

func CallWhileStartingNoResult(parent context.Context, attemptTimeout time.Duration, op func(context.Context) error) error {
	_, err := CallWhileStarting(parent, attemptTimeout, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, op(ctx)
	})
	return err
}
