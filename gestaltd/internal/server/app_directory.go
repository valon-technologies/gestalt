package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

const appCatalogIconPathPrefix = "/api/v1/catalog/apps/"

// appDirectory is a list of apps. The tenant snapshot has identity, copy,
// icons, and connection fields for every installed app. A viewer copy is that
// list filtered to apps this signed-in user may use, with their mount and
// admin paths filled in. It does not include saved accounts or connection
// status — those belong on the overlay.
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
	Provider         core.Provider
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

func (s *Server) tenantAppDirectory(ctx context.Context) (*appDirectory, error) {
	key := s.tenantDirectoryFingerprint()
	s.tenantDirectoryMu.Lock()
	if s.tenantDirectory != nil && s.tenantDirectoryKey == key {
		dir := s.tenantDirectory
		s.tenantDirectoryMu.Unlock()
		return dir, nil
	}
	s.tenantDirectoryMu.Unlock()

	dir, err := s.buildTenantAppDirectory(ctx)
	if err != nil {
		return nil, err
	}

	s.tenantDirectoryMu.Lock()
	defer s.tenantDirectoryMu.Unlock()
	if s.tenantDirectory != nil && s.tenantDirectoryKey == key {
		return s.tenantDirectory, nil
	}
	s.tenantDirectory = dir
	s.tenantDirectoryKey = key
	return dir, nil
}

func (s *Server) tenantDirectoryFingerprint() string {
	var b strings.Builder
	if s.providers != nil {
		fmt.Fprintf(&b, "g:%d", s.providers.Generation())
		for _, name := range s.providers.List() {
			b.WriteByte(',')
			b.WriteString(name)
		}
	}
	names := make([]string, 0, len(s.pluginDefs))
	for name := range s.pluginDefs {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		b.WriteByte(';')
		b.WriteString(name)
		plugin := s.pluginDefs[name]
		if plugin == nil {
			continue
		}
		fmt.Fprintf(&b, ":%s:%t", strings.TrimSpace(plugin.DisplayName), s.integrationHiddenFromCatalog(name))
		if plugin.Static != nil {
			b.WriteByte(':')
			b.WriteString(strings.TrimSpace(plugin.Static.Mount))
		}
	}
	return b.String()
}

func (s *Server) buildTenantAppDirectory(ctx context.Context) (*appDirectory, error) {
	names := []string{}
	if s.providers != nil {
		names = s.providers.List()
	}
	registryApps := s.configuredRegistryApps()
	seen := make(map[string]struct{}, len(names)+len(registryApps))
	dir := &appDirectory{entries: make([]appDirectoryEntry, 0, len(names)+len(registryApps))}
	for _, name := range names {
		entry, ok, err := s.tenantProviderDirectoryEntry(ctx, name)
		if err != nil {
			return nil, err
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
	return dir, nil
}

func (s *Server) tenantProviderDirectoryEntry(ctx context.Context, name string) (appDirectoryEntry, bool, error) {
	if s.integrationHiddenFromCatalog(name) {
		return appDirectoryEntry{}, false, nil
	}
	prov, err := s.providers.GetWithContext(ctx, name)
	if err != nil {
		return appDirectoryEntry{}, false, nil
	}
	plugin := s.pluginDefs[name]
	entry := appDirectoryEntry{
		Name:          name,
		DisplayName:   prov.DisplayName(),
		Description:   prov.Description(),
		Prompts:       s.appPrompts[name],
		SourceTreeURL: plugin.SourceTreeURL(),
		Provider:      prov,
	}
	s.attachDirectoryConnections(&entry, plugin)
	if cat := prov.Catalog(); cat != nil {
		entry.IconSVG = cat.IconSVG
	}
	if plugin != nil && plugin.Static != nil {
		entry.DeclaredMount = strings.TrimSpace(plugin.Static.Mount)
	}
	return entry, true, nil
}

func (s *Server) tenantRegistryDirectoryEntry(name string) appDirectoryEntry {
	plugin := s.pluginDefs[name]
	entry := appDirectoryEntry{
		Name:          name,
		DisplayName:   name,
		Prompts:       s.appPrompts[name],
		SourceTreeURL: plugin.SourceTreeURL(),
	}
	s.attachDirectoryConnections(&entry, plugin)
	if plugin != nil && strings.TrimSpace(plugin.DisplayName) != "" {
		entry.DisplayName = strings.TrimSpace(plugin.DisplayName)
	}
	if plugin != nil && plugin.Static != nil {
		entry.DeclaredMount = strings.TrimSpace(plugin.Static.Mount)
	}
	return entry
}

func (s *Server) projectViewerAppDirectory(r *http.Request, snapshot *appDirectory) (*appDirectory, error) {
	if snapshot == nil {
		return &appDirectory{entries: []appDirectoryEntry{}}, nil
	}
	p := PrincipalFromContext(r.Context())
	ctx, _ := withListingDecisionCache(r.Context())
	r = r.WithContext(ctx)
	names := make([]string, 0, len(snapshot.entries))
	for i := range snapshot.entries {
		names = append(names, snapshot.entries[i].Name)
	}
	s.prefetchIntegrationListingDecisions(ctx, p, names)

	out := &appDirectory{entries: make([]appDirectoryEntry, 0, len(snapshot.entries))}
	for i := range snapshot.entries {
		entry := snapshot.entries[i]
		entry.MountedPath = s.integrationMountedPathForPrincipalContext(ctx, p, entry.Name, entry.DeclaredMount)
		entry.ManagementPath = s.integrationManagementPath(ctx, p, entry.Name)
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

func (s *Server) visibleProviderDirectoryEntry(r *http.Request, name string) (appDirectoryEntry, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return appDirectoryEntry{}, false, nil
	}
	snapshot, err := s.tenantAppDirectory(r.Context())
	if err != nil {
		return appDirectoryEntry{}, false, err
	}
	var found appDirectoryEntry
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
	viewer, err := s.projectViewerAppDirectory(r, &appDirectory{entries: []appDirectoryEntry{found}})
	if err != nil {
		return appDirectoryEntry{}, false, err
	}
	if viewer == nil || len(viewer.entries) == 0 {
		return appDirectoryEntry{}, false, nil
	}
	return viewer.entries[0], true, nil
}

func (s *Server) directoryEntryUsable(ctx context.Context, p *principal.Principal, entry appDirectoryEntry) (bool, error) {
	// Registry-only rows (no loaded provider) stay admin-only. Loaded apps
	// appear when this user may use them (Admin people/groups), including
	// opening the web UI or the admin page. One callable HTTP operation is
	// not enough to put the app in Apps.
	if entry.Provider == nil {
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
		s.applyIntegrationConnectionStatus(&info, entry.Provider, instances, authTypes, p)
		out = append(out, info)
	}
	return out, nil
}
