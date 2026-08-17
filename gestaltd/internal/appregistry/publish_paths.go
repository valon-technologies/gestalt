package appregistry

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"unicode"
)

func validatePublishArtifactFilename(filename string) error {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return fmt.Errorf("filename is required")
	}
	if filename != filepath.Base(filename) {
		return fmt.Errorf("filename must be a safe basename")
	}
	if strings.ContainsAny(filename, "/\\") {
		return fmt.Errorf("filename must not contain path separators")
	}
	if filename == "." || filename == ".." {
		return fmt.Errorf("filename must not be a dot segment")
	}
	if strings.Contains(filename, "..") {
		return fmt.Errorf("filename must not contain dot segments")
	}
	for _, r := range filename {
		if r < 0x20 || r == 0x7f || unicode.IsControl(r) {
			return fmt.Errorf("filename must not contain control characters")
		}
	}
	return nil
}

func normalizePublishArtifactSHA256(digest string) (string, error) {
	digest = strings.ToLower(strings.TrimSpace(digest))
	if digest == "" {
		return "", fmt.Errorf("sha256 is required")
	}
	if len(digest) != 64 {
		return "", fmt.Errorf("sha256 must be 64 hex characters")
	}
	for _, r := range digest {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return "", fmt.Errorf("sha256 must be lowercase hex")
		}
	}
	return digest, nil
}

func normalizePublishPlatform(platform string) (string, error) {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return "", fmt.Errorf("platform is required")
	}
	if err := validateArtifactPlatform(platform); err != nil {
		return "", err
	}
	goos, goarch, err := parsePlatformString(platform)
	if err != nil {
		return "", err
	}
	return goos + "/" + goarch, nil
}

func parsePlatformString(platform string) (string, string, error) {
	return packageioParsePlatform(platform)
}

// JoinRegistryObjectPath joins path segments under prefix and rejects traversal escapes.
func JoinRegistryObjectPath(prefix string, segments ...string) (string, error) {
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		return "", fmt.Errorf("path prefix is required")
	}
	cleanPrefix := path.Clean(prefix)
	if cleanPrefix == "." || strings.HasPrefix(cleanPrefix, "../") {
		return "", fmt.Errorf("invalid path prefix %q", prefix)
	}
	parts := []string{cleanPrefix}
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			return "", fmt.Errorf("path segment is required")
		}
		if segment == "." || segment == ".." {
			return "", fmt.Errorf("invalid path segment %q", segment)
		}
		if strings.ContainsAny(segment, "/\\") {
			return "", fmt.Errorf("path segment %q must not contain path separators", segment)
		}
		for _, r := range segment {
			if r < 0x20 || r == 0x7f || unicode.IsControl(r) {
				return "", fmt.Errorf("path segment %q contains control characters", segment)
			}
		}
		parts = append(parts, segment)
	}
	joined := path.Join(parts...)
	if joined != cleanPrefix && !strings.HasPrefix(joined, cleanPrefix+"/") {
		return "", fmt.Errorf("joined path %q escapes prefix %q", joined, cleanPrefix)
	}
	return joined, nil
}

func PublishVersionStagingPrefix(appName, version string) string {
	return path.Join("apps", strings.TrimSpace(appName), "publish-staging", strings.TrimSpace(version))
}

func PublishStagingPrefix(appName, version, declarationDigest string) string {
	base := PublishVersionStagingPrefix(appName, version)
	joined, err := JoinRegistryObjectPath(base, strings.TrimSpace(declarationDigest))
	if err != nil {
		return path.Join(base, strings.TrimSpace(declarationDigest))
	}
	return joined
}

func PublishStagingArtifactPath(stagingPrefix, platform, filename string) (string, error) {
	goos, goarch, err := parsePlatformString(platform)
	if err != nil {
		return "", err
	}
	return JoinRegistryObjectPath(stagingPrefix, "artifacts", goos, goarch, strings.TrimSpace(filename))
}

func PublishArtifactFinalRel(artifactPrefix, filename string) (string, error) {
	return JoinRegistryObjectPath(strings.TrimSpace(artifactPrefix), strings.TrimSpace(filename))
}
