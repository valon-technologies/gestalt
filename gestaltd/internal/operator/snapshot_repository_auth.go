package operator

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/gcsauth"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

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
	return gcsauth.NewHTTPClient(client, tokenSource, gcsauth.IsStorageURL), nil
}

func googleSnapshotTokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	return google.DefaultTokenSource(ctx, gcsauth.ReadOnlyScope)
}
