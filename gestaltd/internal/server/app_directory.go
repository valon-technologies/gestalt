package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

const appCatalogIconPathPrefix = "/api/v1/catalog/apps/"

// appDirectory is the tenant app directory for one viewer: identity, display,
// connection schema, and which surfaces this principal may see. It does not
// include this subject's stored credentials or derived connection status.
type appDirectory struct {
	entries []appDirectoryEntry
}

type appDirectoryEntry struct {
	Name             string
	DisplayName      string
	Description      string
	IconSVG          string
	MountedPath      string
	ManagementPath   string
	Prompts          []appPromptInfo
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
	McpPassthrough   bool                           `json:"mcpPassthrough,omitempty"`
}

type appCatalogEntry struct {
	Name           string                 `json:"name"`
	DisplayName    string                 `json:"displayName,omitempty"`
	Description    string                 `json:"description,omitempty"`
	IconURL        string                 `json:"iconUrl,omitempty"`
	MountedPath    string                 `json:"mountedPath,omitempty"`
	ManagementPath string                 `json:"managementPath,omitempty"`
	Prompts        []appPromptInfo        `json:"prompts,omitempty"`
	Connections    []connectionSchemaInfo `json:"connections"`
}

type appConnectionStatus struct {
	Name            string                 `json:"name"`
	Status          string                 `json:"status"`
	CredentialState string                 `json:"credentialState"`
	HealthState     string                 `json:"healthState"`
	Actions         []string               `json:"actions"`
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
	for _, entry := range dir.entries {
		out = append(out, entry.catalogJSON())
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
			Connections:     connectionStatusViews(info.Connections),
		})
	}
	return out
}

func (s *Server) assembleAppDirectory(r *http.Request) (*appDirectory, error) {
	p := PrincipalFromContext(r.Context())
	names := s.providers.List()
	registryApps := s.configuredRegistryApps()

	ctx, _ := withListingDecisionCache(r.Context())
	r = r.WithContext(ctx)
	prefetchNames := make([]string, 0, len(names)+len(registryApps))
	prefetchNames = append(prefetchNames, names...)
	for _, app := range registryApps {
		prefetchNames = append(prefetchNames, app.name)
	}
	s.prefetchIntegrationListingDecisions(ctx, p, prefetchNames)

	seen := make(map[string]struct{}, len(names))
	dir := &appDirectory{entries: make([]appDirectoryEntry, 0, len(names))}
	for _, name := range names {
		prov, ok := s.lookupProviderDirectory(r, name)
		if !ok {
			continue
		}
		seen[name] = struct{}{}
		entry, visible, err := s.completeProviderDirectoryEntry(r, p, name, prov)
		if err != nil {
			return nil, err
		}
		if !visible {
			continue
		}
		dir.entries = append(dir.entries, entry)
	}
	for _, app := range registryApps {
		if s.integrationHiddenFromCatalog(app.name) {
			continue
		}
		if _, ok := seen[app.name]; ok {
			continue
		}
		managementPath := s.integrationManagementPath(r.Context(), p, app.name)
		if managementPath == "" {
			continue
		}
		entry := appDirectoryEntry{
			Name:           app.name,
			DisplayName:    app.name,
			ManagementPath: managementPath,
			Prompts:        s.appPrompts[app.name],
		}
		if plugin, ok := s.pluginDefs[app.name]; ok && plugin != nil && plugin.Static != nil {
			entry.MountedPath = s.integrationMountedPathForPrincipalContext(r.Context(), p, app.name, strings.TrimSpace(plugin.Static.Mount))
		}
		dir.entries = append(dir.entries, entry)
	}
	return dir, nil
}

func (s *Server) lookupProviderDirectory(r *http.Request, name string) (core.Provider, bool) {
	if s.integrationHiddenFromCatalog(name) {
		return nil, false
	}
	prov, err := s.providers.GetWithContext(r.Context(), name)
	if err != nil {
		return nil, false
	}
	return prov, true
}

func (s *Server) completeProviderDirectoryEntry(r *http.Request, p *principal.Principal, name string, prov core.Provider) (appDirectoryEntry, bool, error) {
	entry := appDirectoryEntry{
		Name:             name,
		DisplayName:      prov.DisplayName(),
		Description:      prov.Description(),
		Prompts:          s.appPrompts[name],
		ConnectionSchema: s.connectionSchemasForPlugin(name, s.pluginDefs[name]),
		Provider:         prov,
	}
	if cat := prov.Catalog(); cat != nil {
		entry.IconSVG = cat.IconSVG
	}
	mountedPath := ""
	if plugin, ok := s.pluginDefs[name]; ok && plugin != nil && plugin.Static != nil {
		mountedPath = strings.TrimSpace(plugin.Static.Mount)
	}
	entry.MountedPath = s.integrationMountedPathForPrincipalContext(r.Context(), p, name, mountedPath)
	entry.ManagementPath = s.integrationManagementPath(r.Context(), p, name)
	usable, err := s.directoryEntryUsable(r.Context(), p, entry)
	if err != nil {
		return appDirectoryEntry{}, false, &appListingError{
			status: http.StatusServiceUnavailable,
			public: "failed to authorize app access",
			err:    fmt.Errorf("authorizing app %q: %w", name, err),
		}
	}
	if !usable {
		return appDirectoryEntry{}, false, nil
	}
	return entry, true, nil
}

func (s *Server) visibleProviderDirectoryEntry(r *http.Request, name string) (appDirectoryEntry, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return appDirectoryEntry{}, false, nil
	}
	p := PrincipalFromContext(r.Context())
	ctx, _ := withListingDecisionCache(r.Context())
	r = r.WithContext(ctx)
	s.prefetchIntegrationListingDecisions(ctx, p, []string{name})
	prov, ok := s.lookupProviderDirectory(r, name)
	if !ok {
		return appDirectoryEntry{}, false, nil
	}
	return s.completeProviderDirectoryEntry(r, p, name, prov)
}

func (s *Server) directoryEntryUsable(ctx context.Context, p *principal.Principal, entry appDirectoryEntry) (bool, error) {
	if entry.MountedPath != "" || entry.ManagementPath != "" {
		return true, nil
	}
	if s.directoryHasSettingsSurface(p, entry) {
		settingsAccessible, err := s.integrationSettingsAccessibleContext(ctx, p, entry.Name)
		if err != nil {
			return false, err
		}
		if settingsAccessible {
			return true, nil
		}
	}
	return s.integrationHasVisibleHTTPOperationsContext(ctx, p, entry.Name, entry.Provider)
}

func (s *Server) directoryHasSettingsSurface(p *principal.Principal, entry appDirectoryEntry) bool {
	if principal.IsNonUserPrincipal(p) {
		return false
	}
	if len(entry.ConnectionSchema) > 0 {
		return true
	}
	return entry.Provider != nil && core.NormalizeConnectionMode(entry.Provider.ConnectionMode()) == core.ConnectionModeNone
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
	for _, entry := range dir.entries {
		info := integrationInfo{
			Name:            entry.Name,
			DisplayName:     entry.DisplayName,
			Description:     entry.Description,
			IconSVG:         entry.IconSVG,
			MountedPath:     entry.MountedPath,
			ManagementPath:  entry.ManagementPath,
			Prompts:         entry.Prompts,
			Connections:     []connectionDefInfo{},
			Status:          connectionStatusUnknown,
			CredentialState: credentialStateUnknown,
			HealthState:     healthStateUnknown,
			Actions:         []string{},
		}
		instances := connected[entry.Name]
		authTypes := s.populateIntegrationSettings(r.Context(), &info, instances, p)
		s.applyIntegrationConnectionStatus(&info, entry.Provider, instances, authTypes, p)
		out = append(out, info)
	}
	return out, nil
}
