package appregistry

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

type memoryUploadSigner struct {
	baseURL string
	store   *MemoryObjectStore
}

func NewMemoryRegistryUploadSigner(store *MemoryObjectStore, baseURL string) RegistryUploadSigner {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "memory-upload://"
	}
	return &memoryUploadSigner{baseURL: strings.TrimRight(baseURL, "/"), store: store}
}

func (s *memoryUploadSigner) SignCreateUpload(input SignCreateUploadInput) (SignCreateUploadResult, error) {
	if s == nil || s.store == nil {
		return SignCreateUploadResult{}, fmt.Errorf("upload signer is not configured")
	}
	storageURL := strings.TrimSpace(input.StorageURL)
	if storageURL == "" {
		return SignCreateUploadResult{}, fmt.Errorf("storage URL is required")
	}
	expiresAt := input.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = time.Now().UTC().Add(time.Hour)
	}
	u, err := url.Parse(s.baseURL + "/upload")
	if err != nil {
		return SignCreateUploadResult{}, err
	}
	q := u.Query()
	q.Set("object", storageURL)
	q.Set("expires", expiresAt.UTC().Format(time.RFC3339))
	if digest := strings.ToLower(strings.TrimSpace(input.SHA256)); digest != "" {
		q.Set("sha256", digest)
	}
	u.RawQuery = q.Encode()
	return SignCreateUploadResult{UploadURL: u.String(), ExpiresAt: expiresAt.UTC()}, nil
}

// ApplyMemoryUpload applies a signed memory upload URL to the backing store.
func ApplyMemoryUpload(store *MemoryObjectStore, uploadURL string, data []byte, sha256 string) error {
	if store == nil {
		return fmt.Errorf("registry store is required")
	}
	parsed, err := url.Parse(strings.TrimSpace(uploadURL))
	if err != nil {
		return err
	}
	objectURL := strings.TrimSpace(parsed.Query().Get("object"))
	if objectURL == "" {
		return fmt.Errorf("upload URL missing object")
	}
	expiresRaw := strings.TrimSpace(parsed.Query().Get("expires"))
	if expiresRaw != "" {
		expiresAt, err := time.Parse(time.RFC3339, expiresRaw)
		if err != nil {
			return err
		}
		if time.Now().UTC().After(expiresAt) {
			return fmt.Errorf("%w", ErrPublishLeaseExpired)
		}
	}
	expected := strings.ToLower(strings.TrimSpace(parsed.Query().Get("sha256")))
	if expected != "" && expected != strings.ToLower(strings.TrimSpace(sha256)) {
		return fmt.Errorf("%w: upload digest mismatch", ErrPublishUploadMismatch)
	}
	tmpPath, err := WriteTempJSON("gestalt-upload-*", data)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmpPath) }()
	return store.WriteImmutableObject(WriteImmutableObjectInput{
		LocalPath:  tmpPath,
		StorageURL: objectURL,
		SHA256:     strings.ToLower(strings.TrimSpace(sha256)),
	})
}
