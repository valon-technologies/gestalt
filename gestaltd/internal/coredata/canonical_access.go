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

type ServiceAccountManagementGrantService struct {
	store indexeddb.ObjectStore
}

func NewServiceAccountManagementGrantService(ds indexeddb.IndexedDB) *ServiceAccountManagementGrantService {
	return &ServiceAccountManagementGrantService{store: ds.ObjectStore(StoreServiceAccountManagementGrants)}
}

func (s *ServiceAccountManagementGrantService) UpsertGrant(ctx context.Context, grant *core.ServiceAccountManagementGrant) (*core.ServiceAccountManagementGrant, error) {
	if grant == nil {
		return nil, fmt.Errorf("upsert service account management grant: grant is required")
	}
	memberID := strings.TrimSpace(grant.MemberPrincipalID)
	targetID := strings.TrimSpace(grant.TargetServiceAccountPrincipalID)
	role := strings.TrimSpace(grant.Role)
	if memberID == "" || targetID == "" || role == "" {
		return nil, fmt.Errorf("upsert service account management grant: member_principal_id, target_service_account_principal_id, and role are required")
	}

	now := time.Now()
	id := grant.ID
	createdAt := grant.CreatedAt
	if existing, err := s.store.Index("by_member_target").Get(ctx, memberID, targetID); err == nil {
		id = recString(existing, "id")
		if created := recTime(existing, "created_at"); !created.IsZero() {
			createdAt = created
		}
	} else if err != indexeddb.ErrNotFound {
		return nil, fmt.Errorf("upsert service account management grant: %w", err)
	}
	if id == "" {
		id = newRecordID()
	}
	if createdAt.IsZero() {
		createdAt = now
	}

	rec := indexeddb.Record{
		"id":                                  id,
		"member_principal_id":                 memberID,
		"target_service_account_principal_id": targetID,
		"role":                                role,
		"expires_at":                          grant.ExpiresAt,
		"created_at":                          createdAt,
		"updated_at":                          now,
	}
	if err := s.store.Put(ctx, rec); err != nil {
		return nil, fmt.Errorf("upsert service account management grant: %w", err)
	}
	return recordToServiceAccountManagementGrant(rec), nil
}

func (s *ServiceAccountManagementGrantService) GetGrant(ctx context.Context, memberPrincipalID, targetPrincipalID string) (*core.ServiceAccountManagementGrant, error) {
	rec, err := s.store.Index("by_member_target").Get(ctx, strings.TrimSpace(memberPrincipalID), strings.TrimSpace(targetPrincipalID))
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get service account management grant: %w", err)
	}
	return recordToServiceAccountManagementGrant(rec), nil
}

func (s *ServiceAccountManagementGrantService) ListByTarget(ctx context.Context, targetPrincipalID string) ([]*core.ServiceAccountManagementGrant, error) {
	recs, err := s.store.Index("by_target").GetAll(ctx, nil, strings.TrimSpace(targetPrincipalID))
	if err != nil {
		return nil, fmt.Errorf("list service account management grants: %w", err)
	}
	out := make([]*core.ServiceAccountManagementGrant, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToServiceAccountManagementGrant(rec))
	}
	return out, nil
}

func (s *ServiceAccountManagementGrantService) DeleteGrant(ctx context.Context, memberPrincipalID, targetPrincipalID string) error {
	deleted, err := s.store.Index("by_member_target").Delete(ctx, strings.TrimSpace(memberPrincipalID), strings.TrimSpace(targetPrincipalID))
	if err != nil {
		return fmt.Errorf("delete service account management grant: %w", err)
	}
	if deleted == 0 {
		return core.ErrNotFound
	}
	return nil
}

func recordToServiceAccountManagementGrant(rec indexeddb.Record) *core.ServiceAccountManagementGrant {
	return &core.ServiceAccountManagementGrant{
		ID:                              recString(rec, "id"),
		MemberPrincipalID:               recString(rec, "member_principal_id"),
		TargetServiceAccountPrincipalID: recString(rec, "target_service_account_principal_id"),
		Role:                            recString(rec, "role"),
		ExpiresAt:                       recTimePtr(rec, "expires_at"),
		CreatedAt:                       recTime(rec, "created_at"),
		UpdatedAt:                       recTime(rec, "updated_at"),
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
	principalID := strings.TrimSpace(role.PrincipalID)
	roleName := strings.TrimSpace(role.Role)
	if principalID == "" || roleName == "" {
		return nil, fmt.Errorf("upsert workspace role: principal_id and role are required")
	}

	now := time.Now()
	id := role.ID
	createdAt := role.CreatedAt
	if existing, err := s.store.Index("by_principal_role").Get(ctx, principalID, roleName); err == nil {
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
		"id":           id,
		"principal_id": principalID,
		"role":         roleName,
		"created_at":   createdAt,
		"updated_at":   now,
	}
	if err := s.store.Put(ctx, rec); err != nil {
		return nil, fmt.Errorf("upsert workspace role: %w", err)
	}
	return recordToWorkspaceRole(rec), nil
}

func (s *WorkspaceRoleService) ListByPrincipal(ctx context.Context, principalID string) ([]*core.WorkspaceRole, error) {
	recs, err := s.store.Index("by_principal").GetAll(ctx, nil, strings.TrimSpace(principalID))
	if err != nil {
		return nil, fmt.Errorf("list workspace roles: %w", err)
	}
	out := make([]*core.WorkspaceRole, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToWorkspaceRole(rec))
	}
	return out, nil
}

func (s *WorkspaceRoleService) DeleteRole(ctx context.Context, principalID, role string) error {
	deleted, err := s.store.Index("by_principal_role").Delete(ctx, strings.TrimSpace(principalID), strings.TrimSpace(role))
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
		ID:          recString(rec, "id"),
		PrincipalID: recString(rec, "principal_id"),
		Role:        recString(rec, "role"),
		CreatedAt:   recTime(rec, "created_at"),
		UpdatedAt:   recTime(rec, "updated_at"),
	}
}

type PrincipalPluginAccessService struct {
	store indexeddb.ObjectStore
}

func NewPrincipalPluginAccessService(ds indexeddb.IndexedDB) *PrincipalPluginAccessService {
	return &PrincipalPluginAccessService{store: ds.ObjectStore(StorePrincipalPluginAccess)}
}

func (s *PrincipalPluginAccessService) UpsertAccess(ctx context.Context, access *core.PrincipalPluginAccess) (*core.PrincipalPluginAccess, error) {
	if access == nil {
		return nil, fmt.Errorf("upsert principal plugin access: access is required")
	}
	principalID := strings.TrimSpace(access.PrincipalID)
	plugin := strings.TrimSpace(access.Plugin)
	if principalID == "" || plugin == "" {
		return nil, fmt.Errorf("upsert principal plugin access: principal_id and plugin are required")
	}

	now := time.Now()
	id := access.ID
	createdAt := access.CreatedAt
	if existing, err := s.store.Index("by_principal_plugin").Get(ctx, principalID, plugin); err == nil {
		id = recString(existing, "id")
		if created := recTime(existing, "created_at"); !created.IsZero() {
			createdAt = created
		}
	} else if err != indexeddb.ErrNotFound {
		return nil, fmt.Errorf("upsert principal plugin access: %w", err)
	}
	if id == "" {
		id = newRecordID()
	}
	if createdAt.IsZero() {
		createdAt = now
	}
	operations := canonicalOperations(access.Operations)
	rec, err := principalPluginAccessRecord(&core.PrincipalPluginAccess{
		ID:                  id,
		PrincipalID:         principalID,
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
		return nil, fmt.Errorf("upsert principal plugin access: %w", err)
	}
	return recordToPrincipalPluginAccess(rec)
}

func (s *PrincipalPluginAccessService) GetAccess(ctx context.Context, principalID, plugin string) (*core.PrincipalPluginAccess, error) {
	rec, err := s.store.Index("by_principal_plugin").Get(ctx, strings.TrimSpace(principalID), strings.TrimSpace(plugin))
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get principal plugin access: %w", err)
	}
	return recordToPrincipalPluginAccess(rec)
}

func (s *PrincipalPluginAccessService) ListByPrincipal(ctx context.Context, principalID string) ([]*core.PrincipalPluginAccess, error) {
	recs, err := s.store.Index("by_principal").GetAll(ctx, nil, strings.TrimSpace(principalID))
	if err != nil {
		return nil, fmt.Errorf("list principal plugin access: %w", err)
	}
	out := make([]*core.PrincipalPluginAccess, 0, len(recs))
	for _, rec := range recs {
		access, err := recordToPrincipalPluginAccess(rec)
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

func (s *PrincipalPluginAccessService) DeleteAccess(ctx context.Context, principalID, plugin string) error {
	deleted, err := s.store.Index("by_principal_plugin").Delete(ctx, strings.TrimSpace(principalID), strings.TrimSpace(plugin))
	if err != nil {
		return fmt.Errorf("delete principal plugin access: %w", err)
	}
	if deleted == 0 {
		return core.ErrNotFound
	}
	return nil
}

func principalPluginAccessRecord(access *core.PrincipalPluginAccess) (indexeddb.Record, error) {
	operationsJSON := ""
	if len(access.Operations) > 0 {
		b, err := json.Marshal(access.Operations)
		if err != nil {
			return nil, fmt.Errorf("marshal principal plugin access operations: %w", err)
		}
		operationsJSON = string(b)
	}
	return indexeddb.Record{
		"id":                    access.ID,
		"principal_id":          access.PrincipalID,
		"plugin":                access.Plugin,
		"invoke_all_operations": access.InvokeAllOperations,
		"operations_json":       operationsJSON,
		"expires_at":            access.ExpiresAt,
		"created_at":            access.CreatedAt,
		"updated_at":            access.UpdatedAt,
	}, nil
}

func recordToPrincipalPluginAccess(rec indexeddb.Record) (*core.PrincipalPluginAccess, error) {
	access := &core.PrincipalPluginAccess{
		ID:                  recString(rec, "id"),
		PrincipalID:         recString(rec, "principal_id"),
		Plugin:              recString(rec, "plugin"),
		InvokeAllOperations: recBool(rec, "invoke_all_operations"),
		ExpiresAt:           recTimePtr(rec, "expires_at"),
		CreatedAt:           recTime(rec, "created_at"),
		UpdatedAt:           recTime(rec, "updated_at"),
	}
	if raw := recString(rec, "operations_json"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &access.Operations); err != nil {
			return nil, fmt.Errorf("decode principal plugin access operations: %w", err)
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
