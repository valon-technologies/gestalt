package appregistry

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/storage"
)

// GCSUploadSigner mints short-lived create-only signed PUT URLs for staged uploads.
type GCSUploadSigner struct {
	clientOnce sync.Once
	client     *storage.Client
	clientErr  error
}

func NewGCSUploadSigner() *GCSUploadSigner {
	return &GCSUploadSigner{}
}

func (s *GCSUploadSigner) storageClient() (*storage.Client, error) {
	s.clientOnce.Do(func() {
		s.client, s.clientErr = storage.NewClient(context.Background())
	})
	return s.client, s.clientErr
}

func (s *GCSUploadSigner) SignCreateUpload(input SignCreateUploadInput) (SignCreateUploadResult, error) {
	if s == nil {
		return SignCreateUploadResult{}, fmt.Errorf("upload signer is not configured")
	}
	storageURL := strings.TrimSpace(input.StorageURL)
	if storageURL == "" {
		return SignCreateUploadResult{}, fmt.Errorf("storage URL is required")
	}
	if input.ContentLength <= 0 {
		return SignCreateUploadResult{}, fmt.Errorf("content length is required")
	}
	digest := strings.ToLower(strings.TrimSpace(input.SHA256))
	if digest == "" {
		return SignCreateUploadResult{}, fmt.Errorf("sha256 is required")
	}
	expiresAt := input.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = time.Now().UTC().Add(time.Hour)
	}
	client, err := s.storageClient()
	if err != nil {
		return SignCreateUploadResult{}, fmt.Errorf("create storage client: %w", err)
	}
	bucket, object, err := parseGCSStorageURL(storageURL)
	if err != nil {
		return SignCreateUploadResult{}, err
	}
	headers := []string{
		fmt.Sprintf("Content-Length:%d", input.ContentLength),
		"x-goog-if-generation-match:0",
		fmt.Sprintf("x-goog-meta-sha256:%s", digest),
	}
	if sourceRef := strings.TrimSpace(input.SourceRef); sourceRef != "" {
		headers = append(headers, "x-goog-meta-source-ref:"+sourceRef)
	}
	uploadURL, err := client.Bucket(bucket).SignedURL(object, &storage.SignedURLOptions{
		Scheme:  storage.SigningSchemeV4,
		Method:  "PUT",
		Headers: headers,
		Expires: expiresAt.UTC(),
	})
	if err != nil {
		return SignCreateUploadResult{}, fmt.Errorf("sign upload URL for %s: %w", storageURL, err)
	}
	return SignCreateUploadResult{UploadURL: uploadURL, ExpiresAt: expiresAt.UTC()}, nil
}

func parseGCSStorageURL(storageURL string) (bucket, object string, err error) {
	storageURL = strings.TrimSpace(storageURL)
	if !strings.HasPrefix(storageURL, "gs://") {
		return "", "", fmt.Errorf("invalid gcs storage URL %q", storageURL)
	}
	rest := strings.TrimPrefix(storageURL, "gs://")
	rest = strings.Trim(rest, "/")
	if rest == "" {
		return "", "", fmt.Errorf("invalid gcs storage URL %q", storageURL)
	}
	slash := strings.Index(rest, "/")
	if slash < 0 {
		return "", "", fmt.Errorf("gcs storage URL %q missing object path", storageURL)
	}
	bucket = strings.Trim(rest[:slash], "/")
	object = strings.Trim(rest[slash+1:], "/")
	if bucket == "" || object == "" {
		return "", "", fmt.Errorf("invalid gcs storage URL %q", storageURL)
	}
	return bucket, object, nil
}
