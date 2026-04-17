package github

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
)

const (
	headerAccept        = "Accept"
	headerAuthorization = "Authorization"
	acceptOctetStream   = "application/octet-stream"
	authTokenPrefix     = "token "
)

func ResolveGitHubToken(token string) string {
	for _, candidate := range []string{
		strings.TrimSpace(token),
		strings.TrimSpace(os.Getenv("GITHUB_TOKEN")),
		strings.TrimSpace(os.Getenv("GH_TOKEN")),
	} {
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func DownloadResolvedAsset(ctx context.Context, client *http.Client, assetURL, token string) (*providerpkg.DownloadResult, error) {
	if client == nil {
		client = http.DefaultClient
	}
	token = ResolveGitHubToken(token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create asset download request: %w", err)
	}
	req.Header.Set(headerAccept, acceptOctetStream)
	if token != "" {
		req.Header.Set(headerAuthorization, authTokenPrefix+token)
	}
	return providerpkg.DownloadRequest(client, req)
}
