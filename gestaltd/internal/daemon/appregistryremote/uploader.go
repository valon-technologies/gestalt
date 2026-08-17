package appregistryremote

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const defaultUploadTimeout = 30 * time.Minute

// ArtifactUploadInput describes a local archive upload to a scoped signed URL.
type ArtifactUploadInput struct {
	Platform  string
	LocalPath string
	SHA256    string
	UploadURL string
}

// Uploader streams local artifacts to signed upload URLs.
type Uploader struct {
	HTTPClient *http.Client
}

func (u *Uploader) Upload(ctx context.Context, input ArtifactUploadInput) error {
	if u == nil {
		return fmt.Errorf("upload client is not configured")
	}
	localPath := strings.TrimSpace(input.LocalPath)
	if localPath == "" {
		return fmt.Errorf("upload local path is required")
	}
	uploadURL := strings.TrimSpace(input.UploadURL)
	if uploadURL == "" {
		return fmt.Errorf("upload URL is required for platform %q", strings.TrimSpace(input.Platform))
	}
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", localPath, err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", localPath, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, file)
	if err != nil {
		return err
	}
	req.ContentLength = info.Size()
	headers, err := signedUploadHeaders(uploadURL, input.SHA256)
	if err != nil {
		return err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := u.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("upload platform %q: %w", strings.TrimSpace(input.Platform), err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("upload platform %q returned %d: %s", strings.TrimSpace(input.Platform), resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (u *Uploader) httpClient() *http.Client {
	if u != nil && u.HTTPClient != nil {
		return u.HTTPClient
	}
	return &http.Client{Timeout: defaultUploadTimeout}
}

func signedUploadHeaders(uploadURL, digestHex string) (map[string]string, error) {
	headers := map[string]string{
		"Content-Type": "application/octet-stream",
	}
	digestHex = strings.ToLower(strings.TrimSpace(digestHex))
	if digestHex == "" {
		return headers, nil
	}
	sum, err := hex.DecodeString(digestHex)
	if err != nil {
		return nil, fmt.Errorf("decode artifact sha256: %w", err)
	}
	if len(sum) != sha256.Size {
		return nil, fmt.Errorf("artifact sha256 must be %d bytes", sha256.Size)
	}
	headers["x-goog-content-sha256"] = base64.StdEncoding.EncodeToString(sum)
	parsed, err := url.Parse(uploadURL)
	if err != nil {
		return headers, nil
	}
	if strings.EqualFold(parsed.Scheme, "memory-upload") {
		headers["X-Goog-Content-SHA256"] = headers["x-goog-content-sha256"]
	}
	return headers, nil
}
