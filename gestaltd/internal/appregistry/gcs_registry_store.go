package appregistry

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"cloud.google.com/go/storage"
)

// GCSRegistryStore implements WritableRegistryStore against one GCS bucket using
// the production storage client and generation preconditions.
type GCSRegistryStore struct {
	SourceRef string

	clientOnce sync.Once
	client     *storage.Client
	clientErr  error
}

func NewGCSRegistryStore(sourceRef string) *GCSRegistryStore {
	sourceRef = strings.TrimSpace(sourceRef)
	if sourceRef == "" {
		sourceRef = "gestaltd-publish"
	}
	return &GCSRegistryStore{SourceRef: sourceRef}
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
	bucket, object, err := parseGCSStorageURL(storageURL)
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
	bucket, object, err := parseGCSStorageURL(storageURL)
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
	bucket, object, err := parseGCSStorageURL(input.StorageURL)
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
	bucket, object, err := parseGCSStorageURL(input.StorageURL)
	if err != nil {
		return err
	}
	obj := client.Bucket(bucket).Object(object)
	var writer *storage.Writer
	switch {
	case input.Generation == 0:
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
	srcBucket, srcObject, err := parseGCSStorageURL(input.SourceURL)
	if err != nil {
		return err
	}
	destBucket, destObject, err := parseGCSStorageURL(input.DestURL)
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
	copier.ObjectAttrs.Metadata = gcsObjectMetadata(input.SourceRef, expected)
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
	metadata := map[string]string{
		"source-ref": strings.TrimSpace(sourceRef),
	}
	if digest := strings.ToLower(strings.TrimSpace(sha256)); digest != "" {
		metadata["sha256"] = digest
	}
	return metadata
}
