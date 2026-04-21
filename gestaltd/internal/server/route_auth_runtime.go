package server

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/principal"
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

func (s *Server) mountedWebUIAuthRuntime(mounted MountedWebUI) (authRuntime, error) {
	if strings.TrimSpace(mounted.PluginName) == "" {
		return s.serverAuthRuntime(), nil
	}
	return s.pluginAuthRuntime(mounted.PluginName)
}

func (s *Server) loginAuthRuntimeForNextPath(nextPath string) (authRuntime, error) {
	mounted, ok := s.mountedWebUIForNextPath(nextPath)
	if !ok {
		return s.serverAuthRuntime(), nil
	}
	return s.mountedWebUIAuthRuntime(mounted)
}

func (s *Server) mountedWebUIForNextPath(nextPath string) (MountedWebUI, bool) {
	parsed, err := url.Parse(nextPath)
	if err != nil {
		return MountedWebUI{}, false
	}
	path := parsed.Path
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return s.mountedWebUIForPath(path)
}

func (s *Server) mountedWebUIForPath(path string) (MountedWebUI, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	var (
		best        MountedWebUI
		bestLen     int
		bestMatched bool
	)
	consider := func(candidate MountedWebUI) {
		if candidate.Path == "" || !mountedWebUIPathMatches(path, candidate.Path) {
			return
		}
		if !bestMatched || len(candidate.Path) > bestLen {
			best = candidate
			bestLen = len(candidate.Path)
			bestMatched = true
		}
	}

	for _, mounted := range s.mountedWebUIs {
		consider(mounted)
	}
	if s.adminUI != nil {
		consider(s.adminMountedWebUI())
	}
	return best, bestMatched
}

func mountedWebUIPathMatches(requestPath, mountedPath string) bool {
	if mountedPath == "/" {
		return strings.HasPrefix(requestPath, "/")
	}
	return requestPath == mountedPath || strings.HasPrefix(requestPath, mountedPath+"/")
}
