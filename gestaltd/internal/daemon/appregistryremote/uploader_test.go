package appregistryremote

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
)

func TestValidateSignedUploadLeaseHeadersAcceptsCanonicalHeaders(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("a", 64)
	headers, err := appregistry.BuildSignedUploadHeaders(42, digest)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateSignedUploadLeaseHeaders("linux/amd64", headers, 42, digest); err != nil {
		t.Fatalf("validateSignedUploadLeaseHeaders() = %v", err)
	}
}

func TestValidateSignedUploadLeaseHeadersRequiresLowercaseHex(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("A", 64)
	headers, err := appregistry.BuildSignedUploadHeaders(10, strings.ToLower(digest))
	if err != nil {
		t.Fatal(err)
	}
	headers[appregistry.UploadHeaderXGoogContentSHA256] = digest
	if err := validateSignedUploadLeaseHeaders("linux/amd64", headers, 10, strings.ToLower(digest)); err == nil {
		t.Fatal("expected content sha256 mismatch")
	}
}

func TestValidateSignedUploadLeaseHeadersRejectsMissingHeaders(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("b", 64)
	headers, err := appregistry.BuildSignedUploadHeaders(10, digest)
	if err != nil {
		t.Fatal(err)
	}
	delete(headers, appregistry.UploadHeaderXGoogMetaSHA256)
	err = validateSignedUploadLeaseHeaders("linux/amd64", headers, 10, digest)
	if err == nil || !strings.Contains(err.Error(), "missing signed upload header") {
		t.Fatalf("validateSignedUploadLeaseHeaders() = %v", err)
	}
}

func TestValidateSignedUploadLeaseHeadersRejectsUnexpectedHeaders(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("c", 64)
	headers, err := appregistry.BuildSignedUploadHeaders(10, digest)
	if err != nil {
		t.Fatal(err)
	}
	headers["Content-Type"] = "application/octet-stream"
	err = validateSignedUploadLeaseHeaders("linux/amd64", headers, 10, digest)
	if err == nil || !strings.Contains(err.Error(), "unexpected signed upload headers") {
		t.Fatalf("validateSignedUploadLeaseHeaders() = %v", err)
	}
}

func TestValidateSignedUploadLeaseHeadersRejectsContentLengthMismatch(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("d", 64)
	headers, err := appregistry.BuildSignedUploadHeaders(10, digest)
	if err != nil {
		t.Fatal(err)
	}
	err = validateSignedUploadLeaseHeaders("linux/amd64", headers, 11, digest)
	if err == nil || !strings.Contains(err.Error(), "Content-Length") {
		t.Fatalf("validateSignedUploadLeaseHeaders() = %v", err)
	}
}

func TestUploaderAppliesExactSignedHeaders(t *testing.T) {
	t.Parallel()
	data := []byte("artifact-bytes")
	path, digest := writeTestArtifact(t, data)
	headers, err := appregistry.BuildSignedUploadHeaders(int64(len(data)), digest)
	if err != nil {
		t.Fatal(err)
	}

	var gotHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	uploader := &Uploader{}
	if err := uploader.Upload(t.Context(), ArtifactUploadInput{
		Platform:  "linux/amd64",
		LocalPath: path,
		SHA256:    digest,
		UploadURL: server.URL + "/object",
		Headers:   headers,
	}); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	for name, value := range headers {
		if got := gotHeaders.Get(name); got != value {
			t.Fatalf("header %q = %q, want %q", name, got, value)
		}
	}
	if got := gotHeaders.Get("Content-Type"); got != "" {
		t.Fatalf("unexpected derived Content-Type = %q", got)
	}
}

func TestUploaderSkipsRequestOnHeaderMismatch(t *testing.T) {
	t.Parallel()
	data := []byte("artifact-bytes")
	path, digest := writeTestArtifact(t, data)
	headers, err := appregistry.BuildSignedUploadHeaders(int64(len(data)), digest)
	if err != nil {
		t.Fatal(err)
	}
	headers[appregistry.UploadHeaderContentLength] = "999"

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	uploader := &Uploader{}
	err = uploader.Upload(t.Context(), ArtifactUploadInput{
		Platform:  "linux/amd64",
		LocalPath: path,
		SHA256:    digest,
		UploadURL: server.URL + "/object",
		Headers:   headers,
	})
	if err == nil || !strings.Contains(err.Error(), "Content-Length") {
		t.Fatalf("Upload() = %v, want Content-Length mismatch", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("upload server saw %d requests, want 0", requests.Load())
	}
}

func TestUploaderErrorDoesNotLeakSignedURL(t *testing.T) {
	t.Parallel()
	data := []byte("artifact-bytes")
	path, digest := writeTestArtifact(t, data)
	secretURL := "https://storage.googleapis.com/bucket/object?X-Goog-Signature=super-secret"

	uploader := &Uploader{}
	err := uploader.Upload(t.Context(), ArtifactUploadInput{
		Platform:  "linux/amd64",
		LocalPath: path,
		SHA256:    digest,
		UploadURL: secretURL,
		Headers:   nil,
	})
	if err == nil {
		t.Fatal("expected missing headers error")
	}
	if strings.Contains(err.Error(), "super-secret") || strings.Contains(err.Error(), secretURL) {
		t.Fatalf("Upload() leaked signed URL: %v", err)
	}
}

func TestUploaderUploadsWithExactBody(t *testing.T) {
	t.Parallel()
	data := []byte("artifact-bytes")
	path, digest := writeTestArtifact(t, data)
	headers, err := appregistry.BuildSignedUploadHeaders(int64(len(data)), digest)
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			t.Errorf("read body: %v", readErr)
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != digest {
			http.Error(w, "digest mismatch", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	uploader := &Uploader{}
	if err := uploader.Upload(t.Context(), ArtifactUploadInput{
		Platform:  "linux/amd64",
		LocalPath: path,
		SHA256:    digest,
		UploadURL: server.URL + "/object",
		Headers:   headers,
	}); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
}

func writeTestArtifact(t *testing.T, data []byte) (path, digest string) {
	t.Helper()
	path = t.TempDir() + "/artifact.tgz"
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return path, hex.EncodeToString(sum[:])
}

func TestValidateSignedUploadLeaseHeadersRejectsEmptyMap(t *testing.T) {
	t.Parallel()
	err := validateSignedUploadLeaseHeaders("linux/amd64", nil, 1, strings.Repeat("e", 64))
	if err == nil || !strings.Contains(err.Error(), "headers are required") {
		t.Fatalf("validateSignedUploadLeaseHeaders() = %v", err)
	}
}

func TestUploaderValidationErrorDoesNotIncludeHeaderValues(t *testing.T) {
	t.Parallel()
	data := []byte("x")
	path, digest := writeTestArtifact(t, data)
	headers, err := appregistry.BuildSignedUploadHeaders(int64(len(data)), digest)
	if err != nil {
		t.Fatal(err)
	}
	headers[appregistry.UploadHeaderXGoogMetaSHA256] = strings.Repeat("f", 64)

	uploader := &Uploader{}
	err = uploader.Upload(t.Context(), ArtifactUploadInput{
		Platform:  "linux/amd64",
		LocalPath: path,
		SHA256:    digest,
		UploadURL: "https://upload.example/object",
		Headers:   headers,
	})
	if err == nil {
		t.Fatal("expected mismatch error")
	}
	if strings.Contains(err.Error(), strings.Repeat("f", 64)) {
		t.Fatalf("Upload() leaked header value: %v", err)
	}
}

func TestSignedUploadLeaseHeaderContract(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	buf.WriteString("payload")
	data := buf.Bytes()
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	headers, err := appregistry.BuildSignedUploadHeaders(int64(len(data)), digest)
	if err != nil {
		t.Fatal(err)
	}
	if headers[appregistry.UploadHeaderXGoogContentSHA256] != digest {
		t.Fatalf("content sha256 = %q, want lowercase hex %q", headers[appregistry.UploadHeaderXGoogContentSHA256], digest)
	}
	if headers[appregistry.UploadHeaderXGoogMetaSHA256] != digest {
		t.Fatalf("meta sha256 = %q, want lowercase hex %q", headers[appregistry.UploadHeaderXGoogMetaSHA256], digest)
	}
}
