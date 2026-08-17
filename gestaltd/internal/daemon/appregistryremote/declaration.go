package appregistryremote

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

type releaseArchive struct {
	Path     string
	SHA256   string
	Target   string
	Filename string
	Size     int64
}

func buildPublishDeclaration(input buildDeclarationInput) (*appregistry.PublishDeclaration, error) {
	if input.SourceManifest == nil {
		return nil, fmt.Errorf("source manifest is required")
	}
	if input.ReleaseMetadata == nil {
		return nil, fmt.Errorf("release metadata is required")
	}
	if len(input.Archives) == 0 {
		return nil, fmt.Errorf("at least one release archive is required")
	}
	artifacts := make([]appregistry.PublishDeclarationArtifact, 0, len(input.Archives))
	for _, archive := range input.Archives {
		artifacts = append(artifacts, appregistry.PublishDeclarationArtifact{
			Platform: strings.TrimSpace(archive.Target),
			Filename: strings.TrimSpace(archive.Filename),
			SHA256:   strings.ToLower(strings.TrimSpace(archive.SHA256)),
			Size:     archive.Size,
		})
	}
	manifest := cloneManifest(input.SourceManifest)
	if manifest != nil {
		manifest.Version = strings.TrimSpace(input.Version)
	}
	declaration := &appregistry.PublishDeclaration{
		Schema:          appregistry.PublishDeclarationSchemaVersion,
		Manifest:        manifest,
		ManifestPath:    strings.TrimSpace(input.ManifestPath),
		ReleaseMetadata: input.ReleaseMetadata,
		Artifacts:       artifacts,
		PublicationKind: appregistry.PublicationKindLocal,
		LocalSource:     input.LocalSource,
		BuilderVersion:  strings.TrimSpace(input.BuilderVersion),
	}
	if err := appregistry.ValidatePublishDeclaration(input.AppName, declaration, appregistry.DefaultPublishSessionLimits()); err != nil {
		return nil, err
	}
	return declaration, nil
}

type buildDeclarationInput struct {
	AppName         string
	Version         string
	ManifestPath    string
	SourceManifest  *providermanifestv1.Manifest
	ReleaseMetadata *providerrelease.Metadata
	Archives        []releaseArchive
	LocalSource     *appregistry.LocalSourceState
	BuilderVersion  string
}

func releaseArchivesFromDaemon(archives []DaemonReleaseArchive) []releaseArchive {
	out := make([]releaseArchive, 0, len(archives))
	for _, archive := range archives {
		out = append(out, releaseArchive{
			Path:     archive.Path,
			SHA256:   archive.SHA256,
			Target:   archive.Target,
			Filename: filepath.Base(archive.Path),
			Size:     archive.Size,
		})
	}
	return out
}

// DaemonReleaseArchive is the archive view exported to the daemon bridge.
type DaemonReleaseArchive struct {
	Path   string
	SHA256 string
	Target string
	Size   int64
}

func cloneManifest(manifest *providermanifestv1.Manifest) *providermanifestv1.Manifest {
	if manifest == nil {
		return nil
	}
	cloned := *manifest
	return &cloned
}

func platformSet(archives []releaseArchive) map[string]struct{} {
	out := make(map[string]struct{}, len(archives))
	for _, archive := range archives {
		out[strings.TrimSpace(archive.Target)] = struct{}{}
	}
	return out
}
