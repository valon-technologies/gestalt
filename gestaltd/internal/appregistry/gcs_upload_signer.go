package appregistry

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/storage"
	"github.com/google/uuid"
)

// GCSUploadSigner mints short-lived create-only signed PUT URLs for staged uploads.
type GCSUploadSigner struct {
	clientOnce sync.Once
	client     *storage.Client
	clientErr  error

	newClient func(context.Context) (*storage.Client, error)
	signURL   func(client *storage.Client, bucket, object string, opts *storage.SignedURLOptions) (string, error)
}

func NewGCSUploadSigner() *GCSUploadSigner {
	return &GCSUploadSigner{}
}

func (s *GCSUploadSigner) storageClient() (*storage.Client, error) {
	s.clientOnce.Do(func() {
		if s != nil && s.newClient != nil {
			s.client, s.clientErr = s.newClient(context.Background())
			return
		}
		s.client, s.clientErr = storage.NewClient(context.Background())
	})
	return s.client, s.clientErr
}

func (s *GCSUploadSigner) signedURL(client *storage.Client, bucket, object string, opts *storage.SignedURLOptions) (string, error) {
	if s != nil && s.signURL != nil {
		return s.signURL(client, bucket, object, opts)
	}
	return client.Bucket(bucket).SignedURL(object, opts)
}

// CheckSigningReadiness verifies signBlob capability by minting a disposable signed URL.
// It does not write objects or mutate registry state.
func (s *GCSUploadSigner) CheckSigningReadiness(ctx context.Context, storageRoot string) error {
	if s == nil {
		return fmt.Errorf("upload signer is not configured")
	}
	storageRoot = strings.TrimSpace(storageRoot)
	if storageRoot == "" {
		return fmt.Errorf("storage root is required")
	}
	probeURL := strings.TrimRight(storageRoot, "/") + "/.gestaltd-signing-readiness-probe/" + uuid.NewString()
	_, err := s.SignCreateUpload(SignCreateUploadInput{
		StorageURL:    probeURL,
		SHA256:        strings.Repeat("0", 64),
		ContentLength: 1,
		ExpiresAt:     time.Now().UTC().Add(5 * time.Minute),
	})
	if err != nil {
		return fmt.Errorf("gcs upload signing unavailable: %w", err)
	}
	return nil
}

func (s *GCSUploadSigner) SignCreateUpload(input SignCreateUploadInput) (SignCreateUploadResult, error) {
	if s == nil {
		return SignCreateUploadResult{}, fmt.Errorf("upload signer is not configured")
	}
	storageURL := strings.TrimSpace(input.StorageURL)
	if storageURL == "" {
		return SignCreateUploadResult{}, fmt.Errorf("storage URL is required")
	}
	headers, err := BuildSignedUploadHeaders(input.ContentLength, input.SHA256)
	if err != nil {
		return SignCreateUploadResult{}, err
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
	signedHeaders := signedUploadHeaderLines(headers)
	if sourceRef := strings.TrimSpace(input.SourceRef); sourceRef != "" {
		signedHeaders = append(signedHeaders, "x-goog-meta-source-ref:"+sourceRef)
	}
	uploadURL, err := s.signedURL(client, bucket, object, &storage.SignedURLOptions{
		Scheme:  storage.SigningSchemeV4,
		Method:  "PUT",
		Headers: signedHeaders,
		Expires: expiresAt.UTC(),
	})
	if err != nil {
		return SignCreateUploadResult{}, fmt.Errorf("sign upload URL for %s: %w", storageURL, err)
	}
	return SignCreateUploadResult{
		UploadURL: uploadURL,
		ExpiresAt: expiresAt.UTC(),
		Headers:   cloneSignedUploadHeaders(headers),
	}, nil
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
