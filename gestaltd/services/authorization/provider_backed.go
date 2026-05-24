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
	proto "github.com/valon-technologies/gestalt/sdk/go/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"google.golang.org/protobuf/types/known/structpb"
)

type providerBackedRoleState struct {
	modelID           string
	policyStaticRoles map[string][]string
	appStaticRoles    map[string][]string
	appDynamicRoles   map[string][]string
	adminDynamicRoles []string
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
			policyStaticRoles: map[string][]string{},
			appStaticRoles:    map[string][]string{},
			appDynamicRoles:   map[string][]string{},
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
		case resourceTypeAppDynamic:
			if roles.appDynamicRoles == nil {
				roles.appDynamicRoles = map[string][]string{}
			}
			roles.appDynamicRoles[resourceID] = roleListWith(roles.appDynamicRoles[resourceID], role)
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
		a.logProviderEvalError("app", provider, err)
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
		resourceTypeAppStatic,
		provider,
		state.appStaticRoles[provider],
	)
	if err != nil || ok {
		return role, ok, err
	}
	return a.resolveRoleVariants(
		ctx,
		dynamicSubjectRefs(p),
		resourceTypeAppDynamic,
		provider,
		state.appDynamicRoles[provider],
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

type dynamicFragmentRelationship struct {
	Relationship *core.Relationship
	SourceID     string
	OwnerKind    string
	OwnerID      string
}

func (a *ProviderBackedAuthorizer) buildDesiredRelationships(existing map[string]*core.Relationship, fragmentRelationships []dynamicFragmentRelationship) (map[string]*core.Relationship, providerBackedRoleState, error) {
	desired := map[string]*core.Relationship{}
	state := providerBackedRoleState{
		policyStaticRoles: map[string][]string{},
		appStaticRoles:    map[string][]string{},
		appDynamicRoles:   map[string][]string{},
	}
	policyStaticRoles := map[string]map[string]struct{}{}
	appStaticRoles := map[string]map[string]struct{}{}
	appDynamicRoles := map[string]map[string]struct{}{}
	adminDynamicRoles := map[string]struct{}{}

	for _, rel := range existing {
		if rel == nil || rel.GetResource() == nil {
			continue
		}
		switch strings.TrimSpace(rel.GetResource().GetType()) {
		case resourceTypeAppDynamic:
			if a.fragmentSource != nil {
				continue
			}
			resourceID := strings.TrimSpace(rel.GetResource().GetId())
			relation := strings.TrimSpace(rel.GetRelation())
			if resourceID == "" || relation == "" {
				continue
			}
			addDesiredRelationship(desired, synthesizedRelationship(rel, "provider_dynamic", "legacy_app_dynamic", "app", resourceID))
			ensureRoleSet(appDynamicRoles, resourceID)[relation] = struct{}{}
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
		case resourceTypeEveryone, resourceTypeTeam, resourceTypeSlackChannel:
			if !validManagedMembershipRelationship(rel) {
				continue
			}
			addDesiredRelationship(desired, synthesizedRelationship(rel, "provider_existing", "membership", "", ""))
		}
	}

	for _, fragmentRel := range fragmentRelationships {
		rel := fragmentRel.Relationship
		if rel == nil || rel.GetResource() == nil {
			continue
		}
		sourceID := strings.TrimSpace(fragmentRel.SourceID)
		if sourceID == "" {
			sourceID = "dynamic_fragment"
		}
		switch strings.TrimSpace(rel.GetResource().GetType()) {
		case resourceTypeAppDynamic:
			resourceID := strings.TrimSpace(rel.GetResource().GetId())
			relation := strings.TrimSpace(rel.GetRelation())
			if resourceID == "" || relation == "" {
				continue
			}
			addDesiredRelationship(desired, synthesizedRelationship(rel, "dynamic_fragment", sourceID, "app", resourceID))
			ensureRoleSet(appDynamicRoles, resourceID)[relation] = struct{}{}
		case resourceTypeAdminDynamic:
			resourceID := strings.TrimSpace(rel.GetResource().GetId())
			relation := strings.TrimSpace(rel.GetRelation())
			if resourceID != resourceIDAdminDynamicGlobal || relation == "" {
				continue
			}
			addDesiredRelationship(desired, synthesizedRelationship(rel, "dynamic_fragment", sourceID, "global", resourceIDAdminDynamicGlobal))
			adminDynamicRoles[relation] = struct{}{}
		default:
			addDesiredRelationship(desired, synthesizedRelationship(rel, "dynamic_fragment", sourceID, fragmentRel.OwnerKind, fragmentRel.OwnerID))
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
				ensureRoleSet(appStaticRoles, providerName)[role] = struct{}{}
				addDesiredRelationship(desired, synthesizedRelationship(&core.Relationship{
					Subject:  &core.SubjectRef{Type: subjectTypeSubject, Id: subjectID},
					Relation: role,
					Resource: &core.ResourceRef{Type: resourceTypeAppStatic, Id: providerName},
				}, "static_config", "authorization.policies."+policyName, "app", providerName))
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
	for name, roles := range appStaticRoles {
		state.appStaticRoles[name] = normalizeRoleList(roles)
	}
	for name, roles := range appDynamicRoles {
		state.appDynamicRoles[name] = normalizeRoleList(roles)
	}
	state.adminDynamicRoles = normalizeRoleList(adminDynamicRoles)
	return desired, state, nil
}

func (a *ProviderBackedAuthorizer) dynamicFragmentState(ctx context.Context, existing map[string]*core.Relationship) ([]dynamicFragmentRelationship, []*core.AuthorizationModelResourceType, error) {
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
	var relationships []dynamicFragmentRelationship
	var modelFragments []*core.AuthorizationModelResourceType
	staticResourceTypes, resourceRelations := a.staticResourceTypeState(dynamicFragmentRoleState(fragments))
	seenOwners := map[string]string{}
	for _, fragment := range fragments {
		if fragment == nil || strings.TrimSpace(fragment.Status) != coredata.AuthorizationFragmentStatusActive {
			continue
		}
		ownerKey := dynamicFragmentOwnerKey(fragment.Owner)
		if existingID, ok := seenOwners[ownerKey]; ok {
			slog.WarnContext(ctx, "authorization: removing duplicate dynamic authorization fragment owner", "fragment", fragment.ID, "existing_fragment", existingID, "owner", ownerKey)
			if err := a.fragmentSource.DeleteFragment(ctx, fragment.ID); err != nil {
				return nil, nil, fmt.Errorf("delete duplicate dynamic authorization fragment %q: %w", fragment.ID, err)
			}
			continue
		}
		seenOwners[ownerKey] = fragment.ID
		localResourceTypes := dynamicFragmentLocalResourceTypes(fragment)
		fragmentRelationshipsStart := len(relationships)
		fragmentModelsStart := len(modelFragments)
		fragmentResourceNames := []string{}
		fragmentValid := true
		for resourceTypeName, raw := range fragment.ResourceTypes {
			canonicalName, err := dynamicFragmentCanonicalResourceType(fragment.Owner, resourceTypeName, localResourceTypes)
			if err != nil {
				slog.WarnContext(ctx, "authorization: removing invalid dynamic authorization fragment resource type", "fragment", fragment.ID, "resource_type", resourceTypeName, "error", err)
				fragmentValid = false
				break
			}
			if _, ok := staticResourceTypes[canonicalName]; ok {
				slog.WarnContext(ctx, "authorization: removing conflicting dynamic authorization fragment resource type", "fragment", fragment.ID, "resource_type", canonicalName)
				fragmentValid = false
				break
			}
			resourceType, err := dynamicFragmentModelResourceType(fragment.Owner, resourceTypeName, raw, localResourceTypes)
			if err != nil {
				slog.WarnContext(ctx, "authorization: removing invalid dynamic authorization fragment model", "fragment", fragment.ID, "resource_type", resourceTypeName, "error", err)
				fragmentValid = false
				break
			}
			modelFragments = append(modelFragments, resourceType)
			resourceRelations[canonicalName] = modelResourceTypeRelations(resourceType)
			fragmentResourceNames = append(fragmentResourceNames, canonicalName)
		}
		if !fragmentValid {
			if err := a.fragmentSource.DeleteFragment(ctx, fragment.ID); err != nil {
				return nil, nil, fmt.Errorf("delete invalid dynamic authorization fragment %q: %w", fragment.ID, err)
			}
			modelFragments = modelFragments[:fragmentModelsStart]
			for _, resourceType := range fragmentResourceNames {
				delete(resourceRelations, resourceType)
			}
			continue
		}
		for _, relationship := range fragment.Relationships {
			rel, err := relationshipFromDynamicFragment(fragment.Owner, relationship, localResourceTypes)
			if err != nil {
				slog.WarnContext(ctx, "authorization: removing invalid dynamic authorization fragment relationship", "fragment", fragment.ID, "error", err)
				fragmentValid = false
				break
			}
			if rel == nil {
				continue
			}
			if err := a.validateDynamicRelationship(rel, resourceRelations, staticResourceTypes); err != nil {
				slog.WarnContext(ctx, "authorization: removing invalid dynamic authorization fragment relationship", "fragment", fragment.ID, "resource_type", rel.GetResource().GetType(), "relation", rel.GetRelation(), "error", err)
				fragmentValid = false
				break
			}
			relationships = append(relationships, dynamicFragmentRelationship{
				Relationship: rel,
				SourceID:     fragment.ID,
				OwnerKind:    strings.TrimSpace(fragment.Owner.Kind),
				OwnerID:      dynamicFragmentOwnerID(fragment.Owner),
			})
		}
		if !fragmentValid {
			if err := a.fragmentSource.DeleteFragment(ctx, fragment.ID); err != nil {
				return nil, nil, fmt.Errorf("delete invalid dynamic authorization fragment %q: %w", fragment.ID, err)
			}
			relationships = relationships[:fragmentRelationshipsStart]
			modelFragments = modelFragments[:fragmentModelsStart]
			for _, resourceType := range fragmentResourceNames {
				delete(resourceRelations, resourceType)
			}
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

func (a *ProviderBackedAuthorizer) staticResourceTypeState(roles providerBackedRoleState) (map[string]struct{}, map[string]map[string]struct{}) {
	names := map[string]struct{}{}
	relations := map[string]map[string]struct{}{}
	addResourceTypes := func(resourceTypes []*core.AuthorizationModelResourceType) {
		for _, resourceType := range resourceTypes {
			name := strings.TrimSpace(resourceType.GetName())
			if name == "" {
				continue
			}
			names[name] = struct{}{}
			relations[name] = modelResourceTypeRelations(resourceType)
		}
	}
	addResourceTypes(buildProviderAuthorizationModel(roles).GetResourceTypes())
	addResourceTypes(a.base.modelFragments)
	for _, resourceType := range providerAuthorizationResourceTypes {
		if name := strings.TrimSpace(resourceType); name != "" {
			names[name] = struct{}{}
		}
	}
	return names, relations
}

func dynamicFragmentRoleState(fragments []*coredata.AuthorizationDynamicFragment) providerBackedRoleState {
	state := providerBackedRoleState{
		appDynamicRoles: map[string][]string{},
	}
	pluginRoles := map[string]map[string]struct{}{}
	adminRoles := map[string]struct{}{}
	for _, fragment := range fragments {
		if fragment == nil || strings.TrimSpace(fragment.Status) != coredata.AuthorizationFragmentStatusActive {
			continue
		}
		localResourceTypes := dynamicFragmentLocalResourceTypes(fragment)
		for _, relationship := range fragment.Relationships {
			resourceType, err := dynamicFragmentCanonicalResourceType(fragment.Owner, relationship.Resource.Type, localResourceTypes)
			if err != nil {
				continue
			}
			relation := strings.TrimSpace(relationship.Relation)
			if relation == "" {
				continue
			}
			switch resourceType {
			case resourceTypeAppDynamic:
				resourceID := strings.TrimSpace(relationship.Resource.ID)
				if resourceID != "" {
					ensureRoleSet(pluginRoles, resourceID)[relation] = struct{}{}
				}
			case resourceTypeAdminDynamic:
				if strings.TrimSpace(relationship.Resource.ID) == resourceIDAdminDynamicGlobal {
					adminRoles[relation] = struct{}{}
				}
			}
		}
	}
	for resourceID, roles := range pluginRoles {
		state.appDynamicRoles[resourceID] = normalizeRoleList(roles)
	}
	state.adminDynamicRoles = normalizeRoleList(adminRoles)
	return state
}

func modelResourceTypeRelations(resourceType *core.AuthorizationModelResourceType) map[string]struct{} {
	out := map[string]struct{}{}
	for _, relation := range resourceType.GetRelations() {
		if name := strings.TrimSpace(relation.GetName()); name != "" {
			out[name] = struct{}{}
		}
	}
	return out
}

func dynamicFragmentLocalResourceTypes(fragment *coredata.AuthorizationDynamicFragment) map[string]struct{} {
	out := map[string]struct{}{}
	if fragment == nil {
		return out
	}
	for name := range fragment.ResourceTypes {
		if name = strings.TrimSpace(name); name != "" {
			out[name] = struct{}{}
		}
	}
	return out
}

func dynamicFragmentCanonicalResourceType(owner coredata.AuthorizationFragmentOwner, resourceType string, localResourceTypes map[string]struct{}) (string, error) {
	resourceType = strings.TrimSpace(resourceType)
	if resourceType == "" {
		return "", fmt.Errorf("resource type is required")
	}
	if strings.TrimSpace(owner.Kind) != coredata.AuthorizationFragmentOwnerKindApp {
		return resourceType, nil
	}
	appName := strings.TrimSpace(owner.App)
	if appName == "" {
		return "", fmt.Errorf("app owner requires app")
	}
	prefix := "app/" + appName + "/"
	if strings.HasPrefix(resourceType, "app/") {
		if !strings.HasPrefix(resourceType, prefix) {
			return "", fmt.Errorf("app fragment cannot reference resource type %q outside %q", resourceType, prefix)
		}
		return resourceType, nil
	}
	if _, ok := localResourceTypes[resourceType]; ok {
		return prefix + resourceType, nil
	}
	switch resourceType {
	case resourceTypeAppDynamic:
		return resourceType, nil
	default:
		return "", fmt.Errorf("app fragment cannot reference non-local resource type %q", resourceType)
	}
}

func dynamicFragmentOwnerKey(owner coredata.AuthorizationFragmentOwner) string {
	return strings.TrimSpace(owner.Kind) + "\x00" + dynamicFragmentOwnerID(owner)
}

func dynamicFragmentOwnerID(owner coredata.AuthorizationFragmentOwner) string {
	if strings.TrimSpace(owner.Kind) == coredata.AuthorizationFragmentOwnerKindApp {
		return strings.TrimSpace(owner.App)
	}
	return coredata.AuthorizationFragmentOwnerKindGlobal
}

func (a *ProviderBackedAuthorizer) validateDynamicRelationship(rel *core.Relationship, resourceRelations map[string]map[string]struct{}, staticResourceTypes map[string]struct{}) error {
	resourceType := strings.TrimSpace(rel.GetResource().GetType())
	relation := strings.TrimSpace(rel.GetRelation())
	if resourceType == "" || relation == "" {
		return fmt.Errorf("resource type and relation are required")
	}
	relations := resourceRelations[resourceType]
	if len(relations) == 0 {
		return fmt.Errorf("resource type %q is not defined by the composed model", resourceType)
	}
	if _, ok := relations[relation]; !ok {
		return fmt.Errorf("relation %q is not defined on resource type %q", relation, resourceType)
	}
	if _, static := staticResourceTypes[resourceType]; static &&
		resourceType != resourceTypeAppDynamic &&
		resourceType != resourceTypeAdminDynamic &&
		!a.base.resourceDynamicPolicies[resourceType].AllowAdditionalRelationships {
		return fmt.Errorf("static resource type %q does not allow dynamic relationships", resourceType)
	}
	return nil
}

func relationshipFromDynamicFragment(owner coredata.AuthorizationFragmentOwner, relationship coredata.AuthorizationDynamicFragmentRelationship, localResourceTypes map[string]struct{}) (*core.Relationship, error) {
	subject := &core.SubjectRef{Type: strings.TrimSpace(relationship.Subject.Type), Id: strings.TrimSpace(relationship.Subject.ID)}
	resourceType, err := dynamicFragmentCanonicalResourceType(owner, relationship.Resource.Type, localResourceTypes)
	if err != nil {
		return nil, err
	}
	resource := &core.ResourceRef{Type: resourceType, Id: strings.TrimSpace(relationship.Resource.ID)}
	if subject.GetType() == "" || subject.GetId() == "" || resource.GetType() == "" || resource.GetId() == "" || strings.TrimSpace(relationship.Relation) == "" {
		return nil, nil
	}
	target, err := dynamicFragmentRelationshipTarget(owner, relationship.Target, localResourceTypes)
	if err != nil {
		return nil, err
	}
	rel := &core.Relationship{
		Subject:  subject,
		Relation: strings.TrimSpace(relationship.Relation),
		Resource: resource,
		Target:   target,
	}
	if len(relationship.Properties) > 0 {
		properties := make(map[string]any, len(relationship.Properties))
		for key, value := range relationship.Properties {
			properties[key] = value
		}
		rel.Properties, _ = structpb.NewStruct(properties)
	}
	return rel, nil
}

func dynamicFragmentRelationshipFromProvider(rel *core.Relationship) (coredata.AuthorizationDynamicFragmentRelationship, coredata.AuthorizationFragmentOwner, bool) {
	if rel == nil || relationshipTargetSubject(rel) == nil || rel.GetResource() == nil {
		return coredata.AuthorizationDynamicFragmentRelationship{}, coredata.AuthorizationFragmentOwner{}, false
	}
	resource := rel.GetResource()
	switch strings.TrimSpace(resource.GetType()) {
	case resourceTypeAppDynamic:
		appName := strings.TrimSpace(resource.GetId())
		if appName == "" {
			return coredata.AuthorizationDynamicFragmentRelationship{}, coredata.AuthorizationFragmentOwner{}, false
		}
		return dynamicFragmentRelationshipFromCore(rel), coredata.AuthorizationAppFragmentOwner(appName), true
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

func dynamicFragmentModelResourceType(owner coredata.AuthorizationFragmentOwner, name string, raw json.RawMessage, localResourceTypes map[string]struct{}) (*core.AuthorizationModelResourceType, error) {
	canonicalName, err := dynamicFragmentCanonicalResourceType(owner, name, localResourceTypes)
	if err != nil {
		return nil, err
	}
	if canonicalName == "" {
		return nil, fmt.Errorf("name is required")
	}
	var def coredata.AuthorizationDynamicFragmentResourceTypeDef
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &def); err != nil {
			return nil, err
		}
	}
	resourceType := &core.AuthorizationModelResourceType{Name: canonicalName}
	for relationName, relation := range def.Relations {
		allowedTargets, err := dynamicFragmentAllowedTargets(owner, relation.AllowedTargets, localResourceTypes)
		if err != nil {
			return nil, fmt.Errorf("relations.%s.allowedTargets: %w", relationName, err)
		}
		resourceType.Relations = append(resourceType.Relations, &core.AuthorizationModelRelation{
			Name:           relationName,
			SubjectTypes:   append([]string(nil), relation.SubjectTypes...),
			AllowedTargets: allowedTargets,
		})
	}
	for actionName, action := range def.Actions {
		resourceType.Actions = append(resourceType.Actions, &core.AuthorizationModelAction{
			Name:      actionName,
			Relations: append([]string(nil), action.Relations...),
		})
	}
	return resourceType, nil
}

func dynamicFragmentAllowedTargets(owner coredata.AuthorizationFragmentOwner, targets []coredata.AuthorizationDynamicFragmentAllowedTarget, localResourceTypes map[string]struct{}) ([]*core.AuthorizationModelAllowedTarget, error) {
	out := make([]*core.AuthorizationModelAllowedTarget, 0, len(targets))
	for i, target := range targets {
		switch {
		case strings.TrimSpace(target.SubjectType) != "":
			out = append(out, &core.AuthorizationModelAllowedTarget{
				Kind: &proto.AuthorizationModelAllowedTarget_SubjectType{SubjectType: strings.TrimSpace(target.SubjectType)},
			})
		case strings.TrimSpace(target.ResourceType) != "":
			resourceType, err := dynamicFragmentCanonicalResourceType(owner, target.ResourceType, localResourceTypes)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			out = append(out, &core.AuthorizationModelAllowedTarget{
				Kind: &proto.AuthorizationModelAllowedTarget_ResourceType{ResourceType: resourceType},
			})
		case target.SubjectSet != nil:
			resourceType, err := dynamicFragmentCanonicalResourceType(owner, target.SubjectSet.ResourceType, localResourceTypes)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			out = append(out, &core.AuthorizationModelAllowedTarget{
				Kind: &proto.AuthorizationModelAllowedTarget_SubjectSet{SubjectSet: &core.AuthorizationModelSubjectSetTarget{
					ResourceType: resourceType,
					Relation:     strings.TrimSpace(target.SubjectSet.Relation),
				}},
			})
		}
	}
	return out, nil
}

func dynamicFragmentRelationshipTarget(owner coredata.AuthorizationFragmentOwner, target coredata.AuthorizationDynamicFragmentTarget, localResourceTypes map[string]struct{}) (*core.RelationshipTargetRef, error) {
	switch {
	case target.Subject != nil:
		return &core.RelationshipTargetRef{Kind: &proto.RelationshipTarget_Subject{Subject: &core.SubjectRef{Type: strings.TrimSpace(target.Subject.Type), Id: strings.TrimSpace(target.Subject.ID)}}}, nil
	case target.Resource != nil:
		resourceType, err := dynamicFragmentCanonicalResourceType(owner, target.Resource.Type, localResourceTypes)
		if err != nil {
			return nil, err
		}
		return &core.RelationshipTargetRef{Kind: &proto.RelationshipTarget_Resource{Resource: &core.ResourceRef{Type: resourceType, Id: strings.TrimSpace(target.Resource.ID)}}}, nil
	case target.SubjectSet != nil:
		resourceType, err := dynamicFragmentCanonicalResourceType(owner, target.SubjectSet.Resource.Type, localResourceTypes)
		if err != nil {
			return nil, err
		}
		return &core.RelationshipTargetRef{Kind: &proto.RelationshipTarget_SubjectSet{SubjectSet: &core.SubjectSetRef{
			Resource: &core.ResourceRef{Type: resourceType, Id: strings.TrimSpace(target.SubjectSet.Resource.ID)},
			Relation: strings.TrimSpace(target.SubjectSet.Relation),
		}}}, nil
	default:
		return nil, nil
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
