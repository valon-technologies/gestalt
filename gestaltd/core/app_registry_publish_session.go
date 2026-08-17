package core

import "time"

type AppRegistryPublishSessionState string

const (
	AppRegistryPublishSessionCreated    AppRegistryPublishSessionState = "created"
	AppRegistryPublishSessionUploading  AppRegistryPublishSessionState = "uploading"
	AppRegistryPublishSessionFinalizing AppRegistryPublishSessionState = "finalizing"
	AppRegistryPublishSessionPublished  AppRegistryPublishSessionState = "published"
	AppRegistryPublishSessionFailed     AppRegistryPublishSessionState = "failed"
)

func (s AppRegistryPublishSessionState) Terminal() bool {
	return s == AppRegistryPublishSessionPublished || s == AppRegistryPublishSessionFailed
}

type AppRegistryPublishArtifact struct {
	Platform string
	Filename string
	SHA256   string
	Size     int64
}

type AppRegistryUploadLease struct {
	Kind          string
	Platform      string
	StorageURL    string
	UploadURL     string
	UploadHeaders map[string]string
	ExpiresAt     time.Time
	Generation    int64
}

type AppRegistryPublishSession struct {
	ID                     string
	App                    string
	Registry               string
	Version                string
	DedupeKey              string
	DeclarationDigest      string
	DeclarationJSON        []byte
	State                  AppRegistryPublishSessionState
	PublisherSubjectID     string
	Artifacts              []AppRegistryPublishArtifact
	UploadLeases           []AppRegistryUploadLease
	StagingPrefix          string
	FailureReason          string
	PublishStartedAt       time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
	Revision               int64
	PublishedAt            time.Time
	StagingMarkedStale     time.Time
	FinalizeClaimToken     string
	FinalizeClaimExpiresAt time.Time
	FinalizePublishedAt    time.Time
}
