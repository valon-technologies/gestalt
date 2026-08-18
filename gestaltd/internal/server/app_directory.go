package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

const appCatalogIconPathPrefix = "/api/v1/catalog/apps/"

// tenantAppDirectory is the process-wide Apps snapshot: names, copy, icons,
// and connection schema. It never holds a live Provider. Tunnel proxies are
// bound to one HTTP request and must be resolved again for overlay status.
type tenantAppDirectory struct {
	entries []tenantAppDirectoryEntry
}

type tenantAppDirectoryEntry struct {
	Name             string
	DisplayName      string
	Description      string
	IconSVG          string
	DeclaredMount    string
	Prompts          []appPromptInfo
	SourceTreeURL    string
	Advertised       []advertisedConnection
	ConnectionSchema []connectionSchemaInfo
	Loaded           bool
}

// tenantAppDirectoryEpoch is the cheap invalidation key for the tenant
// snapshot. providers is ProviderMap.Generation. remote is the tunnel
// registration topology. plugins covers catalog-relevant plugin config so an
// in-place AppDefs edit does not leave stale connection schema in the cache.
type tenantAppDirectoryEpoch struct {
	providers uint64
	remote    uint64
	plugins   string
}

// appDirectory is one signed-in viewer's slice of the tenant snapshot, with
// mount and admin paths filled in. It still does not include saved accounts
// or connection status, and it still does not hold a live Provider.
type appDirectory struct {
	entries []appDirectoryEntry
}

type appDirectoryEntry struct {
	Name             string
	DisplayName      string
	Description      string
	IconSVG          string
	DeclaredMount    string
	MountedPath      string
	ManagementPath   string
	Prompts          []appPromptInfo
	SourceTreeURL    string
	Advertised       []advertisedConnection
	ConnectionSchema []connectionSchemaInfo
	Loaded           bool
}

type connectionSchemaInfo struct {
	DisplayName      string                         `json:"displayName,omitempty"`
	Name             string                         `json:"name"`
	Mode             string                         `json:"mode,omitempty"`
	AuthTypes        []string                       `json:"authTypes"`
	ConnectionParams map[string]connectionParamInfo `json:"connectionParams,omitempty"`
	CredentialFields []credentialFieldInfo          `json:"credentialFields,omitempty"`
}

type appCatalogEntry struct {
	Name           string                 `json:"name"`
	DisplayName    string                 `json:"displayName,omitempty"`
	Description    string                 `json:"description,omitempty"`
	IconURL        string                 `json:"iconUrl,omitempty"`
	MountedPath    string                 `json:"mountedPath,omitempty"`
	ManagementPath string                 `json:"managementPath,omitempty"`
	Prompts        []appPromptInfo        `json:"prompts,omitempty"`
	SourceTreeURL  string                 `json:"sourceTreeUrl,omitempty"`
	Connections    []connectionSchemaInfo `json:"connections"`
}

type appConnectionStatus struct {
	Name            string                 `json:"name"`
	Status          string                 `json:"status"`
	CredentialState string                 `json:"credentialState"`
	HealthState     string                 `json:"healthState"`
	Actions         []string               `json:"actions"`
	Connected       bool                   `json:"connected"`
	Connections     []connectionStatusView `json:"connections"`
}

type connectionStatusView struct {
	Name              string         `json:"name"`
	Status            string         `json:"status"`
	CredentialState   string         `json:"credentialState"`
	HealthState       string         `json:"healthState"`
	Actions           []string       `json:"actions"`
	CredentialMode    string         `json:"credentialMode,omitempty"`
	OwnerKind         string         `json:"ownerKind,omitempty"`
	Instances         []instanceInfo `json:"instances"`
	PreferredInstance string         `json:"preferredInstance,omitempty"`
	StatusCode        string         `json:"statusCode,omitempty"`
	StatusReason      string         `json:"statusReason,omitempty"`
	Connected         bool           `json:"connected"`
}

type appListingError struct {
	status int
	public string
	err    error
}

func (e *appListingError) Error() string {
	if e == nil {
		return ""
	}
	if e.err != nil {
		return e.err.Error()
	}
	return e.public
}

func (e *appListingError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func appCatalogIconURL(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return appCatalogIconPathPrefix + url.PathEscape(name) + "/icon"
}

func (entry appDirectoryEntry) catalogJSON() appCatalogEntry {
	connections := entry.ConnectionSchema
	if connections == nil {
		connections = []connectionSchemaInfo{}
	}
	out := appCatalogEntry{
		Name:           entry.Name,
		DisplayName:    entry.DisplayName,
		Description:    entry.Description,
		MountedPath:    entry.MountedPath,
		ManagementPath: entry.ManagementPath,
		Prompts:        entry.Prompts,
		SourceTreeURL:  entry.SourceTreeURL,
		Connections:    connections,
	}
	if strings.TrimSpace(entry.IconSVG) != "" {
		out.IconURL = appCatalogIconURL(entry.Name)
	}
	return out
}

func (dir *appDirectory) catalogJSON() []appCatalogEntry {
	if dir == nil {
		return []appCatalogEntry{}
	}
	out := make([]appCatalogEntry, 0, len(dir.entries))
	for i := range dir.entries {
		out = append(out, dir.entries[i].catalogJSON())
	}
	return out
}

func viewerDirectoryEntry(entry tenantAppDirectoryEntry, mountedPath, managementPath string) appDirectoryEntry {
	return appDirectoryEntry{
		Name:             entry.Name,
		DisplayName:      entry.DisplayName,
		Description:      entry.Description,
		IconSVG:          entry.IconSVG,
		DeclaredMount:    entry.DeclaredMount,
		MountedPath:      mountedPath,
		ManagementPath:   managementPath,
		Prompts:          entry.Prompts,
		SourceTreeURL:    entry.SourceTreeURL,
		Advertised:       entry.Advertised,
		ConnectionSchema: entry.ConnectionSchema,
		Loaded:           entry.Loaded,
	}
}

func connectionStatusViews(connections []connectionDefInfo) []connectionStatusView {
	if connections == nil {
		return []connectionStatusView{}
	}
	out := make([]connectionStatusView, 0, len(connections))
	for i := range connections {
		conn := connections[i]
		instances := conn.Instances
		if instances == nil {
			instances = []instanceInfo{}
		}
		actions := conn.Actions
		if actions == nil {
			actions = []string{}
		}
		out = append(out, connectionStatusView{
			Name:              conn.Name,
			Status:            conn.Status,
			CredentialState:   conn.CredentialState,
			HealthState:       conn.HealthState,
			Actions:           actions,
			CredentialMode:    conn.CredentialMode,
			OwnerKind:         conn.OwnerKind,
			Instances:         instances,
			PreferredInstance: conn.PreferredInstance,
			StatusCode:        conn.StatusCode,
			StatusReason:      conn.StatusReason,
			Connected:         conn.Connected,
		})
	}
	return out
}

func appConnectionStatusesFrom(infos []integrationInfo) []appConnectionStatus {
	out := make([]appConnectionStatus, 0, len(infos))
	for i := range infos {
		info := infos[i]
		actions := info.Actions
		if actions == nil {
			actions = []string{}
		}
		out = append(out, appConnectionStatus{
			Name:            info.Name,
			Status:          info.Status,
			CredentialState: info.CredentialState,
			HealthState:     info.HealthState,
			Actions:         actions,
			Connected:       info.Connected,
			Connections:     connectionStatusViews(info.Connections),
		})
	}
	return out
}

func (s *Server) assembleAppDirectory(r *http.Request) (*appDirectory, error) {
	snapshot, err := s.tenantAppDirectory(r.Context())
	if err != nil {
		return nil, err
	}
	return s.projectViewerAppDirectory(r, snapshot)
}

func (s *Server) tenantAppDirectory(ctx context.Context) (*tenantAppDirectory, error) {
	epoch := s.tenantAppDirectoryEpoch()
	s.tenantDirectoryMu.Lock()
	if s.tenantDirectory != nil && s.tenantDirectoryEpoch == epoch {
		dir := s.tenantDirectory
		s.tenantDirectoryMu.Unlock()
		return dir, nil
	}
	s.tenantDirectoryMu.Unlock()

	dir, cacheable, err := s.buildTenantAppDirectory(ctx)
	if err != nil {
		return nil, err
	}
	if !cacheable {
		return dir, nil
	}

	s.tenantDirectoryMu.Lock()
	defer s.tenantDirectoryMu.Unlock()
	if s.tenantDirectory != nil && s.tenantDirectoryEpoch == epoch {
		return s.tenantDirectory, nil
	}
	s.tenantDirectory = dir
	s.tenantDirectoryEpoch = epoch
	return dir, nil
}

func (s *Server) tenantAppDirectoryEpoch() tenantAppDirectoryEpoch {
	var providers uint64
	if s.providers != nil {
		providers = s.providers.Generation()
	}
	return tenantAppDirectoryEpoch{
		providers: providers,
		remote:    s.remoteDirectoryTopology(),
		plugins:   s.pluginDirectoryFingerprint(),
	}
}

func (s *Server) remoteDirectoryTopology() uint64 {
	if s == nil || s.tunnelResolver == nil {
		return 0
	}
	return s.tunnelResolver.cfg.RemoteRegistrations.Topology()
}

func (s *Server) pluginDirectoryFingerprint() string {
	names := make([]string, 0, len(s.pluginDefs)+len(s.appPrompts))
	seen := make(map[string]struct{}, len(s.pluginDefs)+len(s.appPrompts))
	for name := range s.pluginDefs {
		names = append(names, name)
		seen[name] = struct{}{}
	}
	for name := range s.appPrompts {
		if _, ok := seen[name]; ok {
			continue
		}
		names = append(names, name)
	}
	slices.Sort(names)
	var b strings.Builder
	for _, name := range names {
		b.WriteByte(';')
		b.WriteString(name)
		plugin := s.pluginDefs[name]
		if plugin != nil {
			fmt.Fprintf(&b, ":%s:%s:%t:%s:%s",
				strings.TrimSpace(plugin.DisplayName),
				strings.TrimSpace(plugin.Description),
				s.integrationHiddenFromCatalog(name),
				pluginDeclaredMount(plugin),
				plugin.SourceTreeURL(),
			)
			appendConnectionSchemaFingerprint(&b, s.connectionSchemasFromAdvertised(name, s.advertisedConnectionsForPlugin(name, plugin)))
		}
		for _, prompt := range s.appPrompts[name] {
			fmt.Fprintf(&b, "#%s=%s", prompt.ID, prompt.Text)
		}
	}
	return b.String()
}

func appendConnectionSchemaFingerprint(b *strings.Builder, schemas []connectionSchemaInfo) {
	if b == nil || len(schemas) == 0 {
		return
	}
	ordered := append([]connectionSchemaInfo(nil), schemas...)
	slices.SortFunc(ordered, func(a, b connectionSchemaInfo) int {
		return strings.Compare(a.Name, b.Name)
	})
	for _, schema := range ordered {
		fmt.Fprintf(b, ",%s=%s:%s", schema.Name, schema.Mode, schema.DisplayName)
		authTypes := append([]string(nil), schema.AuthTypes...)
		slices.Sort(authTypes)
		for _, authType := range authTypes {
			b.WriteByte('|')
			b.WriteString(authType)
		}
		paramNames := make([]string, 0, len(schema.ConnectionParams))
		for name := range schema.ConnectionParams {
			paramNames = append(paramNames, name)
		}
		slices.Sort(paramNames)
		for _, name := range paramNames {
			param := schema.ConnectionParams[name]
			fmt.Fprintf(b, "@%s=%t:%s:%s", name, param.Required, param.Description, param.Default)
		}
		for _, field := range schema.CredentialFields {
			fmt.Fprintf(b, "$%s=%s:%s", field.Name, field.Label, field.Description)
		}
	}
}

func (s *Server) buildTenantAppDirectory(ctx context.Context) (*tenantAppDirectory, bool, error) {
	names := []string{}
	if s.providers != nil {
		names = s.providers.List()
	}
	registryApps := s.configuredRegistryApps()
	seen := make(map[string]struct{}, len(names)+len(registryApps))
	dir := &tenantAppDirectory{entries: make([]tenantAppDirectoryEntry, 0, len(names)+len(registryApps))}
	cacheable := true
	for _, name := range names {
		entry, ok, err := s.tenantProviderDirectoryEntry(ctx, name)
		if err != nil {
			if ctx.Err() != nil {
				return nil, false, err
			}
			cacheable = false
			continue
		}
		if !ok {
			continue
		}
		seen[name] = struct{}{}
		dir.entries = append(dir.entries, entry)
	}
	for _, app := range registryApps {
		if _, ok := seen[app.name]; ok {
			continue
		}
		if s.integrationHiddenFromCatalog(app.name) {
			continue
		}
		dir.entries = append(dir.entries, s.tenantRegistryDirectoryEntry(app.name))
	}
	return dir, cacheable, nil
}

func (s *Server) tenantProviderDirectoryEntry(ctx context.Context, name string) (tenantAppDirectoryEntry, bool, error) {
	if s.integrationHiddenFromCatalog(name) {
		return tenantAppDirectoryEntry{}, false, nil
	}
	prov, err := s.providers.GetWithContext(ctx, name)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return tenantAppDirectoryEntry{}, false, nil
		}
		return tenantAppDirectoryEntry{}, false, fmt.Errorf("resolve app %q: %w", name, err)
	}
	plugin := s.pluginDefs[name]
	entry := tenantAppDirectoryEntry{
		Name:        name,
		DisplayName: prov.DisplayName(),
		Description: prov.Description(),
		Loaded:      true,
	}
	s.applyPluginDirectoryFields(&entry, plugin)
	s.attachDirectoryConnections(&entry, plugin)
	if cat := prov.Catalog(); cat != nil {
		entry.IconSVG = cat.IconSVG
	}
	return entry, true, nil
}

func (s *Server) tenantRegistryDirectoryEntry(name string) tenantAppDirectoryEntry {
	plugin := s.pluginDefs[name]
	entry := tenantAppDirectoryEntry{
		Name:        name,
		DisplayName: name,
	}
	s.applyPluginDirectoryFields(&entry, plugin)
	if plugin != nil && strings.TrimSpace(plugin.DisplayName) != "" {
		entry.DisplayName = strings.TrimSpace(plugin.DisplayName)
	}
	s.attachDirectoryConnections(&entry, plugin)
	return entry
}

func (s *Server) applyPluginDirectoryFields(entry *tenantAppDirectoryEntry, plugin *config.ProviderEntry) {
	if entry == nil {
		return
	}
	entry.Prompts = s.appPrompts[entry.Name]
	if plugin == nil {
		return
	}
	entry.SourceTreeURL = plugin.SourceTreeURL()
	entry.DeclaredMount = pluginDeclaredMount(plugin)
}

func pluginDeclaredMount(plugin *config.ProviderEntry) string {
	if plugin == nil || plugin.Static == nil {
		return ""
	}
	return strings.TrimSpace(plugin.Static.Mount)
}

func (s *Server) projectViewerAppDirectory(r *http.Request, snapshot *tenantAppDirectory) (*appDirectory, error) {
	if snapshot == nil {
		return &appDirectory{entries: []appDirectoryEntry{}}, nil
	}
	p := PrincipalFromContext(r.Context())
	ctx, _ := withListingDecisionCache(r.Context())
	names := make([]string, 0, len(snapshot.entries))
	for i := range snapshot.entries {
		names = append(names, snapshot.entries[i].Name)
	}
	s.prefetchIntegrationListingDecisions(ctx, p, names)

	out := &appDirectory{entries: make([]appDirectoryEntry, 0, len(snapshot.entries))}
	for i := range snapshot.entries {
		entry := s.viewerDirectoryEntry(ctx, p, snapshot.entries[i])
		usable, err := s.directoryEntryUsable(ctx, p, entry)
		if err != nil {
			return nil, err
		}
		if !usable {
			continue
		}
		out.entries = append(out.entries, entry)
	}
	return out, nil
}

func (s *Server) viewerDirectoryEntry(ctx context.Context, p *principal.Principal, entry tenantAppDirectoryEntry) appDirectoryEntry {
	return viewerDirectoryEntry(
		entry,
		s.integrationMountedPathForPrincipalContext(ctx, p, entry.Name, entry.DeclaredMount),
		s.integrationManagementPath(ctx, p, entry.Name),
	)
}

func (s *Server) visibleProviderDirectoryEntry(r *http.Request, name string) (appDirectoryEntry, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return appDirectoryEntry{}, false, nil
	}
	snapshot, err := s.tenantAppDirectory(r.Context())
	if err != nil {
		return appDirectoryEntry{}, false, err
	}
	var found tenantAppDirectoryEntry
	ok := false
	for i := range snapshot.entries {
		if snapshot.entries[i].Name == name {
			found = snapshot.entries[i]
			ok = true
			break
		}
	}
	if !ok {
		return appDirectoryEntry{}, false, nil
	}
	p := PrincipalFromContext(r.Context())
	ctx, _ := withListingDecisionCache(r.Context())
	s.prefetchIntegrationListingDecisions(ctx, p, []string{found.Name})
	entry := s.viewerDirectoryEntry(ctx, p, found)
	usable, err := s.directoryEntryUsable(ctx, p, entry)
	if err != nil {
		return appDirectoryEntry{}, false, err
	}
	if !usable {
		return appDirectoryEntry{}, false, nil
	}
	return entry, true, nil
}

func (s *Server) directoryEntryUsable(ctx context.Context, p *principal.Principal, entry appDirectoryEntry) (bool, error) {
	// Registry-only rows stay admin-only. Loaded apps appear when this user
	// may use them (Admin people/groups), including opening the web UI or
	// the admin page. One callable HTTP operation is not enough to put the
	// app in Apps.
	if !entry.Loaded {
		return entry.ManagementPath != "", nil
	}
	if entry.MountedPath != "" || entry.ManagementPath != "" {
		return true, nil
	}
	if s.authorization == nil {
		return true, nil
	}
	settingsAccessible, err := s.integrationSettingsAccessibleContext(ctx, p, entry.Name)
	if err != nil {
		return false, &appListingError{
			status: http.StatusServiceUnavailable,
			public: "failed to authorize app access",
			err:    fmt.Errorf("authorizing app %q: %w", entry.Name, err),
		}
	}
	return settingsAccessible, nil
}

func (s *Server) projectComposedAppListing(r *http.Request, dir *appDirectory) ([]integrationInfo, error) {
	if dir == nil {
		return []integrationInfo{}, nil
	}
	p := PrincipalFromContext(r.Context())
	connected, err := s.subjectConnectedIntegrations(r)
	if err != nil {
		return nil, &appListingError{
			status: http.StatusInternalServerError,
			public: "failed to check integration status",
			err:    err,
		}
	}
	out := make([]integrationInfo, 0, len(dir.entries))
	for i := range dir.entries {
		entry := &dir.entries[i]
		info := integrationInfo{
			Name:            entry.Name,
			DisplayName:     entry.DisplayName,
			Description:     entry.Description,
			IconSVG:         entry.IconSVG,
			MountedPath:     entry.MountedPath,
			ManagementPath:  entry.ManagementPath,
			Prompts:         entry.Prompts,
			SourceTreeURL:   entry.SourceTreeURL,
			Connections:     []connectionDefInfo{},
			Status:          connectionStatusUnknown,
			CredentialState: credentialStateUnknown,
			HealthState:     healthStateUnknown,
			Actions:         []string{},
		}
		instances := connected[entry.Name]
		info.Connections = s.connectionInfosFromAdvertised(r.Context(), entry.Name, entry.Advertised, instances, p)
		authTypes := resolvedAuthTypesFromConnections(info.Connections)
		s.applyIntegrationConnectionStatus(&info, s.liveProviderForListing(r.Context(), entry), instances, authTypes, p)
		out = append(out, info)
	}
	return out, nil
}

func (s *Server) liveProviderForListing(ctx context.Context, entry *appDirectoryEntry) core.Provider {
	if entry == nil || !entry.Loaded || s == nil || s.providers == nil {
		return nil
	}
	prov, err := s.providers.GetWithContext(ctx, entry.Name)
	if err != nil {
		return nil
	}
	return prov
}
