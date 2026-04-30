package server

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	declarative "github.com/valon-technologies/gestalt/server/services/plugins/declarative"
)

type operationConnectionProjection struct {
	plan        config.StaticConnectionPlan
	connections map[string]string
	selectors   map[string]core.OperationConnectionSelector
}

func (s *Server) publicHTTPOperations(integration string, prov core.Provider, ops []catalog.CatalogOperation) []catalog.CatalogOperation {
	projector, hasProjector := s.operationConnectionProjection(integration)
	out := make([]catalog.CatalogOperation, 0, len(ops))
	for i := range ops {
		op := ops[i]
		if !catalog.OperationVisibleByDefault(op) {
			continue
		}
		if invocation.OperationTransport(op) == catalog.TransportMCPPassthrough {
			continue
		}
		projected, ok := s.projectPublicOperation(prov, op, projector, hasProjector)
		if !ok {
			continue
		}
		out = append(out, projected)
	}
	return out
}

func (s *Server) publicMCPCatalog(integration string, prov core.Provider, cat *catalog.Catalog) *catalog.Catalog {
	if cat == nil {
		return nil
	}
	filtered := cat.Clone()
	projector, hasProjector := s.operationConnectionProjection(integration)
	ops := filtered.Operations[:0]
	for i := range filtered.Operations {
		op := filtered.Operations[i]
		if !catalog.OperationVisibleByDefault(op) {
			continue
		}
		projected, ok := s.projectPublicOperation(prov, op, projector, hasProjector)
		if !ok {
			continue
		}
		ops = append(ops, projected)
	}
	filtered.Operations = ops
	return filtered
}

func (s *Server) validatePublicOperationInvocation(integration string, prov core.Provider, op catalog.CatalogOperation, params map[string]any, explicitConnection string) error {
	projector, hasProjector := s.operationConnectionProjection(integration)
	if _, ok := s.projectPublicOperation(prov, op, projector, hasProjector); !ok {
		return fmt.Errorf("%w: %s.%s uses an internal connection", invocation.ErrAuthorizationDenied, integration, op.ID)
	}
	if explicitConnection != "" && hasProjector && !publicConnection(projector.plan, explicitConnection) {
		return fmt.Errorf("%w: %s connection %q is internal", invocation.ErrAuthorizationDenied, integration, explicitConnection)
	}
	for _, param := range op.Parameters {
		if !param.Internal {
			continue
		}
		if _, ok := params[param.Name]; ok {
			return fmt.Errorf("%w: parameter %q is not public", invocation.ErrInvalidInvocation, param.Name)
		}
	}
	if hasProjector {
		if selector, ok := projector.selectors[op.ID]; ok {
			if raw, present := params[selector.Parameter]; present && raw != nil {
				value := strings.TrimSpace(fmt.Sprint(raw))
				connection := selector.Values[value]
				if connection != "" && !publicConnection(projector.plan, connection) {
					return fmt.Errorf("%w: %s.%s selector value %q uses an internal connection", invocation.ErrAuthorizationDenied, integration, op.ID, value)
				}
			}
		}
	}
	return nil
}

func (s *Server) projectPublicOperation(prov core.Provider, op catalog.CatalogOperation, projector operationConnectionProjection, hasProjector bool) (catalog.CatalogOperation, bool) {
	if hasProjector {
		if selector, ok := projector.selectors[op.ID]; ok {
			selectorParam, selectorParamOK := catalogParameter(op, selector.Parameter)
			publicValues := make(map[string]string, len(selector.Values))
			publicSelectorValues := make([]string, 0, len(selector.Values))
			for value, connection := range selector.Values {
				if publicConnection(projector.plan, connection) {
					publicValues[value] = connection
					publicSelectorValues = append(publicSelectorValues, value)
				}
			}
			if len(publicValues) == 0 {
				return catalog.CatalogOperation{}, false
			}
			if selector.Default != "" {
				if _, ok := publicValues[selector.Default]; !ok && (!selectorParamOK || selectorParam.Internal || !selectorParam.Required) {
					return catalog.CatalogOperation{}, false
				}
			}
			if selectorParamOK && selectorParam.Internal {
				if selector.Default == "" {
					return catalog.CatalogOperation{}, false
				}
				if _, ok := publicValues[selector.Default]; !ok {
					return catalog.CatalogOperation{}, false
				}
			}
			op = projectSelectorValues(op, selector.Parameter, publicSelectorValues, selector.Default)
		} else {
			connection := projector.connections[op.ID]
			if connection == "" && prov != nil {
				connection = prov.ConnectionForOperation(op.ID)
			}
			if connection != "" && !publicConnection(projector.plan, connection) {
				return catalog.CatalogOperation{}, false
			}
		}
	}
	return projectInternalParameters(op), true
}

func (s *Server) operationConnectionProjection(integration string) (operationConnectionProjection, bool) {
	entry := s.pluginDefs[integration]
	if entry == nil {
		return operationConnectionProjection{}, false
	}
	manifest := entry.ManifestSpec()
	plan, err := config.BuildStaticConnectionPlan(entry, manifest)
	if err != nil {
		return operationConnectionProjection{}, false
	}
	connections, selectors, _, err := plan.RESTOperationConnectionBindings(manifest)
	if err != nil {
		return operationConnectionProjection{}, false
	}
	return operationConnectionProjection{
		plan:        plan,
		connections: connections,
		selectors:   selectors,
	}, true
}

func publicConnection(plan config.StaticConnectionPlan, connection string) bool {
	resolved, ok := plan.LookupResolvedConnection(connection)
	if !ok {
		return true
	}
	return core.NormalizeConnectionExposure(core.ConnectionExposure(resolved.Exposure)) != core.ConnectionExposureInternal
}

func catalogParameter(op catalog.CatalogOperation, name string) (catalog.CatalogParameter, bool) {
	for _, param := range op.Parameters {
		if param.Name == name {
			return param, true
		}
	}
	return catalog.CatalogParameter{}, false
}

func projectInternalParameters(op catalog.CatalogOperation) catalog.CatalogOperation {
	var internal map[string]struct{}
	publicParams := make([]catalog.CatalogParameter, 0, len(op.Parameters))
	for _, param := range op.Parameters {
		if param.Internal {
			if internal == nil {
				internal = map[string]struct{}{}
			}
			internal[param.Name] = struct{}{}
			continue
		}
		publicParams = append(publicParams, param)
	}
	if len(internal) == 0 {
		return op
	}
	op.Parameters = publicParams
	op.InputSchema = projectInputSchema(op.InputSchema, publicParams, internal)
	return op
}

func projectSelectorValues(op catalog.CatalogOperation, paramName string, publicValues []string, defaultValue string) catalog.CatalogOperation {
	publicSet := make(map[string]struct{}, len(publicValues))
	for _, value := range publicValues {
		publicSet[value] = struct{}{}
	}
	if len(op.Parameters) > 0 {
		op.Parameters = append([]catalog.CatalogParameter(nil), op.Parameters...)
	}
	for i := range op.Parameters {
		if op.Parameters[i].Name != paramName {
			continue
		}
		if _, ok := publicSet[fmt.Sprint(op.Parameters[i].Default)]; !ok {
			op.Parameters[i].Default = nil
		}
		if defaultValue != "" {
			if _, ok := publicSet[defaultValue]; ok {
				op.Parameters[i].Default = defaultValue
			}
		}
		break
	}
	op.InputSchema = projectSelectorInputSchema(op.InputSchema, op.Parameters, paramName, publicValues, defaultValue)
	return op
}

func projectSelectorInputSchema(raw json.RawMessage, params []catalog.CatalogParameter, paramName string, publicValues []string, defaultValue string) json.RawMessage {
	if len(raw) == 0 {
		raw = declarative.SynthesizeInputSchema(params)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return declarative.SynthesizeInputSchema(params)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		props = map[string]any{}
		schema["properties"] = props
	}
	prop, _ := props[paramName].(map[string]any)
	if prop == nil {
		prop = map[string]any{"type": "string"}
		props[paramName] = prop
	}
	enum := make([]any, 0, len(publicValues))
	publicSet := make(map[string]struct{}, len(publicValues))
	for _, value := range publicValues {
		enum = append(enum, value)
		publicSet[value] = struct{}{}
	}
	prop["enum"] = enum
	if rawDefault, ok := prop["default"]; ok {
		if _, public := publicSet[fmt.Sprint(rawDefault)]; !public {
			delete(prop, "default")
		}
	}
	if defaultValue != "" {
		if _, ok := publicSet[defaultValue]; ok {
			prop["default"] = defaultValue
		} else {
			delete(prop, "default")
		}
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return declarative.SynthesizeInputSchema(params)
	}
	return data
}

func projectInputSchema(raw json.RawMessage, publicParams []catalog.CatalogParameter, internal map[string]struct{}) json.RawMessage {
	if len(raw) == 0 {
		return declarative.SynthesizeInputSchema(publicParams)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return declarative.SynthesizeInputSchema(publicParams)
	}
	if props, ok := schema["properties"].(map[string]any); ok {
		for name := range internal {
			delete(props, name)
		}
		if len(props) == 0 {
			delete(schema, "properties")
		}
	}
	if required, ok := schema["required"].([]any); ok {
		filtered := required[:0]
		for _, item := range required {
			name, _ := item.(string)
			if _, hidden := internal[name]; hidden {
				continue
			}
			filtered = append(filtered, item)
		}
		if len(filtered) == 0 {
			delete(schema, "required")
		} else {
			schema["required"] = filtered
		}
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return declarative.SynthesizeInputSchema(publicParams)
	}
	return data
}
