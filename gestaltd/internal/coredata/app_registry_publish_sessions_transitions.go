package coredata

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
)

var (
	ErrPublishSessionStateConflict    = errors.New("publish session state conflict")
	ErrPublishSessionFinalizeConflict = errors.New("publish session finalize conflict")
	ErrPublishSessionClaimMismatch    = errors.New("publish session finalize claim mismatch")
)

type PublishSessionTransition struct {
	ExpectedStates []core.AppRegistryPublishSessionState
	ExpectUpdated  time.Time
	Mutate         func(*core.AppRegistryPublishSession) error
}

func (s *AppRegistryPublishSessionService) Transition(ctx context.Context, id string, transition PublishSessionTransition) (*core.AppRegistryPublishSession, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("transition app registry publish session: service is not configured")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("transition app registry publish session: id is required")
	}
	if transition.Mutate == nil {
		return nil, fmt.Errorf("transition app registry publish session: mutate function is required")
	}
	if err := s.EnsureStore(ctx); err != nil {
		return nil, err
	}

	tx, err := s.db.Transaction(ctx, []string{StoreAppRegistryPublishSessions}, idb.TransactionReadwrite, idb.TransactionOptions{})
	if err != nil {
		return nil, fmt.Errorf("transition app registry publish session: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()

	store := tx.ObjectStore(StoreAppRegistryPublishSessions)
	rec, err := store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("transition app registry publish session: load: %w", err)
	}
	session := recordToAppRegistryPublishSession(rec)
	if !publishSessionStateAllowed(session.State, transition.ExpectedStates) {
		return nil, fmt.Errorf("%w: session %q is %s", ErrPublishSessionStateConflict, id, session.State)
	}
	if !transition.ExpectUpdated.IsZero() && !session.UpdatedAt.Equal(transition.ExpectUpdated.UTC().Truncate(time.Millisecond)) {
		return nil, fmt.Errorf("%w: session %q revision mismatch", ErrPublishSessionStateConflict, id)
	}
	if err := transition.Mutate(session); err != nil {
		return nil, err
	}
	session.UpdatedAt = time.Now().UTC().Truncate(time.Millisecond)
	if err := store.Put(ctx, appRegistryPublishSessionRecord(session)); err != nil {
		return nil, fmt.Errorf("transition app registry publish session: write: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("transition app registry publish session: commit: %w", err)
	}
	committed = true
	return session, nil
}

func (s *AppRegistryPublishSessionService) RenewFinalizeClaim(ctx context.Context, id, claimToken string, expectUpdated time.Time, leaseTTL time.Duration) (*core.AppRegistryPublishSession, error) {
	claimToken = strings.TrimSpace(claimToken)
	if claimToken == "" {
		return nil, fmt.Errorf("%w: session %q claim token is required", ErrPublishSessionClaimMismatch, id)
	}
	return s.Transition(ctx, id, PublishSessionTransition{
		ExpectedStates: []core.AppRegistryPublishSessionState{core.AppRegistryPublishSessionFinalizing},
		ExpectUpdated:  expectUpdated,
		Mutate: func(current *core.AppRegistryPublishSession) error {
			if err := requireFinalizeClaimToken(current, claimToken); err != nil {
				return err
			}
			now := time.Now().UTC().Truncate(time.Millisecond)
			if current.FinalizeClaimExpiresAt.IsZero() || !current.FinalizeClaimExpiresAt.After(now) {
				return fmt.Errorf("%w: session %q finalize claim expired", ErrPublishSessionFinalizeConflict, id)
			}
			current.FinalizeClaimExpiresAt = now.Add(normalizeFinalizeClaimLeaseTTL(leaseTTL))
			return nil
		},
	})
}

func (s *AppRegistryPublishSessionService) ClaimFinalize(ctx context.Context, id string, leaseTTL time.Duration) (*core.AppRegistryPublishSession, error) {
	session, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	switch session.State {
	case core.AppRegistryPublishSessionPublished:
		return session, nil
	case core.AppRegistryPublishSessionFailed:
		return nil, fmt.Errorf("%w: %s", ErrPublishSessionTerminal, strings.TrimSpace(session.FailureReason))
	case core.AppRegistryPublishSessionFinalizing:
		now := time.Now().UTC().Truncate(time.Millisecond)
		if !session.FinalizeClaimExpiresAt.IsZero() && session.FinalizeClaimExpiresAt.After(now) {
			return nil, fmt.Errorf("%w: session %q is already finalizing", ErrPublishSessionFinalizeConflict, id)
		}
		return s.Transition(ctx, id, PublishSessionTransition{
			ExpectedStates: []core.AppRegistryPublishSessionState{core.AppRegistryPublishSessionFinalizing},
			ExpectUpdated:  session.UpdatedAt,
			Mutate: func(current *core.AppRegistryPublishSession) error {
				current.FinalizeClaimToken = newFinalizeClaimToken()
				current.FinalizeClaimExpiresAt = now.Add(normalizeFinalizeClaimLeaseTTL(leaseTTL))
				return nil
			},
		})
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	publishedAt := now
	return s.Transition(ctx, id, PublishSessionTransition{
		ExpectedStates: []core.AppRegistryPublishSessionState{
			core.AppRegistryPublishSessionCreated,
			core.AppRegistryPublishSessionUploading,
		},
		ExpectUpdated: session.UpdatedAt,
		Mutate: func(current *core.AppRegistryPublishSession) error {
			current.State = core.AppRegistryPublishSessionFinalizing
			current.FinalizeClaimToken = newFinalizeClaimToken()
			current.FinalizeClaimExpiresAt = now.Add(normalizeFinalizeClaimLeaseTTL(leaseTTL))
			current.FinalizePublishedAt = publishedAt
			return nil
		},
	})
}

func (s *AppRegistryPublishSessionService) MarkPublished(ctx context.Context, id, claimToken string, expectUpdated, publishedAt time.Time) (*core.AppRegistryPublishSession, error) {
	return s.Transition(ctx, id, PublishSessionTransition{
		ExpectedStates: []core.AppRegistryPublishSessionState{core.AppRegistryPublishSessionFinalizing},
		ExpectUpdated:  expectUpdated,
		Mutate: func(current *core.AppRegistryPublishSession) error {
			if err := requireFinalizeClaimToken(current, claimToken); err != nil {
				return err
			}
			markAt := publishedAt.UTC().Truncate(time.Millisecond)
			if !current.FinalizePublishedAt.IsZero() {
				markAt = current.FinalizePublishedAt.UTC().Truncate(time.Millisecond)
			}
			current.State = core.AppRegistryPublishSessionPublished
			current.PublishedAt = markAt
			current.FailureReason = ""
			if current.StagingMarkedStale.IsZero() {
				current.StagingMarkedStale = markAt
			}
			return nil
		},
	})
}

func (s *AppRegistryPublishSessionService) MarkFailed(ctx context.Context, id, claimToken string, expectUpdated time.Time, reason string) (*core.AppRegistryPublishSession, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "finalization failed"
	}
	return s.Transition(ctx, id, PublishSessionTransition{
		ExpectedStates: []core.AppRegistryPublishSessionState{
			core.AppRegistryPublishSessionCreated,
			core.AppRegistryPublishSessionUploading,
			core.AppRegistryPublishSessionFinalizing,
		},
		ExpectUpdated: expectUpdated,
		Mutate: func(current *core.AppRegistryPublishSession) error {
			if current.State == core.AppRegistryPublishSessionFinalizing {
				if err := requireFinalizeClaimToken(current, claimToken); err != nil {
					return err
				}
			}
			current.State = core.AppRegistryPublishSessionFailed
			current.FailureReason = reason
			now := time.Now().UTC().Truncate(time.Millisecond)
			if current.StagingMarkedStale.IsZero() {
				current.StagingMarkedStale = now
			}
			return nil
		},
	})
}

func requireFinalizeClaimToken(session *core.AppRegistryPublishSession, claimToken string) error {
	expected := strings.TrimSpace(session.FinalizeClaimToken)
	got := strings.TrimSpace(claimToken)
	if expected == "" || got == "" || expected != got {
		return fmt.Errorf("%w: session %q claim token mismatch", ErrPublishSessionClaimMismatch, session.ID)
	}
	return nil
}

func newFinalizeClaimToken() string {
	return "fclaim_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func normalizeFinalizeClaimLeaseTTL(leaseTTL time.Duration) time.Duration {
	if leaseTTL <= 0 {
		return 15 * time.Minute
	}
	return leaseTTL
}

func (s *AppRegistryPublishSessionService) RenewLeases(ctx context.Context, id string, expectUpdated time.Time, mutate func(*core.AppRegistryPublishSession) error) (*core.AppRegistryPublishSession, error) {
	if mutate == nil {
		return nil, fmt.Errorf("renew publish session leases: mutate function is required")
	}
	return s.Transition(ctx, id, PublishSessionTransition{
		ExpectedStates: []core.AppRegistryPublishSessionState{
			core.AppRegistryPublishSessionCreated,
			core.AppRegistryPublishSessionUploading,
		},
		ExpectUpdated: expectUpdated,
		Mutate: func(current *core.AppRegistryPublishSession) error {
			if err := mutate(current); err != nil {
				return err
			}
			current.State = core.AppRegistryPublishSessionUploading
			return nil
		},
	})
}

func (s *AppRegistryPublishSessionService) CreateActive(ctx context.Context, input CreateAppRegistryPublishSessionInput) (*core.AppRegistryPublishSession, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("create app registry publish session: service is not configured")
	}
	if err := s.EnsureStore(ctx); err != nil {
		return nil, err
	}
	app := strings.TrimSpace(input.App)
	version := strings.TrimSpace(input.Version)
	dedupeKey := strings.TrimSpace(input.DedupeKey)
	if app == "" || version == "" || dedupeKey == "" {
		return nil, fmt.Errorf("create app registry publish session: app, version, and dedupe key are required")
	}

	tx, err := s.db.Transaction(ctx, []string{StoreAppRegistryPublishSessions}, idb.TransactionReadwrite, idb.TransactionOptions{})
	if err != nil {
		return nil, fmt.Errorf("create app registry publish session: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()
	store := tx.ObjectStore(StoreAppRegistryPublishSessions)

	dedupeRecs, err := store.Index("by_dedupe_key").GetAll(ctx, dedupeKey)
	if err != nil {
		return nil, fmt.Errorf("create app registry publish session: dedupe lookup: %w", err)
	}
	if len(dedupeRecs) > 0 {
		return nil, fmt.Errorf("%w: dedupe key already exists", ErrPublishSessionConflict)
	}
	versionRecs, err := store.Index("by_app_version").GetAll(ctx, []any{app, version})
	if err != nil {
		return nil, fmt.Errorf("create app registry publish session: version lookup: %w", err)
	}
	if err := assertNoConflictingVersionRecords(versionRecs, app, version, dedupeKey); err != nil {
		return nil, err
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	startedAt := input.PublishStartedAt
	if startedAt.IsZero() {
		startedAt = now
	} else {
		startedAt = startedAt.UTC().Truncate(time.Millisecond)
	}
	session := &core.AppRegistryPublishSession{
		ID:                 uuid.NewString(),
		App:                app,
		Registry:           strings.TrimSpace(input.Registry),
		Version:            version,
		DedupeKey:          dedupeKey,
		DeclarationDigest:  strings.TrimSpace(input.DeclarationDigest),
		DeclarationJSON:    append([]byte(nil), input.DeclarationJSON...),
		State:              core.AppRegistryPublishSessionUploading,
		PublisherSubjectID: strings.TrimSpace(input.PublisherSubjectID),
		Artifacts:          append([]core.AppRegistryPublishArtifact(nil), input.Artifacts...),
		UploadLeases:       append([]core.AppRegistryUploadLease(nil), input.UploadLeases...),
		StagingPrefix:      strings.TrimSpace(input.StagingPrefix),
		PublishStartedAt:   startedAt,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := store.Add(ctx, appRegistryPublishSessionRecord(session)); err != nil {
		return nil, fmt.Errorf("create app registry publish session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("create app registry publish session: commit: %w", err)
	}
	committed = true
	return session, nil
}

func assertNoConflictingVersionRecords(recs []idb.Record, app, version, dedupeKey string) error {
	for _, rec := range recs {
		session := recordToAppRegistryPublishSession(rec)
		if session == nil {
			continue
		}
		if session.DedupeKey == dedupeKey {
			continue
		}
		if session.State == core.AppRegistryPublishSessionFailed {
			continue
		}
		return fmt.Errorf("%w: app %q version %q already has publish session %q", ErrPublishSessionVersionLocked, app, version, session.ID)
	}
	return nil
}

func publishSessionStateAllowed(state core.AppRegistryPublishSessionState, allowed []core.AppRegistryPublishSessionState) bool {
	for _, candidate := range allowed {
		if state == candidate {
			return true
		}
	}
	return false
}
