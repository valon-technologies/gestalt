package bootstrap

import (
	"context"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
)

// LazyOperationHistory lets the broker be built before the app registry reader
// exists. Until a target is set it reports no history, which keeps legacy
// app access profiles authoritative.
type LazyOperationHistory struct {
	mu     sync.RWMutex
	target core.OperationHistory
}

func (l *LazyOperationHistory) SetTarget(target core.OperationHistory) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.target = target
}

func (l *LazyOperationHistory) OperationsAt(ctx context.Context, app string, at time.Time) ([]string, bool, error) {
	l.mu.RLock()
	target := l.target
	l.mu.RUnlock()
	if target == nil {
		return nil, false, nil
	}
	return target.OperationsAt(ctx, app, at)
}
