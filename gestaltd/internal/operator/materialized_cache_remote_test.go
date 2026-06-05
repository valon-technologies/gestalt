package operator

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

	objectName := "prefix/" + key.Display + ".tar"
	var sawList bool
	var sawDownload bool
	var sawExists bool
	var sawUpload bool
	remote := &gcsMaterializedCacheRemote{
		client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch r.Method {
			case http.MethodGet:
				if r.URL.EscapedPath() == "/storage/v1/b/cache-bucket/o" {
					sawList = true
					if got := r.URL.Query().Get("prefix"); got != "prefix/materialized/v1/" {
						t.Fatalf("list prefix = %q, want materialized cache prefix", got)
					}
					if got := r.URL.Query().Get("fields"); got != "nextPageToken,items/name" {
						t.Fatalf("list fields = %q, want narrow object projection", got)
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"items":[{"name":"` + objectName + `"}]}`)),
					}, nil
				}
				if r.URL.Query().Get("alt") == "" {
					sawExists = true
					if got := r.URL.EscapedPath(); got != "/storage/v1/b/cache-bucket/o/"+escapeGCSPathSegment(objectName) {
						t.Fatalf("exists escaped path = %q, want encoded object path", got)
					}
					return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(http.NoBody)}, nil
				}
				sawDownload = true
				if got := r.URL.Query().Get("alt"); got != "media" {
					t.Fatalf("download alt query = %q, want media", got)
				}
				if got := r.URL.EscapedPath(); got != "/storage/v1/b/cache-bucket/o/"+escapeGCSPathSegment(objectName) {
					t.Fatalf("download escaped path = %q, want encoded object path", got)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("archive"))}, nil
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
				if got := r.Header.Get("Content-Type"); got != "application/x-tar" {
					t.Fatalf("upload content type = %q, want application/x-tar", got)
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

	objects, err := remote.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(objects) != 1 || objects[0] != key.Display {
		t.Fatalf("List objects = %#v, want listed cache object", objects)
	}
	reader, err := remote.Get(context.Background(), objects[0])
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	_ = reader.Close()
	exists, err := remote.Exists(context.Background(), key)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Fatal("Exists = true, want 404 miss")
	}

	archivePath := filepath.Join(dir, "entry.tar")
	if err := os.WriteFile(archivePath, []byte("archive"), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	if err := remote.Put(context.Background(), key, archivePath); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !sawList || !sawDownload || !sawExists || !sawUpload {
		t.Fatalf("saw list/download/exists/upload = %t/%t/%t/%t, want all", sawList, sawDownload, sawExists, sawUpload)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
