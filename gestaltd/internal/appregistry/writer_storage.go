package appregistry

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

var (
	ErrObjectNotFound           = errors.New("registry object not found")
	ErrObjectPreconditionFailed = errors.New("registry object precondition failed")
)

// ObjectDescription reports remote object metadata used for idempotent publish checks.
type ObjectDescription struct {
	Generation int64
	SHA256     string
	Size       int64
}

// RegistryObjectStore reads and writes registry bucket objects. Implementations must
// never delete objects; the registry writer only creates immutable objects and
// performs compare-and-swap updates on catalog files.
type RegistryObjectStore interface {
	DescribeObject(storageURL string) (ObjectDescription, error)
	ReadObject(storageURL string) (generation int64, data []byte, err error)
	WriteImmutableObject(input WriteImmutableObjectInput) error
	WriteCatalogObject(input WriteCatalogObjectInput) error
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

// WriteImmutableObjectInput creates an object only when it does not already exist.
type WriteImmutableObjectInput struct {
	LocalPath  string
	StorageURL string
	SourceRef  string
	SHA256     string
}

// WriteCatalogObjectInput replaces a catalog object with generation preconditions.
type WriteCatalogObjectInput struct {
	LocalPath  string
	StorageURL string
	SourceRef  string
	Generation int64
}

type memoryStoredObject struct {
	generation int64
	data       []byte
	sha256     string
	size       int64
}

// MemoryObjectStore is an in-memory RegistryObjectStore for unit tests.
type MemoryObjectStore struct {
	mu      sync.Mutex
	objects map[string]memoryStoredObject
	nextGen int64
}

func NewMemoryObjectStore() *MemoryObjectStore {
	return &MemoryObjectStore{
		objects: map[string]memoryStoredObject{},
		nextGen: 1,
	}
}

func (s *MemoryObjectStore) DescribeObject(storageURL string) (ObjectDescription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	object, ok := s.objects[storageURL]
	if !ok {
		return ObjectDescription{}, nil
	}
	return ObjectDescription{Generation: object.generation, SHA256: object.sha256, Size: object.size}, nil
}

func (s *MemoryObjectStore) ReadObject(storageURL string) (int64, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	object, ok := s.objects[storageURL]
	if !ok {
		return 0, nil, nil
	}
	data := append([]byte(nil), object.data...)
	return object.generation, data, nil
}

func (s *MemoryObjectStore) WriteImmutableObject(input WriteImmutableObjectInput) error {
	data, err := os.ReadFile(input.LocalPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", input.LocalPath, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.objects[input.StorageURL]; ok && existing.generation != 0 {
		return fmt.Errorf("%w: %s", ErrObjectPreconditionFailed, input.StorageURL)
	}
	s.nextGen++
	s.objects[input.StorageURL] = memoryStoredObject{
		generation: s.nextGen,
		data:       data,
		sha256:     strings.TrimSpace(input.SHA256),
		size:       int64(len(data)),
	}
	return nil
}

func (s *MemoryObjectStore) WriteCatalogObject(input WriteCatalogObjectInput) error {
	data, err := os.ReadFile(input.LocalPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", input.LocalPath, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.objects[input.StorageURL]
	switch {
	case input.Generation == 0:
		if ok && existing.generation != 0 {
			return fmt.Errorf("%w: %s", ErrObjectPreconditionFailed, input.StorageURL)
		}
	case !ok || existing.generation != input.Generation:
		return fmt.Errorf("%w: %s", ErrObjectPreconditionFailed, input.StorageURL)
	}
	s.nextGen++
	s.objects[input.StorageURL] = memoryStoredObject{generation: s.nextGen, data: data}
	return nil
}

func (s *MemoryObjectStore) PromoteObject(input PromoteObjectInput) error {
	if s == nil {
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
