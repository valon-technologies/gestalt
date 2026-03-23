package sandbox

import (
	"context"
	"encoding/json"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/tools"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/principal"
	pb "github.com/valon-technologies/gestalt/plugins/agentic/sandbox/pb"
)

type ToolServer struct {
	pb.UnimplementedToolServiceServer
	invoker invocation.Invoker
	lister  invocation.CapabilityLister
}

func NewToolServer(invoker invocation.Invoker, lister invocation.CapabilityLister) *ToolServer {
	return &ToolServer{invoker: invoker, lister: lister}
}

func (s *ToolServer) ExecuteTool(ctx context.Context, req *pb.ToolRequest) (*pb.ToolResponse, error) {
	var params map[string]any
	if req.ParamsJson != "" {
		if err := json.Unmarshal([]byte(req.ParamsJson), &params); err != nil {
			return &pb.ToolResponse{IsError: true, ErrorMessage: "invalid params: " + err.Error()}, nil //nolint:nilerr // application error in response body
		}
	}
	p := &principal.Principal{UserID: req.UserId}
	result, err := s.invoker.Invoke(ctx, p, req.Provider, req.Operation, params)
	if err != nil {
		return &pb.ToolResponse{IsError: true, ErrorMessage: err.Error()}, nil //nolint:nilerr // application error in response body
	}
	return &pb.ToolResponse{ResultJson: result.Body}, nil
}

func (s *ToolServer) ListTools(_ context.Context, req *pb.ListToolsRequest) (*pb.ListToolsResponse, error) {
	caps := s.lister.ListCapabilities()

	if len(req.Providers) > 0 {
		allowed := make(map[string]bool, len(req.Providers))
		for _, p := range req.Providers {
			allowed[p] = true
		}
		var filtered []core.Capability
		for _, c := range caps {
			if allowed[c.Provider] {
				filtered = append(filtered, c)
			}
		}
		caps = filtered
	}

	toolDefs := tools.CapabilitiesToTools(caps)

	resp := &pb.ListToolsResponse{}
	for i, td := range toolDefs {
		schemaJSON, _ := json.Marshal(td.InputSchema)
		resp.Tools = append(resp.Tools, &pb.ToolDefinition{
			Name:            td.Name,
			Description:     td.Description,
			InputSchemaJson: string(schemaJSON),
			Provider:        caps[i].Provider,
			Operation:       caps[i].Operation,
		})
	}
	return resp, nil
}
