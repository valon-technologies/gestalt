package pluginpkg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
)

const MaxPackageBytes = 512 << 20 // 512 MB

type DownloadResult struct {
	LocalPath string
	Cleanup   func()
	SHA256Hex string
}

func DownloadRequest(client *http.Client, req *http.Request) (*DownloadResult, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, req.URL)
	}

	tmp, err := createPackageTempFile("gestalt-plugin-*.tar.gz")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	removeTmp := func() { _ = os.Remove(tmpPath) }

	h := sha256.New()
	w := io.MultiWriter(tmp, h)
	if _, err := io.Copy(w, io.LimitReader(resp.Body, MaxPackageBytes+1)); err != nil {
		_ = tmp.Close()
		removeTmp()
		return nil, fmt.Errorf("download body: %w", err)
	}
	info, err := tmp.Stat()
	if err != nil {
		_ = tmp.Close()
		removeTmp()
		return nil, fmt.Errorf("stat temp file: %w", err)
	}
	if info.Size() > MaxPackageBytes {
		_ = tmp.Close()
		removeTmp()
		return nil, fmt.Errorf("download exceeds %d byte limit", MaxPackageBytes)
	}
	if err := tmp.Close(); err != nil {
		removeTmp()
		return nil, fmt.Errorf("close temp file: %w", err)
	}
	return &DownloadResult{
		LocalPath: tmpPath,
		Cleanup:   removeTmp,
		SHA256Hex: hex.EncodeToString(h.Sum(nil)),
	}, nil
}

func FetchPackage(ctx context.Context, url string) (localPath string, cleanup func(), err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", nil, fmt.Errorf("create request: %w", err)
	}
	result, err := DownloadRequest(http.DefaultClient, req)
	if err != nil {
		return "", nil, err
	}
	return result.LocalPath, result.Cleanup, nil
}
