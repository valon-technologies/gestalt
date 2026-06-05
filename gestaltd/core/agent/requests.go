package agent

import proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"

type ResolveToolsRequest struct {
	ToolRefs   []ToolRef
	ToolSource ToolSourceMode
}

type ExecuteToolRequest struct {
	ProviderName   string
	SessionID      string
	TurnID         string
	ToolCallID     string
	ToolID         string
	Arguments      map[string]any
	RunGrant       string
	IdempotencyKey string
	Context        *proto.RequestContext
}

type ExecuteToolResponse struct {
	Status int
	Body   string
}

type ListToolsRequest struct {
	ProviderName string
	SessionID    string
	TurnID       string
	PageSize     int
	PageToken    string
	Query        string
	ToolRefs     []ToolRef
	ToolSource   ToolSourceMode
	RunGrant     string
	Context      *proto.RequestContext
}

type ListToolsResponse struct {
	Tools         []ListedTool
	NextPageToken string
}

type ResolveConnectionRequest struct {
	ProviderName string
	SessionID    string
	TurnID       string
	Connection   string
	Instance     string
	RunGrant     string
	Context      *proto.RequestContext
}
