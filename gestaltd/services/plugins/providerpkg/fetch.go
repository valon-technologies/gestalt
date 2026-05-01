package providerpkg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const MaxPackageBytes = 512 << 20 // 512 MB

var packageFetchRetryDelays = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
}

type DownloadResult struct {
	LocalPath string
	Cleanup   func()
	SHA256Hex string
}

func DownloadRequest(client *http.Client, req *http.Request) (*DownloadResult, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if req == nil {
		return nil, fmt.Errorf("request is required")
	}
	var lastErr error
	for attempt := 0; ; attempt++ {
		result, retry, err := downloadRequestOnce(client, req)
		if !retry {
			return result, err
		}
		lastErr = err
		if attempt >= len(packageFetchRetryDelays) {
			return nil, lastErr
		}
		if err := waitPackageFetchRetry(req.Context(), packageFetchRetryDelays[attempt]); err != nil {
			return nil, err
		}
	}
}

func downloadRequestOnce(client *http.Client, req *http.Request) (*DownloadResult, bool, error) {
	attemptReq := req.Clone(req.Context())
	resp, err := client.Do(attemptReq)
	if err != nil {
		return nil, true, fmt.Errorf("execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("unexpected status %d from %s", resp.StatusCode, req.URL)
		return nil, isTransientPackageFetchHTTPStatus(resp.StatusCode), err
	}

	tmp, err := createPackageTempFile("gestalt-plugin-*.tar.gz")
	if err != nil {
		return nil, false, err
	}
	tmpPath := tmp.Name()
	removeTmp := func() { _ = os.Remove(tmpPath) }

	h := sha256.New()
	w := io.MultiWriter(tmp, h)
	if _, err := io.Copy(w, io.LimitReader(resp.Body, MaxPackageBytes+1)); err != nil {
		_ = tmp.Close()
		removeTmp()
		return nil, true, fmt.Errorf("download body: %w", err)
	}
	info, err := tmp.Stat()
	if err != nil {
		_ = tmp.Close()
		removeTmp()
		return nil, false, fmt.Errorf("stat temp file: %w", err)
	}
	if info.Size() > MaxPackageBytes {
		_ = tmp.Close()
		removeTmp()
		return nil, false, fmt.Errorf("download exceeds %d byte limit", MaxPackageBytes)
	}
	if err := tmp.Close(); err != nil {
		removeTmp()
		return nil, false, fmt.Errorf("close temp file: %w", err)
	}
	return &DownloadResult{
		LocalPath: tmpPath,
		Cleanup:   removeTmp,
		SHA256Hex: hex.EncodeToString(h.Sum(nil)),
	}, false, nil
}

func waitPackageFetchRetry(ctx context.Context, delay time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isTransientPackageFetchHTTPStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
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
