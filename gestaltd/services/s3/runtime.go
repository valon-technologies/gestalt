package s3

import (
	"context"
	"time"
)

const providerRPCTimeout = 10 * time.Second

func providerCallContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, providerRPCTimeout)
}

func providerStreamContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithCancel(parent)
}
