package agentmanager

import coreagent "github.com/valon-technologies/gestalt/server/core/agent"

type ResumeRunInteractionManagingProvider interface {
	ManagesResumeRunInteractions() bool
}

func providerManagesResumeRunInteractions(provider coreagent.Provider) bool {
	manager, ok := provider.(ResumeRunInteractionManagingProvider)
	return ok && manager.ManagesResumeRunInteractions()
}
