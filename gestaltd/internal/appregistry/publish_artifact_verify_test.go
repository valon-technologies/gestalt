package appregistry

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

func TestVerifyArtifactStoredRejectsMetadataOnlyMatch(t *testing.T) {
	t.Parallel()

	store := NewMemoryObjectStore()
	artifactBytes := []byte("artifact-bytes")
	wrongBytes := []byte("wrong-bytes")
	sum := sha256.Sum256(artifactBytes)
	digest := hex.EncodeToString(sum[:])
	storageURL := "gs://gestalt-app-registry/apps/demo/publish-staging/0.1.0/digest/artifacts/linux/amd64/file.tgz"
	store.mu.Lock()
	store.nextGen++
	store.objects[storageURL] = memoryStoredObject{
		generation: store.nextGen,
		data:       wrongBytes,
		sha256:     digest,
		size:       int64(len(wrongBytes)),
	}
	store.mu.Unlock()

	artifact := PublishDeclarationArtifact{
		Platform: "linux/amd64",
		Filename: "file.tgz",
		SHA256:   digest,
		Size:     int64(len(artifactBytes)),
	}
	_, err := verifyArtifactStored(store, storageURL, artifact)
	if err == nil || !errors.Is(err, ErrPublishUploadMismatch) {
		t.Fatalf("verifyArtifactStored() = %v, want ErrPublishUploadMismatch", err)
	}
}

func TestVerifyObjectDigestBytes(t *testing.T) {
	t.Parallel()

	data := []byte("artifact-bytes")
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	if err := verifyObjectDigestBytes(data, digest); err != nil {
		t.Fatalf("verifyObjectDigestBytes() = %v", err)
	}
	if err := verifyObjectDigestBytes([]byte("other"), digest); err != ErrPublishUploadMismatch {
		t.Fatalf("verifyObjectDigestBytes() = %v, want ErrPublishUploadMismatch", err)
	}
}
