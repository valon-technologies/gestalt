package appregistry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

func verifyArtifactDescribed(described ObjectDescription, artifact PublishDeclarationArtifact) error {
	if described.Generation == 0 {
		return fmt.Errorf("%w: %s", ErrPublishUploadMissing, artifact.Platform)
	}
	expected, err := normalizePublishArtifactSHA256(artifact.SHA256)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrPublishUploadMismatch, artifact.Platform)
	}
	if !strings.EqualFold(strings.TrimSpace(described.SHA256), expected) {
		return fmt.Errorf("%w: %s", ErrPublishUploadMismatch, artifact.Platform)
	}
	if artifact.Size > 0 && described.Size > 0 && described.Size != artifact.Size {
		return fmt.Errorf("%w: %s size mismatch", ErrPublishUploadMismatch, artifact.Platform)
	}
	return nil
}

func verifyArtifactStored(store RegistryObjectStore, storageURL string, artifact PublishDeclarationArtifact) (ObjectDescription, error) {
	var zero ObjectDescription
	described, err := store.DescribeObject(storageURL)
	if err != nil {
		return zero, err
	}
	if err := verifyArtifactDescribed(described, artifact); err != nil {
		return zero, err
	}
	_, data, err := store.ReadObject(storageURL)
	if err != nil {
		return zero, err
	}
	if described.Generation == 0 || len(data) == 0 {
		return zero, fmt.Errorf("%w: %s", ErrPublishUploadMissing, artifact.Platform)
	}
	if err := verifyObjectDigestBytes(data, artifact.SHA256); err != nil {
		return zero, fmt.Errorf("%w: %s", err, artifact.Platform)
	}
	if artifact.Size > 0 && int64(len(data)) != artifact.Size {
		return zero, fmt.Errorf("%w: %s size mismatch", ErrPublishUploadMismatch, artifact.Platform)
	}
	return described, nil
}

func verifyObjectDigestBytes(data []byte, expectedSHA256 string) error {
	expected, err := normalizePublishArtifactSHA256(expectedSHA256)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), expected) {
		return ErrPublishUploadMismatch
	}
	return nil
}
