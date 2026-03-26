package pluginpkg

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
)

const maxPackageBytes = 512 << 20 // 512 MB

func FetchPackage(ctx context.Context, url string) (localPath string, cleanup func(), err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", nil, fmt.Errorf("create request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("fetch package: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("fetch package: HTTP %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp("", "gestalt-plugin-*.tar.gz")
	if err != nil {
		return "", nil, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	removeTmp := func() { _ = os.Remove(tmpPath) }

	if _, err := io.Copy(tmp, io.LimitReader(resp.Body, maxPackageBytes+1)); err != nil {
		_ = tmp.Close()
		removeTmp()
		return "", nil, fmt.Errorf("download package: %w", err)
	}
	info, err := tmp.Stat()
	if err != nil {
		_ = tmp.Close()
		removeTmp()
		return "", nil, fmt.Errorf("stat temp file: %w", err)
	}
	if info.Size() > maxPackageBytes {
		_ = tmp.Close()
		removeTmp()
		return "", nil, fmt.Errorf("package exceeds %d byte limit", maxPackageBytes)
	}
	if err := tmp.Close(); err != nil {
		removeTmp()
		return "", nil, fmt.Errorf("close temp file: %w", err)
	}
	return tmpPath, removeTmp, nil
}
