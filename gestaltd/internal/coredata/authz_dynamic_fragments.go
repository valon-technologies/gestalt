package coredata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

const (
	AuthorizationFragmentOwnerKindGlobal = "global"
	AuthorizationFragmentOwnerKindApp = "app"

	AuthorizationFragmentScopeGlobal = "global"
	AuthorizationFragmentScopeApp = "app"

	AuthorizationFragmentStatusActive = "active"
)

type AuthorizationDynamicFragmentService struct {
	store indexeddb.ObjectStore
}

type AuthorizationFragmentOwner struct {
	Kind   string `json:"kind"`
	App string `json:"app,omitempty"`
}

type AuthorizationDynamicFragment struct {
	ID            string                                     `json:"id"`
	Owner         AuthorizationFragmentOwner                 `json:"owner"`
	Scope         string                                     `json:"scope"`
	App        string                                     `json:"app,omitempty"`
	Version       int64                                      `json:"version"`
	Status        string                                     `json:"status"`
	ResourceTypes map[string]json.RawMessage                 `json:"resourceTypes,omitempty"`
	Relationships []AuthorizationDynamicFragmentRelationship `json:"relationships,omitempty"`
	Audit         AuthorizationDynamicFragmentAuditMetadata  `json:"audit,omitempty"`
	CreatedAt     time.Time                                  `json:"createdAt"`
	UpdatedAt     time.Time                                  `json:"updatedAt"`
}

type AuthorizationDynamicFragmentRelationship struct {
	Subject    AuthorizationDynamicFragmentSubject  `json:"subject"`
	Relation   string                               `json:"relation"`
	Resource   AuthorizationDynamicFragmentResource `json:"resource"`
	Target     AuthorizationDynamicFragmentTarget   `json:"target,omitempty"`
	Properties map[string]string                    `json:"properties,omitempty"`
}

type AuthorizationDynamicFragmentSubject struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type AuthorizationDynamicFragmentResource struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type AuthorizationDynamicFragmentTarget struct {
	Subject    *AuthorizationDynamicFragmentSubject    `json:"subject,omitempty"`
	Resource   *AuthorizationDynamicFragmentResource   `json:"resource,omitempty"`
	SubjectSet *AuthorizationDynamicFragmentSubjectSet `json:"subjectSet,omitempty"`
}

type AuthorizationDynamicFragmentSubjectSet struct {
	Resource AuthorizationDynamicFragmentResource `json:"resource"`
	Relation string                               `json:"relation"`
}

type AuthorizationDynamicFragmentAuditMetadata struct {
	UpdatedBySubjectID  string `json:"updatedBySubjectId,omitempty"`
	UpdatedByAuthSource string `json:"updatedByAuthSource,omitempty"`
	Reason              string `json:"reason,omitempty"`
}

type AuthorizationDynamicFragmentUpdate struct {
	ExpectedVersion *int64
	Audit           AuthorizationDynamicFragmentAuditMetadata
}

type AuthorizationDynamicFragmentResourceTypeDef struct {
	Relations map[string]AuthorizationDynamicFragmentRelationDef `json:"relations,omitempty"`
	Actions   map[string]AuthorizationDynamicFragmentActionDef   `json:"actions,omitempty"`
}

type AuthorizationDynamicFragmentRelationDef struct {
	SubjectTypes   []string                                    `json:"subjectTypes,omitempty"`
	AllowedTargets []AuthorizationDynamicFragmentAllowedTarget `json:"allowedTargets,omitempty"`
}

type AuthorizationDynamicFragmentActionDef struct {
	Relations []string `json:"relations,omitempty"`
}

type AuthorizationDynamicFragmentAllowedTarget struct {
	SubjectType  string                                        `json:"subjectType,omitempty"`
	ResourceType string                                        `json:"resourceType,omitempty"`
	SubjectSet   *AuthorizationDynamicFragmentSubjectSetTarget `json:"subjectSet,omitempty"`
}

type AuthorizationDynamicFragmentSubjectSetTarget struct {
	ResourceType string `json:"resourceType,omitempty"`
	Relation     string `json:"relation,omitempty"`
}

func NewAuthorizationDynamicFragmentService(ds indexeddb.IndexedDB) *AuthorizationDynamicFragmentService {
	return &AuthorizationDynamicFragmentService{store: ds.ObjectStore(StoreAuthorizationDynamicFragments)}
}

func AuthorizationGlobalFragmentOwner() AuthorizationFragmentOwner {
	return AuthorizationFragmentOwner{Kind: AuthorizationFragmentOwnerKindGlobal}
}

func AuthorizationAppFragmentOwner(plugin string) AuthorizationFragmentOwner {
	return AuthorizationFragmentOwner{Kind: AuthorizationFragmentOwnerKindApp, App: strings.TrimSpace(plugin)}
}

func (s *AuthorizationDynamicFragmentService) ListFragments(ctx context.Context) ([]*AuthorizationDynamicFragment, error) {
	recs, err := s.store.GetAll(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list authorization dynamic fragments: %w", err)
	}
	out := make([]*AuthorizationDynamicFragment, 0, len(recs))
	for _, rec := range recs {
		fragment, err := recordToAuthorizationDynamicFragment(rec)
		if err != nil {
			return nil, err
		}
		out = append(out, fragment)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *AuthorizationDynamicFragmentService) GetFragment(ctx context.Context, id string) (*AuthorizationDynamicFragment, error) {
	rec, err := s.store.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get authorization dynamic fragment: %w", err)
	}
	return recordToAuthorizationDynamicFragment(rec)
}

func (s *AuthorizationDynamicFragmentService) GetFragmentByOwner(ctx context.Context, owner AuthorizationFragmentOwner) (*AuthorizationDynamicFragment, error) {
	owner = normalizeAuthorizationFragmentOwner(owner)
	if err := validateAuthorizationFragmentOwner(owner); err != nil {
		return nil, fmt.Errorf("get authorization dynamic fragment: %w", err)
	}
	rec, err := s.store.Index("by_owner").Get(ctx, owner.Kind, authorizationFragmentOwnerID(owner))
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get authorization dynamic fragment by owner: %w", err)
	}
	return recordToAuthorizationDynamicFragment(rec)
}

func (s *AuthorizationDynamicFragmentService) PutFragment(ctx context.Context, fragment *AuthorizationDynamicFragment, update AuthorizationDynamicFragmentUpdate) (*AuthorizationDynamicFragment, error) {
	if fragment == nil {
		return nil, fmt.Errorf("put authorization dynamic fragment: fragment is required")
	}
	fragment = cloneAuthorizationDynamicFragment(fragment)
	fragment.Owner = normalizeAuthorizationFragmentOwner(fragment.Owner)
	if err := validateAuthorizationDynamicFragment(fragment); err != nil {
		return nil, fmt.Errorf("put authorization dynamic fragment: %w", err)
	}
	existing, err := s.GetFragmentByOwner(ctx, fragment.Owner)
	switch {
	case err == nil:
		if update.ExpectedVersion != nil && existing.Version != *update.ExpectedVersion {
			return nil, fmt.Errorf("put authorization dynamic fragment: version mismatch")
		}
		fragment.ID = existing.ID
		fragment.CreatedAt = existing.CreatedAt
		fragment.Version = existing.Version + 1
	case errors.Is(err, core.ErrNotFound):
		if update.ExpectedVersion != nil && *update.ExpectedVersion != 0 {
			return nil, fmt.Errorf("put authorization dynamic fragment: version mismatch")
		}
		fragment.ID = authorizationFragmentID(fragment.Owner)
		fragment.CreatedAt = time.Now().UTC().Truncate(time.Second)
		fragment.Version = 1
	default:
		return nil, err
	}
	fragment.Audit = update.Audit
	fragment.Status = defaultAuthorizationFragmentStatus(fragment.Status)
	fragment.Scope = authorizationFragmentScope(fragment.Owner)
	fragment.App = authorizationFragmentPlugin(fragment.Owner)
	fragment.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	if err := s.store.Put(ctx, authorizationDynamicFragmentToRecord(fragment)); err != nil {
		return nil, fmt.Errorf("put authorization dynamic fragment: %w", err)
	}
	return cloneAuthorizationDynamicFragment(fragment), nil
}

func (s *AuthorizationDynamicFragmentService) UpdateFragment(ctx context.Context, owner AuthorizationFragmentOwner, update AuthorizationDynamicFragmentUpdate, mutate func(*AuthorizationDynamicFragment) error) (*AuthorizationDynamicFragment, error) {
	if mutate == nil {
		return nil, fmt.Errorf("update authorization dynamic fragment: mutate is required")
	}
	fragment, err := s.GetFragmentByOwner(ctx, owner)
	if err != nil {
		if !errors.Is(err, core.ErrNotFound) {
			return nil, err
		}
		fragment = &AuthorizationDynamicFragment{Owner: normalizeAuthorizationFragmentOwner(owner)}
	}
	if update.ExpectedVersion != nil && fragment.Version != *update.ExpectedVersion {
		return nil, fmt.Errorf("update authorization dynamic fragment: version mismatch")
	}
	fragment = cloneAuthorizationDynamicFragment(fragment)
	if err := mutate(fragment); err != nil {
		return nil, err
	}
	return s.PutFragment(ctx, fragment, update)
}

func (s *AuthorizationDynamicFragmentService) DeleteFragment(ctx context.Context, id string) error {
	fragment, err := s.GetFragment(ctx, id)
	if err != nil {
		return err
	}
	if err := s.store.Delete(ctx, fragment.ID); err != nil {
		return fmt.Errorf("delete authorization dynamic fragment: %w", err)
	}
	return nil
}

func (s *AuthorizationDynamicFragmentService) UpsertRelationship(ctx context.Context, owner AuthorizationFragmentOwner, relationship AuthorizationDynamicFragmentRelationship, audit AuthorizationDynamicFragmentAuditMetadata) (*AuthorizationDynamicFragment, error) {
	return s.UpdateFragment(ctx, owner, AuthorizationDynamicFragmentUpdate{Audit: audit}, func(fragment *AuthorizationDynamicFragment) error {
		relationship = normalizeAuthorizationFragmentRelationship(relationship)
		if err := validateAuthorizationFragmentRelationship(relationship); err != nil {
			return err
		}
		replaced := false
		key := authorizationFragmentRelationshipKey(relationship)
		for i, existing := range fragment.Relationships {
			if authorizationFragmentRelationshipKey(existing) == key {
				fragment.Relationships[i] = relationship
				replaced = true
				break
			}
		}
		if !replaced {
			fragment.Relationships = append(fragment.Relationships, relationship)
		}
		sortAuthorizationFragmentRelationships(fragment.Relationships)
		return nil
	})
}

func (s *AuthorizationDynamicFragmentService) ReplaceSubjectResourceRelationships(ctx context.Context, owner AuthorizationFragmentOwner, relationship AuthorizationDynamicFragmentRelationship, audit AuthorizationDynamicFragmentAuditMetadata) (*AuthorizationDynamicFragment, error) {
	return s.UpdateFragment(ctx, owner, AuthorizationDynamicFragmentUpdate{Audit: audit}, func(fragment *AuthorizationDynamicFragment) error {
		relationship = normalizeAuthorizationFragmentRelationship(relationship)
		if err := validateAuthorizationFragmentRelationship(relationship); err != nil {
			return err
		}
		out, _ := removeAuthorizationFragmentRelationshipsForSubjectResource(fragment.Relationships, relationship.Subject, relationship.Resource)
		out = append(out, relationship)
		fragment.Relationships = out
		sortAuthorizationFragmentRelationships(fragment.Relationships)
		return nil
	})
}

func (s *AuthorizationDynamicFragmentService) DeleteRelationship(ctx context.Context, owner AuthorizationFragmentOwner, relationship AuthorizationDynamicFragmentRelationship, audit AuthorizationDynamicFragmentAuditMetadata) (bool, *AuthorizationDynamicFragment, error) {
	if _, err := s.GetFragmentByOwner(ctx, owner); err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return false, nil, nil
		}
		return false, nil, err
	}
	deleted := false
	fragment, err := s.UpdateFragment(ctx, owner, AuthorizationDynamicFragmentUpdate{Audit: audit}, func(fragment *AuthorizationDynamicFragment) error {
		relationship = normalizeAuthorizationFragmentRelationship(relationship)
		key := authorizationFragmentRelationshipKey(relationship)
		out := fragment.Relationships[:0]
		for _, existing := range fragment.Relationships {
			if authorizationFragmentRelationshipKey(existing) == key {
				deleted = true
				continue
			}
			out = append(out, existing)
		}
		fragment.Relationships = out
		return nil
	})
	if err != nil || !deleted {
		return deleted, fragment, err
	}
	fragment, err = s.maybeDeleteEmptyFragment(ctx, fragment)
	return true, fragment, err
}

func (s *AuthorizationDynamicFragmentService) DeleteSubjectResourceRelationships(ctx context.Context, owner AuthorizationFragmentOwner, subject AuthorizationDynamicFragmentSubject, resource AuthorizationDynamicFragmentResource, audit AuthorizationDynamicFragmentAuditMetadata) (bool, *AuthorizationDynamicFragment, error) {
	existing, err := s.GetFragmentByOwner(ctx, owner)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return false, nil, nil
		}
		return false, nil, err
	}
	subject.Type = strings.TrimSpace(subject.Type)
	subject.ID = strings.TrimSpace(subject.ID)
	resource.Type = strings.TrimSpace(resource.Type)
	resource.ID = strings.TrimSpace(resource.ID)
	if subject.Type == "" || subject.ID == "" || resource.Type == "" || resource.ID == "" {
		return false, existing, fmt.Errorf("subject and resource are required")
	}

	deleted := false
	fragment, err := s.UpdateFragment(ctx, owner, AuthorizationDynamicFragmentUpdate{Audit: audit}, func(fragment *AuthorizationDynamicFragment) error {
		out, removed := removeAuthorizationFragmentRelationshipsForSubjectResource(fragment.Relationships, subject, resource)
		deleted = removed
		fragment.Relationships = out
		return nil
	})
	if err != nil || !deleted {
		return deleted, fragment, err
	}
	fragment, err = s.maybeDeleteEmptyFragment(ctx, fragment)
	return true, fragment, err
}

func (s *AuthorizationDynamicFragmentService) maybeDeleteEmptyFragment(ctx context.Context, fragment *AuthorizationDynamicFragment) (*AuthorizationDynamicFragment, error) {
	if fragment == nil {
		return nil, nil
	}
	if len(fragment.Relationships) == 0 && len(fragment.ResourceTypes) == 0 {
		if err := s.DeleteFragment(ctx, fragment.ID); err != nil {
			return fragment, err
		}
		return nil, nil
	}
	return fragment, nil
}

func removeAuthorizationFragmentRelationshipsForSubjectResource(relationships []AuthorizationDynamicFragmentRelationship, subject AuthorizationDynamicFragmentSubject, resource AuthorizationDynamicFragmentResource) ([]AuthorizationDynamicFragmentRelationship, bool) {
	out := relationships[:0]
	removed := false
	for _, relationship := range relationships {
		relationship = normalizeAuthorizationFragmentRelationship(relationship)
		if authorizationFragmentRelationshipMatchesSubjectResource(relationship, subject, resource) {
			removed = true
			continue
		}
		out = append(out, relationship)
	}
	return out, removed
}

func authorizationFragmentRelationshipMatchesSubjectResource(relationship AuthorizationDynamicFragmentRelationship, subject AuthorizationDynamicFragmentSubject, resource AuthorizationDynamicFragmentResource) bool {
	return relationship.Subject.Type == subject.Type &&
		relationship.Subject.ID == subject.ID &&
		relationship.Resource.Type == resource.Type &&
		relationship.Resource.ID == resource.ID
}

func normalizeAuthorizationFragmentOwner(owner AuthorizationFragmentOwner) AuthorizationFragmentOwner {
	owner.Kind = strings.TrimSpace(owner.Kind)
	owner.App = strings.TrimSpace(owner.App)
	if owner.Kind == "" && owner.App != "" {
		owner.Kind = AuthorizationFragmentOwnerKindApp
	}
	return owner
}

func validateAuthorizationFragmentOwner(owner AuthorizationFragmentOwner) error {
	switch owner.Kind {
	case AuthorizationFragmentOwnerKindGlobal:
		return nil
	case AuthorizationFragmentOwnerKindApp:
		if owner.App == "" {
			return fmt.Errorf("app owner requires app")
		}
		return nil
	default:
		return fmt.Errorf("unsupported owner kind %q", owner.Kind)
	}
}

func validateAuthorizationDynamicFragment(fragment *AuthorizationDynamicFragment) error {
	if err := validateAuthorizationFragmentOwner(fragment.Owner); err != nil {
		return err
	}
	for resourceType := range fragment.ResourceTypes {
		if strings.TrimSpace(resourceType) == "" {
			return fmt.Errorf("resourceTypes keys must be non-empty")
		}
		if err := validateAuthorizationFragmentResourceType(resourceType, fragment.ResourceTypes[resourceType]); err != nil {
			return fmt.Errorf("resourceTypes.%s: %w", resourceType, err)
		}
	}
	for i, relationship := range fragment.Relationships {
		if err := validateAuthorizationFragmentRelationship(normalizeAuthorizationFragmentRelationship(relationship)); err != nil {
			return fmt.Errorf("relationships[%d]: %w", i, err)
		}
	}
	return nil
}

func validateAuthorizationFragmentResourceType(name string, raw json.RawMessage) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("resource type name is required")
	}
	var def AuthorizationDynamicFragmentResourceTypeDef
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &def); err != nil {
			return err
		}
	}
	if len(def.Relations) == 0 {
		return fmt.Errorf("relations must define at least one relation")
	}
	relationNames := map[string]struct{}{}
	for relationName, relation := range def.Relations {
		relationName = strings.TrimSpace(relationName)
		if relationName == "" {
			return fmt.Errorf("relations keys must be non-empty")
		}
		if _, ok := relationNames[relationName]; ok {
			return fmt.Errorf("relations duplicate key after trimming %q", relationName)
		}
		relationNames[relationName] = struct{}{}
		if len(relation.SubjectTypes) == 0 && len(relation.AllowedTargets) == 0 {
			return fmt.Errorf("relations.%s must set subjectTypes or allowedTargets", relationName)
		}
		for i, target := range relation.AllowedTargets {
			if err := validateAuthorizationFragmentAllowedTarget(target); err != nil {
				return fmt.Errorf("relations.%s.allowedTargets[%d]: %w", relationName, i, err)
			}
		}
	}
	for actionName, action := range def.Actions {
		actionName = strings.TrimSpace(actionName)
		if actionName == "" {
			return fmt.Errorf("actions keys must be non-empty")
		}
		if len(action.Relations) == 0 {
			return fmt.Errorf("actions.%s.relations must contain at least one value", actionName)
		}
		for _, relation := range action.Relations {
			relation = strings.TrimSpace(relation)
			if relation == "" {
				return fmt.Errorf("actions.%s.relations must not contain empty values", actionName)
			}
			if _, ok := relationNames[relation]; !ok {
				return fmt.Errorf("actions.%s.relations references unknown relation %q", actionName, relation)
			}
		}
	}
	return nil
}

func validateAuthorizationFragmentAllowedTarget(target AuthorizationDynamicFragmentAllowedTarget) error {
	set := 0
	if strings.TrimSpace(target.SubjectType) != "" {
		set++
	}
	if strings.TrimSpace(target.ResourceType) != "" {
		set++
	}
	if target.SubjectSet != nil {
		set++
		if strings.TrimSpace(target.SubjectSet.ResourceType) == "" {
			return fmt.Errorf("subjectSet.resourceType is required")
		}
		if strings.TrimSpace(target.SubjectSet.Relation) == "" {
			return fmt.Errorf("subjectSet.relation is required")
		}
	}
	if set != 1 {
		return fmt.Errorf("must set exactly one of subjectType, resourceType, or subjectSet")
	}
	return nil
}

func validateAuthorizationFragmentRelationship(relationship AuthorizationDynamicFragmentRelationship) error {
	if relationship.Subject.Type == "" {
		return fmt.Errorf("subject.type is required")
	}
	if relationship.Subject.ID == "" {
		return fmt.Errorf("subject.id is required")
	}
	if relationship.Relation == "" {
		return fmt.Errorf("relation is required")
	}
	if relationship.Resource.Type == "" {
		return fmt.Errorf("resource.type is required")
	}
	if relationship.Resource.ID == "" {
		return fmt.Errorf("resource.id is required")
	}
	return nil
}

func authorizationFragmentOwnerID(owner AuthorizationFragmentOwner) string {
	switch owner.Kind {
	case AuthorizationFragmentOwnerKindApp:
		return strings.TrimSpace(owner.App)
	default:
		return AuthorizationFragmentOwnerKindGlobal
	}
}

func authorizationFragmentID(owner AuthorizationFragmentOwner) string {
	switch owner.Kind {
	case AuthorizationFragmentOwnerKindApp:
		return "app/" + strings.TrimSpace(owner.App)
	default:
		return AuthorizationFragmentOwnerKindGlobal
	}
}

func authorizationFragmentScope(owner AuthorizationFragmentOwner) string {
	if owner.Kind == AuthorizationFragmentOwnerKindApp {
		return AuthorizationFragmentScopeApp
	}
	return AuthorizationFragmentScopeGlobal
}

func authorizationFragmentPlugin(owner AuthorizationFragmentOwner) string {
	if owner.Kind == AuthorizationFragmentOwnerKindApp {
		return strings.TrimSpace(owner.App)
	}
	return ""
}

func defaultAuthorizationFragmentStatus(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return AuthorizationFragmentStatusActive
	}
	return status
}

func normalizeAuthorizationFragmentRelationship(relationship AuthorizationDynamicFragmentRelationship) AuthorizationDynamicFragmentRelationship {
	relationship.Subject.Type = strings.TrimSpace(relationship.Subject.Type)
	relationship.Subject.ID = strings.TrimSpace(relationship.Subject.ID)
	relationship.Relation = strings.TrimSpace(relationship.Relation)
	relationship.Resource.Type = strings.TrimSpace(relationship.Resource.Type)
	relationship.Resource.ID = strings.TrimSpace(relationship.Resource.ID)
	if relationship.Target.Subject != nil {
		relationship.Target.Subject.Type = strings.TrimSpace(relationship.Target.Subject.Type)
		relationship.Target.Subject.ID = strings.TrimSpace(relationship.Target.Subject.ID)
	}
	if relationship.Target.Resource != nil {
		relationship.Target.Resource.Type = strings.TrimSpace(relationship.Target.Resource.Type)
		relationship.Target.Resource.ID = strings.TrimSpace(relationship.Target.Resource.ID)
	}
	if relationship.Target.SubjectSet != nil {
		relationship.Target.SubjectSet.Resource.Type = strings.TrimSpace(relationship.Target.SubjectSet.Resource.Type)
		relationship.Target.SubjectSet.Resource.ID = strings.TrimSpace(relationship.Target.SubjectSet.Resource.ID)
		relationship.Target.SubjectSet.Relation = strings.TrimSpace(relationship.Target.SubjectSet.Relation)
	}
	if len(relationship.Properties) == 0 {
		relationship.Properties = nil
	}
	return relationship
}

func authorizationDynamicFragmentToRecord(fragment *AuthorizationDynamicFragment) indexeddb.Record {
	resourceTypesJSON, _ := json.Marshal(fragment.ResourceTypes)
	relationshipsJSON, _ := json.Marshal(fragment.Relationships)
	auditJSON, _ := json.Marshal(fragment.Audit)
	ownerID := authorizationFragmentOwnerID(fragment.Owner)
	return indexeddb.Record{
		"id":                  fragment.ID,
		"owner_kind":          fragment.Owner.Kind,
		"owner_id":            ownerID,
		"scope":               fragment.Scope,
		"app":              fragment.App,
		"version":             fragment.Version,
		"status":              fragment.Status,
		"resource_types_json": string(resourceTypesJSON),
		"relationships_json":  string(relationshipsJSON),
		"audit_json":          string(auditJSON),
		"created_at":          fragment.CreatedAt,
		"updated_at":          fragment.UpdatedAt,
	}
}

func recordToAuthorizationDynamicFragment(rec indexeddb.Record) (*AuthorizationDynamicFragment, error) {
	ownerKind := recString(rec, "owner_kind")
	ownerID := recString(rec, "owner_id")
	fragment := &AuthorizationDynamicFragment{
		ID:        recString(rec, "id"),
		Owner:     AuthorizationFragmentOwner{Kind: ownerKind},
		Scope:     recString(rec, "scope"),
		App:    recString(rec, "app"),
		Version:   recInt64(rec, "version"),
		Status:    recString(rec, "status"),
		CreatedAt: recTime(rec, "created_at"),
		UpdatedAt: recTime(rec, "updated_at"),
	}
	if ownerKind == AuthorizationFragmentOwnerKindApp {
		fragment.Owner.App = ownerID
	}
	if raw := recString(rec, "resource_types_json"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &fragment.ResourceTypes); err != nil {
			return nil, fmt.Errorf("decode authorization dynamic fragment resource types: %w", err)
		}
	}
	if raw := recString(rec, "relationships_json"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &fragment.Relationships); err != nil {
			return nil, fmt.Errorf("decode authorization dynamic fragment relationships: %w", err)
		}
	}
	if raw := recString(rec, "audit_json"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &fragment.Audit)
	}
	return fragment, nil
}

func cloneAuthorizationDynamicFragment(fragment *AuthorizationDynamicFragment) *AuthorizationDynamicFragment {
	if fragment == nil {
		return nil
	}
	data, _ := json.Marshal(fragment)
	var out AuthorizationDynamicFragment
	_ = json.Unmarshal(data, &out)
	return &out
}

func authorizationFragmentRelationshipKey(relationship AuthorizationDynamicFragmentRelationship) string {
	relationship = normalizeAuthorizationFragmentRelationship(relationship)
	return strings.Join([]string{
		authorizationFragmentSubjectKey(relationship.Subject),
		relationshipTargetKey(relationship.Target),
		relationship.Relation,
		relationship.Resource.Type,
		relationship.Resource.ID,
		relationshipPropertiesKey(relationship.Properties),
	}, "\x00")
}

func authorizationFragmentSubjectKey(subject AuthorizationDynamicFragmentSubject) string {
	return strings.Join([]string{"subject", subject.Type, subject.ID}, "\x00")
}

func relationshipTargetKey(target AuthorizationDynamicFragmentTarget) string {
	switch {
	case target.Subject != nil:
		return authorizationFragmentSubjectKey(*target.Subject)
	case target.Resource != nil:
		return strings.Join([]string{"resource", target.Resource.Type, target.Resource.ID}, "\x00")
	case target.SubjectSet != nil:
		return strings.Join([]string{"subject_set", target.SubjectSet.Resource.Type, target.SubjectSet.Resource.ID, target.SubjectSet.Relation}, "\x00")
	default:
		return ""
	}
}

func relationshipPropertiesKey(properties map[string]string) string {
	if len(properties) == 0 {
		return ""
	}
	type propertyPart struct {
		key   string
		value string
	}
	propertyParts := make([]propertyPart, 0, len(properties))
	for key, value := range properties {
		propertyParts = append(propertyParts, propertyPart{
			key:   strings.TrimSpace(key),
			value: strings.TrimSpace(value),
		})
	}
	sort.Slice(propertyParts, func(i, j int) bool {
		if propertyParts[i].key != propertyParts[j].key {
			return propertyParts[i].key < propertyParts[j].key
		}
		return propertyParts[i].value < propertyParts[j].value
	})
	parts := make([]string, 0, len(propertyParts)*2)
	for _, part := range propertyParts {
		parts = append(parts, part.key, part.value)
	}
	return strings.Join(parts, "\x00")
}

func sortAuthorizationFragmentRelationships(relationships []AuthorizationDynamicFragmentRelationship) {
	sort.Slice(relationships, func(i, j int) bool {
		return authorizationFragmentRelationshipKey(relationships[i]) < authorizationFragmentRelationshipKey(relationships[j])
	})
}
