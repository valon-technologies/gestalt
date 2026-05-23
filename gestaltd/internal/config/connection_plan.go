package config

import (
	"fmt"
	"maps"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

type StaticConnectionPlan struct {
	manifestBacked    bool
	appConnection     ResolvedConnectionDef
	namedConnections  map[string]ResolvedConnectionDef
	surfaces          map[SpecSurface]ResolvedSpecSurface
	restConnection    string
	defaultConnection string
}

type ConfigSource string

const (
	ConfigSourceDefault  ConfigSource = "default"
	ConfigSourceManifest ConfigSource = "manifest"
	ConfigSourceDeploy   ConfigSource = "deploy"
)

type ResolvedConnectionSource struct {
	DeclaredInManifest bool
	DeclaredInDeploy   bool
	ModeSource         ConfigSource
	AuthSource         ConfigSource
}

type ResolvedConnectionDef struct {
	Provider          string
	Name              string
	Ref               string
	ConnectionID      string
	DisplayName       string
	Mode              providermanifestv1.ConnectionMode
	Exposure          string
	Auth              ConnectionAuthDef
	Params            map[string]ConnectionParamDef
	Discovery         *providermanifestv1.ProviderDiscovery
	PostConnect       *providermanifestv1.ProviderPostConnect
	CredentialRefresh *CredentialRefreshDef
	Source            ResolvedConnectionSource
}

func (r ResolvedConnectionDef) ConnectionDef() ConnectionDef {
	return ConnectionDef{
		Ref:               r.Ref,
		DisplayName:       r.DisplayName,
		Mode:              r.Mode,
		Exposure:          r.Exposure,
		Auth:              r.Auth,
		ConnectionParams:  r.Params,
		Discovery:         r.Discovery,
		PostConnect:       providermanifestv1.CloneProviderPostConnect(r.PostConnect),
		CredentialRefresh: r.CredentialRefresh,
		ConnectionID:      r.ConnectionID,
	}
}

type ResolvedSpecSurface struct {
	Surface           SpecSurface
	URL               string
	ConnectionName    string
	Connection        ConnectionDef
	GraphQLSelections map[string]string
}

func BuildStaticConnectionPlan(app *ProviderEntry, manifestApp *providermanifestv1.Spec) (StaticConnectionPlan, error) {
	declaredNames := namedConnectionNames(app, manifestApp)
	plan := StaticConnectionPlan{
		manifestBacked:   manifestApp != nil && manifestApp.IsManifestBacked(),
		appConnection:    ResolveAppConnectionDef(app),
		namedConnections: make(map[string]ResolvedConnectionDef),
		surfaces:         make(map[SpecSurface]ResolvedSpecSurface),
	}

	for name := range declaredNames {
		conn, ok, err := ResolveNamedConnectionDef(app, manifestApp, name)
		if err != nil {
			return StaticConnectionPlan{}, err
		}
		if !ok {
			continue
		}
		plan.namedConnections[name] = conn
	}

	defaultConnection := resolveDefaultConnectionName(app, manifestApp)
	if defaultConnection != "" {
		if _, err := plan.connectionDef(defaultConnection); err != nil {
			return StaticConnectionPlan{}, fmt.Errorf("default_connection references undeclared connection %q", defaultConnection)
		}
		plan.defaultConnection = defaultConnection
	}

	if manifestApp != nil && manifestApp.Surfaces != nil && manifestApp.Surfaces.REST != nil {
		plan.restConnection = plan.resolveSurfaceConnectionName(manifestApp.Surfaces.REST.Connection)
		if plan.restConnection != "" {
			if _, err := plan.connectionDef(plan.restConnection); err != nil {
				return StaticConnectionPlan{}, fmt.Errorf("rest connection references undeclared connection %q", plan.restConnection)
			}
		}
	}

	for _, surface := range OrderedSpecSurfaces {
		url := surfaceURL(app, manifestApp, surface)
		if url == "" {
			continue
		}
		resolved := ResolvedSpecSurface{
			Surface:        surface,
			URL:            url,
			ConnectionName: plan.resolveSurfaceConnectionName(ManifestProviderSurfaceConnectionName(manifestApp, surface)),
		}
		if surface == SpecSurfaceGraphQL {
			resolved.GraphQLSelections = manifestApp.GraphQLOperationSelections()
		}
		conn, err := plan.connectionDef(resolved.ConnectionName)
		if err != nil {
			return StaticConnectionPlan{}, fmt.Errorf("%s references undeclared connection %q", surface.ConnectionField(), resolved.ConnectionName)
		}
		resolved.Connection = conn.ConnectionDef()
		plan.surfaces[surface] = resolved
	}
	if err := plan.validateConnectionModes(); err != nil {
		return StaticConnectionPlan{}, err
	}
	if _, _, _, err := plan.RESTOperationConnectionBindings(manifestApp); err != nil {
		return StaticConnectionPlan{}, err
	}

	return plan, nil
}

func (plan StaticConnectionPlan) AppConnection() ConnectionDef {
	return plan.appConnection.ConnectionDef()
}

func (plan StaticConnectionPlan) ResolvedAppConnection() ResolvedConnectionDef {
	return plan.appConnection
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
	return conn.ConnectionDef(), ok
}

func (plan StaticConnectionPlan) ResolvedNamedConnectionDef(name string) (ResolvedConnectionDef, bool) {
	conn, ok := plan.namedConnections[name]
	return conn, ok
}

func (plan StaticConnectionPlan) LookupConnection(name string) (ConnectionDef, bool) {
	conn, err := plan.connectionDef(ResolveConnectionAlias(name))
	if err != nil {
		return ConnectionDef{}, false
	}
	return conn.ConnectionDef(), true
}

func (plan StaticConnectionPlan) LookupResolvedConnection(name string) (ResolvedConnectionDef, bool) {
	conn, err := plan.connectionDef(ResolveConnectionAlias(name))
	if err != nil {
		return ResolvedConnectionDef{}, false
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
func (plan StaticConnectionPlan) RESTOperationConnectionBindings(manifestApp *providermanifestv1.Spec) (map[string]string, map[string]core.OperationConnectionSelector, map[string]bool, error) {
	if manifestApp == nil || manifestApp.Surfaces == nil || manifestApp.Surfaces.REST == nil {
		return nil, nil, nil, nil
	}
	operations := manifestApp.Surfaces.REST.Operations
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
	if !plan.shouldAdvertiseAppConnection() {
		return names
	}
	return append([]string{AppConnectionName}, names...)
}

func (plan StaticConnectionPlan) ConnectionMode() core.ConnectionMode {
	if ConnectionModeForConnection(plan.appConnection.ConnectionDef()) == core.ConnectionModeUser {
		return core.ConnectionModeUser
	}
	for _, name := range plan.NamedConnectionNames() {
		mode := ConnectionModeForConnection(plan.namedConnections[name].ConnectionDef())
		if mode == core.ConnectionModeUser {
			return core.ConnectionModeUser
		}
	}
	return core.ConnectionModeNone
}

func (plan StaticConnectionPlan) validateConnectionModes() error {
	addMode := func(scope string, mode core.ConnectionMode) error {
		switch core.NormalizeConnectionMode(mode) {
		case core.ConnectionModeNone, core.ConnectionModeUser:
			return nil
		default:
			return fmt.Errorf("%s uses unsupported connection mode %q", scope, mode)
		}
	}

	if err := addMode("plugin connection", ConnectionModeForConnection(plan.appConnection.ConnectionDef())); err != nil {
		return err
	}
	if err := validateConnectionExposureUnsupported("plugin connection", plan.appConnection.ConnectionDef()); err != nil {
		return err
	}
	for _, name := range plan.NamedConnectionNames() {
		scope := fmt.Sprintf("connection %q", name)
		conn := plan.namedConnections[name].ConnectionDef()
		if err := addMode(scope, ConnectionModeForConnection(conn)); err != nil {
			return err
		}
		if err := validateConnectionExposureUnsupported(scope, conn); err != nil {
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
		return AppConnectionName
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
	return plan.fallbackConnection()
}

func (plan StaticConnectionPlan) shouldAdvertiseAppConnection() bool {
	if !plan.manifestBacked {
		return true
	}
	if len(plan.namedConnections) == 0 {
		return true
	}
	if plan.defaultConnection == AppConnectionName {
		return true
	}
	if plan.APIConnection() == AppConnectionName {
		return true
	}
	if plan.RESTConnection() == AppConnectionName {
		return true
	}
	if plan.MCPConnection() == AppConnectionName {
		return true
	}
	return false
}

func (plan StaticConnectionPlan) connectionDef(name string) (ResolvedConnectionDef, error) {
	if name == "" || name == AppConnectionName {
		return plan.appConnection, nil
	}
	conn, ok := plan.namedConnections[name]
	if !ok {
		return ResolvedConnectionDef{}, fmt.Errorf("undeclared connection %q", name)
	}
	return conn, nil
}

func resolveDefaultConnectionName(app *ProviderEntry, manifestApp *providermanifestv1.Spec) string {
	if app != nil {
		if name := ResolveConnectionAlias(app.DefaultConnection); name != "" {
			return name
		}
	}
	if manifestApp != nil {
		if name := ResolveConnectionAlias(manifestApp.DefaultConnection); name != "" {
			return name
		}
	}
	return ""
}

func surfaceURL(app *ProviderEntry, manifestApp *providermanifestv1.Spec, surface SpecSurface) string {
	if url := ProviderSurfaceURLOverride(app, surface); url != "" {
		return url
	}
	url := ManifestProviderSurfaceURL(manifestApp, surface)
	if url == "" {
		return ""
	}
	return ResolveManifestRelativeSpecURL(app, url)
}

func namedConnectionNames(app *ProviderEntry, manifestApp *providermanifestv1.Spec) map[string]struct{} {
	names := make(map[string]struct{})
	add := func(name string) {
		resolved := ResolveConnectionAlias(name)
		if resolved != "" && resolved != AppConnectionName {
			names[resolved] = struct{}{}
		}
	}
	if manifestApp != nil {
		for name := range manifestApp.Connections {
			add(name)
		}
	}
	if app != nil {
		for name := range app.Connections {
			add(name)
		}
	}
	return names
}

func ResolveAppConnectionDef(app *ProviderEntry) ResolvedConnectionDef {
	conn := ConnectionDef{}
	if app != nil {
		override := &ConnectionDef{
			Mode:             app.ConnectionMode,
			ConnectionParams: app.ConnectionParams,
		}
		if app.Auth != nil {
			override.Auth = *app.Auth
		}
		MergeConnectionDef(&conn, override)
	}
	conn.Mode = providermanifestv1.ConnectionMode(ConnectionModeForConnection(conn))
	source := ResolvedConnectionSource{
		ModeSource: ConfigSourceDefault,
		AuthSource: ConfigSourceDefault,
	}
	if app != nil {
		source.DeclaredInDeploy = true
		if app.ConnectionMode != "" {
			source.ModeSource = ConfigSourceDeploy
		}
		if app.Auth != nil {
			source.AuthSource = ConfigSourceDeploy
		}
	}
	return resolvedConnectionDef(AppConnectionName, conn, source)
}

func ResolveNamedConnectionDef(app *ProviderEntry, manifestApp *providermanifestv1.Spec, name string) (ResolvedConnectionDef, bool, error) {
	conn := ConnectionDef{}
	source := ResolvedConnectionSource{
		ModeSource: ConfigSourceDefault,
		AuthSource: ConfigSourceDefault,
	}
	found := false

	if manifestApp != nil && manifestApp.Connections != nil {
		if def, ok := manifestApp.Connections[name]; ok && def != nil {
			found = true
			source.DeclaredInManifest = true
			conn.DisplayName = def.DisplayName
			if def.Mode != "" {
				conn.Mode = def.Mode
				source.ModeSource = ConfigSourceManifest
			}
			if def.Exposure != "" {
				return ResolvedConnectionDef{}, false, fmt.Errorf("connection %q manifest exposure is not supported", name)
			}
			if def.Auth != nil {
				MergeConnectionAuth(&conn.Auth, ManifestAuthToConnectionAuthDef(def.Auth))
				source.AuthSource = ConfigSourceManifest
			}
			if len(def.Params) > 0 {
				conn.ConnectionParams = maps.Clone(def.Params)
			}
			if def.CredentialRefresh != nil {
				conn.CredentialRefresh = cloneCredentialRefreshDef(def.CredentialRefresh)
			}
			if def.Discovery != nil {
				conn.Discovery = def.Discovery
			}
			if def.PostConnect != nil {
				conn.PostConnect = providermanifestv1.CloneProviderPostConnect(def.PostConnect)
			}
		}
	}
	if app != nil {
		if def, ok := app.Connections[name]; ok && def != nil {
			found = true
			source.DeclaredInDeploy = true
			if def.Exposure != "" {
				return ResolvedConnectionDef{}, false, fmt.Errorf("connection %q deploy exposure is not supported", name)
			}
			if def.Mode != "" {
				source.ModeSource = ConfigSourceDeploy
			}
			if def.Auth.Type != "" || def.Auth.Token != "" || def.Auth.AuthMapping != nil || def.Auth.Credentials != nil {
				source.AuthSource = ConfigSourceDeploy
			}
			MergeConnectionDef(&conn, def)
		}
	}

	if !found {
		return ResolvedConnectionDef{}, false, nil
	}
	if err := validateConnectionExposureUnsupported(fmt.Sprintf("connection %q", name), conn); err != nil {
		return ResolvedConnectionDef{}, false, err
	}
	conn.Mode = providermanifestv1.ConnectionMode(ConnectionModeForConnection(conn))
	return resolvedConnectionDef(name, conn, source), true, nil
}

func resolvedConnectionDef(name string, conn ConnectionDef, source ResolvedConnectionSource) ResolvedConnectionDef {
	return ResolvedConnectionDef{
		Name:              name,
		Ref:               conn.Ref,
		ConnectionID:      conn.ConnectionID,
		DisplayName:       conn.DisplayName,
		Mode:              conn.Mode,
		Exposure:          conn.Exposure,
		Auth:              conn.Auth,
		Params:            conn.ConnectionParams,
		Discovery:         conn.Discovery,
		PostConnect:       providermanifestv1.CloneProviderPostConnect(conn.PostConnect),
		CredentialRefresh: conn.CredentialRefresh,
		Source:            source,
	}
}

// ConnectionModeForConnection returns the effective credential mode for a
// merged connection definition.
func ConnectionModeForConnection(conn ConnectionDef) core.ConnectionMode {
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

func validateConnectionExposureUnsupported(scope string, conn ConnectionDef) error {
	if strings.TrimSpace(conn.Exposure) != "" {
		return fmt.Errorf("%s exposure is not supported", scope)
	}
	return nil
}
