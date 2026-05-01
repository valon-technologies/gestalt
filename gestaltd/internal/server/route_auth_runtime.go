package server

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

type authRuntime struct {
	providerRef  string
	providerName string
	provider     core.AuthenticationProvider
	resolver     *principal.Resolver
	noAuth       bool
	anonymous    *principal.Principal
}

func (s *Server) serverAuthRuntime() authRuntime {
	return authRuntime{
		providerRef:  "server",
		providerName: s.authProviderName(),
		provider:     s.auth,
		resolver:     s.resolver,
		noAuth:       s.noAuth,
		anonymous:    s.anonymousPrincipal,
	}
}

func (s *Server) authRuntimeForProvider(providerName string) (authRuntime, error) {
	providerName = strings.TrimSpace(providerName)
	if providerName == "" || providerName == "server" {
		return s.serverAuthRuntime(), nil
	}

	provider := s.authProviders[providerName]
	resolver := s.authResolvers[providerName]
	if provider == nil || resolver == nil {
		return authRuntime{
			providerRef:  providerName,
			providerName: providerName,
		}, fmt.Errorf("plugin route auth provider %q is not initialized", providerName)
	}

	runtime := authRuntime{
		providerRef:  providerName,
		providerName: providerName,
		provider:     provider,
		resolver:     resolver,
	}
	if provider.Name() == "none" {
		runtime.noAuth = true
		runtime.anonymous = s.anonymousPrincipal
		if runtime.anonymous == nil {
			runtime.anonymous = s.resolver.ResolveEmail(anonymousEmail)
		}
	}
	return runtime, nil
}

func (s *Server) pluginAuthRuntime(pluginName string) (authRuntime, error) {
	pluginName = strings.TrimSpace(pluginName)
	if pluginName == "" {
		return s.serverAuthRuntime(), nil
	}

	entry := s.pluginDefs[pluginName]
	if entry == nil || entry.RouteAuth == nil {
		return s.serverAuthRuntime(), nil
	}
	return s.authRuntimeForProvider(entry.RouteAuth.Provider)
}

func (s *Server) mountedUIAuthRuntime(mounted MountedUI) (authRuntime, error) {
	if strings.TrimSpace(mounted.PluginName) == "" {
		return s.serverAuthRuntime(), nil
	}
	return s.pluginAuthRuntime(mounted.PluginName)
}

func (s *Server) loginAuthRuntimeForNextPath(nextPath string) (authRuntime, error) {
	mounted, ok := s.mountedUIForNextPath(nextPath)
	if !ok {
		return s.serverAuthRuntime(), nil
	}
	return s.mountedUIAuthRuntime(mounted)
}

func (s *Server) mountedUIForNextPath(nextPath string) (MountedUI, bool) {
	parsed, err := url.Parse(nextPath)
	if err != nil {
		return MountedUI{}, false
	}
	path := parsed.Path
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return s.mountedUIForPath(path)
}

func (s *Server) mountedUIForPath(path string) (MountedUI, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	var (
		best        MountedUI
		bestLen     int
		bestMatched bool
	)
	consider := func(candidate MountedUI) {
		if candidate.Path == "" || !mountedUIPathMatches(path, candidate.Path) {
			return
		}
		if !bestMatched || len(candidate.Path) > bestLen {
			best = candidate
			bestLen = len(candidate.Path)
			bestMatched = true
		}
	}

	for _, mounted := range s.mountedUIs {
		consider(mounted)
	}
	if s.adminUI != nil {
		consider(s.adminMountedUI())
	}
	return best, bestMatched
}

func mountedUIPathMatches(requestPath, mountedPath string) bool {
	if mountedPath == "/" {
		return strings.HasPrefix(requestPath, "/")
	}
	return requestPath == mountedPath || strings.HasPrefix(requestPath, mountedPath+"/")
}
