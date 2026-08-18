package appregistry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/storage"
	"github.com/google/uuid"
)

const (
	UploadHeaderContentLength          = "Content-Length"
	UploadHeaderXGoogIfGenerationMatch = "x-goog-if-generation-match"
	UploadHeaderXGoogMetaSHA256        = "x-goog-meta-sha256"
	UploadHeaderXGoogContentSHA256     = "x-goog-content-sha256"
)

var signedUploadHeaderOrder = []string{
	UploadHeaderContentLength,
	UploadHeaderXGoogIfGenerationMatch,
	UploadHeaderXGoogMetaSHA256,
	UploadHeaderXGoogContentSHA256,
}

func gcsBucketObjectFromURL(storageURL string) (bucket, object string, err error) {
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

func gcsBucketFromStorageRoot(storageRoot string) (string, error) {
	storageRoot = strings.TrimSpace(storageRoot)
	if storageRoot == "" {
		return "", fmt.Errorf("storage root is required")
	}
	if !strings.HasPrefix(storageRoot, "gs://") {
		return "", fmt.Errorf("storage root must be a gs:// URL")
	}
	bucket := strings.TrimPrefix(storageRoot, "gs://")
	bucket = strings.Trim(bucket, "/")
	if bucket == "" || strings.Contains(bucket, "/") {
		return "", fmt.Errorf("storage root must name exactly one bucket")
	}
	return bucket, nil
}

// RegistryUploadSigner mints short-lived create-only upload URLs.
type RegistryUploadSigner interface {
	SignCreateUpload(input SignCreateUploadInput) (SignCreateUploadResult, error)
}

type SignCreateUploadInput struct {
	StorageURL    string
	SHA256        string
	ContentLength int64
	ExpiresAt     time.Time
}

type SignCreateUploadResult struct {
	UploadURL string
	ExpiresAt time.Time
	Headers   map[string]string
}

func BuildSignedUploadHeaders(contentLength int64, sha256Hex string) (map[string]string, error) {
	if contentLength <= 0 {
		return nil, fmt.Errorf("content length is required")
	}
	digestHex, err := normalizePublishArtifactSHA256(sha256Hex)
	if err != nil {
		return nil, err
	}
	sum, err := hex.DecodeString(digestHex)
	if err != nil || len(sum) != sha256.Size {
		return nil, fmt.Errorf("sha256 must be %d hex bytes", sha256.Size)
	}
	return map[string]string{
		UploadHeaderContentLength:          fmt.Sprintf("%d", contentLength),
		UploadHeaderXGoogIfGenerationMatch: "0",
		UploadHeaderXGoogMetaSHA256:        digestHex,
		UploadHeaderXGoogContentSHA256:     digestHex,
	}, nil
}

func signedUploadHeaderLines(headers map[string]string) []string {
	if len(headers) == 0 {
		return nil
	}
	lines := make([]string, 0, len(signedUploadHeaderOrder))
	for _, name := range signedUploadHeaderOrder {
		if value, ok := headers[name]; ok {
			lines = append(lines, name+":"+value)
		}
	}
	return lines
}

func cloneSignedUploadHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	for _, name := range signedUploadHeaderOrder {
		if value, ok := headers[name]; ok {
			out[name] = value
		}
	}
	return out
}

// GCSRegistryStore implements RegistryObjectStore against one bound GCS bucket.
type GCSRegistryStore struct {
	SourceRef  string
	Bucket     string
	clientOnce sync.Once
	client     *storage.Client
	clientErr  error
}

func NewGCSRegistryStore(sourceRef, storageRoot string) (*GCSRegistryStore, error) {
	sourceRef = strings.TrimSpace(sourceRef)
	if sourceRef == "" {
		sourceRef = "gestaltd-publish"
	}
	bucket, err := gcsBucketFromStorageRoot(storageRoot)
	if err != nil {
		return nil, err
	}
	return &GCSRegistryStore{SourceRef: sourceRef, Bucket: bucket}, nil
}

func validateBoundGCSStorageURL(storageURL, boundBucket string) (bucket, object string, err error) {
	bucket, object, err = gcsBucketObjectFromURL(storageURL)
	if err != nil {
		return "", "", err
	}
	boundBucket = strings.TrimSpace(boundBucket)
	if boundBucket == "" {
		return "", "", fmt.Errorf("registry store bucket is not configured")
	}
	if bucket != boundBucket {
		return "", "", fmt.Errorf("storage URL bucket %q is outside bound registry bucket %q", bucket, boundBucket)
	}
	return bucket, object, nil
}

func (s *GCSRegistryStore) validateStorageURL(storageURL string) (bucket, object string, err error) {
	if s == nil {
		return validateBoundGCSStorageURL(storageURL, "")
	}
	return validateBoundGCSStorageURL(storageURL, s.Bucket)
}

func (s *GCSRegistryStore) storageClient() (*storage.Client, error) {
	s.clientOnce.Do(func() {
		s.client, s.clientErr = storage.NewClient(context.Background())
	})
	return s.client, s.clientErr
}

func (s *GCSRegistryStore) DescribeObject(storageURL string) (ObjectDescription, error) {
	client, err := s.storageClient()
	if err != nil {
		return ObjectDescription{}, fmt.Errorf("create storage client: %w", err)
	}
	bucket, object, err := s.validateStorageURL(storageURL)
	if err != nil {
		return ObjectDescription{}, err
	}
	attrs, err := client.Bucket(bucket).Object(object).Attrs(context.Background())
	if err == storage.ErrObjectNotExist {
		return ObjectDescription{}, nil
	}
	if err != nil {
		return ObjectDescription{}, fmt.Errorf("describe %s: %w", storageURL, err)
	}
	return ObjectDescription{
		Generation: attrs.Generation,
		SHA256:     strings.ToLower(strings.TrimSpace(attrs.Metadata["sha256"])),
		Size:       attrs.Size,
	}, nil
}

func (s *GCSRegistryStore) ReadObject(storageURL string) (int64, []byte, error) {
	client, err := s.storageClient()
	if err != nil {
		return 0, nil, fmt.Errorf("create storage client: %w", err)
	}
	bucket, object, err := s.validateStorageURL(storageURL)
	if err != nil {
		return 0, nil, err
	}
	attrs, err := client.Bucket(bucket).Object(object).Attrs(context.Background())
	if err == storage.ErrObjectNotExist {
		return 0, nil, nil
	}
	if err != nil {
		return 0, nil, fmt.Errorf("read attrs %s: %w", storageURL, err)
	}
	reader, err := client.Bucket(bucket).Object(object).NewReader(context.Background())
	if err != nil {
		return 0, nil, fmt.Errorf("open %s: %w", storageURL, err)
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(reader)
	if err != nil {
		return 0, nil, fmt.Errorf("read %s: %w", storageURL, err)
	}
	return attrs.Generation, data, nil
}

func (s *GCSRegistryStore) WriteImmutableObject(input WriteImmutableObjectInput) error {
	client, err := s.storageClient()
	if err != nil {
		return fmt.Errorf("create storage client: %w", err)
	}
	bucket, object, err := s.validateStorageURL(input.StorageURL)
	if err != nil {
		return err
	}
	file, err := os.Open(input.LocalPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", input.LocalPath, err)
	}
	defer func() { _ = file.Close() }()
	writer := client.Bucket(bucket).Object(object).If(storage.Conditions{DoesNotExist: true}).NewWriter(context.Background())
	writer.Metadata = gcsObjectMetadata(input.SourceRef, input.SHA256)
	if _, err := io.Copy(writer, file); err != nil {
		_ = writer.Close()
		return fmt.Errorf("write %s: %w", input.StorageURL, err)
	}
	if err := writer.Close(); err != nil {
		if gcsPreconditionFailed(err) {
			return fmt.Errorf("%w: %s", ErrObjectPreconditionFailed, input.StorageURL)
		}
		return fmt.Errorf("finalize %s: %w", input.StorageURL, err)
	}
	return nil
}

func (s *GCSRegistryStore) WriteCatalogObject(input WriteCatalogObjectInput) error {
	data, err := os.ReadFile(input.LocalPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", input.LocalPath, err)
	}
	client, err := s.storageClient()
	if err != nil {
		return fmt.Errorf("create storage client: %w", err)
	}
	bucket, object, err := s.validateStorageURL(input.StorageURL)
	if err != nil {
		return err
	}
	obj := client.Bucket(bucket).Object(object)
	var writer *storage.Writer
	switch input.Generation {
	case 0:
		writer = obj.If(storage.Conditions{DoesNotExist: true}).NewWriter(context.Background())
	default:
		writer = obj.If(storage.Conditions{GenerationMatch: input.Generation}).NewWriter(context.Background())
	}
	writer.ContentType = "application/json"
	writer.Metadata = map[string]string{"source-ref": strings.TrimSpace(input.SourceRef)}
	if _, err := writer.Write(data); err != nil {
		_ = writer.Close()
		return fmt.Errorf("write %s: %w", input.StorageURL, err)
	}
	if err := writer.Close(); err != nil {
		if gcsPreconditionFailed(err) {
			return fmt.Errorf("%w: %s", ErrObjectPreconditionFailed, input.StorageURL)
		}
		return fmt.Errorf("finalize %s: %w", input.StorageURL, err)
	}
	return nil
}

func (s *GCSRegistryStore) PromoteObject(input PromoteObjectInput) error {
	client, err := s.storageClient()
	if err != nil {
		return fmt.Errorf("create storage client: %w", err)
	}
	srcBucket, srcObject, err := s.validateStorageURL(input.SourceURL)
	if err != nil {
		return err
	}
	destBucket, destObject, err := s.validateStorageURL(input.DestURL)
	if err != nil {
		return err
	}
	if srcBucket != destBucket {
		return fmt.Errorf("promote source and destination must share a bucket")
	}
	src := client.Bucket(srcBucket).Object(srcObject)
	srcAttrs, err := src.Attrs(context.Background())
	if err == storage.ErrObjectNotExist {
		return fmt.Errorf("%w: %s", ErrPublishUploadMissing, input.SourceURL)
	}
	if err != nil {
		return fmt.Errorf("describe source %s: %w", input.SourceURL, err)
	}
	if input.SourceGeneration > 0 && srcAttrs.Generation != input.SourceGeneration {
		return fmt.Errorf("%w: %s generation %d != %d", ErrPublishUploadMismatch, input.SourceURL, srcAttrs.Generation, input.SourceGeneration)
	}
	expected := strings.ToLower(strings.TrimSpace(input.ExpectedSHA256))
	reader, err := src.NewReader(context.Background())
	if err != nil {
		return fmt.Errorf("open source %s: %w", input.SourceURL, err)
	}
	sourceData, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		return fmt.Errorf("read source %s: %w", input.SourceURL, err)
	}
	if expected != "" {
		if err := verifyObjectDigestBytes(sourceData, expected); err != nil {
			return fmt.Errorf("%w: %s content digest mismatch", err, input.SourceURL)
		}
	}
	if expected != "" && strings.ToLower(strings.TrimSpace(srcAttrs.Metadata["sha256"])) != expected {
		return fmt.Errorf("%w: %s digest mismatch", ErrPublishUploadMismatch, input.SourceURL)
	}

	dest := client.Bucket(destBucket).Object(destObject)
	destAttrs, err := dest.Attrs(context.Background())
	switch {
	case err == storage.ErrObjectNotExist:
	case err != nil:
		return fmt.Errorf("describe destination %s: %w", input.DestURL, err)
	case expected != "" && strings.ToLower(strings.TrimSpace(destAttrs.Metadata["sha256"])) == expected:
		return nil
	default:
		return fmt.Errorf("%w: %s", ErrObjectPreconditionFailed, input.DestURL)
	}

	copier := dest.If(storage.Conditions{DoesNotExist: true}).CopierFrom(src.If(storage.Conditions{GenerationMatch: srcAttrs.Generation}))
	copier.Metadata = gcsObjectMetadata(input.SourceRef, expected)
	if _, err := copier.Run(context.Background()); err != nil {
		if gcsPreconditionFailed(err) {
			destAttrs, readErr := dest.Attrs(context.Background())
			if readErr == nil && expected != "" && strings.ToLower(strings.TrimSpace(destAttrs.Metadata["sha256"])) == expected {
				return nil
			}
			return fmt.Errorf("%w: %s", ErrObjectPreconditionFailed, input.DestURL)
		}
		return fmt.Errorf("promote %s -> %s: %w", input.SourceURL, input.DestURL, err)
	}
	return nil
}

func gcsObjectMetadata(sourceRef, sha256 string) map[string]string {
	metadata := map[string]string{"source-ref": strings.TrimSpace(sourceRef)}
	if digest := strings.ToLower(strings.TrimSpace(sha256)); digest != "" {
		metadata["sha256"] = digest
	}
	return metadata
}

// GCSUploadSigner mints short-lived create-only signed PUT URLs for staged uploads.
type GCSUploadSigner struct {
	Bucket     string
	clientOnce sync.Once
	client     *storage.Client
	clientErr  error

	newClient func(context.Context) (*storage.Client, error)
	signURL   func(client *storage.Client, bucket, object string, opts *storage.SignedURLOptions) (string, error)
}

func NewGCSUploadSigner(store *GCSRegistryStore) (*GCSUploadSigner, error) {
	if store == nil || strings.TrimSpace(store.Bucket) == "" {
		return nil, fmt.Errorf("registry store is required")
	}
	return &GCSUploadSigner{Bucket: store.Bucket}, nil
}

func (s *GCSUploadSigner) validateStorageURL(storageURL string) (bucket, object string, err error) {
	if s == nil {
		return validateBoundGCSStorageURL(storageURL, "")
	}
	return validateBoundGCSStorageURL(storageURL, s.Bucket)
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

func (s *GCSUploadSigner) CheckSigningReadiness(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("upload signer is not configured")
	}
	probeURL := "gs://" + s.Bucket + "/.gestaltd-signing-readiness-probe/" + uuid.NewString()
	_, err := s.SignCreateUpload(SignCreateUploadInput{
		StorageURL:    probeURL,
		SHA256:        strings.Repeat("0", 64),
		ContentLength: 1,
		ExpiresAt:     time.Now().UTC().Add(5 * time.Minute),
	})
	if err != nil {
		return fmt.Errorf("gcs upload signing unavailable: %w", err)
	}
	_ = ctx
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
	bucket, object, err := s.validateStorageURL(storageURL)
	if err != nil {
		return SignCreateUploadResult{}, err
	}
	client, err := s.storageClient()
	if err != nil {
		return SignCreateUploadResult{}, fmt.Errorf("create storage client: %w", err)
	}
	uploadURL, err := s.signedURL(client, bucket, object, &storage.SignedURLOptions{
		Scheme:  storage.SigningSchemeV4,
		Method:  "PUT",
		Headers: signedUploadHeaderLines(headers),
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

func NewMemoryRegistryUploadSigner(store *MemoryObjectStore, baseURL string) RegistryUploadSigner {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "memory-upload://"
	}
	return &memoryUploadSigner{baseURL: strings.TrimRight(baseURL, "/"), store: store}
}

type memoryUploadSigner struct {
	baseURL string
	store   *MemoryObjectStore
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
	if input.ContentLength > 0 {
		q.Set("length", fmt.Sprintf("%d", input.ContentLength))
	}
	u.RawQuery = q.Encode()
	headers, err := BuildSignedUploadHeaders(input.ContentLength, input.SHA256)
	if err != nil {
		return SignCreateUploadResult{}, err
	}
	return SignCreateUploadResult{
		UploadURL: u.String(),
		ExpiresAt: expiresAt.UTC(),
		Headers:   cloneSignedUploadHeaders(headers),
	}, nil
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
			return fmt.Errorf("upload URL expired")
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

// gcsRegistryStoreIAMPermissions lists IAM permissions checked at publish bootstrap.
//
// Publish code never calls DeleteObject, but GCS authorization requires
// storage.objects.delete to overwrite an existing object during catalog
// compare-and-swap rewrites (NewWriter with generation match).
var gcsRegistryStoreIAMPermissions = []string{
	"storage.objects.get",
	"storage.objects.create",
	"storage.objects.delete",
}

func gcsRegistryStoreIAMPermissionsCopy() []string {
	out := make([]string, len(gcsRegistryStoreIAMPermissions))
	copy(out, gcsRegistryStoreIAMPermissions)
	return out
}

// CheckGCSRegistryStorePermissions verifies IAM permissions for publish CAS flows.
func CheckGCSRegistryStorePermissions(ctx context.Context, storageRoot string) error {
	bucket, err := gcsBucketFromStorageRoot(storageRoot)
	if err != nil {
		return err
	}
	client, err := storage.NewClient(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	required := gcsRegistryStoreIAMPermissionsCopy()
	permissions, err := client.Bucket(bucket).IAM().TestPermissions(ctx, required)
	if err != nil {
		return fmt.Errorf("test gcs registry object replacement permissions: %w", err)
	}
	requiredSet := make(map[string]struct{}, len(required))
	for _, permission := range required {
		requiredSet[permission] = struct{}{}
	}
	granted := make(map[string]struct{}, len(permissions))
	for _, permission := range permissions {
		granted[permission] = struct{}{}
	}
	var missing []string
	for permission := range requiredSet {
		if _, ok := granted[permission]; !ok {
			missing = append(missing, permission)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("gcs registry object replacement permissions missing: %s", strings.Join(missing, ", "))
	}
	return nil
}
