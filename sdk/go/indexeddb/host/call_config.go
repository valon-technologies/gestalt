package host

import (
	"context"
	"time"
)

type rpcConfig struct {
	rpcTimeout time.Duration
}

func (c rpcConfig) withDeadline(parent context.Context) (context.Context, context.CancelFunc) {
	if c.rpcTimeout <= 0 {
		if parent == nil {
			return context.Background(), func() {}
		}
		return parent, func() {}
	}
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, c.rpcTimeout)
}
