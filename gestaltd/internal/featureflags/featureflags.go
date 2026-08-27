package featureflags

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"cloud.google.com/go/storage"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const BucketEnv = "GESTALTD_FEATURE_FLAGS_BUCKET"

type Flag string

const (
	Agent    Flag = "agent"
	Workflow Flag = "workflow"
)

var flags = [...]Flag{Agent, Workflow}

// Snapshot is immutable after startup. Missing flags are disabled.
type Snapshot map[Flag]bool

func Defaults() Snapshot { return nil }

func AllEnabled() Snapshot {
	return Snapshot{Agent: true, Workflow: true}
}

func (s Snapshot) Enabled(flag Flag) bool { return s[flag] }

type DisabledError Flag

func (e DisabledError) Error() string { return fmt.Sprintf("%s feature is not enabled", Flag(e)) }

func (e DisabledError) GRPCStatus() *status.Status {
	return status.New(codes.FailedPrecondition, e.Error())
}

func Disabled(flag Flag) error { return DisabledError(flag) }

type readObjectFunc func(context.Context, string, string) ([]byte, error)

// LoadGCS reads each flag once using application-default credentials.
func LoadGCS(ctx context.Context, bucket string) (snapshot Snapshot, err error) {
	bucket = strings.TrimSpace(bucket)
	if bucket == "" {
		return Defaults(), nil
	}
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("create GCS client: %w", err)
	}
	defer func() { err = errors.Join(err, client.Close()) }()
	return load(ctx, bucket, func(ctx context.Context, bucket, object string) ([]byte, error) {
		reader, err := client.Bucket(bucket).Object(object).NewReader(ctx)
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, 1025))
		return data, errors.Join(readErr, reader.Close())
	})
}

func load(ctx context.Context, bucket string, readObject readObjectFunc) (Snapshot, error) {
	snapshot := make(Snapshot, len(flags))
	for _, flag := range flags {
		data, err := readObject(ctx, bucket, string(flag))
		if errors.Is(err, storage.ErrObjectNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s feature flag: %w", flag, err)
		}
		switch string(bytes.TrimSpace(data)) {
		case "true":
			snapshot[flag] = true
		case "false":
		default:
			return nil, fmt.Errorf("parse %s feature flag: content must be true or false", flag)
		}
	}
	return snapshot, nil
}
