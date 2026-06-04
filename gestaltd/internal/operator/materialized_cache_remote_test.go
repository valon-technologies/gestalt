package operator

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestGCSMaterializedCacheRemoteHTTPContracts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	req := materializedCacheUIRequest(dir, "alpha", "dest")
	key, eligible, err := materializedCacheKeyForRequest(req)
	if err != nil || !eligible {
		t.Fatalf("materializedCacheKeyForRequest eligible=%t err=%v", eligible, err)
	}

	objectName := "prefix/" + key.Display + ".tar.gz"
	var sawDownload bool
	var sawUpload bool
	remote := &gcsMaterializedCacheRemote{
		client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch r.Method {
			case http.MethodGet:
				sawDownload = true
				if got := r.URL.Query().Get("alt"); got != "media" {
					t.Fatalf("download alt query = %q, want media", got)
				}
				if got := r.URL.EscapedPath(); got != "/storage/v1/b/cache-bucket/o/"+escapeGCSPathSegment(objectName) {
					t.Fatalf("download escaped path = %q, want encoded object path", got)
				}
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(http.NoBody)}, nil
			case http.MethodPost:
				sawUpload = true
				if got := r.URL.Query().Get("uploadType"); got != "media" {
					t.Fatalf("uploadType = %q, want media", got)
				}
				if got := r.URL.Query().Get("ifGenerationMatch"); got != "0" {
					t.Fatalf("ifGenerationMatch = %q, want 0", got)
				}
				if got := r.URL.Query().Get("name"); got != objectName {
					t.Fatalf("upload name = %q, want %q", got, objectName)
				}
				if got := r.Header.Get("Content-Type"); got != "application/gzip" {
					t.Fatalf("upload content type = %q, want application/gzip", got)
				}
				return &http.Response{StatusCode: http.StatusPreconditionFailed, Body: io.NopCloser(http.NoBody)}, nil
			default:
				t.Fatalf("unexpected method %s", r.Method)
				return nil, nil
			}
		})},
		bucket: "cache-bucket",
		prefix: "prefix",
	}

	reader, hit, err := remote.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if reader != nil || hit {
		t.Fatalf("Get hit=%t reader=%v, want 404 miss", hit, reader)
	}

	archivePath := filepath.Join(dir, "entry.tar.gz")
	if err := os.WriteFile(archivePath, []byte("archive"), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	if err := remote.Put(context.Background(), key, archivePath); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !sawDownload || !sawUpload {
		t.Fatalf("saw download/upload = %t/%t, want both", sawDownload, sawUpload)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
