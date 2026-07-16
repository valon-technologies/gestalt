package bootstrap

import (
	"context"
	"strings"
	"sync"
)

// appProviderLifecycles serializes initial activation and catalog-driven
// restart work for each app.
type appProviderLifecycles struct {
	gates sync.Map
}

func (l *appProviderLifecycles) acquire(ctx context.Context, app string) (func(), error) {
	if l == nil {
		return func() {}, nil
	}
	gateValue, _ := l.gates.LoadOrStore(strings.TrimSpace(app), make(chan struct{}, 1))
	gate := gateValue.(chan struct{})
	select {
	case gate <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-gate }) }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
