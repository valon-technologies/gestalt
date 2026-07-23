package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	providerBuildRetryInitialDelay = 100 * time.Millisecond
	providerBuildRetryMaxDelay     = 2 * time.Second
)

// retryProviderBuild retries the complete provider build transaction while its
// dependencies are temporarily unavailable. The builder owns cleanup for a
// failed attempt, so lifecycle RPCs with side effects are never replayed
// against the same provider process.
func retryProviderBuild(
	ctx context.Context,
	build func(context.Context) (*ProviderBuildResult, error),
) (*ProviderBuildResult, error) {
	delay := providerBuildRetryInitialDelay
	for {
		result, err := build(ctx)
		if err == nil || !isTransientProviderBuildError(err) {
			return result, err
		}

		slog.DebugContext(ctx, "retrying transient provider build failure", "error", err, "retry_in", delay)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("retry provider build: %w (last error: %v)", ctx.Err(), err)
		case <-timer.C:
		}

		delay *= 2
		if delay > providerBuildRetryMaxDelay {
			delay = providerBuildRetryMaxDelay
		}
	}
}

func isTransientProviderBuildError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if status.Code(err) == codes.Unavailable {
		return true
	}

	// Some provider SDKs currently wrap a callback's gRPC status, causing the
	// outer lifecycle RPC to arrive as Unknown. Retain the transport signal
	// until those SDKs preserve the original status.
	if status.Code(err) != codes.Unknown {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "connection refused") ||
		strings.Contains(message, "connection reset")
}
