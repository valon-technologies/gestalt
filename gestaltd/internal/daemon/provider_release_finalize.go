package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	"github.com/valon-technologies/gestalt/server/internal/staticvalidation"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	appservice "github.com/valon-technologies/gestalt/server/services/apps"
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

	discoveryProgress := startCommandProgress("Discovering release archives in %s", *distDir)
	archivePaths, err := releaseArchivePathsInDirs([]string{*distDir})
	if err != nil {
		return err
	}
	discoveryProgress.done("Discovered %d release archives", len(archivePaths))
	releaseManifest, releaseVersion, releaseArchives, err := collectReleaseArchivePathsWithProgress(archivePaths, *version)
	if err != nil {
		return err
	}
	validationProgress := startCommandProgress("Validating provider release metadata")
	if err := writeProviderReleaseMetadata(*distDir, releaseManifest, releaseVersion, releaseArchives, nil, false); err != nil {
		return fmt.Errorf("write release metadata: %w", err)
	}
	validationProgress.done("Validated provider release metadata for %s", releaseVersion)
	_, _ = fmt.Fprintf(os.Stdout, "created %s\n", filepath.Join(*distDir, providerrelease.MetadataFile))
	progressStatus("Generated %s with %d artifact(s) in %s", providerrelease.MetadataFile, len(releaseArchives), *distDir)

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
	archivePaths, err := releaseArchivePathsInDirs(distDirs)
	if err != nil {
		return nil, "", nil, err
	}
	return collectReleaseArchivePaths(archivePaths, versionGuard)
}

func collectReleaseArchivesFromDirsWithProgress(distDirs []string, versionGuard string) (*providermanifestv1.Manifest, string, []releaseArchive, error) {
	archivePaths, err := releaseArchivePathsInDirs(distDirs)
	if err != nil {
		return nil, "", nil, err
	}
	return collectReleaseArchivePathsWithProgress(archivePaths, versionGuard)
}

func collectReleaseArchivePathsWithProgress(archivePaths []string, versionGuard string) (*providermanifestv1.Manifest, string, []releaseArchive, error) {
	progress := startCommandProgress("Inspecting and hashing %d release archives", len(archivePaths))
	manifest, version, archives, err := collectReleaseArchivePaths(archivePaths, versionGuard)
	if err != nil {
		return nil, "", nil, err
	}
	progress.done("Inspected and hashed %d release archives", len(archives))
	return manifest, version, archives, nil
}

func releaseArchivePathsInDirs(distDirs []string) ([]string, error) {
	var archivePaths []string
	for _, distDir := range distDirs {
		paths, err := releaseArchivePathsInDir(distDir)
		if err != nil {
			return nil, err
		}
		archivePaths = append(archivePaths, paths...)
	}
	sort.Strings(archivePaths)
	if len(archivePaths) == 0 {
		return nil, fmt.Errorf("no .tar.gz release archives found in %s", strings.Join(distDirs, ", "))
	}
	return archivePaths, nil
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
		if target == providerrelease.GenericTarget {
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
	target, err := releaseArchiveTargetFromManifest(manifest, filepath.Base(archivePath))
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

func releaseArchiveTargetFromManifest(manifest *providermanifestv1.Manifest, archiveName string) (string, error) {
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
	if target != "" {
		return target, nil
	}

	// Declarative and UI-only providers ship the same manifest on every platform;
	// the archive filename carries the platform suffix produced by provider package.
	if target, ok := releaseArchiveTargetFromName(manifest, archiveName); ok {
		return target, nil
	}
	return providerrelease.GenericTarget, nil
}

func releaseArchiveTargetFromName(manifest *providermanifestv1.Manifest, archiveName string) (string, bool) {
	if manifest == nil || strings.TrimSpace(manifest.Source) == "" || strings.TrimSpace(manifest.Version) == "" {
		return "", false
	}
	src, err := source.Parse(manifest.Source)
	if err != nil {
		return "", false
	}
	appName := src.AppName()
	base := strings.TrimSuffix(archiveName, ".tar.gz")
	generic := fmt.Sprintf("gestalt-app-%s_v%s", appName, manifest.Version)
	if base == generic {
		return "", false
	}
	prefix := generic + "_"
	if !strings.HasPrefix(base, prefix) {
		return "", false
	}
	suffix := strings.TrimPrefix(base, prefix)
	if strings.Count(suffix, "_") != 1 {
		return "", false
	}
	platform := strings.ReplaceAll(suffix, "_", "/")
	if _, _, err := packageio.ParsePlatformString(platform); err != nil {
		return "", false
	}
	return platform, true
}

func writeProviderReleaseMetadata(dir string, manifest *providermanifestv1.Manifest, version string, archives []releaseArchive, rawManifest []byte, verbose bool) error {
	metadata, err := buildProviderReleaseMetadata(manifest, version, archives, rawManifest)
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encode %s: %w", providerrelease.MetadataFile, err)
	}
	path := filepath.Join(dir, providerrelease.MetadataFile)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	if verbose {
		_, _ = fmt.Fprintf(os.Stdout, "created %s\n", path)
	}
	return nil
}

func buildProviderReleaseMetadata(manifest *providermanifestv1.Manifest, version string, archives []releaseArchive, rawManifest []byte) (*providerrelease.Metadata, error) {
	if manifest == nil {
		return nil, fmt.Errorf("manifest is required")
	}
	staticManifest, err := staticvalidation.ProjectManifest(manifest, "", true)
	if err != nil {
		return nil, fmt.Errorf("project static validation manifest: %w", err)
	}
	staticCatalog, err := staticValidationCatalogForRelease(manifest, archives)
	if err != nil {
		return nil, err
	}
	contractRaw, err := rawManifestBytesForRelease(archives, rawManifest)
	if err != nil {
		return nil, err
	}
	var requires providerrelease.Requires
	var compatibility providerrelease.Compatibility
	if len(rawManifest) > 0 {
		// Provider publish passes authoritative source --manifest bytes separately from
		// the built archive manifest struct; contract metadata must follow the source file.
		requires, compatibility, err = providerrelease.ParseContractFromManifestRaw(contractRaw)
	} else {
		requires, compatibility, err = providerrelease.ParseContract(manifest, contractRaw)
	}
	if err != nil {
		return nil, fmt.Errorf("parse release contract from manifest: %w", err)
	}

	metadata := &providerrelease.Metadata{
		Schema:        providerrelease.SchemaName,
		SchemaVersion: providerrelease.SchemaVersion,
		Package:       manifest.Source,
		Kind:          manifest.Kind,
		Version:       version,
		Runtime:       providerrelease.RuntimeForManifest(manifest.Kind, manifest),
		Artifacts:     providerrelease.Artifacts{},
		StaticValidation: &providerrelease.StaticValidation{
			Manifest: staticManifest,
			Catalog:  staticCatalog,
		},
	}
	if len(requires.Apps) > 0 {
		metadata.StaticValidation.Requires = &requires
	}
	if compatibility.MinGestaltdVersion != "" {
		metadata.StaticValidation.Compatibility = &compatibility
	}
	if staticCatalog == nil && providerrelease.CatalogSessionModeAllowed(manifest.Kind, staticManifest) {
		metadata.StaticValidation.CatalogSessionOnly = true
	}
	for _, archive := range archives {
		target := strings.TrimSpace(archive.Target)
		if target == "" {
			target = providerrelease.GenericTarget
		}
		metadata.Artifacts[target] = providerrelease.Artifact{Path: filepath.Base(archive.Path), SHA256: archive.SHA256}
	}
	if err := providerrelease.ValidateMetadata(metadata); err != nil {
		return nil, fmt.Errorf("validate provider release metadata: %w", err)
	}
	return metadata, nil
}

func staticValidationCatalogForRelease(manifest *providermanifestv1.Manifest, archives []releaseArchive) (*catalog.Catalog, error) {
	if manifest == nil || len(archives) == 0 {
		return nil, nil
	}
	if providerrelease.CatalogSessionModeAllowed(manifest.Kind, manifest) {
		return nil, nil
	}
	var firstCatalog *catalog.Catalog
	var firstData []byte
	firstPath := ""
	found := false
	for _, archive := range archives {
		cat, err := staticValidationCatalogFromArchiveManifest(archive)
		if err != nil {
			return nil, err
		}
		if cat == nil {
			continue
		}
		data, err := yaml.Marshal(cat)
		if err != nil {
			return nil, fmt.Errorf("encode static validation catalog from %s: %w", filepath.Base(archive.Path), err)
		}
		if !found {
			firstData = data
			firstCatalog = cat
			firstPath = archive.Path
			found = true
			continue
		}
		if !bytes.Equal(bytes.TrimSpace(firstData), bytes.TrimSpace(data)) {
			return nil, fmt.Errorf("static validation catalog in %s does not match %s", filepath.Base(archive.Path), filepath.Base(firstPath))
		}
	}
	return firstCatalog, nil
}

func staticValidationCatalogFromArchiveManifest(archive releaseArchive) (*catalog.Catalog, error) {
	tmpDir, err := os.MkdirTemp("", "gestalt-provider-release-catalog-*")
	if err != nil {
		return nil, fmt.Errorf("create static validation catalog temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	if err := packageio.ExtractPackage(archive.Path, tmpDir); err != nil {
		return nil, fmt.Errorf("extract static validation catalog source from %s: %w", filepath.Base(archive.Path), err)
	}
	_, manifest, manifestPath, err := packageio.LoadManifestFromPath(tmpDir)
	if err != nil {
		return nil, fmt.Errorf("read static validation catalog manifest from %s: %w", filepath.Base(archive.Path), err)
	}
	if providermanifestv1.NormalizeKind(manifest.Kind) != providermanifestv1.KindApp || manifest.Spec == nil {
		return nil, nil
	}
	cat, sessionOnly, err := appservice.EffectiveCatalog(context.Background(), manifest.Source, appservice.ValidationAppFromManifest(manifestPath, manifest))
	if err != nil {
		return nil, fmt.Errorf("derive static validation catalog from %s: %w", filepath.Base(archive.Path), err)
	}
	if sessionOnly {
		return nil, nil
	}
	return cat, nil
}

func rawManifestBytesForRelease(archives []releaseArchive, explicit []byte) ([]byte, error) {
	if len(explicit) > 0 {
		return explicit, nil
	}
	if len(archives) == 0 {
		return nil, nil
	}
	data, _, err := packageio.ReadPackageManifestIn(archives[0].Path, []string{"manifest.yaml", "manifest.yml", packageio.ManifestFile})
	if err != nil {
		return nil, fmt.Errorf("read release archive manifest from %s: %w", filepath.Base(archives[0].Path), err)
	}
	return data, nil
}

func printProviderReleaseUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd provider release [--dist-dir DIR] [--version VERSION]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Finalize provider release metadata from already-built archives.")
	writeUsageLine(w, "Reads all .tar.gz archives in --dist-dir, validates package contents, and")
	writeUsageLine(w, "writes provider-release.yaml in the same directory.")
	writeUsageLine(w, "Run this after one or more provider package jobs have produced archives.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --dist-dir  Directory containing release archives (default: dist/)")
	writeUsageLine(w, "  --version   Optional semantic version guard")
}
