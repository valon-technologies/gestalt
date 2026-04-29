package server

import (
	"maps"
	"net/http"
	"strings"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

type workflowTargetInfo struct {
	Plugin *workflowPluginTargetInfo `json:"plugin,omitempty"`
	Agent  *workflowAgentTargetInfo  `json:"agent,omitempty"`
}

type workflowPluginTargetInfo struct {
	Name       string         `json:"name"`
	Operation  string         `json:"operation"`
	Connection string         `json:"connection,omitempty"`
	Instance   string         `json:"instance,omitempty"`
	Input      map[string]any `json:"input,omitempty"`
}

type workflowAgentTargetInfo struct {
	ProviderName    string                `json:"provider,omitempty"`
	Model           string                `json:"model,omitempty"`
	Prompt          string                `json:"prompt,omitempty"`
	Messages        []agentMessageRequest `json:"messages,omitempty"`
	ToolRefs        []agentToolRefRequest `json:"toolRefs,omitempty"`
	ResponseSchema  map[string]any        `json:"responseSchema,omitempty"`
	Metadata        map[string]any        `json:"metadata,omitempty"`
	ProviderOptions map[string]any        `json:"providerOptions,omitempty"`
	TimeoutSeconds  int                   `json:"timeoutSeconds,omitempty"`
}

func (s *Server) resolveWorkflowActor(w http.ResponseWriter, r *http.Request) (*principal.Principal, bool) {
	p := principal.Canonicalized(PrincipalFromContext(r.Context()))
	if p == nil {
		writeError(w, http.StatusUnauthorized, "missing authorization")
		return nil, false
	}
	if strings.TrimSpace(p.SubjectID) == "" {
		writeError(w, http.StatusUnauthorized, "missing subject")
		return nil, false
	}
	return p, true
}

func (s *Server) workflowScheduleCRUDRemoved(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusGone, "workflow schedule CRUD has been removed; configure schedules in YAML under workflows.schedules")
}

func (s *Server) workflowEventTriggerCRUDRemoved(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusGone, "workflow event trigger CRUD has been removed; configure event triggers in YAML under workflows.eventTriggers")
}

func workflowTargetInfoFromCore(target coreworkflow.Target) workflowTargetInfo {
	if target.Agent != nil {
		agentTarget := *target.Agent
		return workflowTargetInfo{
			Agent: &workflowAgentTargetInfo{
				ProviderName:    agentTarget.ProviderName,
				Model:           agentTarget.Model,
				Prompt:          agentTarget.Prompt,
				Messages:        agentMessageInfoFromCore(agentTarget.Messages),
				ToolRefs:        agentToolRefsToRequest(agentTarget.ToolRefs),
				ResponseSchema:  maps.Clone(agentTarget.ResponseSchema),
				Metadata:        maps.Clone(agentTarget.Metadata),
				ProviderOptions: maps.Clone(agentTarget.ProviderOptions),
				TimeoutSeconds:  agentTarget.TimeoutSeconds,
			},
		}
	}
	if target.Plugin == nil {
		return workflowTargetInfo{}
	}
	pluginTarget := *target.Plugin
	return workflowTargetInfo{
		Plugin: &workflowPluginTargetInfo{
			Name:       pluginTarget.PluginName,
			Operation:  pluginTarget.Operation,
			Connection: userFacingConnectionName(pluginTarget.Connection),
			Instance:   pluginTarget.Instance,
			Input:      maps.Clone(pluginTarget.Input),
		},
	}
}
