package appregistryremote

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
)

const defaultUploadTimeout = 30 * time.Minute

var signedUploadHeaderOrder = []string{
	appregistry.UploadHeaderContentLength,
	appregistry.UploadHeaderXGoogIfGenerationMatch,
	appregistry.UploadHeaderXGoogMetaSHA256,
	appregistry.UploadHeaderXGoogContentSHA256,
}

// ArtifactUploadInput describes a local archive upload to a scoped signed URL.
type ArtifactUploadInput struct {
	Platform  string
	LocalPath string
	SHA256    string
	UploadURL string
	Headers   map[string]string
}

// Uploader streams local artifacts to signed upload URLs.
type Uploader struct {
	HTTPClient *http.Client
}

func (u *Uploader) Upload(ctx context.Context, input ArtifactUploadInput) error {
	if u == nil {
		return fmt.Errorf("upload client is not configured")
	}
	platform := strings.TrimSpace(input.Platform)
	localPath := strings.TrimSpace(input.LocalPath)
	if localPath == "" {
		return fmt.Errorf("upload local path is required")
	}
	uploadURL := strings.TrimSpace(input.UploadURL)
	if uploadURL == "" {
		return fmt.Errorf("upload URL is required for platform %q", platform)
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
	if err := validateSignedUploadLeaseHeaders(platform, input.Headers, info.Size(), input.SHA256); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, file)
	if err != nil {
		return err
	}
	req.ContentLength = info.Size()
	for key, value := range input.Headers {
		req.Header.Set(key, value)
	}
	resp, err := u.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("upload platform %q: %w", platform, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("upload platform %q returned %d: %s", platform, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (u *Uploader) httpClient() *http.Client {
	if u != nil && u.HTTPClient != nil {
		return u.HTTPClient
	}
	return &http.Client{Timeout: defaultUploadTimeout}
}

func validateSignedUploadLeaseHeaders(platform string, headers map[string]string, fileSize int64, sha256Hex string) error {
	if len(headers) == 0 {
		return fmt.Errorf("upload platform %q: signed upload headers are required", platform)
	}
	expected, err := appregistry.BuildSignedUploadHeaders(fileSize, sha256Hex)
	if err != nil {
		return fmt.Errorf("upload platform %q: %w", platform, err)
	}
	for _, name := range signedUploadHeaderOrder {
		got, ok := headers[name]
		if !ok {
			return fmt.Errorf("upload platform %q: missing signed upload header %q", platform, name)
		}
		if got != expected[name] {
			return fmt.Errorf("upload platform %q: signed upload header %q mismatch", platform, name)
		}
	}
	extra := make([]string, 0)
	for name := range headers {
		found := false
		for _, expectedName := range signedUploadHeaderOrder {
			if name == expectedName {
				found = true
				break
			}
		}
		if !found {
			extra = append(extra, name)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		return fmt.Errorf("upload platform %q: unexpected signed upload headers: %s", platform, strings.Join(extra, ", "))
	}
	return nil
}
