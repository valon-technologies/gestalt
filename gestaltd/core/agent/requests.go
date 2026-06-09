package agent

import proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"

type ResolveToolsRequest struct {
	ToolRefs   []ToolRef
	ToolSource ToolSourceMode
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
	Context      *proto.RequestContext
}

type ListToolsResponse struct {
	Tools         []ListedTool
	NextPageToken string
}
