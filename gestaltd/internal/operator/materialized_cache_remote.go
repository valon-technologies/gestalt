package operator

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const materializedCacheRemoteEnv = "GESTALTD_SYNC_CACHE_REMOTE"
const materializedCacheRemoteScope = "https://www.googleapis.com/auth/devstorage.read_write"

type materializedCacheRemote interface {
	Get(context.Context, materializedCacheKey) (io.ReadCloser, bool, error)
	Exists(context.Context, materializedCacheKey) (bool, error)
	Put(context.Context, materializedCacheKey, string) error
}

var newMaterializedCacheRemote = newGCSMaterializedCacheRemote

type gcsMaterializedCacheRemote struct {
	bucket string
	prefix string
	client *http.Client
}

func materializedCacheFromSyncOptions(mode artifactMode, opts SyncOptions) (materializedCache, error) {
	if mode != artifactModeMaterialize {
		return materializedCache{}, nil
	}
	remoteURL := strings.TrimSpace(os.Getenv(materializedCacheRemoteEnv))
	if opts.CacheDir == "" {
		if remoteURL != "" {
			return materializedCache{}, fmt.Errorf("%s requires --cache-dir during gestaltd sync", materializedCacheRemoteEnv)
		}
		return materializedCache{}, nil
	}
	cache := materializedCache{dir: resolveCLIArtifactsDir(opts.CacheDir)}
	if remoteURL == "" {
		return cache, nil
	}
	remote, err := newMaterializedCacheRemote(remoteURL)
	if err != nil {
		return materializedCache{}, err
	}
	cache.remote = remote
	return cache, nil
}

func newGCSMaterializedCacheRemote(raw string) (materializedCacheRemote, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", materializedCacheRemoteEnv, err)
	}
	if u.Scheme != "gs" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("%s must be a gs://bucket/prefix URL", materializedCacheRemoteEnv)
	}
	return &gcsMaterializedCacheRemote{
		bucket: u.Host,
		prefix: strings.Trim(strings.TrimPrefix(u.Path, "/"), "/"),
	}, nil
}

func (r *gcsMaterializedCacheRemote) Get(ctx context.Context, key materializedCacheKey) (io.ReadCloser, bool, error) {
	object := r.objectName(key)
	client, err := r.httpClient(ctx)
	if err != nil {
		return nil, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.downloadURL(object), nil)
	if err != nil {
		return nil, false, fmt.Errorf("build materialized cache remote read request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("read materialized cache remote object %s: %w", object, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return nil, false, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := gcsMaterializedCacheStatusError("read", object, resp)
		_ = resp.Body.Close()
		return nil, false, err
	}
	return resp.Body, true, nil
}

func (r *gcsMaterializedCacheRemote) Exists(ctx context.Context, key materializedCacheKey) (bool, error) {
	object := r.objectName(key)
	client, err := r.httpClient(ctx)
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.metadataURL(object), nil)
	if err != nil {
		return false, fmt.Errorf("build materialized cache remote metadata request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("read materialized cache remote object metadata %s: %w", object, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, gcsMaterializedCacheStatusError("read metadata for", object, resp)
	}
	return true, nil
}

func (r *gcsMaterializedCacheRemote) Put(ctx context.Context, key materializedCacheKey, archivePath string) error {
	object := r.objectName(key)
	client, err := r.httpClient(ctx)
	if err != nil {
		return err
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open materialized cache archive for upload: %w", err)
	}
	defer func() { _ = file.Close() }()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.uploadURL(object), file)
	if err != nil {
		return fmt.Errorf("build materialized cache remote upload request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-tar")
	if info, err := file.Stat(); err == nil {
		req.ContentLength = info.Size()
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("upload materialized cache remote object %s: %w", object, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusPreconditionFailed {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return gcsMaterializedCacheStatusError("upload", object, resp)
	}
	return nil
}

func (r *gcsMaterializedCacheRemote) httpClient(ctx context.Context) (*http.Client, error) {
	if r.client != nil {
		return r.client, nil
	}
	tokenSource, err := google.DefaultTokenSource(ctx, materializedCacheRemoteScope)
	if err != nil {
		return nil, fmt.Errorf("create materialized cache GCS token source: %w", err)
	}
	return oauth2.NewClient(ctx, tokenSource), nil
}

func (r *gcsMaterializedCacheRemote) objectName(key materializedCacheKey) string {
	if r.prefix == "" {
		return key.Display + ".tar"
	}
	return path.Join(r.prefix, key.Display+".tar")
}

func (r *gcsMaterializedCacheRemote) downloadURL(object string) string {
	return r.metadataURL(object) + "?alt=media"
}

func (r *gcsMaterializedCacheRemote) metadataURL(object string) string {
	return "https://storage.googleapis.com/storage/v1/b/" + url.PathEscape(r.bucket) + "/o/" + escapeGCSPathSegment(object)
}

func (r *gcsMaterializedCacheRemote) uploadURL(object string) string {
	u := url.URL{
		Scheme: "https",
		Host:   "storage.googleapis.com",
		Path:   "/upload/storage/v1/b/" + url.PathEscape(r.bucket) + "/o",
	}
	q := u.Query()
	q.Set("uploadType", "media")
	q.Set("ifGenerationMatch", "0")
	q.Set("name", object)
	u.RawQuery = q.Encode()
	return u.String()
}

func gcsMaterializedCacheStatusError(action, object string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	detail := strings.TrimSpace(string(body))
	if detail == "" {
		return fmt.Errorf("%s materialized cache remote object %s: status %s", action, object, resp.Status)
	}
	return fmt.Errorf("%s materialized cache remote object %s: status %s: %s", action, object, resp.Status, detail)
}

func escapeGCSPathSegment(value string) string {
	return strings.ReplaceAll(url.PathEscape(value), "/", "%2F")
}
