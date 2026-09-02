package operator

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const snapshotGCSReadOnlyScope = "https://www.googleapis.com/auth/devstorage.read_only"

func (l *Lifecycle) snapshotRepositoryHTTPClient(ctx context.Context, cfg *config.Config, entry *config.ProviderEntry) (*http.Client, error) {
	client := l.metadataHTTPClient()
	git := gitSourceDef(entry)
	if cfg == nil || git == nil || gitSourceMaterialization(git) != gitMaterializationSnapshot {
		return client, nil
	}
	repo, ok := cfg.ProviderSnapshotRepositories[strings.TrimSpace(git.ArtifactRepository)]
	if !ok || repo.Auth == "" {
		return client, nil
	}
	if repo.Auth != config.ProviderSnapshotRepositoryAuthGCPADC {
		return nil, fmt.Errorf("unsupported provider snapshot repository auth %q", repo.Auth)
	}

	newTokenSource := googleSnapshotTokenSource
	if l != nil && l.snapshotGCSTokenSource != nil {
		newTokenSource = l.snapshotGCSTokenSource
	}
	tokenSource, err := newTokenSource(ctx)
	if err != nil {
		return nil, fmt.Errorf("create provider snapshot GCS ADC token source: %w", err)
	}
	clone := *client
	clone.Transport = &gcsADCRoundTripper{
		base:        client.Transport,
		tokenSource: oauth2.ReuseTokenSource(nil, tokenSource),
	}
	return &clone, nil
}

func googleSnapshotTokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	return google.DefaultTokenSource(ctx, snapshotGCSReadOnlyScope)
}

type gcsADCRoundTripper struct {
	base        http.RoundTripper
	tokenSource oauth2.TokenSource
}

func (t *gcsADCRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	if !isGoogleCloudStorageURL(req.URL) {
		return base.RoundTrip(req)
	}
	token, err := t.tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("get provider snapshot GCS ADC token: %w", err)
	}
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	token.SetAuthHeader(clone)
	return base.RoundTrip(clone)
}

func isGoogleCloudStorageURL(u *url.URL) bool {
	if u == nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "storage.googleapis.com") {
		return false
	}
	return u.Port() == "" || u.Port() == "443"
}
