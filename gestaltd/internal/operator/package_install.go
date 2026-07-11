package operator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/packageio"
)

type installedPackage struct {
	Root           string
	ManifestPath   string
	ExecutablePath string
	AssetRoot      string
	Manifest       *providermanifestv1.Manifest
}

// InstalledPackage is the on-disk layout after extracting a published app archive.
type InstalledPackage struct {
	Root           string
	ManifestPath   string
	ExecutablePath string
	AssetRoot      string
	Manifest       *providermanifestv1.Manifest
}

func installedPackageView(pkg *installedPackage) *InstalledPackage {
	if pkg == nil {
		return nil
	}
	return &InstalledPackage{
		Root:           pkg.Root,
		ManifestPath:   pkg.ManifestPath,
		ExecutablePath: pkg.ExecutablePath,
		AssetRoot:      pkg.AssetRoot,
		Manifest:       pkg.Manifest,
	}
}

// InstallPublishedPackage extracts a published app archive into destDir.
func InstallPublishedPackage(ctx context.Context, packagePath, destDir, configuredName string) (*InstalledPackage, error) {
	pkg, err := installPackageAs(ctx, packagePath, destDir, configuredName)
	if err != nil {
		return nil, err
	}
	return installedPackageView(pkg), nil
}

func isAssetOnly(manifest *providermanifestv1.Manifest) bool {
	if manifest == nil || manifest.Spec == nil || strings.TrimSpace(manifest.Spec.AssetRoot) == "" {
		return false
	}
	kind, err := packageio.ManifestKind(manifest)
	if err != nil {
		return false
	}
	return packageio.EntrypointForKind(manifest, kind) == nil
}

func manifestNeedsExecutableArtifact(manifest *providermanifestv1.Manifest) bool {
	kind, err := packageio.ManifestKind(manifest)
	if err != nil {
		return false
	}
	return packageio.EntrypointForKind(manifest, kind) != nil
}

func installPackage(packagePath, destDir string) (*installedPackage, error) {
	return installPackageAs(context.Background(), packagePath, destDir, "")
}

func installPackageAs(ctx context.Context, packagePath, destDir, configuredName string) (*installedPackage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, manifest, err := packageio.ReadPackageManifest(packagePath)
	if err != nil {
		return nil, err
	}

	if isAssetOnly(manifest) {
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return nil, fmt.Errorf("create app directory: %w", err)
		}
		if err := packageio.ExtractPackageContext(ctx, packagePath, destDir); err != nil {
			return nil, err
		}
		manifestPath, _ := packageio.FindManifestFile(destDir)
		if manifestPath == "" {
			manifestPath = filepath.Join(destDir, packageio.ManifestFile)
		}
		assetRoot := filepath.Join(destDir, filepath.FromSlash(manifest.Spec.AssetRoot))
		return &installedPackage{
			Root:         destDir,
			ManifestPath: manifestPath,
			AssetRoot:    assetRoot,
			Manifest:     manifest,
		}, nil
	}

	var artifact *providermanifestv1.Artifact
	if manifestNeedsExecutableArtifact(manifest) {
		artifact, err = packageio.CurrentPlatformArtifact(manifest)
		if err != nil {
			return nil, err
		}
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("create app directory: %w", err)
	}
	if err := packageio.ExtractPackageContext(ctx, packagePath, destDir); err != nil {
		return nil, err
	}

	if artifact != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Verify the artifact digest against the extracted file rather than
		// decompressing the whole archive a second time to read it. The package
		// is never executed before this check succeeds.
		got, err := packageio.FileSHA256(filepath.Join(destDir, filepath.FromSlash(artifact.Path)))
		if err != nil {
			return nil, err
		}
		if got != artifact.SHA256 {
			return nil, fmt.Errorf("artifact digest mismatch for %s: package has %s, manifest expects %s", artifact.Path, got, artifact.SHA256)
		}
	}

	manifestPath, _ := packageio.FindManifestFile(destDir)
	if manifestPath == "" {
		manifestPath = filepath.Join(destDir, packageio.ManifestFile)
	}
	manifest = packageio.ResolveManifestLocalReferences(manifest, manifestPath)
	if configuredName == "" {
		configuredName = filepath.Base(destDir)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := normalizeInstalledExecutable(destDir, manifest, configuredName); err != nil {
		return nil, err
	}
	executablePath, err := executablePathForManifest(destDir, manifest)
	if err != nil {
		return nil, err
	}

	return &installedPackage{
		Root:           destDir,
		ManifestPath:   manifestPath,
		ExecutablePath: executablePath,
		Manifest:       manifest,
	}, nil
}

func executablePathForManifest(root string, manifest *providermanifestv1.Manifest) (string, error) {
	if manifest == nil {
		return "", fmt.Errorf("manifest is required")
	}
	if isAssetOnly(manifest) {
		return "", nil
	}
	kind, err := packageio.ManifestKind(manifest)
	if err != nil {
		return "", err
	}
	entry := packageio.EntrypointForKind(manifest, kind)
	if entry == nil {
		if manifest.Spec != nil && manifest.Spec.IsManifestBacked() {
			return "", nil
		}
		return "", fmt.Errorf("manifest does not define an executable entrypoint")
	}
	if entry.ArtifactPath == "" {
		return "", fmt.Errorf("manifest entrypoint artifact_path is required")
	}
	return filepath.Join(root, filepath.FromSlash(entry.ArtifactPath)), nil
}

func normalizeInstalledExecutable(root string, manifest *providermanifestv1.Manifest, configuredName string) error {
	if manifest == nil || isAssetOnly(manifest) {
		return nil
	}
	kind, err := packageio.ManifestKind(manifest)
	if err != nil {
		return err
	}
	entry := packageio.EntrypointForKind(manifest, kind)
	if entry == nil || strings.TrimSpace(entry.ArtifactPath) == "" {
		return nil
	}
	goos := runtime.GOOS
	if len(manifest.Artifacts) > 0 && manifest.Artifacts[0].OS != "" {
		goos = manifest.Artifacts[0].OS
	}
	targetRel := packageio.InstalledExecutablePath(configuredName, goos)
	if entry.ArtifactPath == targetRel {
		return nil
	}
	currentPath := filepath.Join(root, filepath.FromSlash(entry.ArtifactPath))
	targetPath := filepath.Join(root, filepath.FromSlash(targetRel))
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("create installed executable directory: %w", err)
	}
	if err := os.Rename(currentPath, targetPath); err != nil {
		return fmt.Errorf("rename installed executable to %s: %w", targetRel, err)
	}
	digest, err := packageio.FileSHA256(targetPath)
	if err != nil {
		return fmt.Errorf("hash installed executable %s: %w", targetRel, err)
	}
	manifest.Entrypoint = &providermanifestv1.Entrypoint{
		ArtifactPath: targetRel,
		Args:         append([]string(nil), entry.Args...),
	}
	if len(manifest.Artifacts) > 0 {
		manifest.Artifacts[0].Path = targetRel
		manifest.Artifacts[0].SHA256 = digest
	}
	manifestPath, _ := packageio.FindManifestFile(root)
	if manifestPath == "" {
		manifestPath = filepath.Join(root, packageio.ManifestFile)
	}
	format := packageio.ManifestFormatFromPath(manifestPath)
	data, err := packageio.EncodeManifestFormat(manifest, format)
	if err != nil {
		return fmt.Errorf("encode installed manifest: %w", err)
	}
	return os.WriteFile(manifestPath, data, 0o644)
}
