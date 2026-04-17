package coredata

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type IdentityManagementGrantService struct {
	store indexeddb.ObjectStore
}

func NewIdentityManagementGrantService(ds indexeddb.IndexedDB) *IdentityManagementGrantService {
	return &IdentityManagementGrantService{store: ds.ObjectStore(StoreIdentityManagementGrants)}
}

func (s *IdentityManagementGrantService) UpsertGrant(ctx context.Context, grant *core.IdentityManagementGrant) (*core.IdentityManagementGrant, error) {
	if grant == nil {
		return nil, fmt.Errorf("upsert identity management grant: grant is required")
	}
	managerID := strings.TrimSpace(grant.ManagerIdentityID)
	targetID := strings.TrimSpace(grant.TargetIdentityID)
	role := strings.TrimSpace(grant.Role)
	if managerID == "" || targetID == "" || role == "" {
		return nil, fmt.Errorf("upsert identity management grant: manager_identity_id, target_identity_id, and role are required")
	}

	now := time.Now()
	id := grant.ID
	createdAt := grant.CreatedAt
	if existing, err := s.store.Index("by_manager_target").Get(ctx, managerID, targetID); err == nil {
		id = recString(existing, "id")
		if created := recTime(existing, "created_at"); !created.IsZero() {
			createdAt = created
		}
	} else if err != indexeddb.ErrNotFound {
		return nil, fmt.Errorf("upsert identity management grant: %w", err)
	}
	if id == "" {
		id = newRecordID()
	}
	if createdAt.IsZero() {
		createdAt = now
	}

	rec := indexeddb.Record{
		"id":                  id,
		"manager_identity_id": managerID,
		"target_identity_id":  targetID,
		"role":                role,
		"expires_at":          grant.ExpiresAt,
		"created_at":          createdAt,
		"updated_at":          now,
	}
	if err := s.store.Put(ctx, rec); err != nil {
		return nil, fmt.Errorf("upsert identity management grant: %w", err)
	}
	return recordToIdentityManagementGrant(rec), nil
}

func (s *IdentityManagementGrantService) GetGrant(ctx context.Context, managerIdentityID, targetIdentityID string) (*core.IdentityManagementGrant, error) {
	rec, err := s.store.Index("by_manager_target").Get(ctx, strings.TrimSpace(managerIdentityID), strings.TrimSpace(targetIdentityID))
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get identity management grant: %w", err)
	}
	return recordToIdentityManagementGrant(rec), nil
}

func (s *IdentityManagementGrantService) ListByTarget(ctx context.Context, targetIdentityID string) ([]*core.IdentityManagementGrant, error) {
	recs, err := s.store.Index("by_target").GetAll(ctx, nil, strings.TrimSpace(targetIdentityID))
	if err != nil {
		return nil, fmt.Errorf("list identity management grants: %w", err)
	}
	out := make([]*core.IdentityManagementGrant, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToIdentityManagementGrant(rec))
	}
	return out, nil
}

func (s *IdentityManagementGrantService) DeleteGrant(ctx context.Context, managerIdentityID, targetIdentityID string) error {
	deleted, err := s.store.Index("by_manager_target").Delete(ctx, strings.TrimSpace(managerIdentityID), strings.TrimSpace(targetIdentityID))
	if err != nil {
		return fmt.Errorf("delete identity management grant: %w", err)
	}
	if deleted == 0 {
		return core.ErrNotFound
	}
	return nil
}

func recordToIdentityManagementGrant(rec indexeddb.Record) *core.IdentityManagementGrant {
	return &core.IdentityManagementGrant{
		ID:                recString(rec, "id"),
		ManagerIdentityID: recString(rec, "manager_identity_id"),
		TargetIdentityID:  recString(rec, "target_identity_id"),
		Role:              recString(rec, "role"),
		ExpiresAt:         recTimePtr(rec, "expires_at"),
		CreatedAt:         recTime(rec, "created_at"),
		UpdatedAt:         recTime(rec, "updated_at"),
	}
}

type WorkspaceRoleService struct {
	store indexeddb.ObjectStore
}

func NewWorkspaceRoleService(ds indexeddb.IndexedDB) *WorkspaceRoleService {
	return &WorkspaceRoleService{store: ds.ObjectStore(StoreWorkspaceRoles)}
}

func (s *WorkspaceRoleService) UpsertRole(ctx context.Context, role *core.WorkspaceRole) (*core.WorkspaceRole, error) {
	if role == nil {
		return nil, fmt.Errorf("upsert workspace role: role is required")
	}
	identityID := strings.TrimSpace(role.IdentityID)
	roleName := strings.TrimSpace(role.Role)
	if identityID == "" || roleName == "" {
		return nil, fmt.Errorf("upsert workspace role: identity_id and role are required")
	}

	now := time.Now()
	id := role.ID
	createdAt := role.CreatedAt
	if existing, err := s.store.Index("by_identity_role").Get(ctx, identityID, roleName); err == nil {
		id = recString(existing, "id")
		if created := recTime(existing, "created_at"); !created.IsZero() {
			createdAt = created
		}
	} else if err != indexeddb.ErrNotFound {
		return nil, fmt.Errorf("upsert workspace role: %w", err)
	}
	if id == "" {
		id = newRecordID()
	}
	if createdAt.IsZero() {
		createdAt = now
	}

	rec := indexeddb.Record{
		"id":          id,
		"identity_id": identityID,
		"role":        roleName,
		"created_at":  createdAt,
		"updated_at":  now,
	}
	if err := s.store.Put(ctx, rec); err != nil {
		return nil, fmt.Errorf("upsert workspace role: %w", err)
	}
	return recordToWorkspaceRole(rec), nil
}

func (s *WorkspaceRoleService) ListByIdentity(ctx context.Context, identityID string) ([]*core.WorkspaceRole, error) {
	recs, err := s.store.Index("by_identity").GetAll(ctx, nil, strings.TrimSpace(identityID))
	if err != nil {
		return nil, fmt.Errorf("list workspace roles: %w", err)
	}
	out := make([]*core.WorkspaceRole, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToWorkspaceRole(rec))
	}
	return out, nil
}

func (s *WorkspaceRoleService) ListByPrincipal(ctx context.Context, identityID string) ([]*core.WorkspaceRole, error) {
	return s.ListByIdentity(ctx, identityID)
}

func (s *WorkspaceRoleService) DeleteRole(ctx context.Context, identityID, role string) error {
	deleted, err := s.store.Index("by_identity_role").Delete(ctx, strings.TrimSpace(identityID), strings.TrimSpace(role))
	if err != nil {
		return fmt.Errorf("delete workspace role: %w", err)
	}
	if deleted == 0 {
		return core.ErrNotFound
	}
	return nil
}

func recordToWorkspaceRole(rec indexeddb.Record) *core.WorkspaceRole {
	return &core.WorkspaceRole{
		ID:         recString(rec, "id"),
		IdentityID: recString(rec, "identity_id"),
		Role:       recString(rec, "role"),
		CreatedAt:  recTime(rec, "created_at"),
		UpdatedAt:  recTime(rec, "updated_at"),
	}
}

type IdentityPluginAccessService struct {
	store indexeddb.ObjectStore
}

func NewIdentityPluginAccessService(ds indexeddb.IndexedDB) *IdentityPluginAccessService {
	return &IdentityPluginAccessService{store: ds.ObjectStore(StoreIdentityPluginAccess)}
}

func (s *IdentityPluginAccessService) UpsertAccess(ctx context.Context, access *core.IdentityPluginAccess) (*core.IdentityPluginAccess, error) {
	if access == nil {
		return nil, fmt.Errorf("upsert identity plugin access: access is required")
	}
	identityID := strings.TrimSpace(access.IdentityID)
	plugin := strings.TrimSpace(access.Plugin)
	if identityID == "" || plugin == "" {
		return nil, fmt.Errorf("upsert identity plugin access: identity_id and plugin are required")
	}

	now := time.Now()
	id := access.ID
	createdAt := access.CreatedAt
	if existing, err := s.store.Index("by_identity_plugin").Get(ctx, identityID, plugin); err == nil {
		id = recString(existing, "id")
		if created := recTime(existing, "created_at"); !created.IsZero() {
			createdAt = created
		}
	} else if err != indexeddb.ErrNotFound {
		return nil, fmt.Errorf("upsert identity plugin access: %w", err)
	}
	if id == "" {
		id = newRecordID()
	}
	if createdAt.IsZero() {
		createdAt = now
	}
	operations := canonicalOperations(access.Operations)
	rec, err := identityPluginAccessRecord(&core.IdentityPluginAccess{
		ID:                  id,
		IdentityID:          identityID,
		Plugin:              plugin,
		InvokeAllOperations: access.InvokeAllOperations,
		Operations:          operations,
		ExpiresAt:           access.ExpiresAt,
		CreatedAt:           createdAt,
		UpdatedAt:           now,
	})
	if err != nil {
		return nil, err
	}
	if err := s.store.Put(ctx, rec); err != nil {
		return nil, fmt.Errorf("upsert identity plugin access: %w", err)
	}
	return recordToIdentityPluginAccess(rec)
}

func (s *IdentityPluginAccessService) GetAccess(ctx context.Context, identityID, plugin string) (*core.IdentityPluginAccess, error) {
	rec, err := s.store.Index("by_identity_plugin").Get(ctx, strings.TrimSpace(identityID), strings.TrimSpace(plugin))
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get identity plugin access: %w", err)
	}
	return recordToIdentityPluginAccess(rec)
}

func (s *IdentityPluginAccessService) ListByIdentity(ctx context.Context, identityID string) ([]*core.IdentityPluginAccess, error) {
	recs, err := s.store.Index("by_identity").GetAll(ctx, nil, strings.TrimSpace(identityID))
	if err != nil {
		return nil, fmt.Errorf("list identity plugin access: %w", err)
	}
	out := make([]*core.IdentityPluginAccess, 0, len(recs))
	for _, rec := range recs {
		access, err := recordToIdentityPluginAccess(rec)
		if err != nil {
			return nil, err
		}
		out = append(out, access)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Plugin < out[j].Plugin
	})
	return out, nil
}

func (s *IdentityPluginAccessService) DeleteAccess(ctx context.Context, identityID, plugin string) error {
	deleted, err := s.store.Index("by_identity_plugin").Delete(ctx, strings.TrimSpace(identityID), strings.TrimSpace(plugin))
	if err != nil {
		return fmt.Errorf("delete identity plugin access: %w", err)
	}
	if deleted == 0 {
		return core.ErrNotFound
	}
	return nil
}

func identityPluginAccessRecord(access *core.IdentityPluginAccess) (indexeddb.Record, error) {
	operationsJSON := ""
	if len(access.Operations) > 0 {
		b, err := json.Marshal(access.Operations)
		if err != nil {
			return nil, fmt.Errorf("marshal identity plugin access operations: %w", err)
		}
		operationsJSON = string(b)
	}
	return indexeddb.Record{
		"id":                    access.ID,
		"identity_id":           access.IdentityID,
		"plugin":                access.Plugin,
		"invoke_all_operations": access.InvokeAllOperations,
		"operations_json":       operationsJSON,
		"expires_at":            access.ExpiresAt,
		"created_at":            access.CreatedAt,
		"updated_at":            access.UpdatedAt,
	}, nil
}

func recordToIdentityPluginAccess(rec indexeddb.Record) (*core.IdentityPluginAccess, error) {
	access := &core.IdentityPluginAccess{
		ID:                  recString(rec, "id"),
		IdentityID:          recString(rec, "identity_id"),
		Plugin:              recString(rec, "plugin"),
		InvokeAllOperations: recBool(rec, "invoke_all_operations"),
		ExpiresAt:           recTimePtr(rec, "expires_at"),
		CreatedAt:           recTime(rec, "created_at"),
		UpdatedAt:           recTime(rec, "updated_at"),
	}
	if raw := recString(rec, "operations_json"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &access.Operations); err != nil {
			return nil, fmt.Errorf("decode identity plugin access operations: %w", err)
		}
	}
	return access, nil
}

type APITokenAccessService struct {
	store indexeddb.ObjectStore
}

func NewAPITokenAccessService(ds indexeddb.IndexedDB) *APITokenAccessService {
	return &APITokenAccessService{store: ds.ObjectStore(StoreAPITokenAccess)}
}

func (s *APITokenAccessService) ReplaceForToken(ctx context.Context, tokenID string, access []core.APITokenAccess) error {
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return fmt.Errorf("replace api token access: token_id is required")
	}
	existing, err := s.store.Index("by_token").GetAll(ctx, nil, tokenID)
	if err != nil {
		return fmt.Errorf("replace api token access: %w", err)
	}
	for _, rec := range existing {
		if err := s.store.Delete(ctx, recString(rec, "id")); err != nil && err != indexeddb.ErrNotFound {
			return fmt.Errorf("replace api token access: %w", err)
		}
	}
	now := time.Now()
	for i := range access {
		item := access[i]
		rec, err := apiTokenAccessRecord(&core.APITokenAccess{
			ID:                  newRecordID(),
			TokenID:             tokenID,
			Plugin:              strings.TrimSpace(item.Plugin),
			InvokeAllOperations: item.InvokeAllOperations,
			Operations:          canonicalOperations(item.Operations),
			ExpiresAt:           item.ExpiresAt,
			CreatedAt:           now,
			UpdatedAt:           now,
		})
		if err != nil {
			return err
		}
		if err := s.store.Put(ctx, rec); err != nil {
			return fmt.Errorf("replace api token access: %w", err)
		}
	}
	return nil
}

func (s *APITokenAccessService) ListByToken(ctx context.Context, tokenID string) ([]*core.APITokenAccess, error) {
	recs, err := s.store.Index("by_token").GetAll(ctx, nil, strings.TrimSpace(tokenID))
	if err != nil {
		return nil, fmt.Errorf("list api token access: %w", err)
	}
	out := make([]*core.APITokenAccess, 0, len(recs))
	for _, rec := range recs {
		access, err := recordToAPITokenAccess(rec)
		if err != nil {
			return nil, err
		}
		out = append(out, access)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Plugin < out[j].Plugin
	})
	return out, nil
}

func apiTokenAccessRecord(access *core.APITokenAccess) (indexeddb.Record, error) {
	operationsJSON := ""
	if len(access.Operations) > 0 {
		b, err := json.Marshal(access.Operations)
		if err != nil {
			return nil, fmt.Errorf("marshal api token access operations: %w", err)
		}
		operationsJSON = string(b)
	}
	return indexeddb.Record{
		"id":                    access.ID,
		"token_id":              access.TokenID,
		"plugin":                access.Plugin,
		"invoke_all_operations": access.InvokeAllOperations,
		"operations_json":       operationsJSON,
		"expires_at":            access.ExpiresAt,
		"created_at":            access.CreatedAt,
		"updated_at":            access.UpdatedAt,
	}, nil
}

func recordToAPITokenAccess(rec indexeddb.Record) (*core.APITokenAccess, error) {
	access := &core.APITokenAccess{
		ID:                  recString(rec, "id"),
		TokenID:             recString(rec, "token_id"),
		Plugin:              recString(rec, "plugin"),
		InvokeAllOperations: recBool(rec, "invoke_all_operations"),
		ExpiresAt:           recTimePtr(rec, "expires_at"),
		CreatedAt:           recTime(rec, "created_at"),
		UpdatedAt:           recTime(rec, "updated_at"),
	}
	if raw := recString(rec, "operations_json"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &access.Operations); err != nil {
			return nil, fmt.Errorf("decode api token access operations: %w", err)
		}
	}
	return access, nil
}

func canonicalOperations(operations []string) []string {
	if len(operations) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(operations))
	out := make([]string, 0, len(operations))
	for _, operation := range operations {
		name := strings.TrimSpace(operation)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
