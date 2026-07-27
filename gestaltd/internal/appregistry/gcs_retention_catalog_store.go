package appregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"cloud.google.com/go/storage"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"google.golang.org/api/googleapi"
)

const retentionCatalogUpdateAttempts = 5

// GCSCatalogStore mutates apps/{app}/retention.json in GCS with optimistic concurrency.
type GCSCatalogStore struct {
	Registries map[string]config.AppRegistryConfig
	SourceRef  string

	clientOnce sync.Once
	client     *storage.Client
	clientErr  error
}

func NewGCSCatalogStore(registries map[string]config.AppRegistryConfig) *GCSCatalogStore {
	return &GCSCatalogStore{
		Registries: registries,
		SourceRef:  "gestaltd-serve",
	}
}

func (s *GCSCatalogStore) storageClient(ctx context.Context) (*storage.Client, error) {
	s.clientOnce.Do(func() {
		s.client, s.clientErr = storage.NewClient(ctx)
	})
	return s.client, s.clientErr
}

func (s *GCSCatalogStore) MutateRetention(ctx context.Context, registryName, appName string, mutate func(*RetentionIndex) (bool, error)) error {
	if s == nil {
		return fmt.Errorf("retention catalog store is not configured")
	}
	registry, ok := s.Registries[registryName]
	if !ok {
		return fmt.Errorf("registry %q is not configured", registryName)
	}
	storageRoot, err := registry.StorageURL()
	if err != nil {
		return fmt.Errorf("resolve registry storage URL: %w", err)
	}
	bucket, object, err := gcsObjectRef(storageRoot, AppRetentionPath(appName))
	if err != nil {
		return err
	}
	client, err := s.storageClient(ctx)
	if err != nil {
		return fmt.Errorf("create storage client: %w", err)
	}
	sourceRef := strings.TrimSpace(s.SourceRef)
	if sourceRef == "" {
		sourceRef = "gestaltd-serve"
	}

	for attempt := 1; attempt <= retentionCatalogUpdateAttempts; attempt++ {
		generation, index, err := readRetentionObject(ctx, client, bucket, object)
		if err != nil {
			return err
		}
		changed, err := mutate(index)
		if err != nil {
			return err
		}
		if !changed {
			return nil
		}
		data, err := json.MarshalIndent(index, "", "  ")
		if err != nil {
			return err
		}
		data = append(data, '\n')
		if err := writeRetentionObject(ctx, client, bucket, object, data, generation, sourceRef); err != nil {
			if gcsPreconditionFailed(err) && attempt < retentionCatalogUpdateAttempts {
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("update gs://%s/%s: exceeded retry limit after concurrent retention updates", bucket, object)
}

func readRetentionObject(ctx context.Context, client *storage.Client, bucket, object string) (int64, *RetentionIndex, error) {
	attrs, err := client.Bucket(bucket).Object(object).Attrs(ctx)
	if err == storage.ErrObjectNotExist {
		return 0, NewEmptyRetentionIndex(), nil
	}
	if err != nil {
		return 0, nil, fmt.Errorf("read retention object attrs: %w", err)
	}
	reader, err := client.Bucket(bucket).Object(object).NewReader(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("open retention object: %w", err)
	}
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil {
		return 0, nil, fmt.Errorf("read retention object: %w", err)
	}
	if len(body) == 0 {
		return attrs.Generation, NewEmptyRetentionIndex(), nil
	}
	index, err := DecodeRetentionIndex(body)
	if err != nil {
		return 0, nil, err
	}
	return attrs.Generation, index, nil
}

func writeRetentionObject(ctx context.Context, client *storage.Client, bucket, object string, data []byte, generation int64, sourceRef string) error {
	writer := client.Bucket(bucket).Object(object).If(storage.Conditions{GenerationMatch: generation}).NewWriter(ctx)
	writer.ContentType = "application/json"
	if sourceRef != "" {
		writer.Metadata = map[string]string{"source-ref": sourceRef}
	}
	if _, err := writer.Write(data); err != nil {
		_ = writer.Close()
		return fmt.Errorf("write retention object: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finalize retention object: %w", err)
	}
	return nil
}

func gcsObjectRef(storageRoot, relPath string) (string, string, error) {
	bucket := strings.TrimPrefix(strings.TrimSpace(storageRoot), "gs://")
	bucket = strings.Trim(bucket, "/")
	if bucket == "" || strings.Contains(bucket, "/") {
		return "", "", fmt.Errorf("invalid storage root %q", storageRoot)
	}
	object := strings.TrimPrefix(strings.TrimSpace(relPath), "/")
	if object == "" {
		return "", "", fmt.Errorf("object path is required")
	}
	return bucket, object, nil
}

func gcsPreconditionFailed(err error) bool {
	if err == nil {
		return false
	}
	if apiErr, ok := err.(*googleapi.Error); ok && apiErr.Code == 412 {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "precondition") || strings.Contains(text, "generation") || strings.Contains(text, "412")
}
