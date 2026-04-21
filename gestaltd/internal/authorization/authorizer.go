package authorization

import (
	"context"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/registry"
)

const (
	defaultInstance  = "default"
	defaultHumanRole = "viewer"
)

type CredentialBinding struct {
	Mode                core.ConnectionMode
	CredentialSubjectID string
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

type AccessContext struct {
	Policy string
	Role   string
}

type HumanPolicy struct {
	ID               string
	DefaultAllow     bool
	RolesBySubjectID map[string]string
}

type StaticHumanMember struct {
	SubjectID string
	Role      string
}

type Authorizer struct {
	workloadsByHash      map[string]*Workload
	workloadsBySubjectID map[string]*Workload
	policies             map[string]*HumanPolicy
	providerPolicies     map[string]string
	providerModes        map[string]core.ConnectionMode
}

func New(cfg config.AuthorizationConfig, pluginDefs map[string]*config.ProviderEntry, providers *registry.ProviderMap[core.Provider], defaultConnections map[string]string) (*Authorizer, error) {
	a := &Authorizer{
		workloadsByHash:      map[string]*Workload{},
		workloadsBySubjectID: map[string]*Workload{},
		policies:             map[string]*HumanPolicy{},
		providerPolicies:     map[string]string{},
		providerModes:        map[string]core.ConnectionMode{},
	}

	for policyID, def := range cfg.Policies {
		policy := &HumanPolicy{
			ID:               policyID,
			DefaultAllow:     strings.EqualFold(strings.TrimSpace(def.Default), "allow"),
			RolesBySubjectID: make(map[string]string, len(def.Members)),
		}
		for _, member := range def.Members {
			role := strings.TrimSpace(member.Role)
			if subjectID := strings.TrimSpace(member.SubjectID); subjectID != "" {
				policy.RolesBySubjectID[subjectID] = role
			}
		}
		a.policies[policyID] = policy
	}
	for providerName, entry := range pluginDefs {
		if entry == nil {
			continue
		}
		if mode, ok, err := providerMode(providerName, pluginDefs, providers); err != nil {
			return nil, err
		} else if ok {
			a.providerModes[providerName] = mode
		}
		if policy := strings.TrimSpace(entry.AuthorizationPolicy); policy != "" {
			a.providerPolicies[providerName] = policy
		}
	}
	if providers != nil {
		for _, providerName := range providers.List() {
			if _, ok := a.providerModes[providerName]; ok {
				continue
			}
			prov, err := providers.Get(providerName)
			if err != nil || prov == nil {
				continue
			}
			a.providerModes[providerName] = prov.ConnectionMode()
		}
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

func (a *Authorizer) Start(ctx context.Context) error {
	_ = ctx
	return nil
}

func (a *Authorizer) Close() error {
	return nil
}

func (a *Authorizer) ReloadAuthorizationState(ctx context.Context) error {
	_ = ctx
	return nil
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

func (a *Authorizer) AllowProvider(ctx context.Context, p *principal.Principal, provider string) bool {
	if a.isManagedIdentityPrincipal(p) {
		return a.allowManagedIdentityProvider(p, provider)
	}
	if principal.IsSystemPrincipal(p) {
		return principal.AllowsProviderPermission(p, provider)
	}
	if !a.IsWorkload(p) {
		_, allowed := a.ResolveAccess(ctx, p, provider)
		return allowed
	}
	_, ok := a.bindingForSubject(p, provider)
	return ok
}

func (a *Authorizer) AllowOperation(ctx context.Context, p *principal.Principal, provider, operation string) bool {
	if a.isManagedIdentityPrincipal(p) {
		return a.allowManagedIdentityProvider(p, provider) && principal.AllowsOperationPermission(p, provider, operation)
	}
	if principal.IsSystemPrincipal(p) {
		return principal.AllowsOperationPermission(p, provider, operation)
	}
	if !a.IsWorkload(p) {
		return a.AllowProvider(ctx, p, provider)
	}
	binding, ok := a.bindingForSubject(p, provider)
	if !ok {
		return false
	}
	_, ok = binding.Allow[operation]
	return ok
}

func (a *Authorizer) Binding(p *principal.Principal, provider string) (CredentialBinding, bool) {
	if a.isManagedIdentityPrincipal(p) {
		if !a.allowManagedIdentityProvider(p, provider) {
			return CredentialBinding{}, false
		}
		switch core.NormalizeConnectionMode(a.providerModes[provider]) {
		case core.ConnectionModeNone:
			return CredentialBinding{Mode: core.ConnectionModeNone}, true
		case core.ConnectionModeUser:
			subjectID := principal.EffectiveCredentialSubjectID(p)
			if subjectID == "" {
				return CredentialBinding{}, false
			}
			return CredentialBinding{
				Mode:                core.ConnectionModeUser,
				CredentialSubjectID: subjectID,
			}, true
		default:
			return CredentialBinding{}, false
		}
	}
	if !a.IsWorkload(p) {
		return CredentialBinding{}, false
	}
	binding, ok := a.bindingForSubject(p, provider)
	if !ok {
		return CredentialBinding{}, false
	}
	return binding.CredentialBinding, true
}

func (a *Authorizer) ResolveAccess(_ context.Context, p *principal.Principal, provider string) (AccessContext, bool) {
	if a == nil {
		return AccessContext{}, true
	}
	if a.isManagedIdentityPrincipal(p) {
		return AccessContext{}, a.allowManagedIdentityProvider(p, provider)
	}
	if principal.IsSystemPrincipal(p) {
		return AccessContext{}, principal.AllowsProviderPermission(p, provider)
	}
	policyName := strings.TrimSpace(a.providerPolicies[provider])
	if a.IsWorkload(p) {
		if policyName == "" {
			return AccessContext{}, false
		}
		return AccessContext{Policy: policyName}, false
	}
	if policyName == "" {
		return AccessContext{}, true
	}

	policy := a.policies[policyName]
	if policy == nil {
		return AccessContext{}, false
	}

	access := AccessContext{Policy: policyName}
	if role, ok := policy.roleForPrincipal(p); ok {
		access.Role = role
		return access, true
	}
	if policy.DefaultAllow {
		access.Role = defaultHumanRole
		return access, true
	}
	return access, false
}

func (a *Authorizer) PolicyNameForProvider(provider string) string {
	if a == nil {
		return ""
	}
	return strings.TrimSpace(a.providerPolicies[provider])
}

func (a *Authorizer) StaticRoleForPolicyIdentity(policyName, subjectID string) (AccessContext, bool) {
	if a == nil {
		return AccessContext{}, false
	}
	policyName = strings.TrimSpace(policyName)
	if policyName == "" {
		return AccessContext{}, false
	}
	policy := a.policies[policyName]
	if policy == nil {
		return AccessContext{}, false
	}
	access := AccessContext{Policy: policyName}
	if role, ok := policy.staticRoleForIdentity(subjectID); ok {
		access.Role = role
		return access, true
	}
	return access, false
}

func (a *Authorizer) StaticRoleForProviderIdentity(provider, subjectID string) (AccessContext, bool) {
	if a == nil {
		return AccessContext{}, false
	}
	policyName := a.PolicyNameForProvider(provider)
	return a.StaticRoleForPolicyIdentity(policyName, subjectID)
}

func (a *Authorizer) StaticMembersForPolicy(policyName string) ([]StaticHumanMember, bool) {
	if a == nil {
		return nil, false
	}
	policyName = strings.TrimSpace(policyName)
	if policyName == "" {
		return nil, false
	}
	policy := a.policies[policyName]
	if policy == nil {
		return nil, false
	}
	members := make([]StaticHumanMember, 0, len(policy.RolesBySubjectID))
	for subjectID, role := range policy.RolesBySubjectID {
		members = append(members, StaticHumanMember{
			SubjectID: subjectID,
			Role:      role,
		})
	}
	return members, true
}

func (a *Authorizer) StaticMembersForProvider(provider string) (string, []StaticHumanMember, bool) {
	if a == nil {
		return "", nil, false
	}
	policyName := a.PolicyNameForProvider(provider)
	if policyName == "" {
		return "", nil, false
	}
	members, ok := a.StaticMembersForPolicy(policyName)
	if !ok {
		return policyName, nil, false
	}
	return policyName, members, true
}

func (a *Authorizer) ResolvePolicyAccess(_ context.Context, p *principal.Principal, policyName string) (AccessContext, bool) {
	if a == nil {
		return AccessContext{}, true
	}
	if policyName == "" {
		return AccessContext{}, true
	}
	if a.IsWorkload(p) {
		return AccessContext{Policy: policyName}, false
	}
	policy := a.policies[policyName]
	if policy == nil {
		return AccessContext{}, false
	}
	access := AccessContext{Policy: policyName}
	if role, ok := policy.roleForPrincipal(p); ok {
		access.Role = role
		return access, true
	}
	if policy.DefaultAllow {
		access.Role = defaultHumanRole
		return access, true
	}
	return access, false
}

func (a *Authorizer) ResolveAdminAccess(_ context.Context, p *principal.Principal, policyName string) (AccessContext, bool) {
	if a == nil {
		return AccessContext{}, true
	}
	policyName = strings.TrimSpace(policyName)
	if policyName == "" {
		return AccessContext{}, true
	}
	if a.IsWorkload(p) {
		return AccessContext{Policy: policyName}, false
	}
	policy := a.policies[policyName]
	if policy == nil {
		return AccessContext{}, false
	}
	access := AccessContext{Policy: policyName}
	if role, ok := policy.roleForPrincipal(p); ok {
		access.Role = role
		return access, true
	}
	if policy.DefaultAllow {
		access.Role = defaultHumanRole
		return access, true
	}
	return access, false
}

func (a *Authorizer) AllowCatalogOperation(ctx context.Context, p *principal.Principal, provider string, op catalog.CatalogOperation) bool {
	if a.isManagedIdentityPrincipal(p) {
		return a.allowManagedIdentityProvider(p, provider) && principal.AllowsOperationPermission(p, provider, op.ID)
	}
	if principal.IsSystemPrincipal(p) {
		return principal.AllowsOperationPermission(p, provider, op.ID)
	}
	if a.IsWorkload(p) {
		return a.AllowOperation(ctx, p, provider, op.ID)
	}
	access, allowed := a.ResolveAccess(ctx, p, provider)
	if !allowed {
		return false
	}
	if access.Policy == "" {
		return true
	}
	if access.Policy != "" && len(op.AllowedRoles) == 0 {
		policy := a.policies[access.Policy]
		return policy != nil && policy.DefaultAllow
	}
	if access.Role == "" {
		return false
	}
	for _, role := range op.AllowedRoles {
		if strings.TrimSpace(role) == access.Role {
			return true
		}
	}
	return false
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

func (p *HumanPolicy) roleForPrincipal(pr *principal.Principal) (string, bool) {
	if p == nil || pr == nil {
		return "", false
	}
	return p.staticRoleForIdentity(principal.Canonicalized(pr).SubjectID)
}

func (p *HumanPolicy) staticRoleForIdentity(subjectID string) (string, bool) {
	if p == nil {
		return "", false
	}
	if subjectID = strings.TrimSpace(subjectID); subjectID != "" {
		if role, ok := p.RolesBySubjectID[subjectID]; ok {
			return role, true
		}
	}
	return "", false
}

func buildBinding(mode core.ConnectionMode, workloadID, provider string, def config.WorkloadProviderDef, defaultConnections map[string]string) (WorkloadProviderBinding, error) {
	switch core.NormalizeConnectionMode(mode) {
	case core.ConnectionModeNone:
		if strings.TrimSpace(def.Connection) != "" || strings.TrimSpace(def.Instance) != "" {
			return WorkloadProviderBinding{}, fmt.Errorf("authorization validation: workload %q provider %q does not accept connection or instance bindings", workloadID, provider)
		}
		return WorkloadProviderBinding{
			CredentialBinding: CredentialBinding{
				Mode: core.ConnectionModeNone,
			},
		}, nil
	case core.ConnectionModeUser:
		connection := strings.TrimSpace(def.Connection)
		if connection == "" {
			connection = defaultConnections[provider]
		}
		connection = config.ResolveConnectionAlias(connection)
		if connection == "" {
			return WorkloadProviderBinding{}, fmt.Errorf("authorization validation: workload %q provider %q requires a bound connection", workloadID, provider)
		}
		if !config.SafeConnectionValue(connection) {
			return WorkloadProviderBinding{}, fmt.Errorf("authorization validation: workload %q provider %q connection contains invalid characters", workloadID, provider)
		}

		instance := strings.TrimSpace(def.Instance)
		if instance == "" {
			instance = defaultInstance
		}
		if !config.SafeInstanceValue(instance) {
			return WorkloadProviderBinding{}, fmt.Errorf("authorization validation: workload %q provider %q instance contains invalid characters", workloadID, provider)
		}

		return WorkloadProviderBinding{
			CredentialBinding: CredentialBinding{
				Mode:                core.ConnectionModeUser,
				CredentialSubjectID: principal.WorkloadSubjectID(workloadID),
				Connection:          connection,
				Instance:            instance,
			},
		}, nil
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

func (a *Authorizer) isManagedIdentityPrincipal(p *principal.Principal) bool {
	return a.IsWorkload(p) && principal.ManagedIdentityIDFromSubjectID(strings.TrimSpace(p.SubjectID)) != ""
}

func (a *Authorizer) allowManagedIdentityProvider(p *principal.Principal, provider string) bool {
	if a == nil || p == nil {
		return false
	}
	if !principal.AllowsProviderPermission(p, provider) {
		return false
	}
	mode, ok := a.providerModes[provider]
	if !ok {
		return false
	}
	switch core.NormalizeConnectionMode(mode) {
	case core.ConnectionModeNone, core.ConnectionModeUser:
		return true
	default:
		return false
	}
}

func providerMode(provider string, pluginDefs map[string]*config.ProviderEntry, providers *registry.ProviderMap[core.Provider]) (core.ConnectionMode, bool, error) {
	if entry, ok := pluginDefs[provider]; ok && entry != nil {
		plan, err := config.BuildStaticConnectionPlan(entry, entry.ManifestSpec())
		if err != nil {
			return "", false, err
		}
		return plan.ConnectionMode(), true, nil
	}
	if providers != nil {
		prov, err := providers.Get(provider)
		if err == nil && prov != nil {
			return prov.ConnectionMode(), true, nil
		}
	}
	return "", false, nil
}
