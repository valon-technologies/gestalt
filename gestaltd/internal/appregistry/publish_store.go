package appregistry

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

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

type PublishUpload struct {
	Platform  string
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
	if err := s.WriteImmutableObject(WriteImmutableObjectInput{
		LocalPath:  tmpPath,
		StorageURL: input.DestURL,
		SourceRef:  input.SourceRef,
		SHA256:     expected,
	}); err != nil {
		if errors.Is(err, ErrObjectPreconditionFailed) {
			reread, readErr := s.DescribeObject(input.DestURL)
			if readErr == nil && reread.Generation != 0 && expected != "" && strings.ToLower(strings.TrimSpace(reread.SHA256)) == expected {
				return nil
			}
		}
		return err
	}
	return nil
}
