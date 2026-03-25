package provider

import (
	"sync"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/apiexec"
)

var (
	hooksMu          sync.RWMutex
	requestMutators  = map[string]func(string, *apiexec.Request, map[string]any) error{}
	postConnectHooks = map[string]core.PostConnectHook{}
)

func RegisterRequestMutator(name string, fn func(string, *apiexec.Request, map[string]any) error) {
	hooksMu.Lock()
	defer hooksMu.Unlock()
	requestMutators[name] = fn
}

func lookupRequestMutator(name string) (func(string, *apiexec.Request, map[string]any) error, bool) {
	hooksMu.RLock()
	defer hooksMu.RUnlock()
	fn, ok := requestMutators[name]
	return fn, ok
}

func RegisterPostConnectHook(name string, fn core.PostConnectHook) {
	hooksMu.Lock()
	defer hooksMu.Unlock()
	postConnectHooks[name] = fn
}

func lookupPostConnectHook(name string) (core.PostConnectHook, bool) {
	hooksMu.RLock()
	defer hooksMu.RUnlock()
	fn, ok := postConnectHooks[name]
	return fn, ok
}
