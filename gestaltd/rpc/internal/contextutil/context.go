// Package contextutil provides context handling shared by RPC clients.
package contextutil

import (
	"cmp"
	"context"
	"time"
)

// WithOptionalTimeout applies a positive timeout, defaulting nil contexts to Background.
func WithOptionalTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx = cmp.Or(ctx, context.Background())
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}
