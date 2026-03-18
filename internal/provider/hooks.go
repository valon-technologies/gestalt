package provider

import (
	"github.com/valon-technologies/toolshed/internal/apiexec"
	"github.com/valon-technologies/toolshed/internal/oauth"
)

var responseCheckers = map[string]apiexec.ResponseChecker{}
var tokenParsers = map[string]func(string) (string, map[string]string, error){}
var requestMutators = map[string]func(string, *apiexec.Request, map[string]any) error{}
var responseHooks = map[string]oauth.ResponseHook{}

func RegisterResponseChecker(name string, fn apiexec.ResponseChecker) {
	responseCheckers[name] = fn
}

func RegisterTokenParser(name string, fn func(string) (string, map[string]string, error)) {
	tokenParsers[name] = fn
}

func RegisterRequestMutator(name string, fn func(string, *apiexec.Request, map[string]any) error) {
	requestMutators[name] = fn
}

func RegisterResponseHook(name string, fn oauth.ResponseHook) {
	responseHooks[name] = fn
}
