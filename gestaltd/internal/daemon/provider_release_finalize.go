package daemon

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/staticvalidation"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/packageio"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
	"github.com/valon-technologies/gestalt/server/services/apps/source"
	"gopkg.in/yaml.v3"
)

func runProviderRelease(args []string) (err error) {
	fs := flag.NewFlagSet("gestaltd provider release", flag.ContinueOnError)
	fs.Usage = func() { printProviderReleaseUsage(fs.Output()) }
	version := fs.String("version", "", "semantic version guard")
	distDir := fs.String("dist-dir", defaultReleaseOutputDir, "directory containing release archives")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if *version != "" {
		if err := source.ValidateVersion(*version); err != nil {
			return fmt.Errorf("invalid --version: %w", err)
		}
	}

	releaseManifest, releaseVersion, releaseArchives, err := collectReleaseArchives(*distDir, *version)
	if err != nil {
		return err
	}
	if err := writeChecksums(*distDir, releaseArchives); err != nil {
		return fmt.Errorf("write checksums: %w", err)
	}
	if err := writeProviderReleaseMetadata(*distDir, releaseManifest, releaseVersion, releaseArchives); err != nil {
		return fmt.Errorf("write release metadata: %w", err)
	}

	return nil
}

func writeChecksums(dir string, archives []releaseArchive) error {
	return writeChecksumsWithOutput(dir, archives, true)
}

func writeChecksumsQuiet(dir string, archives []releaseArchive) error {
	return writeChecksumsWithOutput(dir, archives, false)
}

func writeChecksumsWithOutput(dir string, archives []releaseArchive, verbose bool) error {
	sortedArchives := append([]releaseArchive(nil), archives...)
	sort.Slice(sortedArchives, func(i, j int) bool {
		return filepath.Base(sortedArchives[i].Path) < filepath.Base(sortedArchives[j].Path)
	})
	var lines []string
	for _, archive := range sortedArchives {
		lines = append(lines, fmt.Sprintf("%s  %s", archive.SHA256, filepath.Base(archive.Path)))
	}

	if len(lines) == 0 {
		return nil
	}

	checksumPath := filepath.Join(dir, "checksums.txt")
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(checksumPath, []byte(content), 0644); err != nil {
		return err
	}
	if verbose {
		_, _ = fmt.Fprintf(os.Stdout, "created %s\n", checksumPath)
	}
	return nil
}

func describeReleaseArchive(path, target string) (releaseArchive, error) {
	digest, err := providerpkg.ArchiveDigest(path)
	if err != nil {
		return releaseArchive{}, fmt.Errorf("hash release archive %s: %w", path, err)
	}
	return releaseArchive{
		Path:   path,
		SHA256: digest,
		Target: target,
	}, nil
}

func collectReleaseArchives(distDir, versionGuard string) (*providermanifestv1.Manifest, string, []releaseArchive, error) {
	return collectReleaseArchivesFromDirs([]string{distDir}, versionGuard)
}

func collectReleaseArchivesFromDirs(distDirs []string, versionGuard string) (*providermanifestv1.Manifest, string, []releaseArchive, error) {
	var archivePaths []string
	for _, distDir := range distDirs {
		paths, err := releaseArchivePathsInDir(distDir)
		if err != nil {
			return nil, "", nil, err
		}
		archivePaths = append(archivePaths, paths...)
	}
	sort.Strings(archivePaths)
	if len(archivePaths) == 0 {
		return nil, "", nil, fmt.Errorf("no .tar.gz release archives found in %s", strings.Join(distDirs, ", "))
	}
	return collectReleaseArchivePaths(archivePaths, versionGuard)
}

func releaseArchivePathsInDir(distDir string) ([]string, error) {
	entries, err := os.ReadDir(distDir)
	if err != nil {
		return nil, fmt.Errorf("read dist dir %s: %w", distDir, err)
	}
	var archivePaths []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tar.gz") {
			continue
		}
		archivePaths = append(archivePaths, filepath.Join(distDir, entry.Name()))
	}
	return archivePaths, nil
}

func collectReleaseArchivePaths(archivePaths []string, versionGuard string) (*providermanifestv1.Manifest, string, []releaseArchive, error) {
	var releaseManifest *providermanifestv1.Manifest
	var comparableReleaseManifest []byte
	releaseVersion := ""
	seenArchiveNames := map[string]string{}
	seenTargets := map[string]string{}
	var hasGeneric, hasPlatform bool
	archives := make([]releaseArchive, 0, len(archivePaths))
	for _, archivePath := range archivePaths {
		archiveName := filepath.Base(archivePath)
		if existingPath, ok := seenArchiveNames[archiveName]; ok {
			return nil, "", nil, fmt.Errorf("multiple release archives have filename %s: %s and %s", archiveName, existingPath, archivePath)
		}
		seenArchiveNames[archiveName] = archivePath
		manifest, target, err := inspectReleaseArchive(archivePath)
		if err != nil {
			return nil, "", nil, err
		}
		if versionGuard != "" && manifest.Version != versionGuard {
			return nil, "", nil, fmt.Errorf("release archive %s version %q does not match --version %q", filepath.Base(archivePath), manifest.Version, versionGuard)
		}
		if existingPath, ok := seenTargets[target]; ok {
			return nil, "", nil, fmt.Errorf("multiple release archives map to target %s: %s and %s", target, filepath.Base(existingPath), filepath.Base(archivePath))
		}
		if releaseManifest == nil {
			releaseManifest = manifest
			comparableReleaseManifest, err = comparableProviderReleaseManifest(manifest)
			if err != nil {
				return nil, "", nil, fmt.Errorf("normalize release archive %s manifest: %w", filepath.Base(archivePath), err)
			}
			releaseVersion = manifest.Version
		} else {
			if manifest.Source != releaseManifest.Source {
				return nil, "", nil, fmt.Errorf("release archive %s package %q does not match %q", filepath.Base(archivePath), manifest.Source, releaseManifest.Source)
			}
			if manifest.Kind != releaseManifest.Kind {
				return nil, "", nil, fmt.Errorf("release archive %s kind %q does not match %q", filepath.Base(archivePath), manifest.Kind, releaseManifest.Kind)
			}
			if manifest.Version != releaseVersion {
				return nil, "", nil, fmt.Errorf("release archive %s version %q does not match %q", filepath.Base(archivePath), manifest.Version, releaseVersion)
			}
			comparableManifest, err := comparableProviderReleaseManifest(manifest)
			if err != nil {
				return nil, "", nil, fmt.Errorf("normalize release archive %s manifest: %w", filepath.Base(archivePath), err)
			}
			if !bytes.Equal(comparableManifest, comparableReleaseManifest) {
				return nil, "", nil, fmt.Errorf("release archive %s manifest does not match other release archives", filepath.Base(archivePath))
			}
		}
		seenTargets[target] = archivePath
		if target == providerReleaseGenericTarget {
			hasGeneric = true
		} else {
			hasPlatform = true
		}
		archive, err := describeReleaseArchive(archivePath, target)
		if err != nil {
			return nil, "", nil, err
		}
		archives = append(archives, archive)
	}
	if hasGeneric && hasPlatform {
		return nil, "", nil, fmt.Errorf("release archives must not mix generic and platform targets")
	}
	return releaseManifest, releaseVersion, archives, nil
}

func inspectReleaseArchive(archivePath string) (*providermanifestv1.Manifest, string, error) {
	manifest, err := providerpkg.InspectPackage(archivePath)
	if err != nil {
		return nil, "", fmt.Errorf("inspect release archive %s: %w", filepath.Base(archivePath), err)
	}
	target, err := releaseArchiveTargetFromManifest(manifest)
	if err != nil {
		return nil, "", fmt.Errorf("release archive %s: %w", filepath.Base(archivePath), err)
	}
	return manifest, target, nil
}

func comparableProviderReleaseManifest(manifest *providermanifestv1.Manifest) ([]byte, error) {
	cloned, err := packageio.CloneManifest(manifest)
	if err != nil {
		return nil, err
	}
	if cloned == nil {
		return nil, nil
	}
	cloned.Artifacts = nil
	cloned.Entrypoint = nil
	return json.Marshal(cloned)
}

func releaseArchiveTargetFromManifest(manifest *providermanifestv1.Manifest) (string, error) {
	if manifest == nil {
		return "", fmt.Errorf("manifest is required")
	}
	var target string
	for _, artifact := range manifest.Artifacts {
		if artifact.OS == "" && artifact.Arch == "" {
			continue
		}
		if artifact.OS == "" || artifact.Arch == "" {
			return "", fmt.Errorf("artifact platform must include both os and arch")
		}
		candidate := providerpkg.PlatformString(artifact.OS, artifact.Arch)
		if target != "" && candidate != target {
			return "", fmt.Errorf("archive contains artifacts for multiple targets")
		}
		target = candidate
	}
	if target == "" {
		return providerReleaseGenericTarget, nil
	}
	return target, nil
}

func writeProviderReleaseMetadata(dir string, manifest *providermanifestv1.Manifest, version string, archives []releaseArchive) error {
	return writeProviderReleaseMetadataWithOutput(dir, manifest, version, archives, true)
}

func writeProviderReleaseMetadataQuiet(dir string, manifest *providermanifestv1.Manifest, version string, archives []releaseArchive) error {
	return writeProviderReleaseMetadataWithOutput(dir, manifest, version, archives, false)
}

func writeProviderReleaseMetadataWithOutput(dir string, manifest *providermanifestv1.Manifest, version string, archives []releaseArchive, verbose bool) error {
	metadata, err := buildProviderReleaseMetadata(manifest, version, archives)
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encode %s: %w", providerReleaseMetadataFile, err)
	}
	path := filepath.Join(dir, providerReleaseMetadataFile)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	if verbose {
		_, _ = fmt.Fprintf(os.Stdout, "created %s\n", path)
	}
	return nil
}

func buildProviderReleaseMetadata(manifest *providermanifestv1.Manifest, version string, archives []releaseArchive) (*providerReleaseMetadata, error) {
	if manifest == nil {
		return nil, fmt.Errorf("manifest is required")
	}

	runtime, err := releaseRuntimeMetadata(manifest, archives)
	if err != nil {
		return nil, err
	}
	staticManifest, err := staticvalidation.ProjectManifest(manifest, "", true)
	if err != nil {
		return nil, fmt.Errorf("project static validation manifest: %w", err)
	}

	metadata := &providerReleaseMetadata{
		Schema:        providerReleaseSchemaName,
		SchemaVersion: providerReleaseSchemaVersion,
		Package:       manifest.Source,
		Kind:          manifest.Kind,
		Version:       version,
		Runtime:       runtime,
		Artifacts:     make(map[string]providerReleaseArtifact, len(archives)),
		StaticValidation: &providerReleaseStaticValidationData{
			Manifest: staticManifest,
		},
	}
	for _, archive := range archives {
		target := providerReleaseArtifactTarget(manifest, archive)
		metadata.Artifacts[target] = providerReleaseArtifact{
			Path:   filepath.Base(archive.Path),
			SHA256: archive.SHA256,
		}
	}
	return metadata, nil
}

func providerReleaseArtifactTarget(manifest *providermanifestv1.Manifest, archive releaseArchive) string {
	if archive.Target != "" {
		return archive.Target
	}
	return providerReleaseGenericTarget
}

func releaseRuntimeMetadata(manifest *providermanifestv1.Manifest, archives []releaseArchive) (string, error) {
	kind, err := providerpkg.ManifestKind(manifest)
	if err != nil {
		return "", err
	}

	switch kind {
	case providermanifestv1.KindUI:
		return providerReleaseRuntimeKindUI, nil
	case providermanifestv1.KindApp:
		if manifest.IsDeclarativeOnlyProvider() && !releaseIncludesBuiltAppArtifact(archives) {
			return providerReleaseRuntimeKindDeclarative, nil
		}
	}
	return providerReleaseRuntimeKindExecutable, nil
}

func releaseIncludesBuiltAppArtifact(archives []releaseArchive) bool {
	for _, archive := range archives {
		if archive.Target != "" && archive.Target != providerReleaseGenericTarget {
			return true
		}
	}
	return false
}

func printProviderReleaseUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd provider release [--dist-dir DIR] [--version VERSION]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Finalize provider release metadata from already-built archives.")
	writeUsageLine(w, "Reads all .tar.gz archives in --dist-dir, validates package contents, writes")
	writeUsageLine(w, "checksums.txt, and writes provider-release.yaml in the same directory.")
	writeUsageLine(w, "Run this after one or more provider package jobs have produced archives.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --dist-dir  Directory containing release archives (default: dist/)")
	writeUsageLine(w, "  --version   Optional semantic version guard")
}
