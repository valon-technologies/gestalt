package appregistry

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// PublishSessionLimits bounds remote publish sessions.
type PublishSessionLimits struct {
	UploadLeaseTTL        time.Duration
	FinalizeClaimLeaseTTL time.Duration
	MaxArtifacts          int
	MaxArtifactBytes      int64
	RequiredPlatforms     []string
}

func DefaultPublishSessionLimits() PublishSessionLimits {
	return PublishSessionLimits{
		UploadLeaseTTL:        time.Hour,
		FinalizeClaimLeaseTTL: 15 * time.Minute,
		MaxArtifacts:          16,
		MaxArtifactBytes:      512 << 20,
		RequiredPlatforms:     []string{"linux/amd64", "darwin/arm64"},
	}
}

func (l PublishSessionLimits) normalized() PublishSessionLimits {
	defaults := DefaultPublishSessionLimits()
	if l.UploadLeaseTTL <= 0 {
		l.UploadLeaseTTL = defaults.UploadLeaseTTL
	}
	if l.FinalizeClaimLeaseTTL <= 0 {
		l.FinalizeClaimLeaseTTL = defaults.FinalizeClaimLeaseTTL
	}
	if l.MaxArtifacts <= 0 {
		l.MaxArtifacts = defaults.MaxArtifacts
	}
	if l.MaxArtifactBytes <= 0 {
		l.MaxArtifactBytes = defaults.MaxArtifactBytes
	}
	if len(l.RequiredPlatforms) == 0 {
		l.RequiredPlatforms = append([]string(nil), defaults.RequiredPlatforms...)
	}
	return l
}

// WritableRegistryStore extends RegistryObjectStore with staging promotion helpers.
type WritableRegistryStore interface {
	RegistryObjectStore
	PromoteObject(input PromoteObjectInput) error
}

// PromoteObjectInput copies a staged object to an immutable final path.
type PromoteObjectInput struct {
	SourceURL        string
	SourceGeneration int64
	DestURL          string
	ExpectedSHA256   string
	SourceRef        string
}

// RegistryUploadSigner mints short-lived create-only upload URLs.
type RegistryUploadSigner interface {
	SignCreateUpload(input SignCreateUploadInput) (SignCreateUploadResult, error)
}

type SignCreateUploadInput struct {
	StorageURL    string
	SHA256        string
	ContentLength int64
	SourceRef     string
	ExpiresAt     time.Time
}

type SignCreateUploadResult struct {
	UploadURL string
	ExpiresAt time.Time
	Headers   map[string]string
}

type memoryPromoteStore struct {
	*MemoryObjectStore
}

func NewMemoryWritableRegistryStore() WritableRegistryStore {
	return &memoryPromoteStore{MemoryObjectStore: NewMemoryObjectStore()}
}

// NewMemoryPublishStores returns a writable store and its backing memory object store for tests.
func NewMemoryPublishStores() (WritableRegistryStore, *MemoryObjectStore) {
	mem := NewMemoryObjectStore()
	return &memoryPromoteStore{MemoryObjectStore: mem}, mem
}

func (s *memoryPromoteStore) PromoteObject(input PromoteObjectInput) error {
	if s == nil || s.MemoryObjectStore == nil {
		return fmt.Errorf("registry store is required")
	}
	described, err := s.DescribeObject(input.SourceURL)
	if err != nil {
		return err
	}
	if described.Generation == 0 {
		return fmt.Errorf("%w: %s", ErrPublishUploadMissing, input.SourceURL)
	}
	if input.SourceGeneration > 0 && described.Generation != input.SourceGeneration {
		return fmt.Errorf("%w: %s generation %d != %d", ErrPublishUploadMismatch, input.SourceURL, described.Generation, input.SourceGeneration)
	}
	expected := strings.ToLower(strings.TrimSpace(input.ExpectedSHA256))
	if expected != "" && strings.ToLower(strings.TrimSpace(described.SHA256)) != expected {
		return fmt.Errorf("%w: %s digest mismatch", ErrPublishUploadMismatch, input.SourceURL)
	}
	finalDescribed, err := s.DescribeObject(input.DestURL)
	if err != nil {
		return err
	}
	if finalDescribed.Generation != 0 {
		if expected != "" && strings.ToLower(strings.TrimSpace(finalDescribed.SHA256)) == expected {
			return nil
		}
		return fmt.Errorf("%w: %s", ErrObjectPreconditionFailed, input.DestURL)
	}
	_, data, err := s.ReadObject(input.SourceURL)
	if err != nil {
		return err
	}
	tmpPath, err := WriteTempJSON("gestalt-promote-*", data)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmpPath) }()
	return s.WriteImmutableObject(WriteImmutableObjectInput{
		LocalPath:  tmpPath,
		StorageURL: input.DestURL,
		SourceRef:  input.SourceRef,
		SHA256:     expected,
	})
}
