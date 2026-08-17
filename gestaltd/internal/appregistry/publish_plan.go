package appregistry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	PublishManifestSchemaVersion = "gestaltd.app.publish.plan.v1"
	RepublishCorruptObjectGuidance = "delete the object or entire snapshot SHA prefix and republish"
)

// BuildPublishManifestInput collects everything required to plan a registry publish.
type BuildPublishManifestInput struct {
	StorageRoot    string
	PublicRoot     string
	DisplayName    string
	Description    string
	EntryInput     BuildEntryInput
	LocalArtifacts []LocalPublishArtifact
	OnHashStart    func(fileCount int)
	OnHashDone     func(fileCount int)
}

// LocalPublishArtifact is a release archive on disk awaiting upload.
type LocalPublishArtifact struct {
	Target    string
	LocalPath string
}

// PublishObjectKind identifies immutable publish object types.
type PublishObjectKind string

const (
	PublishObjectKindEntry   PublishObjectKind = "entry"
	PublishObjectKindArchive PublishObjectKind = "archive"
	PublishObjectKindIndex   PublishObjectKind = "index"
)

// PublishObject describes one registry object in a publish plan.
type PublishObject struct {
	Kind       PublishObjectKind `json:"kind"`
	Target     string            `json:"target,omitempty"`
	LocalPath  string            `json:"localPath"`
	StorageURL string            `json:"storageUrl"`
	PublicURL  string            `json:"publicUrl"`
	SHA256     string            `json:"sha256,omitempty"`
}

// PublishManifest is the upload plan for one app version.
type PublishManifest struct {
	Schema          string          `json:"schema"`
	AppName         string          `json:"appName"`
	DisplayName     string          `json:"displayName,omitempty"`
	Description     string          `json:"description,omitempty"`
	Version         string          `json:"version"`
	Entry           Entry           `json:"entry"`
	EntryObject     PublishObject   `json:"entryObject"`
	IndexObject     PublishObject   `json:"indexObject"`
	ArtifactObjects []PublishObject `json:"artifactObjects"`
}

// BuildPublishManifest hashes local artifacts, builds version metadata, and resolves storage URLs.
func BuildPublishManifest(input BuildPublishManifestInput) (PublishManifest, error) {
	storageRoot := strings.TrimRight(strings.TrimSpace(input.StorageRoot), "/")
	publicRoot := strings.TrimRight(strings.TrimSpace(input.PublicRoot), "/")
	if storageRoot == "" {
		return PublishManifest{}, fmt.Errorf("storage root is required")
	}
	if publicRoot == "" {
		return PublishManifest{}, fmt.Errorf("public root is required")
	}

	layout, err := ResolvePublishLayout(input.EntryInput.Manifest.Source, input.EntryInput.Version)
	if err != nil {
		return PublishManifest{}, err
	}

	sortedArtifacts := append([]LocalPublishArtifact(nil), input.LocalArtifacts...)
	sort.Slice(sortedArtifacts, func(i, j int) bool {
		return filepath.Base(sortedArtifacts[i].LocalPath) < filepath.Base(sortedArtifacts[j].LocalPath)
	})

	if input.OnHashStart != nil {
		input.OnHashStart(len(sortedArtifacts) + 1)
	}

	publishArtifacts := make([]PublishArtifact, 0, len(sortedArtifacts))
	artifactObjects := make([]PublishObject, 0, len(sortedArtifacts))
	for _, artifact := range sortedArtifacts {
		filename := filepath.Base(artifact.LocalPath)
		rel := path.Join(layout.ArtifactPrefix, filename)
		digest, err := SHA256File(artifact.LocalPath)
		if err != nil {
			return PublishManifest{}, err
		}
		publishArtifacts = append(publishArtifacts, PublishArtifact{
			Target:     artifact.Target,
			LocalPath:  artifact.LocalPath,
			Filename:   filename,
			StorageURL: StorageURL(storageRoot, rel),
			PublicURL:  PublicURL(publicRoot, rel),
			SHA256:     digest,
		})
		artifactObjects = append(artifactObjects, PublishObject{
			Kind:       PublishObjectKindArchive,
			Target:     artifact.Target,
			LocalPath:  artifact.LocalPath,
			StorageURL: StorageURL(storageRoot, rel),
			PublicURL:  PublicURL(publicRoot, rel),
			SHA256:     digest,
		})
	}

	buildInput := input.EntryInput
	buildInput.Artifacts = publishArtifacts
	entry, err := BuildEntry(buildInput)
	if err != nil {
		return PublishManifest{}, err
	}

	entryData, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return PublishManifest{}, err
	}
	entryPath, err := WriteTempJSON("gestalt-app-entry-*", entryData)
	if err != nil {
		return PublishManifest{}, err
	}
	entryDigest, err := SHA256File(entryPath)
	if err != nil {
		_ = os.Remove(entryPath)
		return PublishManifest{}, err
	}
	if input.OnHashDone != nil {
		input.OnHashDone(len(sortedArtifacts) + 1)
	}

	return PublishManifest{
		Schema:      PublishManifestSchemaVersion,
		AppName:     entry.App,
		DisplayName: input.DisplayName,
		Description: input.Description,
		Version:     input.EntryInput.Version,
		Entry:       entry,
		EntryObject: PublishObject{
			Kind:       PublishObjectKindEntry,
			LocalPath:  entryPath,
			StorageURL: StorageURL(storageRoot, layout.EntryPath),
			PublicURL:  PublicURL(publicRoot, layout.EntryPath),
			SHA256:     entryDigest,
		},
		IndexObject: PublishObject{
			Kind:       PublishObjectKindIndex,
			StorageURL: StorageURL(storageRoot, layout.IndexPath),
			PublicURL:  PublicURL(publicRoot, layout.IndexPath),
		},
		ArtifactObjects: artifactObjects,
	}, nil
}

// Cleanup removes temporary local files referenced by the publish manifest.
func (plan PublishManifest) Cleanup() {
	if path := strings.TrimSpace(plan.EntryObject.LocalPath); path != "" {
		_ = os.Remove(path)
	}
}

func RetentionStorageURL(indexStorageURL string, appName string) string {
	storageRoot := strings.TrimSuffix(indexStorageURL, AppIndexPath(appName))
	return StorageURL(storageRoot, AppRetentionPath(appName))
}

// SHA256File returns the lowercase hex SHA-256 digest of a file.
func SHA256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// WriteTempJSON writes data to a temporary file and returns its path.
func WriteTempJSON(pattern string, data []byte) (string, error) {
	file, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	path := file.Name()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}
