package pluginsdk

import (
	"fmt"

	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginapi/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

func coreConnectionModeToProto(mode ConnectionMode) pluginapiv1.ConnectionMode {
	switch mode {
	case ConnectionModeNone, "":
		return pluginapiv1.ConnectionMode_CONNECTION_MODE_NONE
	case ConnectionModeUser:
		return pluginapiv1.ConnectionMode_CONNECTION_MODE_USER
	case ConnectionModeIdentity:
		return pluginapiv1.ConnectionMode_CONNECTION_MODE_IDENTITY
	case ConnectionModeEither:
		return pluginapiv1.ConnectionMode_CONNECTION_MODE_EITHER
	default:
		return pluginapiv1.ConnectionMode_CONNECTION_MODE_UNSPECIFIED
	}
}

func protoPluginMode(mode pluginapiv1.PluginMode) string {
	switch mode {
	case pluginapiv1.PluginMode_PLUGIN_MODE_OVERLAY:
		return PluginModeOverlay
	case pluginapiv1.PluginMode_PLUGIN_MODE_REPLACE:
		return PluginModeReplace
	default:
		return ""
	}
}

func mapFromStruct(s *structpb.Struct) map[string]any {
	if s == nil {
		return nil
	}
	return s.AsMap()
}

func valueToProto(v any) (*structpb.Value, error) {
	if v == nil {
		return nil, nil
	}
	return structpb.NewValue(v)
}

func parameterToProto(p Parameter) (*pluginapiv1.Parameter, error) {
	def, err := valueToProto(p.Default)
	if err != nil {
		return nil, fmt.Errorf("parameter %q default: %w", p.Name, err)
	}
	return &pluginapiv1.Parameter{
		Name:         p.Name,
		Type:         p.Type,
		Description:  p.Description,
		Required:     p.Required,
		DefaultValue: def,
	}, nil
}

func parametersToProto(params []Parameter) ([]*pluginapiv1.Parameter, error) {
	out := make([]*pluginapiv1.Parameter, 0, len(params))
	for _, p := range params {
		msg, err := parameterToProto(p)
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, nil
}

func operationsToProto(ops []Operation) ([]*pluginapiv1.Operation, error) {
	out := make([]*pluginapiv1.Operation, 0, len(ops))
	for _, op := range ops {
		params, err := parametersToProto(op.Parameters)
		if err != nil {
			return nil, fmt.Errorf("operation %q: %w", op.Name, err)
		}
		out = append(out, &pluginapiv1.Operation{
			Name:        op.Name,
			Description: op.Description,
			Method:      op.Method,
			Parameters:  params,
		})
	}
	return out, nil
}

func connectionParamDefsToProto(defs map[string]ConnectionParamDef) map[string]*pluginapiv1.ConnectionParamDef {
	if len(defs) == 0 {
		return nil
	}
	out := make(map[string]*pluginapiv1.ConnectionParamDef, len(defs))
	for name, def := range defs {
		out[name] = &pluginapiv1.ConnectionParamDef{
			Required:     def.Required,
			Description:  def.Description,
			DefaultValue: def.Default,
			From:         def.From,
			Field:        def.Field,
		}
	}
	return out
}
