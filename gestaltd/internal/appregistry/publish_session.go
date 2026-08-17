package appregistry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

type PublishSessionStore interface {
	EnsureStore(ctx context.Context) error
	Get(ctx context.Context, id string) (*core.AppRegistryPublishSession, error)
	GetByDedupeKey(ctx context.Context, dedupeKey string) (*core.AppRegistryPublishSession, error)
	Create(ctx context.Context, input coredata.CreateAppRegistryPublishSessionInput) (*core.AppRegistryPublishSession, error)
	Update(ctx context.Context, id string, update func(*core.AppRegistryPublishSession) error) (*core.AppRegistryPublishSession, error)
	ClaimFinalize(ctx context.Context, id string, leaseTTL time.Duration) (*core.AppRegistryPublishSession, error)
	RenewFinalizeClaim(ctx context.Context, id, claimToken string, expectRevision int64, leaseTTL time.Duration) (*core.AppRegistryPublishSession, error)
	MarkPublished(ctx context.Context, id, claimToken string, expectRevision int64, publishedAt time.Time) (*core.AppRegistryPublishSession, error)
	MarkFailed(ctx context.Context, id, claimToken string, expectRevision int64, reason string) (*core.AppRegistryPublishSession, error)
	RenewLeases(ctx context.Context, id string, expectRevision int64, mutate func(*core.AppRegistryPublishSession) error) (*core.AppRegistryPublishSession, error)
}

type PublishSessionIndexChecker interface {
	VersionPublished(ctx context.Context, storageRoot, appName, version string) (bool, error)
}

type PublishSessionService struct {
	Sessions PublishSessionStore
	Store    WritableRegistryStore
	Signer   RegistryUploadSigner
	Writer   *Writer
	Index    PublishSessionIndexChecker
	Limits   PublishSessionLimits
	Now      func() time.Time
}

func (s *PublishSessionService) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *PublishSessionService) limits() PublishSessionLimits {
	if s == nil {
		return DefaultPublishSessionLimits()
	}
	return s.Limits.normalized()
}

type CreatePublishSessionInput struct {
	App                string
	Registry           string
	StorageRoot        string
	PublicRoot         string
	DisplayName        string
	Description        string
	PublisherSubjectID string
	Declaration        *PublishDeclaration
}

type CreatePublishSessionResult struct {
	Session *core.AppRegistryPublishSession
	Renewed bool
}

func (s *PublishSessionService) Create(ctx context.Context, input CreatePublishSessionInput) (*CreatePublishSessionResult, error) {
	if s == nil || s.Sessions == nil || s.Store == nil || s.Signer == nil {
		return nil, ErrPublishSessionUnavailable
	}
	limits := s.limits()
	declaration := input.Declaration
	if err := ValidatePublishDeclaration(input.App, declaration, limits); err != nil {
		return nil, err
	}
	version := strings.TrimSpace(declaration.Manifest.Version)
	digest, err := DeclarationDigest(declaration)
	if err != nil {
		return nil, err
	}
	dedupeKey := PublishSessionDedupeKey(input.App, version, digest)
	if err := s.Sessions.EnsureStore(ctx); err != nil {
		return nil, err
	}
	if existing, err := s.Sessions.GetByDedupeKey(ctx, dedupeKey); err == nil {
		renewed, session, renewErr := s.renewExistingSession(ctx, existing, input.StorageRoot, limits)
		if renewErr != nil {
			return nil, renewErr
		}
		return &CreatePublishSessionResult{Session: session, Renewed: renewed}, nil
	} else if !errors.Is(err, core.ErrNotFound) {
		return nil, err
	}
	if s.Index != nil {
		published, err := s.Index.VersionPublished(ctx, input.StorageRoot, input.App, version)
		if err != nil {
			return nil, err
		}
		if published {
			return nil, fmt.Errorf("%w: version %q is already published", ErrPublishVersionConflict, version)
		}
	}
	declarationJSON, err := EncodePublishDeclaration(declaration)
	if err != nil {
		return nil, err
	}
	publishID := newPublishID()
	stagingPrefix := PublishStagingPrefix(input.App, publishID)
	artifacts, leases, err := s.buildUploadLeases(input.StorageRoot, stagingPrefix, declaration.Artifacts, limits)
	if err != nil {
		return nil, err
	}
	session, err := s.Sessions.Create(ctx, coredata.CreateAppRegistryPublishSessionInput{
		App:                input.App,
		Registry:           input.Registry,
		Version:            version,
		DedupeKey:          dedupeKey,
		DeclarationDigest:  digest,
		DeclarationJSON:    declarationJSON,
		PublisherSubjectID: input.PublisherSubjectID,
		Artifacts:          artifacts,
		UploadLeases:       leases,
		StagingPrefix:      stagingPrefix,
		PublishStartedAt:   s.now(),
	})
	if err != nil {
		if errors.Is(err, coredata.ErrPublishSessionVersionLocked) {
			return nil, fmt.Errorf("%w: %v", ErrPublishVersionConflict, err)
		}
		return nil, err
	}
	return &CreatePublishSessionResult{Session: session}, nil
}

func (s *PublishSessionService) renewExistingSession(
	ctx context.Context,
	session *core.AppRegistryPublishSession,
	storageRoot string,
	limits PublishSessionLimits,
) (bool, *core.AppRegistryPublishSession, error) {
	if session == nil {
		return false, nil, core.ErrNotFound
	}
	switch session.State {
	case core.AppRegistryPublishSessionFailed:
		return false, session, fmt.Errorf("%w: %s", ErrPublishSessionFailed, strings.TrimSpace(session.FailureReason))
	case core.AppRegistryPublishSessionPublished:
		return false, session, nil
	}
	now := s.now()
	needsRenewal := false
	for _, lease := range session.UploadLeases {
		if lease.ExpiresAt.IsZero() || !lease.ExpiresAt.After(now) {
			needsRenewal = true
			break
		}
	}
	if !needsRenewal {
		return false, session, nil
	}
	declaration, err := DecodePublishDeclaration(session.DeclarationJSON)
	if err != nil {
		return false, nil, err
	}
	artifacts, leases, err := s.buildUploadLeases(storageRoot, session.StagingPrefix, declaration.Artifacts, limits)
	if err != nil {
		return false, nil, err
	}
	updated, err := s.Sessions.RenewLeases(ctx, session.ID, session.Revision, func(current *core.AppRegistryPublishSession) error {
		current.Artifacts = artifacts
		current.UploadLeases = leases
		return nil
	})
	if err != nil {
		return false, nil, err
	}
	return true, updated, nil
}

func (s *PublishSessionService) buildUploadLeases(
	storageRoot, stagingPrefix string,
	artifacts []PublishDeclarationArtifact,
	limits PublishSessionLimits,
) ([]core.AppRegistryPublishArtifact, []core.AppRegistryUploadLease, error) {
	expiresAt := s.now().Add(limits.UploadLeaseTTL)
	outArtifacts := make([]core.AppRegistryPublishArtifact, 0, len(artifacts))
	outLeases := make([]core.AppRegistryUploadLease, 0, len(artifacts))
	for _, artifact := range artifacts {
		platform := strings.TrimSpace(artifact.Platform)
		filename := strings.TrimSpace(artifact.Filename)
		digest := strings.ToLower(strings.TrimSpace(artifact.SHA256))
		stagingPath := PublishStagingArtifactPath(stagingPrefix, platform, filename)
		storageURL := StorageURL(storageRoot, stagingPath)
		signed, err := s.Signer.SignCreateUpload(SignCreateUploadInput{
			StorageURL:    storageURL,
			SHA256:        digest,
			ContentLength: artifact.Size,
			ExpiresAt:     expiresAt,
		})
		if err != nil {
			return nil, nil, err
		}
		outArtifacts = append(outArtifacts, core.AppRegistryPublishArtifact{
			Platform: platform,
			Filename: filename,
			SHA256:   digest,
			Size:     artifact.Size,
		})
		outLeases = append(outLeases, core.AppRegistryUploadLease{
			Kind:          string(PublishObjectKindArchive),
			Platform:      platform,
			StorageURL:    storageURL,
			UploadURL:     signed.UploadURL,
			UploadHeaders: cloneSignedUploadHeaders(signed.Headers),
			ExpiresAt:     signed.ExpiresAt,
		})
	}
	return outArtifacts, outLeases, nil
}

type PublishSessionStatus struct {
	Session           *core.AppRegistryPublishSession
	MissingUploads    []string
	MismatchedUploads []string
}

func (s *PublishSessionService) Status(ctx context.Context, app, publishID, storageRoot, _ string) (*PublishSessionStatus, error) {
	if s == nil || s.Sessions == nil || s.Store == nil {
		return nil, ErrPublishSessionUnavailable
	}
	session, err := s.loadSession(ctx, app, publishID)
	if err != nil {
		return nil, err
	}
	declaration, err := DecodePublishDeclaration(session.DeclarationJSON)
	if err != nil {
		return nil, err
	}
	sourceRef := strings.ToLower(strings.TrimSpace(declaration.SourceRef))
	if reconciled, reconcileErr := s.reconcilePublishedSession(ctx, session, storageRoot, sourceRef, time.Time{}); reconcileErr != nil {
		return nil, reconcileErr
	} else if reconciled != nil {
		session = reconciled
	}
	status := &PublishSessionStatus{Session: session}
	if session.State.Terminal() {
		return status, nil
	}
	for _, artifact := range session.Artifacts {
		stagingPath := PublishStagingArtifactPath(session.StagingPrefix, artifact.Platform, artifact.Filename)
		storageURL := StorageURL(storageRoot, stagingPath)
		described, describeErr := s.Store.DescribeObject(storageURL)
		if describeErr != nil {
			return nil, describeErr
		}
		switch {
		case described.Generation == 0:
			status.MissingUploads = append(status.MissingUploads, artifact.Platform)
		case strings.ToLower(strings.TrimSpace(described.SHA256)) != strings.ToLower(strings.TrimSpace(artifact.SHA256)):
			status.MismatchedUploads = append(status.MismatchedUploads, artifact.Platform)
		}
	}
	return status, nil
}

func (s *PublishSessionService) loadSession(ctx context.Context, app, publishID string) (*core.AppRegistryPublishSession, error) {
	session, err := s.Sessions.Get(ctx, publishID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(session.App) != strings.TrimSpace(app) {
		return nil, core.ErrNotFound
	}
	return session, nil
}
