package provider

import (
	"sync"

	"github.com/valon-technologies/toolshed/internal/apiexec"
	"github.com/valon-technologies/toolshed/internal/oauth"
)

var (
	hooksMu          sync.RWMutex
	responseCheckers = map[string]apiexec.ResponseChecker{}
	tokenParsers     = map[string]func(string) (string, map[string]string, error){}
	requestMutators  = map[string]func(string, *apiexec.Request, map[string]any) error{}
	responseHooks    = map[string]oauth.ResponseHook{}
)

func RegisterResponseChecker(name string, fn apiexec.ResponseChecker) {
	hooksMu.Lock()
	defer hooksMu.Unlock()
	responseCheckers[name] = fn
}

func RegisterTokenParser(name string, fn func(string) (string, map[string]string, error)) {
	hooksMu.Lock()
	defer hooksMu.Unlock()
	tokenParsers[name] = fn
}

func RegisterRequestMutator(name string, fn func(string, *apiexec.Request, map[string]any) error) {
	hooksMu.Lock()
	defer hooksMu.Unlock()
	requestMutators[name] = fn
}

func RegisterResponseHook(name string, fn oauth.ResponseHook) {
	hooksMu.Lock()
	defer hooksMu.Unlock()
	responseHooks[name] = fn
}

func lookupResponseChecker(name string) (apiexec.ResponseChecker, bool) {
	hooksMu.RLock()
	defer hooksMu.RUnlock()
	fn, ok := responseCheckers[name]
	return fn, ok
}

func lookupTokenParser(name string) (func(string) (string, map[string]string, error), bool) {
	hooksMu.RLock()
	defer hooksMu.RUnlock()
	fn, ok := tokenParsers[name]
	return fn, ok
}

func lookupRequestMutator(name string) (func(string, *apiexec.Request, map[string]any) error, bool) {
	hooksMu.RLock()
	defer hooksMu.RUnlock()
	fn, ok := requestMutators[name]
	return fn, ok
}

func lookupResponseHook(name string) (oauth.ResponseHook, bool) {
	hooksMu.RLock()
	defer hooksMu.RUnlock()
	fn, ok := responseHooks[name]
	return fn, ok
}
