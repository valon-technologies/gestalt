package invocation

import (
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

type AgentAppAuthorizationRequest struct {
	AgentProviderName string
	CallerKind        ProviderKind
	CallerName        string
	Agent             AgentInvocationContext
	Principal         *principal.Principal
	App               string
	Operation         string
	Connection        string
	Instance          string
	CredentialMode    core.ConnectionMode
	RequestContext    *proto.RequestContext
}

type AgentAppAuthorization struct {
	Principal      *principal.Principal
	CredentialMode core.ConnectionMode
	Connection     string
	Instance       string
	RunAs          *core.RunAsSubject
	ToolRefs       []coreagent.ToolRef
	ToolRefsSet    bool
}

type AgentWorkflowAuthorizationRequest struct {
	AgentProviderName string
	CallerKind        ProviderKind
	CallerName        string
	Agent             AgentInvocationContext
	Principal         *principal.Principal
	Operation         string
	Target            *coreworkflow.Target
	RequestContext    *proto.RequestContext
}

type AgentWorkflowAuthorization struct {
	Principal *principal.Principal
}
