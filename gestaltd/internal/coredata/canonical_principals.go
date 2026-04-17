package coredata

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/emailutil"
)

const principalStatusActive = "active"
const legacySharedServiceAccountPrincipalID = "__identity__"

type PrincipalService struct {
	store indexeddb.ObjectStore
}

func NewPrincipalService(ds indexeddb.IndexedDB) *PrincipalService {
	return &PrincipalService{store: ds.ObjectStore(StorePrincipals)}
}

func (s *PrincipalService) UpsertPrincipal(ctx context.Context, principal *core.Principal) (*core.Principal, error) {
	if principal == nil {
		return nil, fmt.Errorf("upsert principal: principal is required")
	}
	id := strings.TrimSpace(principal.ID)
	kind := strings.TrimSpace(principal.Kind)
	displayName := strings.TrimSpace(principal.DisplayName)
	if id == "" || kind == "" || displayName == "" {
		return nil, fmt.Errorf("upsert principal: id, kind, and display_name are required")
	}
	status := strings.TrimSpace(principal.Status)
	if status == "" {
		status = principalStatusActive
	}

	now := time.Now()
	createdAt := principal.CreatedAt
	if existing, err := s.store.Get(ctx, id); err == nil {
		if created := recTime(existing, "created_at"); !created.IsZero() {
			createdAt = created
		}
	} else if err != indexeddb.ErrNotFound {
		return nil, fmt.Errorf("upsert principal: %w", err)
	}
	if createdAt.IsZero() {
		createdAt = now
	}

	rec := indexeddb.Record{
		"id":           id,
		"kind":         kind,
		"status":       status,
		"display_name": displayName,
		"created_at":   createdAt,
		"updated_at":   now,
	}
	if err := s.store.Put(ctx, rec); err != nil {
		return nil, fmt.Errorf("upsert principal: %w", err)
	}
	return recordToPrincipal(rec), nil
}

func (s *PrincipalService) GetPrincipal(ctx context.Context, id string) (*core.Principal, error) {
	rec, err := s.store.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get principal: %w", err)
	}
	return recordToPrincipal(rec), nil
}

func (s *PrincipalService) DeletePrincipal(ctx context.Context, id string) error {
	if err := s.store.Delete(ctx, strings.TrimSpace(id)); err != nil {
		if err == indexeddb.ErrNotFound {
			return core.ErrNotFound
		}
		return fmt.Errorf("delete principal: %w", err)
	}
	return nil
}

func recordToPrincipal(rec indexeddb.Record) *core.Principal {
	return &core.Principal{
		ID:          recString(rec, "id"),
		Kind:        recString(rec, "kind"),
		Status:      recString(rec, "status"),
		DisplayName: recString(rec, "display_name"),
		CreatedAt:   recTime(rec, "created_at"),
		UpdatedAt:   recTime(rec, "updated_at"),
	}
}

type UserProfileService struct {
	store indexeddb.ObjectStore
}

func NewUserProfileService(ds indexeddb.IndexedDB) *UserProfileService {
	return &UserProfileService{store: ds.ObjectStore(StoreUserProfiles)}
}

func (s *UserProfileService) UpsertProfile(ctx context.Context, profile *core.UserProfile) (*core.UserProfile, error) {
	if profile == nil {
		return nil, fmt.Errorf("upsert user profile: profile is required")
	}
	principalID := strings.TrimSpace(profile.PrincipalID)
	email := emailutil.Normalize(profile.Email)
	normalizedEmail := emailutil.Normalize(profile.NormalizedEmail)
	if normalizedEmail == "" {
		normalizedEmail = email
	}
	if principalID == "" || email == "" || normalizedEmail == "" {
		return nil, fmt.Errorf("upsert user profile: principal_id and email are required")
	}

	now := time.Now()
	createdAt := profile.CreatedAt
	if existing, err := s.store.Get(ctx, principalID); err == nil {
		if created := recTime(existing, "created_at"); !created.IsZero() {
			createdAt = created
		}
	} else if err != indexeddb.ErrNotFound {
		return nil, fmt.Errorf("upsert user profile: %w", err)
	}
	if createdAt.IsZero() {
		createdAt = now
	}

	rec := indexeddb.Record{
		"id":               principalID,
		"principal_id":     principalID,
		"email":            email,
		"normalized_email": normalizedEmail,
		"avatar_url":       strings.TrimSpace(profile.AvatarURL),
		"created_at":       createdAt,
		"updated_at":       now,
	}
	authProvider := strings.TrimSpace(profile.AuthProvider)
	authSubject := strings.TrimSpace(profile.AuthSubject)
	if authProvider != "" && authSubject != "" {
		rec["auth_provider"] = authProvider
		rec["auth_subject"] = authSubject
	}
	if err := s.store.Put(ctx, rec); err != nil {
		return nil, fmt.Errorf("upsert user profile: %w", err)
	}
	return recordToUserProfile(rec), nil
}

func (s *UserProfileService) GetProfile(ctx context.Context, principalID string) (*core.UserProfile, error) {
	rec, err := s.store.Get(ctx, strings.TrimSpace(principalID))
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get user profile: %w", err)
	}
	return recordToUserProfile(rec), nil
}

func recordToUserProfile(rec indexeddb.Record) *core.UserProfile {
	return &core.UserProfile{
		PrincipalID:     recString(rec, "principal_id"),
		Email:           recString(rec, "email"),
		NormalizedEmail: recString(rec, "normalized_email"),
		AuthProvider:    recString(rec, "auth_provider"),
		AuthSubject:     recString(rec, "auth_subject"),
		AvatarURL:       recString(rec, "avatar_url"),
		CreatedAt:       recTime(rec, "created_at"),
		UpdatedAt:       recTime(rec, "updated_at"),
	}
}

type ServiceAccountService struct {
	store indexeddb.ObjectStore
}

func NewServiceAccountService(ds indexeddb.IndexedDB) *ServiceAccountService {
	return &ServiceAccountService{store: ds.ObjectStore(StoreServiceAccounts)}
}

func (s *ServiceAccountService) UpsertServiceAccount(ctx context.Context, sa *core.ServiceAccount) (*core.ServiceAccount, error) {
	if sa == nil {
		return nil, fmt.Errorf("upsert service account: service account is required")
	}
	principalID := strings.TrimSpace(sa.PrincipalID)
	name := strings.TrimSpace(sa.Name)
	if principalID == "" || name == "" {
		return nil, fmt.Errorf("upsert service account: principal_id and name are required")
	}

	now := time.Now()
	createdAt := sa.CreatedAt
	if existing, err := s.store.Get(ctx, principalID); err == nil {
		if created := recTime(existing, "created_at"); !created.IsZero() {
			createdAt = created
		}
	} else if err != indexeddb.ErrNotFound {
		return nil, fmt.Errorf("upsert service account: %w", err)
	}
	if createdAt.IsZero() {
		createdAt = now
	}

	rec := indexeddb.Record{
		"id":                      principalID,
		"principal_id":            principalID,
		"name":                    name,
		"description":             strings.TrimSpace(sa.Description),
		"created_by_principal_id": strings.TrimSpace(sa.CreatedByPrincipalID),
		"created_at":              createdAt,
		"updated_at":              now,
	}
	if err := s.store.Put(ctx, rec); err != nil {
		return nil, fmt.Errorf("upsert service account: %w", err)
	}
	return recordToServiceAccount(rec), nil
}

func (s *ServiceAccountService) GetServiceAccount(ctx context.Context, principalID string) (*core.ServiceAccount, error) {
	rec, err := s.store.Get(ctx, strings.TrimSpace(principalID))
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get service account: %w", err)
	}
	return recordToServiceAccount(rec), nil
}

func (s *ServiceAccountService) DeleteServiceAccount(ctx context.Context, principalID string) error {
	if err := s.store.Delete(ctx, strings.TrimSpace(principalID)); err != nil {
		if err == indexeddb.ErrNotFound {
			return core.ErrNotFound
		}
		return fmt.Errorf("delete service account: %w", err)
	}
	return nil
}

func recordToServiceAccount(rec indexeddb.Record) *core.ServiceAccount {
	return &core.ServiceAccount{
		PrincipalID:          recString(rec, "principal_id"),
		Name:                 recString(rec, "name"),
		Description:          recString(rec, "description"),
		CreatedByPrincipalID: recString(rec, "created_by_principal_id"),
		CreatedAt:            recTime(rec, "created_at"),
		UpdatedAt:            recTime(rec, "updated_at"),
	}
}

func newRecordID() string {
	return uuid.NewString()
}

func ensureLegacySharedServiceAccount(ctx context.Context, principals *PrincipalService, serviceAccounts *ServiceAccountService) error {
	if principals == nil || serviceAccounts == nil {
		return nil
	}
	if _, err := principals.UpsertPrincipal(ctx, &core.Principal{
		ID:          legacySharedServiceAccountPrincipalID,
		Kind:        core.PrincipalKindServiceAccount,
		Status:      principalStatusActive,
		DisplayName: "Legacy Shared Identity",
	}); err != nil {
		return fmt.Errorf("seed legacy shared service-account principal: %w", err)
	}
	if _, err := serviceAccounts.UpsertServiceAccount(ctx, &core.ServiceAccount{
		PrincipalID: legacySharedServiceAccountPrincipalID,
		Name:        "legacy-shared-identity",
		Description: "Compatibility shim for legacy __identity__-owned credentials",
	}); err != nil {
		return fmt.Errorf("seed legacy shared service account: %w", err)
	}
	return nil
}
