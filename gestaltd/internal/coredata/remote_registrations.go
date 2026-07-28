package coredata

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

var (
	ErrGenerationMismatch     = errors.New("remote registration generation mismatch")
	ErrProviderOwnedElsewhere = errors.New("remote provider owned by another registration")
	ErrNotRegistered          = errors.New("remote provider is not registered")
)

const remoteProviderIDSeparator = "\x1f"

type RemoteRegistrationState string

const (
	RemoteRegistrationPreparing RemoteRegistrationState = "preparing"
	RemoteRegistrationActive    RemoteRegistrationState = "active"
	RemoteRegistrationDeleting  RemoteRegistrationState = "deleting"
)

type RemoteRegistration struct {
	ID                        string
	OwnerSubjectID            string
	Generation                uint64
	SessionID                 string
	State                     RemoteRegistrationState
	TunnelHost                string
	TunnelCertificate         []byte
	ServerSPKISHA256          string
	ConnectURL                string
	LeaseExpiresAt            time.Time
	CreatedAt                 time.Time
	UpdatedAt                 time.Time
	LastCheckedAt             *time.Time
	LastSuccessfulHeartbeatAt *time.Time
	LastError                 string
}

type RemoteProvider struct {
	ID             string
	ProviderKind   string
	ProviderName   string
	RegistrationID string
	Generation     uint64
	Definition     map[string]any
}

type RemoteRegistrationService struct {
	db  indexeddb.IndexedDB
	now func() time.Time
}

func NewRemoteRegistrationService(ds indexeddb.IndexedDB) *RemoteRegistrationService {
	return &RemoteRegistrationService{db: ds, now: time.Now}
}

func (s *RemoteRegistrationService) SetClock(now func() time.Time) {
	if s != nil && now != nil {
		s.now = now
	}
}

// Now returns the store's current time so callers computing lease deadlines
// share one notion of now with the store.
func (s *RemoteRegistrationService) Now() time.Time {
	if s == nil || s.now == nil {
		return time.Now().UTC().Truncate(time.Millisecond)
	}
	return normalizedRemoteTime(s.now())
}

func (s *RemoteRegistrationService) Replace(
	ctx context.Context,
	owner string,
	reg *RemoteRegistration,
	providers []*RemoteProvider,
	expectedGeneration uint64,
) (*RemoteRegistration, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("replace remote registration: service is not configured")
	}
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, fmt.Errorf("replace remote registration: owner is required")
	}
	if err := validateRemoteRegistration(reg); err != nil {
		return nil, fmt.Errorf("replace remote registration: %w", err)
	}
	if err := validateRemoteProviders(providers); err != nil {
		return nil, fmt.Errorf("replace remote registration: %w", err)
	}

	regStore := s.db.ObjectStore(StoreRemoteRegistrations)
	providerStore := s.db.ObjectStore(StoreRemoteProviders)

	existing, err := getRegistrationByOwner(ctx, regStore, owner)
	if err != nil {
		return nil, fmt.Errorf("replace remote registration: load owner registration: %w", err)
	}

	now := normalizedRemoteTime(s.now())
	if existing != nil && !registrationActive(existing, now) {
		if err := deleteProvidersForRegistration(ctx, providerStore, existing.ID); err != nil {
			return nil, fmt.Errorf("replace remote registration: sweep expired: %w", err)
		}
		if err := regStore.Delete(ctx, existing.ID); err != nil && !errors.Is(err, idb.ErrNotFound) {
			return nil, fmt.Errorf("replace remote registration: sweep expired: %w", err)
		}
		existing = nil
	}

	var storedGeneration uint64
	var registrationID string
	if existing == nil {
		if expectedGeneration != 0 {
			return nil, ErrGenerationMismatch
		}
		registrationID = strings.TrimSpace(reg.ID)
		if registrationID == "" {
			return nil, fmt.Errorf("replace remote registration: id is required")
		}
	} else {
		storedGeneration = existing.Generation
		if storedGeneration != expectedGeneration {
			return nil, ErrGenerationMismatch
		}
		if incomingID := strings.TrimSpace(reg.ID); incomingID != "" && incomingID != existing.ID {
			return nil, fmt.Errorf("replace remote registration: registration id mismatch")
		}
		registrationID = existing.ID
	}

	if err := s.ensureProviderOwnership(ctx, providerStore, regStore, providers, registrationID, now); err != nil {
		return nil, err
	}

	if err := deleteProvidersForRegistration(ctx, providerStore, registrationID); err != nil {
		return nil, fmt.Errorf("replace remote registration: sweep providers: %w", err)
	}

	now = normalizedRemoteTime(s.now())
	nextGeneration := storedGeneration + 1
	leaseExpiresAt := normalizedRemoteTime(reg.LeaseExpiresAt)
	if leaseExpiresAt.IsZero() {
		return nil, fmt.Errorf("replace remote registration: lease_expires_at is required")
	}

	stored := &RemoteRegistration{
		ID:                        registrationID,
		OwnerSubjectID:            owner,
		Generation:                nextGeneration,
		TunnelHost:                strings.TrimSpace(reg.TunnelHost),
		TunnelCertificate:         append([]byte(nil), reg.TunnelCertificate...),
		ServerSPKISHA256:          strings.TrimSpace(reg.ServerSPKISHA256),
		ConnectURL:                strings.TrimSpace(reg.ConnectURL),
		LeaseExpiresAt:            leaseExpiresAt,
		CreatedAt:                 now,
		UpdatedAt:                 now,
		LastSuccessfulHeartbeatAt: &now,
	}
	if existing != nil {
		stored.CreatedAt = existing.CreatedAt
	}

	tx, err := s.db.Transaction(ctx, []string{StoreRemoteRegistrations, StoreRemoteProviders}, idb.TransactionReadwrite, idb.TransactionOptions{})
	if err != nil {
		return nil, fmt.Errorf("replace remote registration: begin write transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()

	current, err := getRegistrationByID(ctx, tx.ObjectStore(StoreRemoteRegistrations), registrationID)
	if err != nil {
		return nil, fmt.Errorf("replace remote registration: re-check: %w", err)
	}
	if existing == nil {
		if current != nil {
			return nil, ErrGenerationMismatch
		}
	} else if current == nil || current.Generation != storedGeneration {
		return nil, ErrGenerationMismatch
	}

	if err := tx.ObjectStore(StoreRemoteRegistrations).Put(ctx, remoteRegistrationRecord(stored)); err != nil {
		return nil, fmt.Errorf("replace remote registration: store registration: %w", err)
	}
	for _, provider := range providers {
		if err := tx.ObjectStore(StoreRemoteProviders).Put(ctx, remoteProviderRecord(
			provider.ProviderKind,
			provider.ProviderName,
			registrationID,
			nextGeneration,
			provider.Definition,
		)); err != nil {
			return nil, fmt.Errorf("replace remote registration: store provider %s/%s: %w", provider.ProviderKind, provider.ProviderName, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("replace remote registration: commit: %w", err)
	}
	committed = true
	return stored, nil
}

func (s *RemoteRegistrationService) RenewLease(ctx context.Context, registrationID string, generation uint64, leaseDuration time.Duration) error {
	if leaseDuration <= 0 {
		return fmt.Errorf("renew remote registration lease: lease duration must be positive")
	}
	return s.updateRegistrationAtGeneration(ctx, "renew remote registration lease", registrationID, generation, func(reg *RemoteRegistration, now time.Time) {
		reg.UpdatedAt = now
		reg.LastSuccessfulHeartbeatAt = &now
		reg.LeaseExpiresAt = normalizedRemoteTime(now.Add(leaseDuration))
	})
}

func (s *RemoteRegistrationService) RecordCheckFailure(ctx context.Context, registrationID string, generation uint64, errMessage string) error {
	lastError := sanitizeRemoteRegistrationError(errMessage)
	return s.updateRegistrationAtGeneration(ctx, "record remote registration check failure", registrationID, generation, func(reg *RemoteRegistration, now time.Time) {
		reg.UpdatedAt = now
		reg.LastCheckedAt = &now
		reg.LastError = lastError
	})
}

func (s *RemoteRegistrationService) Delete(ctx context.Context, registrationID string, expectedGeneration uint64) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("delete remote registration: service is not configured")
	}
	registrationID = strings.TrimSpace(registrationID)
	if registrationID == "" {
		return fmt.Errorf("delete remote registration: registration id is required")
	}

	regStore := s.db.ObjectStore(StoreRemoteRegistrations)
	reg, err := getRegistrationByID(ctx, regStore, registrationID)
	if err != nil {
		return fmt.Errorf("delete remote registration: %w", err)
	}
	if reg == nil {
		return ErrNotRegistered
	}
	if reg.Generation != expectedGeneration {
		return ErrGenerationMismatch
	}
	return s.removeRegistrationWithGenerationCheck(ctx, registrationID, expectedGeneration)
}

func (s *RemoteRegistrationService) Expire(ctx context.Context, registrationID string, generation uint64, observedDeadline time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("expire remote registration: service is not configured")
	}
	registrationID = strings.TrimSpace(registrationID)
	if registrationID == "" {
		return fmt.Errorf("expire remote registration: registration id is required")
	}
	observedDeadline = normalizedRemoteTime(observedDeadline)
	if observedDeadline.IsZero() {
		return fmt.Errorf("expire remote registration: observed deadline is required")
	}

	regStore := s.db.ObjectStore(StoreRemoteRegistrations)
	reg, err := getRegistrationByID(ctx, regStore, registrationID)
	if err != nil {
		return fmt.Errorf("expire remote registration: %w", err)
	}
	if reg == nil || reg.Generation != generation || !reg.LeaseExpiresAt.Equal(observedDeadline) || normalizedRemoteTime(s.now()).Before(reg.LeaseExpiresAt) {
		return nil
	}
	return s.removeRegistrationWithGenerationCheck(ctx, registrationID, generation)
}

func (s *RemoteRegistrationService) ResolveProvider(ctx context.Context, kind, name string) (*RemoteProvider, *RemoteRegistration, error) {
	if s == nil || s.db == nil {
		return nil, nil, fmt.Errorf("resolve remote provider: service is not configured")
	}
	kind = providermanifestv1.NormalizeKind(kind)
	name = strings.TrimSpace(name)
	if kind == "" || name == "" {
		return nil, nil, fmt.Errorf("resolve remote provider: kind and name are required")
	}

	providerRec, err := s.db.ObjectStore(StoreRemoteProviders).Get(ctx, remoteProviderID(kind, name))
	if errors.Is(err, idb.ErrNotFound) {
		return nil, nil, ErrNotRegistered
	}
	if err != nil {
		return nil, nil, fmt.Errorf("resolve remote provider: load provider: %w", err)
	}
	provider := recordToRemoteProvider(providerRec)

	regRec, err := s.db.ObjectStore(StoreRemoteRegistrations).Get(ctx, provider.RegistrationID)
	if errors.Is(err, idb.ErrNotFound) {
		return nil, nil, ErrNotRegistered
	}
	if err != nil {
		return nil, nil, fmt.Errorf("resolve remote provider: load registration: %w", err)
	}
	reg := recordToRemoteRegistration(regRec)
	if provider.Generation != reg.Generation || !registrationActive(reg, normalizedRemoteTime(s.now())) {
		return nil, nil, ErrNotRegistered
	}
	return provider, reg, nil
}

func (s *RemoteRegistrationService) Get(ctx context.Context, registrationID string) (*RemoteRegistration, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("get remote registration: service is not configured")
	}
	registrationID = strings.TrimSpace(registrationID)
	if registrationID == "" {
		return nil, fmt.Errorf("get remote registration: registration id is required")
	}

	rec, err := s.db.ObjectStore(StoreRemoteRegistrations).Get(ctx, registrationID)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil, ErrNotRegistered
		}
		return nil, fmt.Errorf("get remote registration: %w", err)
	}
	return recordToRemoteRegistration(rec), nil
}

func (s *RemoteRegistrationService) ListByOwner(ctx context.Context, ownerSubjectID string) (*RemoteRegistration, []*RemoteProvider, error) {
	if s == nil || s.db == nil {
		return nil, nil, fmt.Errorf("list remote registration by owner: service is not configured")
	}
	ownerSubjectID = strings.TrimSpace(ownerSubjectID)
	if ownerSubjectID == "" {
		return nil, nil, fmt.Errorf("list remote registration by owner: owner is required")
	}

	regRec, err := s.db.ObjectStore(StoreRemoteRegistrations).Index("by_owner_subject").Get(ctx, ownerSubjectID)
	if errors.Is(err, idb.ErrNotFound) {
		return nil, nil, ErrNotRegistered
	}
	if err != nil {
		return nil, nil, fmt.Errorf("list remote registration by owner: %w", err)
	}
	reg := recordToRemoteRegistration(regRec)
	if !registrationActive(reg, normalizedRemoteTime(s.now())) {
		return nil, nil, ErrNotRegistered
	}

	providerRecs, err := s.db.ObjectStore(StoreRemoteProviders).Index("by_registration").GetAll(ctx, reg.ID)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			providerRecs = nil
		} else {
			return nil, nil, fmt.Errorf("list remote registration by owner: %w", err)
		}
	}
	providers := make([]*RemoteProvider, 0, len(providerRecs))
	for _, rec := range providerRecs {
		providers = append(providers, recordToRemoteProvider(rec))
	}
	return reg, providers, nil
}

func (s *RemoteRegistrationService) updateRegistrationAtGeneration(
	ctx context.Context,
	op string,
	registrationID string,
	generation uint64,
	mutate func(reg *RemoteRegistration, now time.Time),
) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("%s: service is not configured", op)
	}
	registrationID = strings.TrimSpace(registrationID)
	if registrationID == "" {
		return fmt.Errorf("%s: registration id is required", op)
	}

	store := s.db.ObjectStore(StoreRemoteRegistrations)
	reg, err := getRegistrationByID(ctx, store, registrationID)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if reg == nil {
		return ErrNotRegistered
	}
	if reg.Generation != generation {
		return ErrGenerationMismatch
	}
	now := normalizedRemoteTime(s.now())
	if !registrationActive(reg, now) {
		return ErrNotRegistered
	}

	mutate(reg, now)

	tx, err := s.db.Transaction(ctx, []string{StoreRemoteRegistrations}, idb.TransactionReadwrite, idb.TransactionOptions{})
	if err != nil {
		return fmt.Errorf("%s: begin write transaction: %w", op, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()
	current, err := getRegistrationByID(ctx, tx.ObjectStore(StoreRemoteRegistrations), registrationID)
	if err != nil {
		return fmt.Errorf("%s: re-check: %w", op, err)
	}
	if current == nil || current.Generation != generation {
		return ErrGenerationMismatch
	}
	if err := tx.ObjectStore(StoreRemoteRegistrations).Put(ctx, remoteRegistrationRecord(reg)); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%s: commit: %w", op, err)
	}
	committed = true
	return nil
}

func getRegistrationByOwner(ctx context.Context, store idb.ObjectStore, owner string) (*RemoteRegistration, error) {
	rec, err := store.Index("by_owner_subject").Get(ctx, owner)
	if errors.Is(err, idb.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return recordToRemoteRegistration(rec), nil
}

func getRegistrationByID(ctx context.Context, store interface {
	Get(context.Context, string) (idb.Record, error)
}, registrationID string) (*RemoteRegistration, error) {
	rec, err := store.Get(ctx, registrationID)
	if errors.Is(err, idb.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return recordToRemoteRegistration(rec), nil
}

func (s *RemoteRegistrationService) ensureProviderOwnership(ctx context.Context, store, regStore idb.ObjectStore, providers []*RemoteProvider, registrationID string, now time.Time) error {
	for _, provider := range providers {
		rec, err := store.Get(ctx, remoteProviderID(provider.ProviderKind, provider.ProviderName))
		if errors.Is(err, idb.ErrNotFound) {
			continue
		}
		if err != nil {
			return fmt.Errorf("check provider ownership: %w", err)
		}
		owner := recordToRemoteProvider(rec)
		if owner.RegistrationID == registrationID {
			continue
		}
		ownerReg, err := getRegistrationByID(ctx, regStore, owner.RegistrationID)
		if err != nil {
			return fmt.Errorf("check provider ownership: load owner registration: %w", err)
		}
		if ownerReg != nil && registrationActive(ownerReg, now) {
			return ErrProviderOwnedElsewhere
		}
		if err := deleteProvidersForRegistration(ctx, store, owner.RegistrationID); err != nil {
			return fmt.Errorf("check provider ownership: sweep expired owner: %w", err)
		}
		if err := regStore.Delete(ctx, owner.RegistrationID); err != nil && !errors.Is(err, idb.ErrNotFound) {
			return fmt.Errorf("check provider ownership: sweep expired owner: %w", err)
		}
	}
	return nil
}

func (s *RemoteRegistrationService) removeRegistrationWithGenerationCheck(ctx context.Context, registrationID string, expectedGeneration uint64) error {
	regStore := s.db.ObjectStore(StoreRemoteRegistrations)
	providerStore := s.db.ObjectStore(StoreRemoteProviders)
	if err := deleteProvidersForRegistration(ctx, providerStore, registrationID); err != nil {
		return fmt.Errorf("sweep providers: %w", err)
	}
	if err := regStore.Delete(ctx, registrationID); err != nil && !errors.Is(err, idb.ErrNotFound) {
		return err
	}
	return nil
}

func deleteProvidersForRegistration(ctx context.Context, store idb.ObjectStore, registrationID string) error {
	recs, err := store.Index("by_registration").GetAll(ctx, registrationID)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil
		}
		return err
	}
	for _, rec := range recs {
		if err := store.Delete(ctx, recString(rec, "id")); err != nil && !errors.Is(err, idb.ErrNotFound) {
			return err
		}
	}
	return nil
}

func validateRemoteRegistration(reg *RemoteRegistration) error {
	if reg == nil {
		return fmt.Errorf("registration is required")
	}
	if strings.TrimSpace(reg.TunnelHost) == "" {
		return fmt.Errorf("tunnel_host is required")
	}
	if len(reg.TunnelCertificate) == 0 {
		return fmt.Errorf("tunnel_certificate is required")
	}
	if strings.TrimSpace(reg.ServerSPKISHA256) == "" {
		return fmt.Errorf("server_spki_sha256 is required")
	}
	return nil
}

func validateRemoteProviders(providers []*RemoteProvider) error {
	if len(providers) == 0 {
		return fmt.Errorf("at least one provider is required")
	}
	seen := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		if provider == nil {
			return fmt.Errorf("provider is required")
		}
		kind := providermanifestv1.NormalizeKind(provider.ProviderKind)
		name := strings.TrimSpace(provider.ProviderName)
		if kind == "" || name == "" {
			return fmt.Errorf("provider kind and name are required")
		}
		switch kind {
		case providermanifestv1.KindIdentity, providermanifestv1.KindAuthorization:
			return fmt.Errorf("provider kind %q cannot be registered remotely", kind)
		}
		id := remoteProviderID(kind, name)
		if _, ok := seen[id]; ok {
			return fmt.Errorf("duplicate provider %s/%s", kind, name)
		}
		seen[id] = struct{}{}
		if len(provider.Definition) == 0 {
			return fmt.Errorf("provider definition is required for %s/%s", kind, name)
		}
	}
	return nil
}

func remoteProviderID(kind, name string) string {
	return providermanifestv1.NormalizeKind(kind) + remoteProviderIDSeparator + strings.TrimSpace(name)
}

func normalizedRemoteTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Millisecond)
}

// registrationActive reports whether a registration's lease is still alive and
// its state allows routing; reads and writes share it so an expired or
// non-active registration resolves as unregistered uniformly.
func registrationActive(reg *RemoteRegistration, now time.Time) bool {
	if reg == nil || !reg.LeaseExpiresAt.After(now) {
		return false
	}
	// Legacy registrations have an empty state and are treated as active.
	state := reg.State
	if state == "" {
		state = RemoteRegistrationActive
	}
	return state == RemoteRegistrationActive
}

var (
	remoteRegistrationBearerPattern = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]+`)
	remoteRegistrationTokenPattern  = regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password)\s*[:=]\s*\S+`)
)

func sanitizeRemoteRegistrationError(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}
	msg = remoteRegistrationBearerPattern.ReplaceAllString(msg, "[redacted]")
	msg = remoteRegistrationTokenPattern.ReplaceAllString(msg, "[redacted]")
	const maxLen = 512
	if len(msg) > maxLen {
		msg = strings.ToValidUTF8(msg[:maxLen], "")
	}
	return msg
}

func remoteRegistrationRecord(reg *RemoteRegistration) idb.Record {
	state := reg.State
	if state == "" {
		state = RemoteRegistrationActive
	}
	rec := idb.Record{
		"id":                 reg.ID,
		"owner_subject_id":   reg.OwnerSubjectID,
		"generation":         reg.Generation,
		"session_id":         reg.SessionID,
		"state":              string(state),
		"tunnel_host":        reg.TunnelHost,
		"tunnel_certificate": append([]byte(nil), reg.TunnelCertificate...),
		"server_spki_sha256": reg.ServerSPKISHA256,
		"connect_url":        reg.ConnectURL,
		"lease_expires_at":   reg.LeaseExpiresAt,
		"created_at":         reg.CreatedAt,
		"updated_at":         reg.UpdatedAt,
		"last_error":         reg.LastError,
	}
	if reg.LastCheckedAt != nil {
		rec["last_checked_at"] = *reg.LastCheckedAt
	}
	if reg.LastSuccessfulHeartbeatAt != nil {
		rec["last_successful_heartbeat_at"] = *reg.LastSuccessfulHeartbeatAt
	}
	return rec
}

func remoteProviderRecord(kind, name, registrationID string, generation uint64, definition map[string]any) idb.Record {
	kind = providermanifestv1.NormalizeKind(kind)
	name = strings.TrimSpace(name)
	return idb.Record{
		"id":              remoteProviderID(kind, name),
		"provider_kind":   kind,
		"provider_name":   name,
		"registration_id": registrationID,
		"generation":      generation,
		"definition":      jsonValue(definition),
	}
}

func recordToRemoteRegistration(rec idb.Record) *RemoteRegistration {
	state := RemoteRegistrationState(recString(rec, "state"))
	if state == "" {
		state = RemoteRegistrationActive
	}
	return &RemoteRegistration{
		ID:                        recString(rec, "id"),
		OwnerSubjectID:            recString(rec, "owner_subject_id"),
		Generation:                recUint64(rec, "generation"),
		SessionID:                 recString(rec, "session_id"),
		State:                     state,
		TunnelHost:                recString(rec, "tunnel_host"),
		TunnelCertificate:         recBytes(rec, "tunnel_certificate"),
		ServerSPKISHA256:          recString(rec, "server_spki_sha256"),
		ConnectURL:                recString(rec, "connect_url"),
		LeaseExpiresAt:            recTime(rec, "lease_expires_at"),
		CreatedAt:                 recTime(rec, "created_at"),
		UpdatedAt:                 recTime(rec, "updated_at"),
		LastCheckedAt:             recTimePtr(rec, "last_checked_at"),
		LastSuccessfulHeartbeatAt: recTimePtr(rec, "last_successful_heartbeat_at"),
		LastError:                 recString(rec, "last_error"),
	}
}

func recordToRemoteProvider(rec idb.Record) *RemoteProvider {
	return &RemoteProvider{
		ID:             recString(rec, "id"),
		ProviderKind:   recString(rec, "provider_kind"),
		ProviderName:   recString(rec, "provider_name"),
		RegistrationID: recString(rec, "registration_id"),
		Generation:     recUint64(rec, "generation"),
		Definition:     recAnyMap(rec, "definition"),
	}
}
