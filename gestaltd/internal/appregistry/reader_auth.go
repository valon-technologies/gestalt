package appregistry

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/gcsauth"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type gcpADCTokenSourceFactory func(context.Context) (oauth2.TokenSource, error)

// NewRegistryReader creates one reader for all configured app registries.
func NewRegistryReader(ctx context.Context, registries map[string]config.AppRegistryConfig) (*RegistryReader, error) {
	return newRegistryReader(ctx, registries, http.DefaultClient, func(ctx context.Context) (oauth2.TokenSource, error) {
		return google.DefaultTokenSource(ctx, gcsauth.ReadOnlyScope)
	})
}

func newRegistryReader(
	ctx context.Context,
	registries map[string]config.AppRegistryConfig,
	baseClient *http.Client,
	newTokenSource gcpADCTokenSourceFactory,
) (*RegistryReader, error) {
	buckets := make(map[string]struct{})
	for name, registry := range registries {
		switch registry.Auth {
		case "":
			continue
		case config.AppRegistryAuthGCPADC:
			storageURL, err := registry.StorageURL()
			if err != nil {
				return nil, fmt.Errorf("app registry %q: %w", name, err)
			}
			buckets[strings.TrimPrefix(storageURL, "gs://")] = struct{}{}
		default:
			return nil, fmt.Errorf("app registry %q has unsupported auth %q", name, registry.Auth)
		}
	}

	reader := &RegistryReader{HTTPClient: baseClient}
	if len(buckets) == 0 {
		return reader, nil
	}
	tokenSource, err := newTokenSource(ctx)
	if err != nil {
		return nil, fmt.Errorf("create app registry GCS ADC token source: %w", err)
	}
	reader.HTTPClient = gcsauth.NewHTTPClient(baseClient, tokenSource, func(rawURL *url.URL) bool {
		bucket, ok := gcsBucketFromPublicURL(rawURL)
		if !ok {
			return false
		}
		_, ok = buckets[bucket]
		return ok
	})
	return reader, nil
}

func gcsBucketFromPublicURL(rawURL *url.URL) (string, bool) {
	if !gcsauth.IsStorageURL(rawURL) {
		return "", false
	}
	bucket, _, _ := strings.Cut(strings.TrimPrefix(rawURL.Path, "/"), "/")
	return bucket, bucket != ""
}
