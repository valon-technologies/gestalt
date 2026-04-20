package authorization

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/emailutil"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/registry"
)

const (
	defaultInstance  = "default"
	defaultHumanRole = "viewer"
)

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
	IdentityID  string
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
	RolesByEmail     map[string]string
}

type StaticHumanMember struct {
	SubjectID string
	Email     string
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
			RolesByEmail:     make(map[string]string, len(def.Members)),
		}
		for _, member := range def.Members {
			role := strings.TrimSpace(member.Role)
			if subjectID := strings.TrimSpace(member.SubjectID); subjectID != "" {
				policy.RolesBySubjectID[subjectID] = role
				continue
			}
			if email := normalizeEmail(member.Email); email != "" {
				policy.RolesByEmail[email] = role
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

	if len(cfg.IdentityTokens) == 0 {
		return a, nil
	}

	for identityID, def := range cfg.IdentityTokens {
		ownerIdentityID := strings.TrimSpace(def.IdentityID)
		if ownerIdentityID == "" {
			ownerIdentityID = identityID
		}
		token := strings.TrimSpace(def.Token)
		if token == "" {
			return nil, fmt.Errorf("authorization validation: identity token %q token is required", identityID)
		}
		if !strings.HasPrefix(token, "gst_wld_") {
			return nil, fmt.Errorf("authorization validation: identity token %q token must use gst_wld_ prefix", identityID)
		}
		tokenHash := principal.HashToken(token)
		if _, exists := a.workloadsByHash[tokenHash]; exists {
			return nil, fmt.Errorf("authorization validation: identity token %q token duplicates another configured identity token", identityID)
		}

		workload := &Workload{
			ID:          identityID,
			DisplayName: def.DisplayName,
			IdentityID:  ownerIdentityID,
			Providers:   make(map[string]WorkloadProviderBinding, len(def.Providers)),
		}

		for providerName, providerDef := range def.Providers {
			mode, ok, err := providerMode(providerName, pluginDefs, providers)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, fmt.Errorf("authorization validation: identity token %q references unknown provider %q", identityID, providerName)
			}

			allow := normalizeAllowedOperations(providerDef.Allow)
			if len(allow) == 0 {
				return nil, fmt.Errorf("authorization validation: identity token %q provider %q allow must not be empty", identityID, providerName)
			}

			binding, err := buildBinding(mode, identityID, workload.IdentityID, providerName, providerDef, defaultConnections)
			if err != nil {
				return nil, err
			}
			binding.Allow = allow
			workload.Providers[providerName] = binding
		}

		a.workloadsByHash[tokenHash] = workload
		a.workloadsBySubjectID[principal.IdentitySubjectID(identityID)] = workload
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

func (a *Authorizer) ResolveIdentityToken(token string) (*principal.ResolvedIdentityToken, bool) {
	if a == nil {
		return nil, false
	}
	workload, ok := a.workloadsByHash[principal.HashToken(token)]
	if !ok || workload == nil {
		return nil, false
	}
	return &principal.ResolvedIdentityToken{ID: workload.ID, DisplayName: workload.DisplayName}, true
}

func (a *Authorizer) AllowProvider(ctx context.Context, p *principal.Principal, provider string) bool {
	if a.isConfiguredIdentityToken(p) {
		_, ok := a.bindingForSubject(p, provider)
		return ok
	}
	if a.isManagedIdentityPrincipal(p) {
		return a.allowManagedIdentityProvider(p, provider)
	}
	_, allowed := a.ResolveAccess(ctx, p, provider)
	return allowed
}

func (a *Authorizer) AllowOperation(ctx context.Context, p *principal.Principal, provider, operation string) bool {
	if a.isConfiguredIdentityToken(p) {
		binding, ok := a.bindingForSubject(p, provider)
		if !ok {
			return false
		}
		_, ok = binding.Allow[operation]
		return ok
	}
	if a.isManagedIdentityPrincipal(p) {
		return a.allowManagedIdentityProvider(p, provider) && principal.AllowsOperationPermission(p, provider, operation)
	}
	return a.AllowProvider(ctx, p, provider)
}

func (a *Authorizer) Binding(p *principal.Principal, provider string) (CredentialBinding, bool) {
	if a.isConfiguredIdentityToken(p) {
		binding, ok := a.bindingForSubject(p, provider)
		if !ok {
			return CredentialBinding{}, false
		}
		return binding.CredentialBinding, true
	}
	if a.isManagedIdentityPrincipal(p) {
		if !a.allowManagedIdentityProvider(p, provider) {
			return CredentialBinding{}, false
		}
		return CredentialBinding{Mode: core.ConnectionModeNone}, true
	}
	return CredentialBinding{}, false
}

func (a *Authorizer) ResolveAccess(_ context.Context, p *principal.Principal, provider string) (AccessContext, bool) {
	if a == nil {
		return AccessContext{}, true
	}
	if a.isConfiguredIdentityToken(p) {
		return AccessContext{}, a.AllowProvider(context.Background(), p, provider)
	}
	if a.isManagedIdentityPrincipal(p) {
		return AccessContext{}, a.allowManagedIdentityProvider(p, provider)
	}
	policyName := strings.TrimSpace(a.providerPolicies[provider])
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

func (a *Authorizer) StaticRoleForPolicyIdentity(policyName, subjectID, userID, email string) (AccessContext, bool) {
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
	if role, ok := policy.staticRoleForIdentity(subjectID, userID, email); ok {
		access.Role = role
		return access, true
	}
	return access, false
}

func (a *Authorizer) StaticRoleForProviderIdentity(provider, subjectID, userID, email string) (AccessContext, bool) {
	if a == nil {
		return AccessContext{}, false
	}
	policyName := a.PolicyNameForProvider(provider)
	return a.StaticRoleForPolicyIdentity(policyName, subjectID, userID, email)
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
	members := make([]StaticHumanMember, 0, len(policy.RolesBySubjectID)+len(policy.RolesByEmail))
	for subjectID, role := range policy.RolesBySubjectID {
		members = append(members, StaticHumanMember{
			SubjectID: subjectID,
			Role:      role,
		})
	}
	for email, role := range policy.RolesByEmail {
		members = append(members, StaticHumanMember{
			Email: email,
			Role:  role,
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
	if p != nil && !p.HasUserContext() {
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
	if p != nil && !p.HasUserContext() {
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
	if a.isConfiguredIdentityToken(p) {
		return a.AllowOperation(ctx, p, provider, op.ID)
	}
	if a.isManagedIdentityPrincipal(p) {
		return a.allowManagedIdentityProvider(p, provider) && principal.AllowsOperationPermission(p, provider, op.ID)
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
	if a == nil || !a.isConfiguredIdentityToken(p) || p.SubjectID == "" {
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
	email := ""
	if pr.Identity != nil {
		email = pr.Identity.Email
	}
	return p.staticRoleForIdentity(pr.SubjectID, pr.UserID, email)
}

func (p *HumanPolicy) staticRoleForIdentity(subjectID, userID, email string) (string, bool) {
	if p == nil {
		return "", false
	}
	if subjectID = strings.TrimSpace(subjectID); subjectID != "" {
		if role, ok := p.RolesBySubjectID[subjectID]; ok {
			return role, true
		}
	}
	if userID = strings.TrimSpace(userID); userID != "" {
		if role, ok := p.RolesBySubjectID[principal.UserSubjectID(userID)]; ok {
			return role, true
		}
	}
	if email = normalizeEmail(email); email != "" {
		if role, ok := p.RolesByEmail[email]; ok {
			return role, true
		}
	}
	return "", false
}

func buildBinding(mode core.ConnectionMode, workloadID, identityID, provider string, def config.WorkloadProviderDef, defaultConnections map[string]string) (WorkloadProviderBinding, error) {
	switch mode {
	case core.ConnectionModeNone:
		if strings.TrimSpace(def.Connection) != "" || strings.TrimSpace(def.Instance) != "" {
			return WorkloadProviderBinding{}, fmt.Errorf("authorization validation: identity token %q provider %q does not accept connection or instance bindings", workloadID, provider)
		}
		return WorkloadProviderBinding{
			CredentialBinding: CredentialBinding{
				Mode: core.ConnectionModeNone,
			},
		}, nil
	case core.ConnectionModeIdentity:
		ownerIdentityID := strings.TrimSpace(identityID)
		if ownerIdentityID == "" {
			return WorkloadProviderBinding{}, fmt.Errorf("authorization validation: identity token %q provider %q requires identity ownership for identity-mode credentials", workloadID, provider)
		}
		if !safeConnectionValue.MatchString(ownerIdentityID) {
			return WorkloadProviderBinding{}, fmt.Errorf("authorization validation: identity token %q identity ID contains invalid characters", workloadID)
		}
		connection := strings.TrimSpace(def.Connection)
		if connection == "" {
			connection = defaultConnections[provider]
		}
		connection = config.ResolveConnectionAlias(connection)
		if connection == "" {
			return WorkloadProviderBinding{}, fmt.Errorf("authorization validation: identity token %q provider %q requires a bound connection", workloadID, provider)
		}
		if !safeConnectionValue.MatchString(connection) {
			return WorkloadProviderBinding{}, fmt.Errorf("authorization validation: identity token %q provider %q connection contains invalid characters", workloadID, provider)
		}

		instance := strings.TrimSpace(def.Instance)
		if instance == "" {
			instance = defaultInstance
		}
		if !safeInstanceValue.MatchString(instance) {
			return WorkloadProviderBinding{}, fmt.Errorf("authorization validation: identity token %q provider %q instance contains invalid characters", workloadID, provider)
		}

		return WorkloadProviderBinding{
			CredentialBinding: CredentialBinding{
				Mode:                core.ConnectionModeIdentity,
				CredentialSubjectID: principal.IdentitySubjectID(ownerIdentityID),
				CredentialOwnerID:   ownerIdentityID,
				Connection:          connection,
				Instance:            instance,
			},
		}, nil
	case core.ConnectionModeUser, core.ConnectionMode("either"), "":
		return WorkloadProviderBinding{}, fmt.Errorf("authorization validation: identity token %q provider %q uses unsupported connection mode %q in v1", workloadID, provider, mode)
	default:
		return WorkloadProviderBinding{}, fmt.Errorf("authorization validation: identity token %q provider %q uses unknown connection mode %q", workloadID, provider, mode)
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

func normalizeEmail(email string) string {
	return emailutil.Normalize(email)
}

func (a *Authorizer) isManagedIdentityPrincipal(p *principal.Principal) bool {
	return p != nil && p.Kind == principal.KindIdentity && p.Source == principal.SourceAPIToken
}

func (a *Authorizer) isConfiguredIdentityToken(p *principal.Principal) bool {
	return p != nil && p.Kind == principal.KindIdentity && p.Source == principal.SourceIdentityToken
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
	return mode == core.ConnectionModeNone
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
