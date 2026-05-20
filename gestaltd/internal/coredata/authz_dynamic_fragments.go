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
	AuthorizationFragmentOwnerKindPlugin = "plugin"

	AuthorizationFragmentScopeGlobal = "global"
	AuthorizationFragmentScopePlugin = "plugin"

	AuthorizationFragmentStatusActive = "active"
)

type AuthorizationDynamicFragmentService struct {
	store indexeddb.ObjectStore
}

type AuthorizationFragmentOwner struct {
	Kind   string `json:"kind"`
	Plugin string `json:"plugin,omitempty"`
}

type AuthorizationDynamicFragment struct {
	ID            string                                     `json:"id"`
	Owner         AuthorizationFragmentOwner                 `json:"owner"`
	Scope         string                                     `json:"scope"`
	Plugin        string                                     `json:"plugin,omitempty"`
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

func NewAuthorizationDynamicFragmentService(ds indexeddb.IndexedDB) *AuthorizationDynamicFragmentService {
	return &AuthorizationDynamicFragmentService{store: ds.ObjectStore(StoreAuthorizationDynamicFragments)}
}

func AuthorizationGlobalFragmentOwner() AuthorizationFragmentOwner {
	return AuthorizationFragmentOwner{Kind: AuthorizationFragmentOwnerKindGlobal}
}

func AuthorizationPluginFragmentOwner(plugin string) AuthorizationFragmentOwner {
	return AuthorizationFragmentOwner{Kind: AuthorizationFragmentOwnerKindPlugin, Plugin: strings.TrimSpace(plugin)}
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
	fragment.Plugin = authorizationFragmentPlugin(fragment.Owner)
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
		out := make([]AuthorizationDynamicFragmentRelationship, 0, len(fragment.Relationships)+1)
		for _, existing := range fragment.Relationships {
			existing = normalizeAuthorizationFragmentRelationship(existing)
			if existing.Subject.Type == relationship.Subject.Type &&
				existing.Subject.ID == relationship.Subject.ID &&
				existing.Resource.Type == relationship.Resource.Type &&
				existing.Resource.ID == relationship.Resource.ID {
				continue
			}
			out = append(out, existing)
		}
		out = append(out, relationship)
		fragment.Relationships = out
		sortAuthorizationFragmentRelationships(fragment.Relationships)
		return nil
	})
}

func (s *AuthorizationDynamicFragmentService) DeleteRelationship(ctx context.Context, owner AuthorizationFragmentOwner, relationship AuthorizationDynamicFragmentRelationship, audit AuthorizationDynamicFragmentAuditMetadata) (bool, *AuthorizationDynamicFragment, error) {
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
	return deleted, fragment, err
}

func normalizeAuthorizationFragmentOwner(owner AuthorizationFragmentOwner) AuthorizationFragmentOwner {
	owner.Kind = strings.TrimSpace(owner.Kind)
	owner.Plugin = strings.TrimSpace(owner.Plugin)
	if owner.Kind == "" && owner.Plugin != "" {
		owner.Kind = AuthorizationFragmentOwnerKindPlugin
	}
	return owner
}

func validateAuthorizationFragmentOwner(owner AuthorizationFragmentOwner) error {
	switch owner.Kind {
	case AuthorizationFragmentOwnerKindGlobal:
		return nil
	case AuthorizationFragmentOwnerKindPlugin:
		if owner.Plugin == "" {
			return fmt.Errorf("plugin owner requires plugin")
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
	}
	for i, relationship := range fragment.Relationships {
		if err := validateAuthorizationFragmentRelationship(normalizeAuthorizationFragmentRelationship(relationship)); err != nil {
			return fmt.Errorf("relationships[%d]: %w", i, err)
		}
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
	case AuthorizationFragmentOwnerKindPlugin:
		return strings.TrimSpace(owner.Plugin)
	default:
		return AuthorizationFragmentOwnerKindGlobal
	}
}

func authorizationFragmentID(owner AuthorizationFragmentOwner) string {
	switch owner.Kind {
	case AuthorizationFragmentOwnerKindPlugin:
		return "plugin/" + strings.TrimSpace(owner.Plugin)
	default:
		return AuthorizationFragmentOwnerKindGlobal
	}
}

func authorizationFragmentScope(owner AuthorizationFragmentOwner) string {
	if owner.Kind == AuthorizationFragmentOwnerKindPlugin {
		return AuthorizationFragmentScopePlugin
	}
	return AuthorizationFragmentScopeGlobal
}

func authorizationFragmentPlugin(owner AuthorizationFragmentOwner) string {
	if owner.Kind == AuthorizationFragmentOwnerKindPlugin {
		return strings.TrimSpace(owner.Plugin)
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
		"plugin":              fragment.Plugin,
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
		Plugin:    recString(rec, "plugin"),
		Version:   recInt64(rec, "version"),
		Status:    recString(rec, "status"),
		CreatedAt: recTime(rec, "created_at"),
		UpdatedAt: recTime(rec, "updated_at"),
	}
	if ownerKind == AuthorizationFragmentOwnerKindPlugin {
		fragment.Owner.Plugin = ownerID
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
	return relationship.Subject.Type + ":" + relationship.Subject.ID + "|" + relationship.Relation + "|" + relationship.Resource.Type + ":" + relationship.Resource.ID
}

func sortAuthorizationFragmentRelationships(relationships []AuthorizationDynamicFragmentRelationship) {
	sort.Slice(relationships, func(i, j int) bool {
		return authorizationFragmentRelationshipKey(relationships[i]) < authorizationFragmentRelationshipKey(relationships[j])
	})
}

func recInt64(rec indexeddb.Record, key string) int64 {
	v := rec[key]
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	default:
		return 0
	}
}
