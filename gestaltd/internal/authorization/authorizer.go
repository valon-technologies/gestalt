package authorization

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/emailutil"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/registry"
)

const (
	defaultInstance  = "default"
	defaultHumanRole = "viewer"
)

const defaultDynamicReloadInterval = 5 * time.Second

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

type dynamicGrant struct {
	UserID string
	Email  string
	Role   string
}

type dynamicSnapshot struct {
	byPluginUserID map[string]map[string]dynamicGrant
	byPluginEmail  map[string]map[string]dynamicGrant
	adminByUserID  map[string]dynamicGrant
	adminByEmail   map[string]dynamicGrant
}

type Authorizer struct {
	workloadsByHash      map[string]*Workload
	workloadsBySubjectID map[string]*Workload
	policies             map[string]*HumanPolicy
	providerPolicies     map[string]string
	providerModes        map[string]core.ConnectionMode
	dynamicService       *coredata.PluginAuthorizationService
	adminDynamicService  *coredata.AdminAuthorizationService
	dynamicReloadEvery   time.Duration
	dynamic              atomic.Pointer[dynamicSnapshot]
	lifecycleMu          sync.Mutex
	started              bool
	closed               bool
	pollCancel           context.CancelFunc
	pollDone             chan struct{}
}

func New(cfg config.AuthorizationConfig, pluginDefs map[string]*config.ProviderEntry, providers *registry.ProviderMap[core.Provider], defaultConnections map[string]string, dynamicServices ...*coredata.PluginAuthorizationService) (*Authorizer, error) {
	var dynamicService *coredata.PluginAuthorizationService
	if len(dynamicServices) > 0 {
		dynamicService = dynamicServices[0]
	}
	a := &Authorizer{
		workloadsByHash:      map[string]*Workload{},
		workloadsBySubjectID: map[string]*Workload{},
		policies:             map[string]*HumanPolicy{},
		providerPolicies:     map[string]string{},
		providerModes:        map[string]core.ConnectionMode{},
		dynamicService:       dynamicService,
		dynamicReloadEvery:   defaultDynamicReloadInterval,
	}
	a.dynamic.Store(emptyDynamicSnapshot())

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

func (a *Authorizer) SetAdminAuthorizationService(svc *coredata.AdminAuthorizationService) {
	if a == nil {
		return
	}
	a.adminDynamicService = svc
}

func (a *Authorizer) Start(ctx context.Context) error {
	if a == nil || !a.hasDynamicSources() {
		return nil
	}

	a.lifecycleMu.Lock()
	defer a.lifecycleMu.Unlock()
	if a.closed {
		return fmt.Errorf("authorizer already closed")
	}
	if a.started {
		return nil
	}
	if err := a.ReloadDynamic(ctx); err != nil {
		return err
	}

	pollCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	a.pollCancel = cancel
	a.pollDone = done
	a.started = true
	go a.pollLoop(pollCtx, done)
	return nil
}

func (a *Authorizer) Close() error {
	if a == nil {
		return nil
	}

	a.lifecycleMu.Lock()
	if a.closed {
		a.lifecycleMu.Unlock()
		return nil
	}
	cancel := a.pollCancel
	done := a.pollDone
	a.pollCancel = nil
	a.pollDone = nil
	a.closed = true
	a.lifecycleMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	return nil
}

func (a *Authorizer) HasDynamicPluginAuthorizations() bool {
	return a != nil && a.dynamicService != nil
}

func (a *Authorizer) HasDynamicAdminAuthorizations() bool {
	return a != nil && a.adminDynamicService != nil
}

func (a *Authorizer) ReloadDynamic(ctx context.Context) error {
	if a == nil || !a.hasDynamicSources() {
		return nil
	}

	previous := a.dynamic.Load()
	snapshot := emptyDynamicSnapshot()
	var reloadErr error
	if a.dynamicService != nil {
		grants, err := a.dynamicService.ListPluginAuthorizations(ctx)
		if err != nil {
			copyPluginDynamicSnapshot(snapshot, previous)
			reloadErr = errors.Join(reloadErr, fmt.Errorf("reload dynamic authorizations: %w", err))
		} else {
			loadPluginDynamicSnapshot(snapshot, grants)
		}
	}
	if a.adminDynamicService != nil {
		grants, err := a.adminDynamicService.ListAdminAuthorizations(ctx)
		if err != nil {
			copyAdminDynamicSnapshot(snapshot, previous)
			reloadErr = errors.Join(reloadErr, fmt.Errorf("reload admin authorizations: %w", err))
		} else {
			loadAdminDynamicSnapshot(snapshot, grants)
		}
	}
	a.dynamic.Store(snapshot)
	return reloadErr
}

func (a *Authorizer) pollLoop(ctx context.Context, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(a.dynamicReloadEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.ReloadDynamic(ctx); err != nil {
				slog.WarnContext(ctx, "authorization: dynamic reload failed", "error", err)
			}
		}
	}
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
	if a.isManagedIdentityPrincipal(p) {
		return a.allowManagedIdentityProvider(p, provider)
	}
	if !a.IsWorkload(p) {
		_, allowed := a.ResolveAccess(p, provider)
		return allowed
	}
	_, ok := a.bindingForSubject(p, provider)
	return ok
}

func (a *Authorizer) AllowOperation(p *principal.Principal, provider, operation string) bool {
	if a.isManagedIdentityPrincipal(p) {
		return a.allowManagedIdentityProvider(p, provider) && principal.AllowsOperationPermission(p, provider, operation)
	}
	if !a.IsWorkload(p) {
		return a.AllowProvider(p, provider)
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
		return CredentialBinding{Mode: core.ConnectionModeNone}, true
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

func (a *Authorizer) ResolveAccess(p *principal.Principal, provider string) (AccessContext, bool) {
	if a == nil {
		return AccessContext{}, true
	}
	if a.isManagedIdentityPrincipal(p) {
		return AccessContext{}, a.allowManagedIdentityProvider(p, provider)
	}
	policyName := strings.TrimSpace(a.providerPolicies[provider])
	if policyName == "" {
		return AccessContext{}, true
	}
	if a.IsWorkload(p) {
		return a.ResolvePolicyAccess(p, policyName)
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
	if role, ok := a.dynamicRoleForPrincipal(provider, p); ok {
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

func (a *Authorizer) ResolvePolicyAccess(p *principal.Principal, policyName string) (AccessContext, bool) {
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

func (a *Authorizer) ResolveAdminAccess(p *principal.Principal, policyName string) (AccessContext, bool) {
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
	if role, ok := a.dynamicAdminRoleForPrincipal(p); ok {
		access.Role = role
		return access, true
	}
	if policy.DefaultAllow {
		access.Role = defaultHumanRole
		return access, true
	}
	return access, false
}

func (a *Authorizer) AllowCatalogOperation(p *principal.Principal, provider string, op catalog.CatalogOperation) bool {
	if a.isManagedIdentityPrincipal(p) {
		return a.allowManagedIdentityProvider(p, provider) && principal.AllowsOperationPermission(p, provider, op.ID)
	}
	if a.IsWorkload(p) {
		return a.AllowOperation(p, provider, op.ID)
	}
	access, allowed := a.ResolveAccess(p, provider)
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

func (a *Authorizer) dynamicRoleForPrincipal(provider string, p *principal.Principal) (string, bool) {
	if a == nil || p == nil {
		return "", false
	}
	snapshot := a.dynamic.Load()
	if snapshot == nil {
		return "", false
	}
	if p.UserID != "" {
		if byUserID := snapshot.byPluginUserID[provider]; byUserID != nil {
			if grant, ok := byUserID[p.UserID]; ok {
				return grant.Role, true
			}
		}
	}
	email := ""
	if p.Identity != nil {
		email = p.Identity.Email
	}
	if email = normalizeEmail(email); email != "" {
		if byEmail := snapshot.byPluginEmail[provider]; byEmail != nil {
			if grant, ok := byEmail[email]; ok {
				return grant.Role, true
			}
		}
	}
	return "", false
}

func (a *Authorizer) dynamicAdminRoleForPrincipal(p *principal.Principal) (string, bool) {
	if a == nil || p == nil {
		return "", false
	}
	snapshot := a.dynamic.Load()
	if snapshot == nil {
		return "", false
	}
	if p.UserID != "" {
		if grant, ok := snapshot.adminByUserID[p.UserID]; ok {
			return grant.Role, true
		}
	}
	email := ""
	if p.Identity != nil {
		email = p.Identity.Email
	}
	if email = normalizeEmail(email); email != "" {
		if grant, ok := snapshot.adminByEmail[email]; ok {
			return grant.Role, true
		}
	}
	return "", false
}

func (a *Authorizer) hasDynamicSources() bool {
	return a != nil && (a.dynamicService != nil || a.adminDynamicService != nil)
}

func loadPluginDynamicSnapshot(snapshot *dynamicSnapshot, grants []*coredata.PluginAuthorizationMembership) {
	if snapshot == nil {
		return
	}
	for _, grant := range grants {
		if grant == nil {
			continue
		}
		plugin := strings.TrimSpace(grant.Plugin)
		userID := strings.TrimSpace(grant.UserID)
		email := normalizeEmail(grant.Email)
		role := strings.TrimSpace(grant.Role)
		if plugin == "" || role == "" {
			continue
		}
		if userID != "" {
			byUserID := snapshot.byPluginUserID[plugin]
			if byUserID == nil {
				byUserID = map[string]dynamicGrant{}
				snapshot.byPluginUserID[plugin] = byUserID
			}
			byUserID[userID] = dynamicGrant{UserID: userID, Email: email, Role: role}
		}
		if email != "" {
			byEmail := snapshot.byPluginEmail[plugin]
			if byEmail == nil {
				byEmail = map[string]dynamicGrant{}
				snapshot.byPluginEmail[plugin] = byEmail
			}
			byEmail[email] = dynamicGrant{UserID: userID, Email: email, Role: role}
		}
	}
}

func loadAdminDynamicSnapshot(snapshot *dynamicSnapshot, grants []*coredata.AdminAuthorizationMembership) {
	if snapshot == nil {
		return
	}
	for _, grant := range grants {
		if grant == nil {
			continue
		}
		userID := strings.TrimSpace(grant.UserID)
		email := normalizeEmail(grant.Email)
		role := strings.TrimSpace(grant.Role)
		if role == "" {
			continue
		}
		if userID != "" {
			snapshot.adminByUserID[userID] = dynamicGrant{UserID: userID, Email: email, Role: role}
		}
		if email != "" {
			snapshot.adminByEmail[email] = dynamicGrant{UserID: userID, Email: email, Role: role}
		}
	}
}

func copyPluginDynamicSnapshot(dst, src *dynamicSnapshot) {
	if dst == nil || src == nil {
		return
	}
	for plugin, byUserID := range src.byPluginUserID {
		cloned := make(map[string]dynamicGrant, len(byUserID))
		for userID, grant := range byUserID {
			cloned[userID] = grant
		}
		dst.byPluginUserID[plugin] = cloned
	}
	for plugin, byEmail := range src.byPluginEmail {
		cloned := make(map[string]dynamicGrant, len(byEmail))
		for email, grant := range byEmail {
			cloned[email] = grant
		}
		dst.byPluginEmail[plugin] = cloned
	}
}

func copyAdminDynamicSnapshot(dst, src *dynamicSnapshot) {
	if dst == nil || src == nil {
		return
	}
	for userID, grant := range src.adminByUserID {
		dst.adminByUserID[userID] = grant
	}
	for email, grant := range src.adminByEmail {
		dst.adminByEmail[email] = grant
	}
}

func emptyDynamicSnapshot() *dynamicSnapshot {
	return &dynamicSnapshot{
		byPluginUserID: map[string]map[string]dynamicGrant{},
		byPluginEmail:  map[string]map[string]dynamicGrant{},
		adminByUserID:  map[string]dynamicGrant{},
		adminByEmail:   map[string]dynamicGrant{},
	}
}

func normalizeEmail(email string) string {
	return emailutil.Normalize(email)
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
