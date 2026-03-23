package sandbox

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/valon-technologies/gestalt/core"
	pb "github.com/valon-technologies/gestalt/internal/sandbox/pb"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/principal"
)

type toolServer struct {
	pb.UnimplementedToolServiceServer
	invoker invocation.Invoker
	lister  invocation.CapabilityLister
}

func NewToolServer(invoker invocation.Invoker, lister invocation.CapabilityLister) *toolServer {
	return &toolServer{invoker: invoker, lister: lister}
}

func (s *toolServer) ExecuteTool(ctx context.Context, req *pb.ToolRequest) (*pb.ToolResponse, error) {
	p := &principal.Principal{UserID: req.GetUserId()}

	var params map[string]any
	if raw := req.GetParamsJson(); raw != "" {
		if err := json.Unmarshal([]byte(raw), &params); err != nil {
			return &pb.ToolResponse{Error: fmt.Sprintf("invalid params_json: %v", err)}, nil
		}
	}

	result, err := s.invoker.Invoke(ctx, p, req.GetProvider(), req.GetOperation(), params)
	if err != nil {
		return &pb.ToolResponse{Error: err.Error()}, nil
	}

	return &pb.ToolResponse{
		ResultJson: result.Body,
		Status:     int32(result.Status),
	}, nil
}

func (s *toolServer) ListTools(_ context.Context, req *pb.ListToolsRequest) (*pb.ListToolsResponse, error) {
	caps := s.lister.ListCapabilities()

	providerFilter := make(map[string]struct{}, len(req.GetProviders()))
	for _, p := range req.GetProviders() {
		providerFilter[p] = struct{}{}
	}

	var tools []*pb.ToolDefinition
	for _, cap := range caps {
		if len(providerFilter) > 0 {
			if _, ok := providerFilter[cap.Provider]; !ok {
				continue
			}
		}
		schema := buildInputSchema(cap.Parameters)
		schemaJSON, _ := json.Marshal(schema)

		tools = append(tools, &pb.ToolDefinition{
			Name:            cap.Provider + "." + cap.Operation,
			Provider:        cap.Provider,
			Operation:       cap.Operation,
			Description:     cap.Description,
			InputSchemaJson: string(schemaJSON),
		})
	}

	return &pb.ListToolsResponse{Tools: tools}, nil
}

func buildInputSchema(params []core.Parameter) map[string]any {
	properties := make(map[string]any, len(params))
	var required []string

	for _, p := range params {
		prop := map[string]any{"type": p.Type}
		if p.Description != "" {
			prop["description"] = p.Description
		}
		properties[p.Name] = prop
		if p.Required {
			required = append(required, p.Name)
		}
	}

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}
