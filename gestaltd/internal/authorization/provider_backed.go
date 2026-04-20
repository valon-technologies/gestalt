package authorization

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/emailutil"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

type providerBackedRoleState struct {
	modelID            string
	policyStaticRoles  map[string][]string
	pluginStaticRoles  map[string][]string
	pluginDynamicRoles map[string][]string
	adminDynamicRoles  []string
}

type ProviderBackedAuthorizer struct {
	base *Authorizer

	provider core.AuthorizationProvider

	lifecycleMu sync.Mutex
	started     bool
	closed      bool
	pollCancel  context.CancelFunc
	pollDone    chan struct{}

	stateMu sync.RWMutex
	state   providerBackedRoleState
}

var _ RuntimeAuthorizer = (*ProviderBackedAuthorizer)(nil)

const providerBackedReloadInterval = 5 * time.Second

func NewProviderBacked(base *Authorizer, provider core.AuthorizationProvider) (*ProviderBackedAuthorizer, error) {
	if base == nil {
		return nil, errors.New("base authorizer is required")
	}
	if provider == nil {
		return nil, errors.New("authorization provider is required")
	}
	return &ProviderBackedAuthorizer{
		base:     base,
		provider: provider,
		state: providerBackedRoleState{
			policyStaticRoles:  map[string][]string{},
			pluginStaticRoles:  map[string][]string{},
			pluginDynamicRoles: map[string][]string{},
		},
	}, nil
}

func (a *ProviderBackedAuthorizer) Start(ctx context.Context) error {
	if a == nil {
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
	if err := a.ReloadAuthorizationState(ctx); err != nil {
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

func (a *ProviderBackedAuthorizer) Close() error {
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
	return a.base.Close()
}

func (a *ProviderBackedAuthorizer) ReloadAuthorizationState(ctx context.Context) error {
	if a == nil {
		return nil
	}

	sourceModelID, err := a.sourceModelID(ctx)
	if err != nil {
		return err
	}
	sourceExisting := map[string]*core.Relationship{}
	if sourceModelID != "" {
		sourceExisting, err = a.readAllRelationships(ctx, sourceModelID)
		if err != nil {
			return err
		}
	}
	desired, roles, err := a.buildDesiredRelationships(sourceExisting)
	if err != nil {
		return err
	}
	model, err := a.provider.WriteModel(ctx, &core.WriteModelRequest{Model: buildProviderAuthorizationModel(roles)})
	if err != nil {
		return fmt.Errorf("write authorization model: %w", err)
	}
	if model == nil || strings.TrimSpace(model.GetId()) == "" {
		return fmt.Errorf("write authorization model: missing model id")
	}
	modelID := strings.TrimSpace(model.GetId())

	targetExisting := sourceExisting
	if modelID != sourceModelID {
		targetExisting, err = a.readAllRelationships(ctx, modelID)
		if err != nil {
			return err
		}
	}

	writes, deletes := diffRelationships(targetExisting, desired)
	if len(writes) > 0 || len(deletes) > 0 {
		if err := a.provider.WriteRelationships(ctx, &core.WriteRelationshipsRequest{
			Writes:  writes,
			Deletes: deletes,
			ModelId: modelID,
		}); err != nil {
			return fmt.Errorf("sync authorization relationships: %w", err)
		}
	}

	a.stateMu.Lock()
	roles.modelID = modelID
	a.state = roles
	a.stateMu.Unlock()
	return nil
}

func (a *ProviderBackedAuthorizer) ManagedModelID(ctx context.Context) (string, error) {
	if a == nil {
		return "", fmt.Errorf("authorization provider is unavailable")
	}
	state := a.currentState()
	if modelID := strings.TrimSpace(state.modelID); modelID != "" {
		return modelID, nil
	}
	return a.sourceModelID(ctx)
}

func (a *ProviderBackedAuthorizer) pollLoop(ctx context.Context, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(providerBackedReloadInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.ReloadAuthorizationState(ctx); err != nil {
				slog.WarnContext(ctx, "authorization: provider-backed authorization state reload failed", "error", err)
			}
		}
	}
}

func (a *ProviderBackedAuthorizer) ResolveWorkloadToken(token string) (*principal.ResolvedWorkload, bool) {
	if a == nil {
		return nil, false
	}
	return a.base.ResolveWorkloadToken(token)
}

func (a *ProviderBackedAuthorizer) IsWorkload(p *principal.Principal) bool {
	if a == nil {
		return false
	}
	return a.base.IsWorkload(p)
}

func (a *ProviderBackedAuthorizer) AllowProvider(ctx context.Context, p *principal.Principal, provider string) bool {
	if a == nil {
		return true
	}
	if a.base.IsWorkload(p) || a.base.isManagedIdentityPrincipal(p) {
		return a.base.AllowProvider(ctx, p, provider)
	}
	_, allowed := a.ResolveAccess(ctx, p, provider)
	return allowed
}

func (a *ProviderBackedAuthorizer) AllowOperation(ctx context.Context, p *principal.Principal, provider, operation string) bool {
	if a == nil {
		return true
	}
	if a.base.IsWorkload(p) || a.base.isManagedIdentityPrincipal(p) {
		return a.base.AllowOperation(ctx, p, provider, operation)
	}
	return a.AllowProvider(ctx, p, provider)
}

func (a *ProviderBackedAuthorizer) Binding(p *principal.Principal, provider string) (CredentialBinding, bool) {
	if a == nil {
		return CredentialBinding{}, false
	}
	return a.base.Binding(p, provider)
}

func (a *ProviderBackedAuthorizer) ResolveAccess(ctx context.Context, p *principal.Principal, provider string) (AccessContext, bool) {
	if a == nil {
		return AccessContext{}, true
	}
	if a.base.isManagedIdentityPrincipal(p) || a.base.IsWorkload(p) {
		return a.base.ResolveAccess(ctx, p, provider)
	}

	policyName := strings.TrimSpace(a.base.providerPolicies[provider])
	if policyName == "" {
		return AccessContext{}, true
	}
	policy := a.base.policies[policyName]
	if policy == nil {
		return AccessContext{}, false
	}

	access := AccessContext{Policy: policyName}
	role, ok, err := a.resolveProviderRole(ctx, provider, p)
	if err != nil {
		a.logProviderEvalError("plugin", provider, err)
		if policy.DefaultAllow {
			access.Role = defaultHumanRole
			return access, true
		}
		return access, false
	}
	if ok {
		access.Role = role
		return access, true
	}
	if policy.DefaultAllow {
		access.Role = defaultHumanRole
		return access, true
	}
	return access, false
}

func (a *ProviderBackedAuthorizer) ResolvePolicyAccess(ctx context.Context, p *principal.Principal, policyName string) (AccessContext, bool) {
	if a == nil {
		return AccessContext{}, true
	}
	if a.base.IsWorkload(p) {
		return a.base.ResolvePolicyAccess(ctx, p, policyName)
	}
	policyName = strings.TrimSpace(policyName)
	if policyName == "" {
		return AccessContext{}, true
	}
	policy := a.base.policies[policyName]
	if policy == nil {
		return AccessContext{}, false
	}

	access := AccessContext{Policy: policyName}
	role, ok, err := a.resolvePolicyStaticRole(ctx, policyName, p)
	if err != nil {
		a.logProviderEvalError("policy", policyName, err)
		if policy.DefaultAllow {
			access.Role = defaultHumanRole
			return access, true
		}
		return access, false
	}
	if ok {
		access.Role = role
		return access, true
	}
	if policy.DefaultAllow {
		access.Role = defaultHumanRole
		return access, true
	}
	return access, false
}

func (a *ProviderBackedAuthorizer) ResolveAdminAccess(ctx context.Context, p *principal.Principal, policyName string) (AccessContext, bool) {
	if a == nil {
		return AccessContext{}, true
	}
	if a.base.IsWorkload(p) {
		return a.base.ResolveAdminAccess(ctx, p, policyName)
	}
	policyName = strings.TrimSpace(policyName)
	if policyName == "" {
		return AccessContext{}, true
	}
	policy := a.base.policies[policyName]
	if policy == nil {
		return AccessContext{}, false
	}

	access := AccessContext{Policy: policyName}
	role, ok, err := a.resolveAdminStaticRole(ctx, policyName, p)
	if err != nil {
		a.logProviderEvalError("admin_policy", policyName, err)
		if policy.DefaultAllow {
			access.Role = defaultHumanRole
			return access, true
		}
		return access, false
	}
	if ok {
		access.Role = role
		return access, true
	}
	role, ok, err = a.resolveAdminDynamicRole(ctx, p)
	if err != nil {
		a.logProviderEvalError("admin_dynamic", policyName, err)
		if policy.DefaultAllow {
			access.Role = defaultHumanRole
			return access, true
		}
		return access, false
	}
	if ok {
		access.Role = role
		return access, true
	}
	if policy.DefaultAllow {
		access.Role = defaultHumanRole
		return access, true
	}
	return access, false
}

func (a *ProviderBackedAuthorizer) AllowCatalogOperation(ctx context.Context, p *principal.Principal, provider string, op catalog.CatalogOperation) bool {
	if a == nil {
		return true
	}
	if a.base.IsWorkload(p) || a.base.isManagedIdentityPrincipal(p) {
		return a.base.AllowCatalogOperation(ctx, p, provider, op)
	}

	access, allowed := a.ResolveAccess(ctx, p, provider)
	if !allowed {
		return false
	}
	if access.Policy == "" {
		return true
	}
	if access.Policy != "" && len(op.AllowedRoles) == 0 {
		policy := a.base.policies[access.Policy]
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

func (a *ProviderBackedAuthorizer) PolicyNameForProvider(provider string) string {
	if a == nil {
		return ""
	}
	return a.base.PolicyNameForProvider(provider)
}

func (a *ProviderBackedAuthorizer) StaticRoleForPolicyIdentity(policyName, subjectID, userID, email string) (AccessContext, bool) {
	if a == nil {
		return AccessContext{}, false
	}
	return a.base.StaticRoleForPolicyIdentity(policyName, subjectID, userID, email)
}

func (a *ProviderBackedAuthorizer) StaticRoleForProviderIdentity(provider, subjectID, userID, email string) (AccessContext, bool) {
	if a == nil {
		return AccessContext{}, false
	}
	return a.base.StaticRoleForProviderIdentity(provider, subjectID, userID, email)
}

func (a *ProviderBackedAuthorizer) StaticMembersForPolicy(policyName string) ([]StaticHumanMember, bool) {
	if a == nil {
		return nil, false
	}
	return a.base.StaticMembersForPolicy(policyName)
}

func (a *ProviderBackedAuthorizer) StaticMembersForProvider(provider string) (string, []StaticHumanMember, bool) {
	if a == nil {
		return "", nil, false
	}
	return a.base.StaticMembersForProvider(provider)
}

func (a *ProviderBackedAuthorizer) resolveProviderRole(ctx context.Context, provider string, p *principal.Principal) (string, bool, error) {
	state := a.currentState()
	role, ok, err := a.resolveRoleVariants(
		ctx,
		staticSubjectRefs(p),
		resourceTypePluginStatic,
		provider,
		state.pluginStaticRoles[provider],
	)
	if err != nil || ok {
		return role, ok, err
	}
	return a.resolveRoleVariants(
		ctx,
		dynamicSubjectRefs(p),
		resourceTypePluginDynamic,
		provider,
		state.pluginDynamicRoles[provider],
	)
}

func (a *ProviderBackedAuthorizer) resolvePolicyStaticRole(ctx context.Context, policyName string, p *principal.Principal) (string, bool, error) {
	state := a.currentState()
	return a.resolveRoleVariants(
		ctx,
		staticSubjectRefs(p),
		resourceTypePolicyStatic,
		policyName,
		state.policyStaticRoles[policyName],
	)
}

func (a *ProviderBackedAuthorizer) resolveAdminStaticRole(ctx context.Context, policyName string, p *principal.Principal) (string, bool, error) {
	state := a.currentState()
	return a.resolveRoleVariants(
		ctx,
		staticSubjectRefs(p),
		resourceTypeAdminPolicyStatic,
		policyName,
		state.policyStaticRoles[policyName],
	)
}

func (a *ProviderBackedAuthorizer) resolveAdminDynamicRole(ctx context.Context, p *principal.Principal) (string, bool, error) {
	state := a.currentState()
	return a.resolveRoleVariants(
		ctx,
		dynamicSubjectRefs(p),
		resourceTypeAdminDynamic,
		resourceIDAdminDynamicGlobal,
		state.adminDynamicRoles,
	)
}

func (a *ProviderBackedAuthorizer) resolveRoleVariants(ctx context.Context, subjects []*core.SubjectRef, resourceType, resourceID string, roles []string) (string, bool, error) {
	if len(subjects) == 0 || len(roles) == 0 {
		return "", false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	a.stateMu.RLock()
	expectedModelID := strings.TrimSpace(a.state.modelID)
	a.stateMu.RUnlock()

	resource := &core.ResourceRef{Type: resourceType, Id: resourceID}
	for _, subject := range subjects {
		reqs := make([]*core.AccessEvaluationRequest, 0, len(roles))
		for _, role := range roles {
			reqs = append(reqs, &core.AccessEvaluationRequest{
				Subject:  subject,
				Action:   &core.ActionRef{Name: role},
				Resource: resource,
			})
		}
		resp, err := a.provider.EvaluateMany(ctx, &core.AccessEvaluationsRequest{Requests: reqs})
		if err != nil {
			return "", false, err
		}
		for i, decision := range resp.GetDecisions() {
			if i >= len(roles) {
				break
			}
			if decisionModelID := strings.TrimSpace(decision.GetModelId()); expectedModelID != "" && decisionModelID != "" && decisionModelID != expectedModelID {
				return "", false, fmt.Errorf("authorization provider active model changed: expected %q, got %q", expectedModelID, decisionModelID)
			}
			if decision != nil && decision.GetAllowed() {
				return roles[i], true, nil
			}
		}
	}
	return "", false, nil
}

func (a *ProviderBackedAuthorizer) sourceModelID(ctx context.Context) (string, error) {
	state := a.currentState()
	if expectedModelID := strings.TrimSpace(state.modelID); expectedModelID != "" {
		return expectedModelID, nil
	}
	active, err := a.provider.GetActiveModel(ctx)
	if err != nil {
		return "", fmt.Errorf("get active authorization model: %w", err)
	}
	if model := active.GetModel(); model != nil {
		return strings.TrimSpace(model.GetId()), nil
	}
	return "", nil
}

func (a *ProviderBackedAuthorizer) readAllRelationships(ctx context.Context, modelID string) (map[string]*core.Relationship, error) {
	out := map[string]*core.Relationship{}
	pageToken := ""
	for {
		resp, err := a.provider.ReadRelationships(ctx, &core.ReadRelationshipsRequest{
			PageSize:  500,
			PageToken: pageToken,
			ModelId:   modelID,
		})
		if err != nil {
			return nil, fmt.Errorf("read authorization relationships: %w", err)
		}
		for _, rel := range resp.GetRelationships() {
			if !managedRelationship(rel) {
				continue
			}
			out[relationshipMapKey(rel)] = rel
		}
		pageToken = strings.TrimSpace(resp.GetNextPageToken())
		if pageToken == "" {
			return out, nil
		}
	}
}

func (a *ProviderBackedAuthorizer) buildDesiredRelationships(existing map[string]*core.Relationship) (map[string]*core.Relationship, providerBackedRoleState, error) {
	desired := map[string]*core.Relationship{}
	state := providerBackedRoleState{
		policyStaticRoles:  map[string][]string{},
		pluginStaticRoles:  map[string][]string{},
		pluginDynamicRoles: map[string][]string{},
	}
	policyStaticRoles := map[string]map[string]struct{}{}
	pluginStaticRoles := map[string]map[string]struct{}{}
	pluginDynamicRoles := map[string]map[string]struct{}{}
	adminDynamicRoles := map[string]struct{}{}

	for _, rel := range existing {
		if rel == nil || rel.GetResource() == nil {
			continue
		}
		switch strings.TrimSpace(rel.GetResource().GetType()) {
		case resourceTypePluginDynamic:
			resourceID := strings.TrimSpace(rel.GetResource().GetId())
			relation := strings.TrimSpace(rel.GetRelation())
			if resourceID == "" || relation == "" {
				continue
			}
			addDesiredRelationship(desired, rel)
			ensureRoleSet(pluginDynamicRoles, resourceID)[relation] = struct{}{}
		case resourceTypeAdminDynamic:
			resourceID := strings.TrimSpace(rel.GetResource().GetId())
			relation := strings.TrimSpace(rel.GetRelation())
			if resourceID != resourceIDAdminDynamicGlobal || relation == "" {
				continue
			}
			addDesiredRelationship(desired, rel)
			adminDynamicRoles[relation] = struct{}{}
		}
	}

	providersByPolicy := map[string][]string{}
	for providerName, policyName := range a.base.providerPolicies {
		policyName = strings.TrimSpace(policyName)
		if policyName == "" {
			continue
		}
		providersByPolicy[policyName] = append(providersByPolicy[policyName], providerName)
	}

	for policyName, policy := range a.base.policies {
		if policy == nil {
			continue
		}
		policyRoleSet := ensureRoleSet(policyStaticRoles, policyName)
		for subjectID, role := range policy.RolesBySubjectID {
			role = strings.TrimSpace(role)
			if subjectID == "" || role == "" {
				continue
			}
			policyRoleSet[role] = struct{}{}
			addDesiredRelationship(desired, &core.Relationship{
				Subject:  &core.SubjectRef{Type: subjectTypeSubject, Id: subjectID},
				Relation: role,
				Resource: &core.ResourceRef{Type: resourceTypePolicyStatic, Id: policyName},
			})
			addDesiredRelationship(desired, &core.Relationship{
				Subject:  &core.SubjectRef{Type: subjectTypeSubject, Id: subjectID},
				Relation: role,
				Resource: &core.ResourceRef{Type: resourceTypeAdminPolicyStatic, Id: policyName},
			})
			for _, providerName := range providersByPolicy[policyName] {
				ensureRoleSet(pluginStaticRoles, providerName)[role] = struct{}{}
				addDesiredRelationship(desired, &core.Relationship{
					Subject:  &core.SubjectRef{Type: subjectTypeSubject, Id: subjectID},
					Relation: role,
					Resource: &core.ResourceRef{Type: resourceTypePluginStatic, Id: providerName},
				})
			}
		}
		for email, role := range policy.RolesByEmail {
			role = strings.TrimSpace(role)
			email = normalizeProviderEmail(email)
			if email == "" || role == "" {
				continue
			}
			policyRoleSet[role] = struct{}{}
			addDesiredRelationship(desired, &core.Relationship{
				Subject:  &core.SubjectRef{Type: subjectTypeEmail, Id: email},
				Relation: role,
				Resource: &core.ResourceRef{Type: resourceTypePolicyStatic, Id: policyName},
			})
			addDesiredRelationship(desired, &core.Relationship{
				Subject:  &core.SubjectRef{Type: subjectTypeEmail, Id: email},
				Relation: role,
				Resource: &core.ResourceRef{Type: resourceTypeAdminPolicyStatic, Id: policyName},
			})
			for _, providerName := range providersByPolicy[policyName] {
				ensureRoleSet(pluginStaticRoles, providerName)[role] = struct{}{}
				addDesiredRelationship(desired, &core.Relationship{
					Subject:  &core.SubjectRef{Type: subjectTypeEmail, Id: email},
					Relation: role,
					Resource: &core.ResourceRef{Type: resourceTypePluginStatic, Id: providerName},
				})
			}
		}
	}

	for name, roles := range policyStaticRoles {
		state.policyStaticRoles[name] = normalizeRoleList(roles)
	}
	for name, roles := range pluginStaticRoles {
		state.pluginStaticRoles[name] = normalizeRoleList(roles)
	}
	for name, roles := range pluginDynamicRoles {
		state.pluginDynamicRoles[name] = normalizeRoleList(roles)
	}
	state.adminDynamicRoles = normalizeRoleList(adminDynamicRoles)
	return desired, state, nil
}

func addDesiredRelationship(target map[string]*core.Relationship, rel *core.Relationship) {
	if rel == nil {
		return
	}
	target[relationshipMapKey(rel)] = rel
}

func diffRelationships(existing, desired map[string]*core.Relationship) ([]*core.Relationship, []*core.RelationshipKey) {
	writes := make([]*core.Relationship, 0)
	deletes := make([]*core.RelationshipKey, 0)
	for key, rel := range desired {
		if _, ok := existing[key]; !ok {
			writes = append(writes, rel)
		}
	}
	for key, rel := range existing {
		if _, ok := desired[key]; ok {
			continue
		}
		deletes = append(deletes, &core.RelationshipKey{
			Subject:  rel.GetSubject(),
			Relation: rel.GetRelation(),
			Resource: rel.GetResource(),
		})
	}
	sort.Slice(writes, func(i, j int) bool { return relationshipMapKey(writes[i]) < relationshipMapKey(writes[j]) })
	sort.Slice(deletes, func(i, j int) bool { return relationshipKeyMapKey(deletes[i]) < relationshipKeyMapKey(deletes[j]) })
	return writes, deletes
}

func normalizeRoleList(roles map[string]struct{}) []string {
	if len(roles) == 0 {
		return nil
	}
	out := make([]string, 0, len(roles))
	for role := range roles {
		if strings.TrimSpace(role) == "" {
			continue
		}
		out = append(out, role)
	}
	sort.Slice(out, func(i, j int) bool {
		return roleSortKey(out[i]) < roleSortKey(out[j])
	})
	return out
}

func ensureRoleSet(target map[string]map[string]struct{}, key string) map[string]struct{} {
	values := target[key]
	if values == nil {
		values = map[string]struct{}{}
		target[key] = values
	}
	return values
}

func roleSortKey(role string) string {
	switch strings.TrimSpace(role) {
	case "admin":
		return "0:admin"
	case "editor":
		return "1:editor"
	case "viewer":
		return "2:viewer"
	default:
		return "9:" + strings.TrimSpace(role)
	}
}

func staticSubjectRefs(p *principal.Principal) []*core.SubjectRef {
	if p == nil {
		return nil
	}
	out := make([]*core.SubjectRef, 0, 3)
	seen := make(map[string]struct{}, 3)
	appendSubject := func(kind, id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		key := kind + "\x00" + id
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, &core.SubjectRef{Type: kind, Id: id})
	}
	appendSubject(subjectTypeSubject, p.SubjectID)
	appendSubject(subjectTypeSubject, principal.UserSubjectID(p.UserID))
	appendSubject(subjectTypeEmail, normalizeProviderEmail(identityEmail(p)))
	return out
}

func dynamicSubjectRefs(p *principal.Principal) []*core.SubjectRef {
	if p == nil {
		return nil
	}
	out := make([]*core.SubjectRef, 0, 1)
	if userID := strings.TrimSpace(p.UserID); userID != "" {
		out = append(out, &core.SubjectRef{Type: subjectTypeUser, Id: userID})
	}
	return out
}

func identityEmail(p *principal.Principal) string {
	if p == nil || p.Identity == nil {
		return ""
	}
	return p.Identity.Email
}

func normalizeProviderEmail(email string) string {
	return emailutil.Normalize(email)
}

func relationshipMapKey(rel *core.Relationship) string {
	if rel == nil {
		return ""
	}
	return strings.Join([]string{
		rel.GetSubject().GetType(),
		rel.GetSubject().GetId(),
		rel.GetRelation(),
		rel.GetResource().GetType(),
		rel.GetResource().GetId(),
	}, "\x00")
}

func relationshipKeyMapKey(rel *core.RelationshipKey) string {
	if rel == nil {
		return ""
	}
	return strings.Join([]string{
		rel.GetSubject().GetType(),
		rel.GetSubject().GetId(),
		rel.GetRelation(),
		rel.GetResource().GetType(),
		rel.GetResource().GetId(),
	}, "\x00")
}

func (a *ProviderBackedAuthorizer) logProviderEvalError(scope, name string, err error) {
	if err == nil {
		return
	}
	slog.Warn("authorization: provider evaluation failed; denying provider-backed human access",
		"scope", scope,
		"name", name,
		"error", err,
	)
}

func (a *ProviderBackedAuthorizer) currentState() providerBackedRoleState {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	return a.state
}

func managedRelationship(rel *core.Relationship) bool {
	return IsManagedProviderRelationship(rel)
}
