package coredata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

var (
	ErrPublishSessionConflict      = errors.New("publish session conflict")
	ErrPublishSessionNotFound      = errors.New("publish session not found")
	ErrPublishSessionTerminal      = errors.New("publish session is terminal")
	ErrPublishSessionVersionLocked = errors.New("publish version conflict")
)

type AppRegistryPublishSessionService struct {
	db    indexeddb.IndexedDB
	store idb.ObjectStore
}

func NewAppRegistryPublishSessionService(ds indexeddb.IndexedDB) *AppRegistryPublishSessionService {
	return &AppRegistryPublishSessionService{
		db:    ds,
		store: ds.ObjectStore(StoreAppRegistryPublishSessions),
	}
}

func (s *AppRegistryPublishSessionService) EnsureStore(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("ensure app registry publish sessions store: service is not configured")
	}
	_, err := s.db.CreateObjectStore(ctx, StoreAppRegistryPublishSessions, AppRegistryPublishSessionsSchema)
	if err != nil {
		return fmt.Errorf("ensure app registry publish sessions store: %w", err)
	}
	return nil
}

func (s *AppRegistryPublishSessionService) Get(ctx context.Context, id string) (*core.AppRegistryPublishSession, error) {
	if s == nil {
		return nil, fmt.Errorf("get app registry publish session: service is not configured")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("get app registry publish session: id is required")
	}
	rec, err := s.store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get app registry publish session: %w", err)
	}
	return recordToAppRegistryPublishSession(rec), nil
}

func (s *AppRegistryPublishSessionService) GetByDedupeKey(ctx context.Context, dedupeKey string) (*core.AppRegistryPublishSession, error) {
	if s == nil {
		return nil, fmt.Errorf("get app registry publish session by dedupe key: service is not configured")
	}
	dedupeKey = strings.TrimSpace(dedupeKey)
	if dedupeKey == "" {
		return nil, fmt.Errorf("get app registry publish session by dedupe key: dedupe key is required")
	}
	recs, err := s.store.Index("by_dedupe_key").GetAll(ctx, dedupeKey)
	if err != nil {
		return nil, fmt.Errorf("get app registry publish session by dedupe key: %w", err)
	}
	if len(recs) == 0 {
		return nil, core.ErrNotFound
	}
	return recordToAppRegistryPublishSession(recs[0]), nil
}

func (s *AppRegistryPublishSessionService) ListActiveByApp(ctx context.Context, app string) ([]*core.AppRegistryPublishSession, error) {
	if s == nil {
		return nil, fmt.Errorf("list active app registry publish sessions: service is not configured")
	}
	app = strings.TrimSpace(app)
	if app == "" {
		return nil, fmt.Errorf("list active app registry publish sessions: app is required")
	}
	recs, err := s.store.Index("by_app").GetAll(ctx, app)
	if err != nil {
		return nil, fmt.Errorf("list active app registry publish sessions: %w", err)
	}
	out := make([]*core.AppRegistryPublishSession, 0, len(recs))
	for _, rec := range recs {
		session := recordToAppRegistryPublishSession(rec)
		if session == nil || session.State.Terminal() {
			continue
		}
		out = append(out, session)
	}
	return out, nil
}

type CreateAppRegistryPublishSessionInput struct {
	App                string
	Registry           string
	Version            string
	DedupeKey          string
	DeclarationDigest  string
	DeclarationJSON    []byte
	PublisherSubjectID string
	Artifacts          []core.AppRegistryPublishArtifact
	UploadLeases       []core.AppRegistryUploadLease
	StagingPrefix      string
	PublishStartedAt   time.Time
}

func (s *AppRegistryPublishSessionService) Create(ctx context.Context, input CreateAppRegistryPublishSessionInput) (*core.AppRegistryPublishSession, error) {
	return s.CreateActive(ctx, input)
}

func appRegistryPublishSessionRecord(session *core.AppRegistryPublishSession) idb.Record {
	artifactsJSON, _ := json.Marshal(session.Artifacts)
	leasesJSON, _ := json.Marshal(session.UploadLeases)
	rec := idb.Record{
		"id":                   session.ID,
		"app":                  session.App,
		"registry":             session.Registry,
		"version":              session.Version,
		"dedupe_key":           session.DedupeKey,
		"declaration_digest":   session.DeclarationDigest,
		"declaration_json":     jsonValue(session.DeclarationJSON),
		"state":                string(session.State),
		"publisher_subject_id": session.PublisherSubjectID,
		"artifacts_json":       jsonValue(artifactsJSON),
		"upload_leases_json":   jsonValue(leasesJSON),
		"staging_prefix":       session.StagingPrefix,
		"failure_reason":       strings.TrimSpace(session.FailureReason),
		"publish_started_at":   session.PublishStartedAt,
		"created_at":           session.CreatedAt,
		"updated_at":           session.UpdatedAt,
		"revision":             session.Revision,
	}
	if !session.PublishedAt.IsZero() {
		rec["published_at"] = session.PublishedAt
	}
	if !session.StagingMarkedStale.IsZero() {
		rec["staging_marked_stale_at"] = session.StagingMarkedStale
	}
	if token := strings.TrimSpace(session.FinalizeClaimToken); token != "" {
		rec["finalize_claim_token"] = token
	}
	if !session.FinalizeClaimExpiresAt.IsZero() {
		rec["finalize_claim_expires_at"] = session.FinalizeClaimExpiresAt
	}
	if !session.FinalizePublishedAt.IsZero() {
		rec["finalize_published_at"] = session.FinalizePublishedAt
	}
	return rec
}

func recordToAppRegistryPublishSession(rec idb.Record) *core.AppRegistryPublishSession {
	artifacts := decodePublishArtifacts(recJSON(rec, "artifacts_json"))
	leases := decodeUploadLeases(recJSON(rec, "upload_leases_json"))
	return &core.AppRegistryPublishSession{
		ID:                     recString(rec, "id"),
		App:                    recString(rec, "app"),
		Registry:               recString(rec, "registry"),
		Version:                recString(rec, "version"),
		DedupeKey:              recString(rec, "dedupe_key"),
		DeclarationDigest:      recString(rec, "declaration_digest"),
		DeclarationJSON:        append([]byte(nil), recJSON(rec, "declaration_json")...),
		State:                  core.AppRegistryPublishSessionState(recString(rec, "state")),
		PublisherSubjectID:     recString(rec, "publisher_subject_id"),
		Artifacts:              artifacts,
		UploadLeases:           leases,
		StagingPrefix:          recString(rec, "staging_prefix"),
		FailureReason:          recString(rec, "failure_reason"),
		PublishStartedAt:       recTime(rec, "publish_started_at"),
		CreatedAt:              recTime(rec, "created_at"),
		UpdatedAt:              recTime(rec, "updated_at"),
		Revision:               recInt64(rec, "revision"),
		PublishedAt:            recTime(rec, "published_at"),
		StagingMarkedStale:     recTime(rec, "staging_marked_stale_at"),
		FinalizeClaimToken:     recString(rec, "finalize_claim_token"),
		FinalizeClaimExpiresAt: recTime(rec, "finalize_claim_expires_at"),
		FinalizePublishedAt:    recTime(rec, "finalize_published_at"),
	}
}

func decodePublishArtifacts(raw []byte) []core.AppRegistryPublishArtifact {
	if len(raw) == 0 {
		return nil
	}
	var artifacts []core.AppRegistryPublishArtifact
	if err := json.Unmarshal(raw, &artifacts); err != nil {
		return nil
	}
	return artifacts
}

func decodeUploadLeases(raw []byte) []core.AppRegistryUploadLease {
	if len(raw) == 0 {
		return nil
	}
	var leases []core.AppRegistryUploadLease
	if err := json.Unmarshal(raw, &leases); err != nil {
		return nil
	}
	return leases
}
