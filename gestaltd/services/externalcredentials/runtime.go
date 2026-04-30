package externalcredentials

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
