package authorization

import (
	"context"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

const (
	defaultSubjectRole = "viewer"
)

type Workload struct {
	ID          string
	DisplayName string
}

type AccessContext struct {
	Policy string
	Role   string
}

type SubjectPolicy struct {
	ID               string
	DefaultAllow     bool
	RolesBySubjectID map[string]string
}

type StaticSubjectMember struct {
	SubjectID string
	Role      string
}

type Authorizer struct {
	workloadsByHash  map[string]*Workload
	policies         map[string]*SubjectPolicy
	providerPolicies map[string]string
}

func New(cfg config.AuthorizationConfig, pluginDefs map[string]*config.ProviderEntry) (*Authorizer, error) {
	a := &Authorizer{
		workloadsByHash:  map[string]*Workload{},
		policies:         map[string]*SubjectPolicy{},
		providerPolicies: map[string]string{},
	}

	for policyID, def := range cfg.Policies {
		policy := &SubjectPolicy{
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
		if policy := strings.TrimSpace(entry.AuthorizationPolicy); policy != "" {
			a.providerPolicies[providerName] = policy
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
		}

		a.workloadsByHash[tokenHash] = workload
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

func (a *Authorizer) AllowProvider(ctx context.Context, p *principal.Principal, provider string) bool {
	if principal.IsSystemPrincipal(p) {
		return principal.AllowsProviderPermission(p, provider)
	}
	_, allowed := a.ResolveAccess(ctx, p, provider)
	return allowed
}

func (a *Authorizer) AllowOperation(ctx context.Context, p *principal.Principal, provider, operation string) bool {
	if principal.IsSystemPrincipal(p) {
		return principal.AllowsOperationPermission(p, provider, operation)
	}
	return a.AllowProvider(ctx, p, provider)
}

func (a *Authorizer) ResolveAccess(_ context.Context, p *principal.Principal, provider string) (AccessContext, bool) {
	if a == nil {
		return AccessContext{}, true
	}
	if principal.IsSystemPrincipal(p) {
		return AccessContext{}, principal.AllowsProviderPermission(p, provider)
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
		access.Role = defaultSubjectRole
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

func (a *Authorizer) StaticMembersForPolicy(policyName string) ([]StaticSubjectMember, bool) {
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
	members := make([]StaticSubjectMember, 0, len(policy.RolesBySubjectID))
	for subjectID, role := range policy.RolesBySubjectID {
		members = append(members, StaticSubjectMember{
			SubjectID: subjectID,
			Role:      role,
		})
	}
	return members, true
}

func (a *Authorizer) StaticMembersForProvider(provider string) (string, []StaticSubjectMember, bool) {
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
		access.Role = defaultSubjectRole
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
		access.Role = defaultSubjectRole
		return access, true
	}
	return access, false
}

func (a *Authorizer) AllowCatalogOperation(ctx context.Context, p *principal.Principal, provider string, op catalog.CatalogOperation) bool {
	if principal.IsSystemPrincipal(p) {
		return principal.AllowsOperationPermission(p, provider, op.ID)
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

func (p *SubjectPolicy) roleForPrincipal(pr *principal.Principal) (string, bool) {
	if p == nil || pr == nil {
		return "", false
	}
	return p.staticRoleForIdentity(principal.Canonicalized(pr).SubjectID)
}

func (p *SubjectPolicy) staticRoleForIdentity(subjectID string) (string, bool) {
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
