package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

type StaticConnectionPlan struct {
	manifestBacked    bool
	pluginConnection  ConnectionDef
	namedConnections  map[string]ConnectionDef
	surfaces          map[SpecSurface]ResolvedSpecSurface
	restConnection    string
	defaultConnection string
}

type ResolvedSpecSurface struct {
	Surface           SpecSurface
	URL               string
	ConnectionName    string
	Connection        ConnectionDef
	GraphQLSelections map[string]string
}

func BuildStaticConnectionPlan(plugin *ProviderEntry, manifestPlugin *providermanifestv1.Spec) (StaticConnectionPlan, error) {
	declaredNames := namedConnectionNames(plugin, manifestPlugin)
	plan := StaticConnectionPlan{
		manifestBacked:   manifestPlugin != nil && manifestPlugin.IsManifestBacked(),
		pluginConnection: EffectivePluginConnectionDef(plugin),
		namedConnections: make(map[string]ConnectionDef),
		surfaces:         make(map[SpecSurface]ResolvedSpecSurface),
	}

	for name := range declaredNames {
		conn, ok := EffectiveNamedConnectionDef(plugin, manifestPlugin, name)
		if !ok {
			continue
		}
		plan.namedConnections[name] = conn
	}

	defaultConnection := resolveDefaultConnectionName(plugin, manifestPlugin)
	if defaultConnection != "" {
		if _, err := plan.connectionDef(defaultConnection); err != nil {
			return StaticConnectionPlan{}, fmt.Errorf("default_connection references undeclared connection %q", defaultConnection)
		}
		plan.defaultConnection = defaultConnection
	}

	if manifestPlugin != nil && manifestPlugin.Surfaces != nil && manifestPlugin.Surfaces.REST != nil {
		plan.restConnection = plan.resolveSurfaceConnectionName(manifestPlugin.Surfaces.REST.Connection)
		if plan.restConnection != "" {
			if _, err := plan.connectionDef(plan.restConnection); err != nil {
				return StaticConnectionPlan{}, fmt.Errorf("rest connection references undeclared connection %q", plan.restConnection)
			}
		}
	}

	for _, surface := range OrderedSpecSurfaces {
		url := surfaceURL(plugin, manifestPlugin, surface)
		if url == "" {
			continue
		}
		resolved := ResolvedSpecSurface{
			Surface:        surface,
			URL:            url,
			ConnectionName: plan.resolveSurfaceConnectionName(ManifestProviderSurfaceConnectionName(manifestPlugin, surface)),
		}
		if surface == SpecSurfaceGraphQL {
			resolved.GraphQLSelections = manifestPlugin.GraphQLOperationSelections()
		}
		conn, err := plan.connectionDef(resolved.ConnectionName)
		if err != nil {
			return StaticConnectionPlan{}, fmt.Errorf("%s references undeclared connection %q", surface.ConnectionField(), resolved.ConnectionName)
		}
		resolved.Connection = conn
		plan.surfaces[surface] = resolved
	}
	if err := plan.validateConnectionModes(); err != nil {
		return StaticConnectionPlan{}, err
	}
	if _, _, _, err := plan.RESTOperationConnectionBindings(manifestPlugin); err != nil {
		return StaticConnectionPlan{}, err
	}

	return plan, nil
}

func (plan StaticConnectionPlan) PluginConnection() ConnectionDef {
	return plan.pluginConnection
}

func (plan StaticConnectionPlan) NamedConnectionNames() []string {
	names := make([]string, 0, len(plan.namedConnections))
	for name := range plan.namedConnections {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (plan StaticConnectionPlan) NamedConnectionDef(name string) (ConnectionDef, bool) {
	conn, ok := plan.namedConnections[name]
	return conn, ok
}

func (plan StaticConnectionPlan) LookupConnection(name string) (ConnectionDef, bool) {
	conn, err := plan.connectionDef(ResolveConnectionAlias(name))
	if err != nil {
		return ConnectionDef{}, false
	}
	return conn, true
}

func (plan StaticConnectionPlan) ConfiguredAPISurface() (ResolvedSpecSurface, bool) {
	surfaces := plan.ConfiguredAPISurfaces()
	if len(surfaces) > 0 {
		return surfaces[0], true
	}
	return ResolvedSpecSurface{}, false
}

func (plan StaticConnectionPlan) ConfiguredAPISurfaces() []ResolvedSpecSurface {
	surfaces := make([]ResolvedSpecSurface, 0, 2)
	for _, surface := range []SpecSurface{SpecSurfaceOpenAPI, SpecSurfaceGraphQL} {
		if resolved, ok := plan.ResolvedSurface(surface); ok {
			surfaces = append(surfaces, resolved)
		}
	}
	return surfaces
}

func (plan StaticConnectionPlan) ConfiguredSpecSurface() (ResolvedSpecSurface, bool) {
	if resolved, ok := plan.ConfiguredAPISurface(); ok {
		return resolved, true
	}
	return plan.ResolvedSurface(SpecSurfaceMCP)
}

func (plan StaticConnectionPlan) ResolvedSurface(surface SpecSurface) (ResolvedSpecSurface, bool) {
	resolved, ok := plan.surfaces[surface]
	return resolved, ok
}

func (plan StaticConnectionPlan) AuthDefaultConnection() string {
	return plan.fallbackConnection()
}

func (plan StaticConnectionPlan) APIConnection() string {
	if resolved, ok := plan.ConfiguredAPISurface(); ok {
		return resolved.ConnectionName
	}
	if plan.restConnection != "" {
		return plan.restConnection
	}
	return plan.fallbackConnection()
}

func (plan StaticConnectionPlan) RESTConnection() string {
	if plan.restConnection != "" {
		return plan.restConnection
	}
	return plan.fallbackConnection()
}

// RESTOperationConnectionBindings returns the effective static connection and
// optional selector for each declarative REST operation in the manifest.
func (plan StaticConnectionPlan) RESTOperationConnectionBindings(manifestPlugin *providermanifestv1.Spec) (map[string]string, map[string]core.OperationConnectionSelector, map[string]bool, error) {
	if manifestPlugin == nil || manifestPlugin.Surfaces == nil || manifestPlugin.Surfaces.REST == nil {
		return nil, nil, nil, nil
	}
	operations := manifestPlugin.Surfaces.REST.Operations
	if len(operations) == 0 {
		return nil, nil, nil, nil
	}

	connections := make(map[string]string, len(operations))
	selectors := make(map[string]core.OperationConnectionSelector)
	locked := make(map[string]bool)
	defaultConnection := plan.RESTConnection()
	for i := range operations {
		op := &operations[i]
		operationName := strings.TrimSpace(op.Name)
		if operationName == "" {
			continue
		}

		parameterNames := make(map[string]struct{}, len(op.Parameters))
		for j := range op.Parameters {
			if name := strings.TrimSpace(op.Parameters[j].Name); name != "" {
				parameterNames[name] = struct{}{}
			}
		}

		connection := defaultConnection
		operationConnection := strings.TrimSpace(op.Connection)
		if operationConnection != "" {
			connection = ResolveConnectionAlias(operationConnection)
			locked[operationName] = true
		}
		if connection != "" {
			if _, err := plan.connectionDef(connection); err != nil {
				return nil, nil, nil, fmt.Errorf("operation %q connection references %w", operationName, err)
			}
			connections[operationName] = connection
		}

		selector, err := plan.resolveOperationConnectionSelector(operationName, op.ConnectionSelector)
		if err != nil {
			return nil, nil, nil, err
		}
		if selector.Parameter != "" {
			if _, ok := parameterNames[selector.Parameter]; !ok {
				return nil, nil, nil, fmt.Errorf("operation %q connectionSelector.parameter %q must be declared in parameters", operationName, selector.Parameter)
			}
			if operationConnection != "" && selector.Default != "" {
				return nil, nil, nil, fmt.Errorf("operation %q cannot declare both connection and connectionSelector.default", operationName)
			}
			selectors[operationName] = selector
			if selector.Default != "" {
				connections[operationName] = selector.Values[selector.Default]
			}
		}
	}
	if len(connections) == 0 {
		connections = nil
	}
	if len(selectors) == 0 {
		selectors = nil
	}
	if len(locked) == 0 {
		locked = nil
	}
	return connections, selectors, locked, nil
}

func (plan StaticConnectionPlan) MCPConnection() string {
	if resolved, ok := plan.ResolvedSurface(SpecSurfaceMCP); ok {
		return resolved.ConnectionName
	}
	return plan.fallbackConnection()
}

func (plan StaticConnectionPlan) AdvertisedConnectionNames() []string {
	names := plan.NamedConnectionNames()
	if !plan.shouldAdvertisePluginConnection() {
		return names
	}
	return append([]string{PluginConnectionName}, names...)
}

func (plan StaticConnectionPlan) ConnectionMode() core.ConnectionMode {
	if connectionModeForConnection(plan.pluginConnection) == core.ConnectionModeUser {
		return core.ConnectionModeUser
	}
	for _, name := range plan.NamedConnectionNames() {
		if connectionModeForConnection(plan.namedConnections[name]) == core.ConnectionModeUser {
			return core.ConnectionModeUser
		}
	}
	return core.ConnectionModeNone
}

func (plan StaticConnectionPlan) validateConnectionModes() error {
	addMode := func(scope string, mode core.ConnectionMode) error {
		switch core.NormalizeConnectionMode(mode) {
		case core.ConnectionModeNone:
			return nil
		case core.ConnectionModeUser:
			return nil
		default:
			return fmt.Errorf("%s uses unsupported connection mode %q", scope, mode)
		}
	}

	if err := addMode("plugin connection", connectionModeForConnection(plan.pluginConnection)); err != nil {
		return err
	}
	for _, name := range plan.NamedConnectionNames() {
		if err := addMode(fmt.Sprintf("connection %q", name), connectionModeForConnection(plan.namedConnections[name])); err != nil {
			return err
		}
	}
	return nil
}

func (plan StaticConnectionPlan) resolveOperationConnectionSelector(operationName string, raw *providermanifestv1.OperationConnectionSelector) (core.OperationConnectionSelector, error) {
	if raw == nil {
		return core.OperationConnectionSelector{}, nil
	}
	parameter := strings.TrimSpace(raw.Parameter)
	if parameter == "" {
		return core.OperationConnectionSelector{}, fmt.Errorf("operation %q connectionSelector.parameter is required", operationName)
	}
	if len(raw.Values) == 0 {
		return core.OperationConnectionSelector{}, fmt.Errorf("operation %q connectionSelector.values is required", operationName)
	}
	values := make(map[string]string, len(raw.Values))
	for value, rawConnection := range raw.Values {
		selectorValue := strings.TrimSpace(value)
		if selectorValue == "" {
			return core.OperationConnectionSelector{}, fmt.Errorf("operation %q connectionSelector.values contains an empty value", operationName)
		}
		connection := ResolveConnectionAlias(strings.TrimSpace(rawConnection))
		if connection == "" {
			return core.OperationConnectionSelector{}, fmt.Errorf("operation %q connectionSelector value %q references an empty connection", operationName, selectorValue)
		}
		if _, err := plan.connectionDef(connection); err != nil {
			return core.OperationConnectionSelector{}, fmt.Errorf("operation %q connectionSelector value %q references %w", operationName, selectorValue, err)
		}
		values[selectorValue] = connection
	}
	defaultValue := strings.TrimSpace(raw.Default)
	if defaultValue != "" {
		if _, ok := values[defaultValue]; !ok {
			return core.OperationConnectionSelector{}, fmt.Errorf("operation %q connectionSelector.default %q is not declared in values", operationName, defaultValue)
		}
	}
	return core.OperationConnectionSelector{
		Parameter: parameter,
		Default:   defaultValue,
		Values:    values,
	}, nil
}

func (plan StaticConnectionPlan) fallbackConnection() string {
	if plan.defaultConnection != "" {
		return plan.defaultConnection
	}
	if _, ok := plan.namedConnections["default"]; ok {
		return "default"
	}
	if len(plan.namedConnections) == 0 {
		return PluginConnectionName
	}
	if len(plan.namedConnections) == 1 {
		for name := range plan.namedConnections {
			return name
		}
	}
	return ""
}

func (plan StaticConnectionPlan) resolveSurfaceConnectionName(raw string) string {
	if name := ResolveConnectionAlias(raw); name != "" {
		return name
	}
	if plan.defaultConnection != "" {
		return plan.defaultConnection
	}
	if _, ok := plan.namedConnections["default"]; ok {
		return "default"
	}
	if len(plan.namedConnections) == 1 {
		for name := range plan.namedConnections {
			return name
		}
	}
	return PluginConnectionName
}

func (plan StaticConnectionPlan) shouldAdvertisePluginConnection() bool {
	if !plan.manifestBacked {
		return true
	}
	if len(plan.namedConnections) == 0 {
		return true
	}
	if plan.defaultConnection == PluginConnectionName {
		return true
	}
	if plan.APIConnection() == PluginConnectionName {
		return true
	}
	if plan.RESTConnection() == PluginConnectionName {
		return true
	}
	if plan.MCPConnection() == PluginConnectionName {
		return true
	}
	return false
}

func (plan StaticConnectionPlan) connectionDef(name string) (ConnectionDef, error) {
	if name == "" || name == PluginConnectionName {
		return plan.pluginConnection, nil
	}
	conn, ok := plan.namedConnections[name]
	if !ok {
		return ConnectionDef{}, fmt.Errorf("undeclared connection %q", name)
	}
	return conn, nil
}

func resolveDefaultConnectionName(plugin *ProviderEntry, manifestPlugin *providermanifestv1.Spec) string {
	if plugin != nil {
		if name := ResolveConnectionAlias(plugin.DefaultConnection); name != "" {
			return name
		}
	}
	if manifestPlugin != nil {
		if name := ResolveConnectionAlias(manifestPlugin.DefaultConnection); name != "" {
			return name
		}
	}
	return ""
}

func surfaceURL(plugin *ProviderEntry, manifestPlugin *providermanifestv1.Spec, surface SpecSurface) string {
	if url := ProviderSurfaceURLOverride(plugin, surface); url != "" {
		return url
	}
	url := ManifestProviderSurfaceURL(manifestPlugin, surface)
	if url == "" {
		return ""
	}
	return ResolveManifestRelativeSpecURL(plugin, url)
}

func namedConnectionNames(plugin *ProviderEntry, manifestPlugin *providermanifestv1.Spec) map[string]struct{} {
	names := make(map[string]struct{})
	add := func(name string) {
		resolved := ResolveConnectionAlias(name)
		if resolved != "" && resolved != PluginConnectionName {
			names[resolved] = struct{}{}
		}
	}
	if manifestPlugin != nil {
		for name := range manifestPlugin.Connections {
			add(name)
		}
	}
	if plugin != nil {
		for name := range plugin.Connections {
			add(name)
		}
	}
	return names
}

func connectionModeForConnection(conn ConnectionDef) core.ConnectionMode {
	if conn.Mode != "" {
		return core.NormalizeConnectionMode(core.ConnectionMode(conn.Mode))
	}
	switch conn.Auth.Type {
	case "", providermanifestv1.AuthTypeNone:
		return core.ConnectionModeNone
	default:
		return core.ConnectionModeUser
	}
}
