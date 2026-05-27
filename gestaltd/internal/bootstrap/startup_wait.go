package bootstrap

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type startupWaitTracker struct {
	mu    sync.Mutex
	waits map[startupProviderNode]map[startupProviderNode]int
}

func newStartupWaitTracker() *startupWaitTracker {
	return &startupWaitTracker{
		waits: make(map[startupProviderNode]map[startupProviderNode]int),
	}
}

type startupProviderNode struct {
	kind invocation.ProviderKind
	name string
}

func newStartupProviderNode(kind invocation.ProviderKind, name string) startupProviderNode {
	return startupProviderNode{kind: kind, name: strings.TrimSpace(name)}
}

func (n startupProviderNode) valid() bool {
	return n.kind != "" && strings.TrimSpace(n.name) != ""
}

func (n startupProviderNode) String() string {
	return fmt.Sprintf("%s %q", n.kind, n.name)
}

func startupProviderNodeFromContext(ctx context.Context) (startupProviderNode, bool) {
	caller := invocation.CallerProviderFromContext(ctx)
	if caller.Kind == "" || strings.TrimSpace(caller.Name) == "" {
		return startupProviderNode{}, false
	}
	return newStartupProviderNode(caller.Kind, caller.Name), true
}

func (t *startupWaitTracker) beginWait(waiting, target startupProviderNode) (func(), error) {
	waiting.name = strings.TrimSpace(waiting.name)
	target.name = strings.TrimSpace(target.name)
	if t == nil || !waiting.valid() || !target.valid() {
		return func() {}, nil
	}
	if waiting == target {
		return nil, fmt.Errorf("startup dependency cycle: %s -> %s", waiting, target)
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if path := t.waitPathLocked(target, waiting, nil); len(path) > 0 {
		return nil, fmt.Errorf("startup dependency cycle: %s", formatStartupWaitPath(append([]startupProviderNode{waiting}, path...)))
	}
	if t.waits[waiting] == nil {
		t.waits[waiting] = make(map[startupProviderNode]int)
	}
	t.waits[waiting][target]++
	return func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		if remaining := t.waits[waiting][target] - 1; remaining > 0 {
			t.waits[waiting][target] = remaining
		} else {
			delete(t.waits[waiting], target)
			if len(t.waits[waiting]) == 0 {
				delete(t.waits, waiting)
			}
		}
	}, nil
}

func (t *startupWaitTracker) beginCallerProviderWait(ctx context.Context, target startupProviderNode) (func(), bool, error) {
	if t == nil {
		return func() {}, false, nil
	}
	source, ok := startupProviderNodeFromContext(ctx)
	if !ok {
		return func() {}, false, nil
	}
	done, err := t.beginWait(source, target)
	if err != nil {
		return nil, true, err
	}
	return done, true, nil
}

func (t *startupWaitTracker) waitPathLocked(from, to startupProviderNode, seen map[startupProviderNode]bool) []startupProviderNode {
	if from == to {
		return []startupProviderNode{from}
	}
	if seen == nil {
		seen = make(map[startupProviderNode]bool)
	}
	if seen[from] {
		return nil
	}
	seen[from] = true
	for next, count := range t.waits[from] {
		if count <= 0 {
			continue
		}
		if path := t.waitPathLocked(next, to, seen); len(path) > 0 {
			return append([]startupProviderNode{from}, path...)
		}
	}
	return nil
}

func formatStartupWaitPath(path []startupProviderNode) string {
	if len(path) == 0 {
		return ""
	}
	parts := make([]string, 0, len(path))
	for _, node := range path {
		parts = append(parts, node.String())
	}
	return strings.Join(parts, " -> ")
}

type startupGate[T any] struct {
	ready chan struct{}
	once  sync.Once

	mu    sync.RWMutex
	value T
	err   error
}

func newStartupGate[T any]() startupGate[T] {
	return startupGate[T]{ready: make(chan struct{})}
}

func (g *startupGate[T]) finish(value T, err error) {
	g.once.Do(func() {
		g.mu.Lock()
		g.value = value
		g.err = err
		g.mu.Unlock()
		close(g.ready)
	})
}

func (g *startupGate[T]) await(ctx context.Context) (T, error) {
	var zero T
	select {
	case <-g.ready:
	case <-ctx.Done():
		return zero, ctx.Err()
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.err != nil {
		return zero, g.err
	}
	return g.value, nil
}

func (g *startupGate[T]) resolved() (T, bool, error) {
	var zero T
	select {
	case <-g.ready:
	default:
		return zero, false, nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.value, true, g.err
}
