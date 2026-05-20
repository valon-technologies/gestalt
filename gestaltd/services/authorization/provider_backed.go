package authorization

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"google.golang.org/protobuf/types/known/structpb"
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

	provider       core.AuthorizationProvider
	fragmentSource *coredata.AuthorizationDynamicFragmentService
	backfillMu     sync.Mutex
	backfilled     bool

	lifecycleMu sync.Mutex
	started     bool
	closed      bool
	pollCancel  context.CancelFunc
	pollDone    chan struct{}

	reloadMu sync.Mutex

	stateMu sync.RWMutex
	state   providerBackedRoleState
}

var _ RuntimeAuthorizer = (*ProviderBackedAuthorizer)(nil)

const providerBackedReloadInterval = 5 * time.Second

type ProviderBackedOption func(*ProviderBackedAuthorizer)

func WithDynamicFragmentSource(source *coredata.AuthorizationDynamicFragmentService) ProviderBackedOption {
	return func(a *ProviderBackedAuthorizer) {
		a.fragmentSource = source
	}
}

func NewProviderBacked(base *Authorizer, provider core.AuthorizationProvider, opts ...ProviderBackedOption) (*ProviderBackedAuthorizer, error) {
	if base == nil {
		return nil, errors.New("base authorizer is required")
	}
	if provider == nil {
		return nil, errors.New("authorization provider is required")
	}
	a := &ProviderBackedAuthorizer{
		base:     base,
		provider: provider,
		state: providerBackedRoleState{
			policyStaticRoles:  map[string][]string{},
			pluginStaticRoles:  map[string][]string{},
			pluginDynamicRoles: map[string][]string{},
		},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(a)
		}
	}
	return a, nil
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

	a.reloadMu.Lock()
	defer a.reloadMu.Unlock()
	_, err := a.reloadAuthorizationStateLocked(ctx, nil)
	return err
}

func (a *ProviderBackedAuthorizer) EnsureManagedDynamicRole(ctx context.Context, resource *core.ResourceRef, role string) (string, error) {
	if a == nil {
		return "", fmt.Errorf("authorization provider is unavailable")
	}
	resourceType := strings.TrimSpace(resource.GetType())
	resourceID := strings.TrimSpace(resource.GetId())
	role = strings.TrimSpace(role)
	if resourceType == "" || resourceID == "" {
		return "", fmt.Errorf("authorization resource is required")
	}
	if role == "" {
		return "", fmt.Errorf("authorization role is required")
	}

	a.reloadMu.Lock()
	defer a.reloadMu.Unlock()
	return a.reloadAuthorizationStateLocked(ctx, func(roles *providerBackedRoleState) error {
		switch resourceType {
		case resourceTypePluginDynamic:
			if roles.pluginDynamicRoles == nil {
				roles.pluginDynamicRoles = map[string][]string{}
			}
			roles.pluginDynamicRoles[resourceID] = roleListWith(roles.pluginDynamicRoles[resourceID], role)
		case resourceTypeAdminDynamic:
			if resourceID != resourceIDAdminDynamicGlobal {
				return fmt.Errorf("unsupported admin dynamic resource %q", resourceID)
			}
			roles.adminDynamicRoles = roleListWith(roles.adminDynamicRoles, role)
		default:
			return fmt.Errorf("unsupported managed dynamic resource type %q", resourceType)
		}
		return nil
	})
}

func (a *ProviderBackedAuthorizer) reloadAuthorizationStateLocked(ctx context.Context, mutateRoles func(*providerBackedRoleState) error) (string, error) {
	sourceModelID, err := a.sourceModelID(ctx)
	if err != nil {
		return "", err
	}
	sourceExisting := map[string]*core.Relationship{}
	if sourceModelID != "" {
		sourceExisting, err = a.readAllRelationships(ctx, sourceModelID)
		if err != nil {
			return "", err
		}
	}
	fragmentRelationships, fragmentModelFragments, err := a.dynamicFragmentState(ctx, sourceExisting)
	if err != nil {
		return "", err
	}
	desired, roles, err := a.buildDesiredRelationships(sourceExisting, fragmentRelationships)
	if err != nil {
		return "", err
	}
	if mutateRoles != nil {
		if err := mutateRoles(&roles); err != nil {
			return "", err
		}
	}
	model, err := a.provider.WriteModel(ctx, &core.WriteModelRequest{Model: a.buildComposedAuthorizationModel(roles, fragmentModelFragments)})
	if err != nil {
		return "", fmt.Errorf("write authorization model: %w", err)
	}
	if model == nil || strings.TrimSpace(model.GetId()) == "" {
		return "", fmt.Errorf("write authorization model: missing model id")
	}
	modelID := strings.TrimSpace(model.GetId())

	targetExisting := sourceExisting
	if modelID != sourceModelID {
		targetExisting, err = a.readAllRelationships(ctx, modelID)
		if err != nil {
			return "", err
		}
	}

	writes, deletes := diffRelationships(targetExisting, desired)
	if len(writes) > 0 || len(deletes) > 0 {
		if err := a.provider.WriteRelationships(ctx, &core.WriteRelationshipsRequest{
			Writes:  writes,
			Deletes: deletes,
			ModelId: modelID,
		}); err != nil {
			return "", fmt.Errorf("sync authorization relationships: %w", err)
		}
	}

	a.stateMu.Lock()
	roles.modelID = modelID
	a.state = roles
	a.stateMu.Unlock()
	return modelID, nil
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

func (a *ProviderBackedAuthorizer) AllowProvider(ctx context.Context, p *principal.Principal, provider string) bool {
	if a == nil {
		return true
	}
	if principal.IsSystemPrincipal(p) {
		return a.base.AllowProvider(ctx, p, provider)
	}
	_, allowed := a.ResolveAccess(ctx, p, provider)
	return allowed
}

func (a *ProviderBackedAuthorizer) AllowOperation(ctx context.Context, p *principal.Principal, provider, operation string) bool {
	if a == nil {
		return true
	}
	if principal.IsSystemPrincipal(p) {
		return a.base.AllowOperation(ctx, p, provider, operation)
	}
	return a.AllowProvider(ctx, p, provider)
}

func (a *ProviderBackedAuthorizer) AllowExternalIdentityAssumption(ctx context.Context, p *principal.Principal, identity *core.ExternalIdentityRef) bool {
	if a == nil {
		return false
	}
	identity = core.NormalizeExternalIdentityRef(identity)
	if identity == nil {
		return false
	}
	resourceID := core.ExternalIdentityResourceID(identity)
	if resourceID == "" {
		return false
	}
	_, allowed, err := a.resolveRoleVariants(ctx, externalIdentityAssumptionSubjectRefs(p), resourceTypeExternalIdentity, resourceID, []string{relationExternalIdentityAssume})
	if err != nil {
		a.logProviderEvalError("external_identity", resourceID, err)
		return false
	}
	return allowed
}

func (a *ProviderBackedAuthorizer) ResolveAccess(ctx context.Context, p *principal.Principal, provider string) (AccessContext, bool) {
	if a == nil {
		return AccessContext{}, true
	}
	if principal.IsSystemPrincipal(p) {
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
		if policy.DefaultAllow && defaultAllowAppliesToPrincipal(p) {
			access.Role = defaultSubjectRole
			return access, true
		}
		return access, false
	}
	if ok {
		access.Role = role
		return access, true
	}
	if policy.DefaultAllow && defaultAllowAppliesToPrincipal(p) {
		access.Role = defaultSubjectRole
		return access, true
	}
	return access, false
}

func (a *ProviderBackedAuthorizer) ResolvePolicyAccess(ctx context.Context, p *principal.Principal, policyName string) (AccessContext, bool) {
	if a == nil {
		return AccessContext{}, true
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
		if policy.DefaultAllow && defaultAllowAppliesToPrincipal(p) {
			access.Role = defaultSubjectRole
			return access, true
		}
		return access, false
	}
	if ok {
		access.Role = role
		return access, true
	}
	if policy.DefaultAllow && defaultAllowAppliesToPrincipal(p) {
		access.Role = defaultSubjectRole
		return access, true
	}
	return access, false
}

func (a *ProviderBackedAuthorizer) ResolveAdminAccess(ctx context.Context, p *principal.Principal, policyName string) (AccessContext, bool) {
	if a == nil {
		return AccessContext{}, true
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
		if policy.DefaultAllow && defaultAllowAppliesToPrincipal(p) {
			access.Role = defaultSubjectRole
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
		if policy.DefaultAllow && defaultAllowAppliesToPrincipal(p) {
			access.Role = defaultSubjectRole
			return access, true
		}
		return access, false
	}
	if ok {
		access.Role = role
		return access, true
	}
	if policy.DefaultAllow && defaultAllowAppliesToPrincipal(p) {
		access.Role = defaultSubjectRole
		return access, true
	}
	return access, false
}

func (a *ProviderBackedAuthorizer) AllowCatalogOperation(ctx context.Context, p *principal.Principal, provider string, op catalog.CatalogOperation) bool {
	if a == nil {
		return true
	}
	if principal.IsSystemPrincipal(p) {
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

func (a *ProviderBackedAuthorizer) StaticRoleForPolicyIdentity(policyName, subjectID string) (AccessContext, bool) {
	if a == nil {
		return AccessContext{}, false
	}
	return a.base.StaticRoleForPolicyIdentity(policyName, subjectID)
}

func (a *ProviderBackedAuthorizer) StaticRoleForProviderIdentity(provider, subjectID string) (AccessContext, bool) {
	if a == nil {
		return AccessContext{}, false
	}
	return a.base.StaticRoleForProviderIdentity(provider, subjectID)
}

func (a *ProviderBackedAuthorizer) StaticMembersForPolicy(policyName string) ([]StaticSubjectMember, bool) {
	if a == nil {
		return nil, false
	}
	return a.base.StaticMembersForPolicy(policyName)
}

func (a *ProviderBackedAuthorizer) StaticMembersForProvider(provider string) (string, []StaticSubjectMember, bool) {
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
		for _, role := range roles {
			resp, err := a.provider.ReadRelationships(ctx, &core.ReadRelationshipsRequest{
				Subject:  subject,
				Relation: role,
				Resource: resource,
				PageSize: 1,
				ModelId:  expectedModelID,
			})
			if err != nil {
				return "", false, err
			}
			if respModelID := strings.TrimSpace(resp.GetModelId()); expectedModelID != "" && respModelID != "" && respModelID != expectedModelID {
				return "", false, fmt.Errorf("authorization provider active model changed: expected %q, got %q", expectedModelID, respModelID)
			}
			if len(resp.GetRelationships()) > 0 {
				return role, true, nil
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

func (a *ProviderBackedAuthorizer) buildDesiredRelationships(existing map[string]*core.Relationship, fragmentRelationships []*core.Relationship) (map[string]*core.Relationship, providerBackedRoleState, error) {
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
			if a.fragmentSource != nil {
				continue
			}
			resourceID := strings.TrimSpace(rel.GetResource().GetId())
			relation := strings.TrimSpace(rel.GetRelation())
			if resourceID == "" || relation == "" {
				continue
			}
			addDesiredRelationship(desired, synthesizedRelationship(rel, "provider_dynamic", "legacy_plugin_dynamic", "plugin", resourceID))
			ensureRoleSet(pluginDynamicRoles, resourceID)[relation] = struct{}{}
		case resourceTypeAdminDynamic:
			if a.fragmentSource != nil {
				continue
			}
			resourceID := strings.TrimSpace(rel.GetResource().GetId())
			relation := strings.TrimSpace(rel.GetRelation())
			if resourceID != resourceIDAdminDynamicGlobal || relation == "" {
				continue
			}
			addDesiredRelationship(desired, synthesizedRelationship(rel, "provider_dynamic", "legacy_admin_dynamic", "global", resourceIDAdminDynamicGlobal))
			adminDynamicRoles[relation] = struct{}{}
		case resourceTypeExternalIdentity:
			relation := strings.TrimSpace(rel.GetRelation())
			subject := relationshipTargetSubject(rel)
			if relation != relationExternalIdentityAssume || subject == nil || strings.TrimSpace(subject.GetId()) == "" {
				continue
			}
			switch strings.TrimSpace(subject.GetType()) {
			case subjectTypeSubject, subjectTypeUser:
			default:
				continue
			}
			addDesiredRelationship(desired, synthesizedRelationship(rel, "provider_existing", "external_identity", "", ""))
		case resourceTypeManagedSubject:
			relation := strings.TrimSpace(rel.GetRelation())
			subject := relationshipTargetSubject(rel)
			if !managedSubjectManagementRelation(relation) || subject == nil || strings.TrimSpace(subject.GetId()) == "" {
				continue
			}
			if strings.TrimSpace(subject.GetType()) != subjectTypeSubject {
				continue
			}
			addDesiredRelationship(desired, synthesizedRelationship(rel, "provider_existing", "managed_subject", "", ""))
		case resourceTypeAgentSession:
			if !validManagedAgentSessionRelationship(rel) {
				continue
			}
			addDesiredRelationship(desired, synthesizedRelationship(rel, "provider_existing", "agent_session", "", ""))
		case resourceTypeEveryone, resourceTypeTeam, resourceTypeSlackChannel:
			if !validManagedMembershipRelationship(rel) {
				continue
			}
			addDesiredRelationship(desired, synthesizedRelationship(rel, "provider_existing", "membership", "", ""))
		}
	}

	for _, rel := range fragmentRelationships {
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
			addDesiredRelationship(desired, synthesizedRelationship(rel, "dynamic_fragment", "plugin/"+resourceID, "plugin", resourceID))
			ensureRoleSet(pluginDynamicRoles, resourceID)[relation] = struct{}{}
		case resourceTypeAdminDynamic:
			resourceID := strings.TrimSpace(rel.GetResource().GetId())
			relation := strings.TrimSpace(rel.GetRelation())
			if resourceID != resourceIDAdminDynamicGlobal || relation == "" {
				continue
			}
			addDesiredRelationship(desired, synthesizedRelationship(rel, "dynamic_fragment", "global", "global", resourceIDAdminDynamicGlobal))
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
			addDesiredRelationship(desired, synthesizedRelationship(&core.Relationship{
				Subject:  &core.SubjectRef{Type: subjectTypeSubject, Id: subjectID},
				Relation: role,
				Resource: &core.ResourceRef{Type: resourceTypePolicyStatic, Id: policyName},
			}, "static_config", "authorization.policies."+policyName, "policy", policyName))
			addDesiredRelationship(desired, synthesizedRelationship(&core.Relationship{
				Subject:  &core.SubjectRef{Type: subjectTypeSubject, Id: subjectID},
				Relation: role,
				Resource: &core.ResourceRef{Type: resourceTypeAdminPolicyStatic, Id: policyName},
			}, "static_config", "authorization.policies."+policyName, "policy", policyName))
			for _, providerName := range providersByPolicy[policyName] {
				ensureRoleSet(pluginStaticRoles, providerName)[role] = struct{}{}
				addDesiredRelationship(desired, synthesizedRelationship(&core.Relationship{
					Subject:  &core.SubjectRef{Type: subjectTypeSubject, Id: subjectID},
					Relation: role,
					Resource: &core.ResourceRef{Type: resourceTypePluginStatic, Id: providerName},
				}, "static_config", "authorization.policies."+policyName, "plugin", providerName))
			}
		}
	}

	for i, rel := range a.base.relationships {
		if rel == nil {
			continue
		}
		addDesiredRelationship(desired, synthesizedRelationship(rel, "static_config", fmt.Sprintf("authorization.relationships[%d]", i), "", ""))
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

func (a *ProviderBackedAuthorizer) dynamicFragmentState(ctx context.Context, existing map[string]*core.Relationship) ([]*core.Relationship, []*core.AuthorizationModelResourceType, error) {
	if a.fragmentSource == nil {
		return nil, nil, nil
	}
	if err := a.ensureDynamicFragmentsBackfilled(ctx, existing); err != nil {
		return nil, nil, err
	}
	fragments, err := a.fragmentSource.ListFragments(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list dynamic authorization fragments: %w", err)
	}
	var relationships []*core.Relationship
	var modelFragments []*core.AuthorizationModelResourceType
	for _, fragment := range fragments {
		if fragment == nil || strings.TrimSpace(fragment.Status) != coredata.AuthorizationFragmentStatusActive {
			continue
		}
		for resourceTypeName, raw := range fragment.ResourceTypes {
			resourceType, err := dynamicFragmentModelResourceType(resourceTypeName, raw)
			if err != nil {
				return nil, nil, fmt.Errorf("dynamic authorization fragment %q resource type %q: %w", fragment.ID, resourceTypeName, err)
			}
			modelFragments = append(modelFragments, resourceType)
		}
		for _, relationship := range fragment.Relationships {
			rel := relationshipFromDynamicFragment(relationship)
			if rel == nil {
				continue
			}
			relationships = append(relationships, rel)
		}
	}
	return relationships, modelFragments, nil
}

func (a *ProviderBackedAuthorizer) ensureDynamicFragmentsBackfilled(ctx context.Context, existing map[string]*core.Relationship) error {
	a.backfillMu.Lock()
	defer a.backfillMu.Unlock()
	if a.backfilled {
		return nil
	}
	if err := a.backfillDynamicFragments(ctx, existing); err != nil {
		return err
	}
	a.backfilled = true
	return nil
}

func (a *ProviderBackedAuthorizer) backfillDynamicFragments(ctx context.Context, existing map[string]*core.Relationship) error {
	for _, rel := range existing {
		fragmentRelationship, owner, ok := dynamicFragmentRelationshipFromProvider(rel)
		if !ok {
			continue
		}
		if _, err := a.fragmentSource.UpsertRelationship(ctx, owner, fragmentRelationship, coredata.AuthorizationDynamicFragmentAuditMetadata{Reason: "provider_backed_reload_backfill"}); err != nil {
			return fmt.Errorf("backfill dynamic authorization fragment: %w", err)
		}
	}
	return nil
}

func relationshipFromDynamicFragment(relationship coredata.AuthorizationDynamicFragmentRelationship) *core.Relationship {
	subject := &core.SubjectRef{Type: strings.TrimSpace(relationship.Subject.Type), Id: strings.TrimSpace(relationship.Subject.ID)}
	resource := &core.ResourceRef{Type: strings.TrimSpace(relationship.Resource.Type), Id: strings.TrimSpace(relationship.Resource.ID)}
	if subject.GetType() == "" || subject.GetId() == "" || resource.GetType() == "" || resource.GetId() == "" || strings.TrimSpace(relationship.Relation) == "" {
		return nil
	}
	rel := &core.Relationship{
		Subject:  subject,
		Relation: strings.TrimSpace(relationship.Relation),
		Resource: resource,
		Target:   dynamicFragmentRelationshipTarget(relationship.Target),
	}
	if len(relationship.Properties) > 0 {
		properties := make(map[string]any, len(relationship.Properties))
		for key, value := range relationship.Properties {
			properties[key] = value
		}
		rel.Properties, _ = structpb.NewStruct(properties)
	}
	return rel
}

func dynamicFragmentRelationshipFromProvider(rel *core.Relationship) (coredata.AuthorizationDynamicFragmentRelationship, coredata.AuthorizationFragmentOwner, bool) {
	if rel == nil || relationshipTargetSubject(rel) == nil || rel.GetResource() == nil {
		return coredata.AuthorizationDynamicFragmentRelationship{}, coredata.AuthorizationFragmentOwner{}, false
	}
	resource := rel.GetResource()
	switch strings.TrimSpace(resource.GetType()) {
	case resourceTypePluginDynamic:
		plugin := strings.TrimSpace(resource.GetId())
		if plugin == "" {
			return coredata.AuthorizationDynamicFragmentRelationship{}, coredata.AuthorizationFragmentOwner{}, false
		}
		return dynamicFragmentRelationshipFromCore(rel), coredata.AuthorizationPluginFragmentOwner(plugin), true
	case resourceTypeAdminDynamic:
		if strings.TrimSpace(resource.GetId()) != resourceIDAdminDynamicGlobal {
			return coredata.AuthorizationDynamicFragmentRelationship{}, coredata.AuthorizationFragmentOwner{}, false
		}
		return dynamicFragmentRelationshipFromCore(rel), coredata.AuthorizationGlobalFragmentOwner(), true
	default:
		return coredata.AuthorizationDynamicFragmentRelationship{}, coredata.AuthorizationFragmentOwner{}, false
	}
}

func dynamicFragmentRelationshipFromCore(rel *core.Relationship) coredata.AuthorizationDynamicFragmentRelationship {
	subject := relationshipTargetSubject(rel)
	relationship := coredata.AuthorizationDynamicFragmentRelationship{
		Subject: coredata.AuthorizationDynamicFragmentSubject{
			Type: strings.TrimSpace(subject.GetType()),
			ID:   strings.TrimSpace(subject.GetId()),
		},
		Relation: strings.TrimSpace(rel.GetRelation()),
		Resource: coredata.AuthorizationDynamicFragmentResource{
			Type: strings.TrimSpace(rel.GetResource().GetType()),
			ID:   strings.TrimSpace(rel.GetResource().GetId()),
		},
		Target: dynamicFragmentTargetFromCore(rel.GetTarget()),
	}
	if len(rel.GetProperties().GetFields()) > 0 {
		relationship.Properties = map[string]string{}
		for key, value := range rel.GetProperties().GetFields() {
			if stringValue := value.GetStringValue(); stringValue != "" {
				relationship.Properties[key] = stringValue
			}
		}
		if len(relationship.Properties) == 0 {
			relationship.Properties = nil
		}
	}
	return relationship
}

func dynamicFragmentTargetFromCore(target *core.RelationshipTargetRef) coredata.AuthorizationDynamicFragmentTarget {
	if target == nil {
		return coredata.AuthorizationDynamicFragmentTarget{}
	}
	if subject := target.GetSubject(); subject != nil {
		return coredata.AuthorizationDynamicFragmentTarget{Subject: &coredata.AuthorizationDynamicFragmentSubject{
			Type: strings.TrimSpace(subject.GetType()),
			ID:   strings.TrimSpace(subject.GetId()),
		}}
	}
	if resource := target.GetResource(); resource != nil {
		return coredata.AuthorizationDynamicFragmentTarget{Resource: &coredata.AuthorizationDynamicFragmentResource{
			Type: strings.TrimSpace(resource.GetType()),
			ID:   strings.TrimSpace(resource.GetId()),
		}}
	}
	if subjectSet := target.GetSubjectSet(); subjectSet != nil {
		resource := subjectSet.GetResource()
		return coredata.AuthorizationDynamicFragmentTarget{SubjectSet: &coredata.AuthorizationDynamicFragmentSubjectSet{
			Resource: coredata.AuthorizationDynamicFragmentResource{
				Type: strings.TrimSpace(resource.GetType()),
				ID:   strings.TrimSpace(resource.GetId()),
			},
			Relation: strings.TrimSpace(subjectSet.GetRelation()),
		}}
	}
	return coredata.AuthorizationDynamicFragmentTarget{}
}

func (a *ProviderBackedAuthorizer) buildComposedAuthorizationModel(roles providerBackedRoleState, dynamicFragments []*core.AuthorizationModelResourceType) *core.AuthorizationModel {
	model := buildProviderAuthorizationModel(roles)
	seen := make(map[string]struct{}, len(model.GetResourceTypes())+len(a.base.modelFragments)+len(dynamicFragments))
	for _, resourceType := range model.GetResourceTypes() {
		seen[strings.TrimSpace(resourceType.GetName())] = struct{}{}
	}
	appendResourceTypes := func(resourceTypes []*core.AuthorizationModelResourceType) {
		for _, resourceType := range resourceTypes {
			if resourceType == nil || strings.TrimSpace(resourceType.GetName()) == "" {
				continue
			}
			if _, ok := seen[strings.TrimSpace(resourceType.GetName())]; ok {
				continue
			}
			model.ResourceTypes = append(model.ResourceTypes, cloneModelResourceTypes([]*core.AuthorizationModelResourceType{resourceType})...)
			seen[strings.TrimSpace(resourceType.GetName())] = struct{}{}
		}
	}
	appendResourceTypes(a.base.modelFragments)
	appendResourceTypes(dynamicFragments)
	sort.Slice(model.ResourceTypes, func(i, j int) bool {
		return strings.Compare(model.ResourceTypes[i].GetName(), model.ResourceTypes[j].GetName()) < 0
	})
	return model
}

type dynamicFragmentResourceTypeDef struct {
	Relations map[string]dynamicFragmentRelationDef `json:"relations,omitempty"`
	Actions   map[string]dynamicFragmentActionDef   `json:"actions,omitempty"`
}

type dynamicFragmentRelationDef struct {
	SubjectTypes   []string                          `json:"subjectTypes,omitempty"`
	AllowedTargets []dynamicFragmentAllowedTargetDef `json:"allowedTargets,omitempty"`
	Rewrite        *dynamicFragmentRewriteDef        `json:"rewrite,omitempty"`
}

type dynamicFragmentActionDef struct {
	Relations []string                   `json:"relations,omitempty"`
	Rewrite   *dynamicFragmentRewriteDef `json:"rewrite,omitempty"`
}

type dynamicFragmentAllowedTargetDef struct {
	SubjectType  string                            `json:"subjectType,omitempty"`
	ResourceType string                            `json:"resourceType,omitempty"`
	SubjectSet   *dynamicFragmentSubjectSetTypeDef `json:"subjectSet,omitempty"`
}

type dynamicFragmentSubjectSetTypeDef struct {
	ResourceType string `json:"resourceType,omitempty"`
	Relation     string `json:"relation,omitempty"`
}

type dynamicFragmentRewriteDef struct {
	This            *struct{}                      `json:"this,omitempty"`
	ComputedUserset *dynamicFragmentRelationRef    `json:"computedUserset,omitempty"`
	TupleToUserset  *dynamicFragmentTupleToUserset `json:"tupleToUserset,omitempty"`
	Union           []dynamicFragmentRewriteDef    `json:"union,omitempty"`
}

type dynamicFragmentRelationRef struct {
	Relation string `json:"relation,omitempty"`
}

type dynamicFragmentTupleToUserset struct {
	TuplesetRelation string `json:"tuplesetRelation,omitempty"`
	ComputedRelation string `json:"computedRelation,omitempty"`
}

func dynamicFragmentModelResourceType(name string, raw json.RawMessage) (*core.AuthorizationModelResourceType, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	var def dynamicFragmentResourceTypeDef
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &def); err != nil {
			return nil, err
		}
	}
	resourceType := &core.AuthorizationModelResourceType{Name: name}
	for relationName, relation := range def.Relations {
		resourceType.Relations = append(resourceType.Relations, &core.AuthorizationModelRelation{
			Name:           relationName,
			SubjectTypes:   append([]string(nil), relation.SubjectTypes...),
			AllowedTargets: dynamicFragmentAllowedTargets(relation.AllowedTargets),
			Rewrite:        dynamicFragmentRewrite(relation.Rewrite),
		})
	}
	for actionName, action := range def.Actions {
		resourceType.Actions = append(resourceType.Actions, &core.AuthorizationModelAction{
			Name:      actionName,
			Relations: append([]string(nil), action.Relations...),
			Rewrite:   dynamicFragmentRewrite(action.Rewrite),
		})
	}
	return resourceType, nil
}

func dynamicFragmentAllowedTargets(targets []dynamicFragmentAllowedTargetDef) []*core.AuthorizationModelAllowedTarget {
	out := make([]*core.AuthorizationModelAllowedTarget, 0, len(targets))
	for _, target := range targets {
		switch {
		case strings.TrimSpace(target.SubjectType) != "":
			out = append(out, &core.AuthorizationModelAllowedTarget{
				Kind: &proto.AuthorizationModelAllowedTarget_SubjectType{SubjectType: strings.TrimSpace(target.SubjectType)},
			})
		case strings.TrimSpace(target.ResourceType) != "":
			out = append(out, &core.AuthorizationModelAllowedTarget{
				Kind: &proto.AuthorizationModelAllowedTarget_ResourceType{ResourceType: strings.TrimSpace(target.ResourceType)},
			})
		case target.SubjectSet != nil:
			out = append(out, &core.AuthorizationModelAllowedTarget{
				Kind: &proto.AuthorizationModelAllowedTarget_SubjectSet{SubjectSet: &core.AuthorizationModelSubjectSetTarget{
					ResourceType: strings.TrimSpace(target.SubjectSet.ResourceType),
					Relation:     strings.TrimSpace(target.SubjectSet.Relation),
				}},
			})
		}
	}
	return out
}

func dynamicFragmentRewrite(def *dynamicFragmentRewriteDef) *core.AuthorizationModelRewrite {
	if def == nil {
		return nil
	}
	switch {
	case def.This != nil:
		return &core.AuthorizationModelRewrite{Kind: &proto.AuthorizationModelRewrite_This{This: &core.AuthorizationModelRewriteThis{}}}
	case def.ComputedUserset != nil:
		return &core.AuthorizationModelRewrite{Kind: &proto.AuthorizationModelRewrite_ComputedUserset{ComputedUserset: &core.AuthorizationModelComputedUserset{Relation: strings.TrimSpace(def.ComputedUserset.Relation)}}}
	case def.TupleToUserset != nil:
		return &core.AuthorizationModelRewrite{Kind: &proto.AuthorizationModelRewrite_TupleToUserset{TupleToUserset: &core.AuthorizationModelTupleToUserset{
			TuplesetRelation: strings.TrimSpace(def.TupleToUserset.TuplesetRelation),
			ComputedRelation: strings.TrimSpace(def.TupleToUserset.ComputedRelation),
		}}}
	case len(def.Union) > 0:
		children := make([]*core.AuthorizationModelRewrite, 0, len(def.Union))
		for i := range def.Union {
			children = append(children, dynamicFragmentRewrite(&def.Union[i]))
		}
		return &core.AuthorizationModelRewrite{Kind: &proto.AuthorizationModelRewrite_Union{Union: &core.AuthorizationModelRewriteUnion{Children: children}}}
	default:
		return nil
	}
}

func dynamicFragmentRelationshipTarget(target coredata.AuthorizationDynamicFragmentTarget) *core.RelationshipTargetRef {
	switch {
	case target.Subject != nil:
		return &core.RelationshipTargetRef{Kind: &proto.RelationshipTarget_Subject{Subject: &core.SubjectRef{Type: strings.TrimSpace(target.Subject.Type), Id: strings.TrimSpace(target.Subject.ID)}}}
	case target.Resource != nil:
		return &core.RelationshipTargetRef{Kind: &proto.RelationshipTarget_Resource{Resource: &core.ResourceRef{Type: strings.TrimSpace(target.Resource.Type), Id: strings.TrimSpace(target.Resource.ID)}}}
	case target.SubjectSet != nil:
		return &core.RelationshipTargetRef{Kind: &proto.RelationshipTarget_SubjectSet{SubjectSet: &core.SubjectSetRef{
			Resource: &core.ResourceRef{Type: strings.TrimSpace(target.SubjectSet.Resource.Type), Id: strings.TrimSpace(target.SubjectSet.Resource.ID)},
			Relation: strings.TrimSpace(target.SubjectSet.Relation),
		}}}
	default:
		return nil
	}
}

func synthesizedRelationship(rel *core.Relationship, sourceLayer, sourceID, ownerKind, ownerID string) *core.Relationship {
	if rel == nil {
		return nil
	}
	out := cloneRelationships([]*core.Relationship{rel})[0]
	properties := map[string]any{}
	for key, value := range rel.GetProperties().GetFields() {
		properties[key] = value.AsInterface()
	}
	properties["gestalt.authz.synthesized"] = true
	if strings.TrimSpace(sourceLayer) != "" {
		properties["gestalt.authz.source_layer"] = strings.TrimSpace(sourceLayer)
	}
	if strings.TrimSpace(sourceID) != "" {
		properties["gestalt.authz.source_id"] = strings.TrimSpace(sourceID)
	}
	if strings.TrimSpace(ownerKind) != "" {
		properties["gestalt.authz.owner_kind"] = strings.TrimSpace(ownerKind)
	}
	if strings.TrimSpace(ownerID) != "" {
		properties["gestalt.authz.owner_id"] = strings.TrimSpace(ownerID)
	}
	props, err := structpb.NewStruct(properties)
	if err == nil {
		out.Properties = props
	}
	return out
}

func synthesizedProviderRelationship(rel *core.Relationship) bool {
	if rel == nil {
		return false
	}
	return rel.GetProperties().GetFields()["gestalt.authz.synthesized"].GetBoolValue()
}

func managedRelationship(rel *core.Relationship) bool {
	return IsManagedProviderRelationship(rel) || synthesizedProviderRelationship(rel)
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
		deletes = append(deletes, relationshipKeyForRelationship(rel))
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

func roleListWith(roles []string, role string) []string {
	role = strings.TrimSpace(role)
	if role == "" {
		return append([]string(nil), roles...)
	}
	roleSet := make(map[string]struct{}, len(roles)+1)
	for _, existing := range roles {
		existing = strings.TrimSpace(existing)
		if existing == "" {
			continue
		}
		roleSet[existing] = struct{}{}
	}
	roleSet[role] = struct{}{}
	return normalizeRoleList(roleSet)
}

func ensureRoleSet(target map[string]map[string]struct{}, key string) map[string]struct{} {
	values := target[key]
	if values == nil {
		values = map[string]struct{}{}
		target[key] = values
	}
	return values
}

func managedSubjectManagementRelation(relation string) bool {
	switch strings.TrimSpace(relation) {
	case relationManagedSubjectViewer, relationManagedSubjectEditor, relationManagedSubjectAdmin:
		return true
	default:
		return false
	}
}

func validManagedMembershipRelationship(rel *core.Relationship) bool {
	if rel == nil || rel.GetResource() == nil {
		return false
	}
	if strings.TrimSpace(rel.GetRelation()) != relationMember || strings.TrimSpace(rel.GetResource().GetId()) == "" {
		return false
	}
	if strings.TrimSpace(rel.GetResource().GetType()) == resourceTypeEveryone && strings.TrimSpace(rel.GetResource().GetId()) != resourceIDEveryoneGlobal {
		return false
	}
	subject := relationshipTargetSubject(rel)
	return subject != nil &&
		strings.TrimSpace(subject.GetType()) == subjectTypeSubject &&
		strings.TrimSpace(subject.GetId()) != ""
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
	p = principal.Canonicalized(p)
	if p == nil {
		return nil
	}
	out := make([]*core.SubjectRef, 0, 1)
	seen := make(map[string]struct{}, 1)
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
	return out
}

func dynamicSubjectRefs(p *principal.Principal) []*core.SubjectRef {
	p = principal.Canonicalized(p)
	if p == nil {
		return nil
	}
	subjectID := strings.TrimSpace(p.SubjectID)
	if subjectID == "" || principal.IsSystemSubjectID(subjectID) {
		return nil
	}
	return []*core.SubjectRef{{Type: subjectTypeSubject, Id: subjectID}}
}

func externalIdentityAssumptionSubjectRefs(p *principal.Principal) []*core.SubjectRef {
	p = principal.Canonicalized(p)
	if p == nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]*core.SubjectRef, 0, 2)
	appendRef := func(typ, id string) {
		typ = strings.TrimSpace(typ)
		id = strings.TrimSpace(id)
		if typ == "" || id == "" {
			return
		}
		key := typ + "\x00" + id
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, &core.SubjectRef{Type: typ, Id: id})
	}
	for _, ref := range dynamicSubjectRefs(p) {
		if ref != nil {
			appendRef(ref.GetType(), ref.GetId())
		}
	}
	userSubjectID := strings.TrimSpace(p.SubjectID)
	if userSubjectID == "" && strings.TrimSpace(p.UserID) != "" {
		userSubjectID = principal.UserSubjectID(strings.TrimSpace(p.UserID))
	}
	if strings.HasPrefix(userSubjectID, string(principal.KindUser)+":") {
		appendRef(subjectTypeUser, userSubjectID)
	}
	return out
}

func relationshipMapKey(rel *core.Relationship) string {
	if rel == nil {
		return ""
	}
	return strings.Join([]string{
		RelationshipTargetMapKey(rel.GetTarget(), rel.GetSubject()),
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
		RelationshipTargetMapKey(rel.GetTarget(), rel.GetSubject()),
		rel.GetRelation(),
		rel.GetResource().GetType(),
		rel.GetResource().GetId(),
	}, "\x00")
}

func relationshipKeyForRelationship(rel *core.Relationship) *core.RelationshipKey {
	if rel == nil {
		return nil
	}
	key := &core.RelationshipKey{
		Subject:  rel.GetSubject(),
		Relation: rel.GetRelation(),
		Resource: rel.GetResource(),
		Target:   rel.GetTarget(),
	}
	if key.GetTarget() == nil && key.GetSubject() != nil {
		key.Target = &core.RelationshipTargetRef{
			Kind: &proto.RelationshipTarget_Subject{Subject: key.GetSubject()},
		}
	}
	return key
}

// RelationshipTargetMapKey returns the canonical key for a target-aware
// relationship target. Legacy subject-only tuples are represented as subject
// targets.
func RelationshipTargetMapKey(target *core.RelationshipTargetRef, subject *core.SubjectRef) string {
	if target != nil {
		if targetSubject := target.GetSubject(); targetSubject != nil {
			return strings.Join([]string{"subject", strings.TrimSpace(targetSubject.GetType()), strings.TrimSpace(targetSubject.GetId())}, "\x00")
		}
		if targetResource := target.GetResource(); targetResource != nil {
			return strings.Join([]string{"resource", strings.TrimSpace(targetResource.GetType()), strings.TrimSpace(targetResource.GetId())}, "\x00")
		}
		if targetSet := target.GetSubjectSet(); targetSet != nil {
			resource := targetSet.GetResource()
			return strings.Join([]string{
				"subject_set",
				strings.TrimSpace(resource.GetType()),
				strings.TrimSpace(resource.GetId()),
				strings.TrimSpace(targetSet.GetRelation()),
			}, "\x00")
		}
	}
	if subject != nil {
		return strings.Join([]string{"subject", strings.TrimSpace(subject.GetType()), strings.TrimSpace(subject.GetId())}, "\x00")
	}
	return ""
}

// RelationshipSubject returns the effective direct subject for a relationship.
// Non-subject targets and inconsistent legacy subject/target pairs return nil.
func RelationshipSubject(rel *core.Relationship) *core.SubjectRef {
	if rel == nil {
		return nil
	}
	if target := rel.GetTarget(); target != nil {
		targetSubject := target.GetSubject()
		if targetSubject == nil {
			return nil
		}
		if subject := rel.GetSubject(); subject != nil && !sameSubjectRef(subject, targetSubject) {
			return nil
		}
		return targetSubject
	}
	return rel.GetSubject()
}

func relationshipTargetSubject(rel *core.Relationship) *core.SubjectRef {
	return RelationshipSubject(rel)
}

func sameSubjectRef(left, right *core.SubjectRef) bool {
	return left != nil &&
		right != nil &&
		strings.TrimSpace(left.GetType()) == strings.TrimSpace(right.GetType()) &&
		strings.TrimSpace(left.GetId()) == strings.TrimSpace(right.GetId())
}

func (a *ProviderBackedAuthorizer) logProviderEvalError(scope, name string, err error) {
	if err == nil {
		return
	}
	slog.Warn("authorization: provider evaluation failed; denying provider-backed subject access",
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
