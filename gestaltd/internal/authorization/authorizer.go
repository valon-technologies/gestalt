package authorization

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/registry"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

const defaultInstance = "default"

var (
	safeConnectionValue = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	safeInstanceValue   = regexp.MustCompile(`^[a-zA-Z0-9._ -]+$`)
)

type CredentialBinding struct {
	Mode                core.ConnectionMode
	CredentialSubjectID string
	CredentialOwnerID   string
	Connection          string
	Instance            string
}

type WorkloadProviderBinding struct {
	CredentialBinding
	Allow map[string]struct{}
}

type Workload struct {
	ID          string
	DisplayName string
	Providers   map[string]WorkloadProviderBinding
}

type Authorizer struct {
	workloadsByHash      map[string]*Workload
	workloadsBySubjectID map[string]*Workload
}

func New(cfg config.AuthorizationConfig, pluginDefs map[string]*config.ProviderEntry, providers *registry.ProviderMap[core.Provider], defaultConnections map[string]string) (*Authorizer, error) {
	a := &Authorizer{
		workloadsByHash:      map[string]*Workload{},
		workloadsBySubjectID: map[string]*Workload{},
	}
	if len(cfg.Workloads) == 0 {
		return a, nil
	}

	for workloadID, def := range cfg.Workloads {
		token := strings.TrimSpace(def.Token)
		if token == "" {
			return nil, fmt.Errorf("authorization validation: workload %q token is required", workloadID)
		}
		if !strings.HasPrefix(token, "gst_wld_") {
			return nil, fmt.Errorf("authorization validation: workload %q token must use gst_wld_ prefix", workloadID)
		}
		tokenHash := principal.HashToken(token)
		if _, exists := a.workloadsByHash[tokenHash]; exists {
			return nil, fmt.Errorf("authorization validation: workload %q token duplicates another workload", workloadID)
		}

		workload := &Workload{
			ID:          workloadID,
			DisplayName: def.DisplayName,
			Providers:   make(map[string]WorkloadProviderBinding, len(def.Providers)),
		}

		for providerName, providerDef := range def.Providers {
			mode, ok, err := providerMode(providerName, pluginDefs, providers)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, fmt.Errorf("authorization validation: workload %q references unknown provider %q", workloadID, providerName)
			}

			allow := normalizeAllowedOperations(providerDef.Allow)
			if len(allow) == 0 {
				return nil, fmt.Errorf("authorization validation: workload %q provider %q allow must not be empty", workloadID, providerName)
			}

			binding, err := buildBinding(mode, workloadID, providerName, providerDef, defaultConnections)
			if err != nil {
				return nil, err
			}
			binding.Allow = allow
			workload.Providers[providerName] = binding
		}

		a.workloadsByHash[tokenHash] = workload
		a.workloadsBySubjectID[principal.WorkloadSubjectID(workloadID)] = workload
	}

	return a, nil
}

func (a *Authorizer) ResolveWorkloadToken(token string) (*principal.ResolvedWorkload, bool) {
	if a == nil {
		return nil, false
	}
	workload, ok := a.workloadsByHash[principal.HashToken(token)]
	if !ok || workload == nil {
		return nil, false
	}
	return &principal.ResolvedWorkload{ID: workload.ID, DisplayName: workload.DisplayName}, true
}

func (a *Authorizer) IsWorkload(p *principal.Principal) bool {
	return p != nil && p.Kind == principal.KindWorkload
}

func (a *Authorizer) AllowProvider(p *principal.Principal, provider string) bool {
	if !a.IsWorkload(p) {
		return true
	}
	_, ok := a.bindingForSubject(p, provider)
	return ok
}

func (a *Authorizer) AllowOperation(p *principal.Principal, provider, operation string) bool {
	if !a.IsWorkload(p) {
		return true
	}
	binding, ok := a.bindingForSubject(p, provider)
	if !ok {
		return false
	}
	_, ok = binding.Allow[operation]
	return ok
}

func (a *Authorizer) Binding(p *principal.Principal, provider string) (CredentialBinding, bool) {
	if !a.IsWorkload(p) {
		return CredentialBinding{}, false
	}
	binding, ok := a.bindingForSubject(p, provider)
	if !ok {
		return CredentialBinding{}, false
	}
	return binding.CredentialBinding, true
}

func (a *Authorizer) bindingForSubject(p *principal.Principal, provider string) (WorkloadProviderBinding, bool) {
	if a == nil || p == nil || p.SubjectID == "" {
		return WorkloadProviderBinding{}, false
	}
	workload, ok := a.workloadsBySubjectID[p.SubjectID]
	if !ok || workload == nil {
		return WorkloadProviderBinding{}, false
	}
	binding, ok := workload.Providers[provider]
	return binding, ok
}

func buildBinding(mode core.ConnectionMode, workloadID, provider string, def config.WorkloadProviderDef, defaultConnections map[string]string) (WorkloadProviderBinding, error) {
	switch mode {
	case core.ConnectionModeNone:
		if strings.TrimSpace(def.Connection) != "" || strings.TrimSpace(def.Instance) != "" {
			return WorkloadProviderBinding{}, fmt.Errorf("authorization validation: workload %q provider %q does not accept connection or instance bindings", workloadID, provider)
		}
		return WorkloadProviderBinding{
			CredentialBinding: CredentialBinding{
				Mode: core.ConnectionModeNone,
			},
		}, nil
	case core.ConnectionModeIdentity:
		connection := strings.TrimSpace(def.Connection)
		if connection == "" {
			connection = defaultConnections[provider]
		}
		connection = config.ResolveConnectionAlias(connection)
		if connection == "" {
			return WorkloadProviderBinding{}, fmt.Errorf("authorization validation: workload %q provider %q requires a bound connection", workloadID, provider)
		}
		if !safeConnectionValue.MatchString(connection) {
			return WorkloadProviderBinding{}, fmt.Errorf("authorization validation: workload %q provider %q connection contains invalid characters", workloadID, provider)
		}

		instance := strings.TrimSpace(def.Instance)
		if instance == "" {
			instance = defaultInstance
		}
		if !safeInstanceValue.MatchString(instance) {
			return WorkloadProviderBinding{}, fmt.Errorf("authorization validation: workload %q provider %q instance contains invalid characters", workloadID, provider)
		}

		return WorkloadProviderBinding{
			CredentialBinding: CredentialBinding{
				Mode:                core.ConnectionModeIdentity,
				CredentialSubjectID: principal.IdentitySubjectID(),
				CredentialOwnerID:   principal.IdentityPrincipal,
				Connection:          connection,
				Instance:            instance,
			},
		}, nil
	case core.ConnectionModeUser, core.ConnectionModeEither, "":
		return WorkloadProviderBinding{}, fmt.Errorf("authorization validation: workload %q provider %q uses unsupported connection mode %q in v1", workloadID, provider, mode)
	default:
		return WorkloadProviderBinding{}, fmt.Errorf("authorization validation: workload %q provider %q uses unknown connection mode %q", workloadID, provider, mode)
	}
}

func normalizeAllowedOperations(ops []string) map[string]struct{} {
	allowed := make(map[string]struct{}, len(ops))
	for _, op := range ops {
		name := strings.TrimSpace(op)
		if name == "" {
			continue
		}
		allowed[name] = struct{}{}
	}
	return allowed
}

func providerMode(provider string, pluginDefs map[string]*config.ProviderEntry, providers *registry.ProviderMap[core.Provider]) (core.ConnectionMode, bool, error) {
	if entry, ok := pluginDefs[provider]; ok && entry != nil {
		return pluginConnectionMode(entry), true, nil
	}
	if providers != nil {
		prov, err := providers.Get(provider)
		if err == nil && prov != nil {
			return prov.ConnectionMode(), true, nil
		}
	}
	return "", false, nil
}

func pluginConnectionMode(entry *config.ProviderEntry) core.ConnectionMode {
	needUser := false
	needIdentity := false

	addMode := func(mode core.ConnectionMode) {
		switch mode {
		case core.ConnectionModeUser:
			needUser = true
		case core.ConnectionModeIdentity:
			needIdentity = true
		case core.ConnectionModeEither:
			needUser = true
			needIdentity = true
		}
	}

	addMode(connectionModeForConnection(config.EffectivePluginConnectionDef(entry, entry.ManifestSpec())))

	for name := range namedConnectionNames(entry) {
		conn, ok := config.EffectiveNamedConnectionDef(entry, entry.ManifestSpec(), name)
		if !ok {
			continue
		}
		addMode(connectionModeForConnection(conn))
	}

	switch {
	case needUser && needIdentity:
		return core.ConnectionModeEither
	case needUser:
		return core.ConnectionModeUser
	case needIdentity:
		return core.ConnectionModeIdentity
	default:
		return core.ConnectionModeNone
	}
}

func namedConnectionNames(entry *config.ProviderEntry) map[string]struct{} {
	names := make(map[string]struct{})
	if entry == nil {
		return names
	}
	if spec := entry.ManifestSpec(); spec != nil {
		for name := range spec.Connections {
			resolved := config.ResolveConnectionAlias(name)
			if resolved != "" && resolved != config.PluginConnectionName {
				names[resolved] = struct{}{}
			}
		}
	}
	for name := range entry.Connections {
		resolved := config.ResolveConnectionAlias(name)
		if resolved != "" && resolved != config.PluginConnectionName {
			names[resolved] = struct{}{}
		}
	}
	return names
}

func connectionModeForConnection(conn config.ConnectionDef) core.ConnectionMode {
	if conn.Mode != "" {
		return core.ConnectionMode(conn.Mode)
	}
	switch conn.Auth.Type {
	case "", providermanifestv1.AuthTypeNone:
		return core.ConnectionModeNone
	default:
		return core.ConnectionModeUser
	}
}
