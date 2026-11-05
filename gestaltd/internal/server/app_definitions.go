package server

import (
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

// AppDefinitionOverlay supplies app entries and connection routing that
// replace boot-time config for registry-installed apps once a version starts.
// bootstrap.AppDefinitions implements it.
type AppDefinitionOverlay interface {
	invocation.ConnectionOverrides
	AppEntry(name string) (*config.ProviderEntry, bool)
	Generation() uint64
}

// appDef returns app's effective config entry: the registry overlay while an
// installed version is running, otherwise the boot-time entry.
func (s *Server) appDef(name string) *config.ProviderEntry {
	entry, _ := s.lookupAppDef(name)
	return entry
}

// lookupAppDef is appDef with presence reporting, for callers that
// distinguish an unconfigured app from a nil entry.
func (s *Server) lookupAppDef(name string) (*config.ProviderEntry, bool) {
	if s.appDefinitions != nil {
		if entry, ok := s.appDefinitions.AppEntry(name); ok {
			return entry, true
		}
	}
	entry, ok := s.pluginDefs[name]
	return entry, ok
}

func (s *Server) appDefinitionsGeneration() uint64 {
	if s.appDefinitions == nil {
		return 0
	}
	return s.appDefinitions.Generation()
}

func (s *Server) connectionNameFor(surface invocation.ConnectionSurface, static map[string]string, provider string) string {
	if s.appDefinitions == nil {
		return static[provider]
	}
	return invocation.ConnectionNameFor(s.appDefinitions, surface, static, provider)
}

// overlayConnectionOverrides converts a possibly nil overlay to a
// ConnectionOverrides that is nil when the overlay is.
func overlayConnectionOverrides(overlay AppDefinitionOverlay) invocation.ConnectionOverrides {
	if overlay == nil {
		return nil
	}
	return overlay
}
