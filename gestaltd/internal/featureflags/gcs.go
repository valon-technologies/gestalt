package featureflags

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"cloud.google.com/go/storage"
)

const maxFlagObjectBytes = 1024

type objectReader interface {
	ReadObject(context.Context, string, string) ([]byte, error)
}

type gcsObjectReader struct {
	client *storage.Client
}

func (r gcsObjectReader) ReadObject(ctx context.Context, bucket, object string) ([]byte, error) {
	reader, err := r.client.Bucket(bucket).Object(object).NewReader(ctx)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(reader, maxFlagObjectBytes+1))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(data) > maxFlagObjectBytes {
		return nil, fmt.Errorf("object exceeds %d bytes", maxFlagObjectBytes)
	}
	return data, nil
}

// LoadGCS resolves all declared feature flags from a GCS bucket using
// application-default credentials. An empty bucket returns declared defaults
// without creating a GCS client.
func LoadGCS(ctx context.Context, bucket string) (Snapshot, error) {
	bucket = strings.TrimSpace(bucket)
	if bucket == "" {
		return Defaults(), nil
	}
	client, err := storage.NewClient(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("create GCS feature flag client: %w", err)
	}
	defer client.Close()
	return load(ctx, bucket, gcsObjectReader{client: client})
}

func load(ctx context.Context, bucket string, reader objectReader) (Snapshot, error) {
	values := make(map[Flag]bool, len(declared))
	for _, flag := range declared {
		data, err := reader.ReadObject(ctx, bucket, flag.Name())
		if errors.Is(err, storage.ErrObjectNotExist) {
			values[flag] = flag.Default()
			continue
		}
		if err != nil {
			return Snapshot{}, fmt.Errorf("read feature flag %q from bucket %q: %w", flag.Name(), bucket, err)
		}
		if len(data) > maxFlagObjectBytes {
			return Snapshot{}, fmt.Errorf("read feature flag %q from bucket %q: object exceeds %d bytes", flag.Name(), bucket, maxFlagObjectBytes)
		}
		value, err := parseBoolean(data)
		if err != nil {
			return Snapshot{}, fmt.Errorf("parse feature flag %q from bucket %q: %w", flag.Name(), bucket, err)
		}
		values[flag] = value
	}
	return NewSnapshot(values), nil
}

func parseBoolean(data []byte) (bool, error) {
	switch string(bytes.TrimSpace(data)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("content must be exactly true or false")
	}
}
