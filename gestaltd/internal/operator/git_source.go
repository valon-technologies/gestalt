package operator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

const (
	gitSourceRefType           = "git"
	gitMaterializationSource   = "source"
	gitMaterializationSnapshot = "snapshot"
)

type gitSnapshotSource struct {
	MetadataURL string
	GestaltRef  string
}

type SnapshotSourceRefPath struct {
	RelRoot          string
	SourceRepository string
	SourceRef        string
	ProviderDir      string
	ManifestPath     string
}

func NewSnapshotSourceRefPath(repoURL, ref, manifestPath string) (SnapshotSourceRefPath, error) {
	repo, err := config.ParseGitHubSnapshotRemote(repoURL)
	if err != nil {
		return SnapshotSourceRefPath{}, err
	}
	sourceRef := strings.ToLower(strings.TrimSpace(ref))
	manifestPath = path.Clean(filepath.ToSlash(strings.TrimSpace(manifestPath)))
	if manifestPath == "" || manifestPath == "." || strings.HasPrefix(manifestPath, "../") || path.IsAbs(manifestPath) {
		return SnapshotSourceRefPath{}, fmt.Errorf("source.git.path must be a clean relative path")
	}
	providerDir := config.AppDirFromManifestPath(manifestPath)
	sourceRepository := path.Join("github.com", repo.Owner, repo.Name)
	parts := []string{sourceRepository, sourceRef}
	if providerDir != "" {
		parts = append(parts, strings.Split(providerDir, "/")...)
	}
	return SnapshotSourceRefPath{
		RelRoot:          path.Join(parts...),
		SourceRepository: sourceRepository,
		SourceRef:        sourceRef,
		ProviderDir:      providerDir,
		ManifestPath:     manifestPath,
	}, nil
}

func (p SnapshotSourceRefPath) FileRelPath(filename string) string {
	return path.Join(p.RelRoot, filename)
}

func gitSourceDef(entry *config.ProviderEntry) *config.GitSourceDef {
	if entry == nil {
		return nil
	}
	return entry.Source.GitSource()
}

func gitSourceMaterialization(git *config.GitSourceDef) string {
	if git == nil {
		return ""
	}
	if materialization := strings.TrimSpace(git.Materialization); materialization != "" {
		return materialization
	}
	return gitMaterializationSource
}

func gitSourceLockRef(entry *config.ProviderEntry, resolvedGestaltRef string) *LockSourceRef {
	git := gitSourceDef(entry)
	if git == nil {
		return nil
	}
	repo, ref, manifestPath := git.NormalizedLocationParts()
	return &LockSourceRef{
		Type:               gitSourceRefType,
		Repo:               repo,
		Ref:                ref,
		Path:               manifestPath,
		ArtifactRepository: strings.TrimSpace(git.ArtifactRepository),
		Materialization:    gitSourceMaterialization(git),
		ResolvedGestaltRef: strings.TrimSpace(resolvedGestaltRef),
	}
}

func gitSourceFingerprintLocation(entry *config.ProviderEntry) string {
	ref := gitSourceLockRef(entry, "")
	if ref == nil {
		return ""
	}
	return strings.Join([]string{
		ref.Type,
		ref.Repo,
		ref.Ref,
		ref.Path,
		ref.ArtifactRepository,
		ref.Materialization,
	}, "\x00")
}

func gitSourceMatchesLockRef(entry *config.ProviderEntry, lockRef *LockSourceRef) bool {
	expected := gitSourceLockRef(entry, "")
	if expected == nil || lockRef == nil {
		return false
	}
	return lockRef.Type == gitSourceRefType &&
		lockRef.Repo == expected.Repo &&
		strings.EqualFold(lockRef.Ref, expected.Ref) &&
		lockRef.Path == expected.Path &&
		lockRef.ArtifactRepository == expected.ArtifactRepository &&
		lockRef.Materialization == expected.Materialization
}

func canonicalGitSourceLocation(entry *config.ProviderEntry) string {
	git := gitSourceDef(entry)
	if git == nil {
		return ""
	}
	return git.Location()
}

func resolveGitSnapshotSource(cfg *config.Config, entry *config.ProviderEntry) (gitSnapshotSource, error) {
	git := gitSourceDef(entry)
	if git == nil {
		return gitSnapshotSource{}, fmt.Errorf("source.git is required")
	}
	repoName := strings.TrimSpace(git.ArtifactRepository)
	if repoName == "" {
		return gitSnapshotSource{}, fmt.Errorf("source.git.artifactRepository is required")
	}
	repo, ok := cfg.ProviderSnapshotRepositories[repoName]
	if !ok {
		return gitSnapshotSource{}, fmt.Errorf("providerSnapshotRepositories.%s is not configured", repoName)
	}
	base := strings.TrimRight(strings.TrimSpace(repo.URL), "/")
	_, ref, manifestPath := git.NormalizedLocationParts()
	snapshotPath, err := NewSnapshotSourceRefPath(git.Repo, ref, manifestPath)
	if err != nil {
		return gitSnapshotSource{}, err
	}
	metadataURL, err := snapshotSourceFileURL(base, snapshotPath, providerrelease.MetadataFile)
	if err != nil {
		return gitSnapshotSource{}, err
	}
	return gitSnapshotSource{
		MetadataURL: metadataURL,
		GestaltRef:  strings.TrimSpace(repo.GestaltRef),
	}, nil
}

func snapshotSourceFileURL(base string, snapshotPath SnapshotSourceRefPath, filename string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(base), "/") + "/" + snapshotPath.FileRelPath(filename))
	if err != nil {
		return "", fmt.Errorf("parse snapshot URL: %w", err)
	}
	query := parsed.Query()
	query.Set("sourceRef", snapshotPath.SourceRef)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func (l *Lifecycle) gitSourceManifestPath(ctx context.Context, paths lifecyclePaths, entry *config.ProviderEntry) (string, error) {
	git := gitSourceDef(entry)
	if git == nil {
		return "", fmt.Errorf("source.git is required")
	}
	repo, ref, manifestRel := git.NormalizedLocationParts()
	cacheRoot := filepath.Join(paths.artifactsDir, "git-source-cache")
	cacheKey := sha256.Sum256([]byte(repo + "\x00" + ref))
	checkoutDir := filepath.Join(cacheRoot, hex.EncodeToString(cacheKey[:]))
	if err := ensureGitCheckout(ctx, checkoutDir, repo, ref); err != nil {
		return "", err
	}
	manifestPath := filepath.Join(checkoutDir, filepath.FromSlash(manifestRel))
	if !pathWithinRoot(checkoutDir, manifestPath) {
		return "", fmt.Errorf("source.git.path %q escapes repository checkout", git.Path)
	}
	if _, err := os.Stat(manifestPath); err != nil {
		return "", fmt.Errorf("source.git.path %q not found at ref %s: %w", git.Path, ref, err)
	}
	return manifestPath, nil
}

func (l *Lifecycle) lockGitProviderEntryForSource(ctx context.Context, cfg *config.Config, paths lifecyclePaths, name string, app *config.ProviderEntry, configMap map[string]any, mode artifactMode) (LockEntry, error) {
	destDir := providerDestDir(paths, name)
	if gitSourceMaterialization(gitSourceDef(app)) == gitMaterializationSnapshot {
		entry, installed, err := l.lockGitSnapshotSource(ctx, cfg, paths, providermanifestv1.KindApp, name, fmt.Sprintf("provider %q", name), destDir, app, mode)
		if err != nil {
			return LockEntry{}, err
		}
		return finalizeGitSnapshotLockEntry(paths, providermanifestv1.KindApp, name, fmt.Sprintf("provider %q", name), app, entry, installed, configMap)
	}

	install, err := l.prepareGitSourceInstall(ctx, paths, providermanifestv1.KindApp, name, destDir, app)
	if err != nil {
		return LockEntry{}, err
	}
	if err := providerpkg.ValidateConfigForManifest(install.manifestPath, install.manifest, providermanifestv1.KindApp, configMap); err != nil {
		return LockEntry{}, fmt.Errorf("provider config validation for provider %q: %w", name, err)
	}
	return gitLocalLockEntryFromPreparedInstall(paths, providermanifestv1.KindApp, name, app, install)
}

func (l *Lifecycle) lockGitComponentEntryForSource(ctx context.Context, cfg *config.Config, paths lifecyclePaths, kind, name, destDir string, app *config.ProviderEntry, configMap map[string]any, mode artifactMode) (LockEntry, error) {
	subject := fmt.Sprintf("%s %q", kind, name)
	if gitSourceMaterialization(gitSourceDef(app)) == gitMaterializationSnapshot {
		entry, installed, err := l.lockGitSnapshotSource(ctx, cfg, paths, kind, name, subject, destDir, app, mode)
		if err != nil {
			return LockEntry{}, err
		}
		return finalizeGitSnapshotLockEntry(paths, kind, name, subject, app, entry, installed, configMap)
	}

	install, err := l.prepareGitSourceInstall(ctx, paths, kind, name, destDir, app)
	if err != nil {
		return LockEntry{}, err
	}
	if err := providerpkg.ValidateConfigForManifest(install.manifestPath, install.manifest, kind, configMap); err != nil {
		return LockEntry{}, fmt.Errorf("provider config validation for %s %q: %w", kind, name, err)
	}
	return gitLocalLockEntryFromPreparedInstall(paths, kind, name, app, install)
}

func (l *Lifecycle) lockGitSnapshotSource(ctx context.Context, cfg *config.Config, paths lifecyclePaths, expectedKind, name, subject, destDir string, app *config.ProviderEntry, mode artifactMode) (LockEntry, *installedPackage, error) {
	if cfg == nil {
		return LockEntry{}, nil, fmt.Errorf("%s source.git snapshot resolution requires loaded config", subject)
	}
	snapshot, err := resolveGitSnapshotSource(cfg, app)
	if err != nil {
		return LockEntry{}, nil, fmt.Errorf("%s resolve source.git snapshot: %w", subject, err)
	}
	metadataProvider := *app
	metadataProvider.Source = config.NewMetadataSource(snapshot.MetadataURL)
	metadataProvider.Source.Auth = app.Source.Auth
	installed, entry, err := l.installMetadataSourcePackage(ctx, expectedKind, name, subject, destDir, &metadataProvider, paths.configDir, mode)
	if err != nil {
		return LockEntry{}, nil, fmt.Errorf("%s source.git snapshot %s: %w", subject, snapshot.MetadataURL, err)
	}
	entry.Source = snapshot.MetadataURL
	entry.SourceRef = gitSourceLockRef(app, snapshot.GestaltRef)
	return entry, installed, nil
}

func (l *Lifecycle) prepareGitSourceInstall(ctx context.Context, paths lifecyclePaths, kind, name, destDir string, app *config.ProviderEntry) (*preparedInstall, error) {
	manifestPath, err := l.gitSourceManifestPath(ctx, paths, app)
	if err != nil {
		return nil, err
	}
	install, err := l.prepareLocalSourceInstall(kind, name, manifestPath, destDir)
	if err != nil {
		return nil, err
	}
	if err := validateInstalledManifestKind(kind, name, install.manifest); err != nil {
		return nil, err
	}
	return install, nil
}

func (l *Lifecycle) stageGitSourceInstall(ctx context.Context, paths lifecyclePaths, kind, name, destDir string, app *config.ProviderEntry, opts providerpkg.StageSourcePreparedInstallOptions) (*preparedInstall, func() error, func() error, error) {
	manifestPath, err := l.gitSourceManifestPath(ctx, paths, app)
	if err != nil {
		return nil, nil, nil, err
	}
	return stageLocalSourceInstall(kind, name, manifestPath, destDir, opts)
}

func gitLocalLockEntryFromPreparedInstall(paths lifecyclePaths, kind, fingerprintName string, app *config.ProviderEntry, install *preparedInstall) (LockEntry, error) {
	entry, err := localLockEntryFromPreparedInstall(paths, kind, fingerprintName, app, install)
	if err != nil {
		return LockEntry{}, err
	}
	if install.assetRootPath != "" {
		assetRoot, err := relativePreparedPath(paths.artifactsDir, install.assetRootPath)
		if err != nil {
			return LockEntry{}, fmt.Errorf("compute asset root path for %s %q: %w", kind, fingerprintName, err)
		}
		entry.AssetRoot = assetRoot
	}
	entry.Source = canonicalGitSourceLocation(app)
	entry.SourceRef = gitSourceLockRef(app, "")
	entry.Package = install.manifest.Source
	entry.Kind = install.manifest.Kind
	entry.Runtime = providerrelease.RuntimeForManifest(archivePolicyKind(kind), install.manifest)
	entry.Version = install.manifest.Version
	return entry, nil
}

func finalizeGitSnapshotLockEntry(paths lifecyclePaths, kind, fingerprintName, subject string, app *config.ProviderEntry, entry LockEntry, installed *installedPackage, configMap map[string]any) (LockEntry, error) {
	manifestPath, manifest := gitSnapshotLockManifest(entry, installed)
	if err := providerpkg.ValidateConfigForManifest(manifestPath, manifest, kind, configMap); err != nil {
		return LockEntry{}, fmt.Errorf("provider config validation for %s: %w", subject, err)
	}
	return finalizeGitLockEntry(paths, fingerprintName, app, entry, installed, kind)
}

func gitSnapshotLockManifest(entry LockEntry, installed *installedPackage) (string, *providermanifestv1.Manifest) {
	if installed != nil {
		return installed.ManifestPath, installed.Manifest
	}
	return "", entry.ValidationManifest
}

func finalizeGitLockEntry(paths lifecyclePaths, fingerprintName string, app *config.ProviderEntry, entry LockEntry, installed *installedPackage, kind string) (LockEntry, error) {
	fingerprint, err := ProviderFingerprint(fingerprintName, app, paths.configDir)
	if err != nil {
		return LockEntry{}, fmt.Errorf("fingerprinting %s provider: %w", kind, err)
	}
	entry.InputDigest = fingerprint
	manifestPath, manifest := gitSnapshotLockManifest(entry, installed)
	bindGitSnapshotResolvedMetadata(app, entry, manifestPath, manifest)
	if installed == nil {
		return entry, nil
	}
	manifestRel, err := filepath.Rel(paths.artifactsDir, installed.ManifestPath)
	if err != nil {
		return LockEntry{}, fmt.Errorf("compute manifest path for %s provider: %w", kind, err)
	}
	entry.ArtifactManifest = filepath.ToSlash(manifestRel)
	if installed.AssetRoot != "" {
		assetRoot, err := filepath.Rel(paths.artifactsDir, installed.AssetRoot)
		if err != nil {
			return LockEntry{}, fmt.Errorf("compute asset root path for %s provider: %w", kind, err)
		}
		entry.AssetRoot = filepath.ToSlash(assetRoot)
	}
	if installed.ExecutablePath != "" {
		executableRel, err := filepath.Rel(paths.artifactsDir, installed.ExecutablePath)
		if err != nil {
			return LockEntry{}, fmt.Errorf("compute executable path for %s provider: %w", kind, err)
		}
		entry.Executable = filepath.ToSlash(executableRel)
	}
	return entry, nil
}

func bindGitSnapshotResolvedMetadata(app *config.ProviderEntry, entry LockEntry, manifestPath string, manifest *providermanifestv1.Manifest) {
	if app == nil {
		return
	}
	if manifestPath != "" && manifest != nil {
		manifest = providerpkg.ResolveManifestLocalReferences(manifest, manifestPath)
		resolveProviderIcon(manifest, manifestPath, app)
	}
	app.ResolvedManifest = manifest
	app.ResolvedManifestPath = manifestPath
	app.ResolvedCatalog = catalogFromValidationManifest(entry.ValidationManifest, entry.CatalogAvailable)
	app.ResolvedCatalogAvailable = entry.CatalogAvailable
	app.ResolvedCatalogSessionOnly = entry.CatalogSessionOnly
}

func ensureGitCheckout(ctx context.Context, checkoutDir, repo, ref string) error {
	if head, err := gitOutput(ctx, checkoutDir, "rev-parse", "HEAD"); err == nil && strings.EqualFold(strings.TrimSpace(head), ref) {
		return nil
	}
	if _, err := os.Stat(filepath.Join(checkoutDir, ".git")); os.IsNotExist(err) {
		if err := os.RemoveAll(checkoutDir); err != nil {
			return fmt.Errorf("remove stale git source cache: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(checkoutDir), 0o755); err != nil {
			return fmt.Errorf("create git source cache: %w", err)
		}
		if err := gitRun(ctx, "", "clone", "--quiet", "--filter=blob:none", "--no-checkout", repo, checkoutDir); err != nil {
			_ = os.RemoveAll(checkoutDir)
			if fallbackErr := gitRun(ctx, "", "clone", "--quiet", "--no-checkout", repo, checkoutDir); fallbackErr != nil {
				return fmt.Errorf("clone source.git repo %s: %w", repo, err)
			}
		}
	} else if err != nil {
		return fmt.Errorf("inspect git source cache: %w", err)
	}
	if err := gitRun(ctx, checkoutDir, "fetch", "--quiet", "--depth=1", "origin", ref); err != nil {
		if fallbackErr := gitRun(ctx, checkoutDir, "fetch", "--quiet", "origin", ref); fallbackErr != nil {
			return fmt.Errorf("fetch source.git ref %s from %s: %w", ref, repo, err)
		}
	}
	if err := gitRun(ctx, checkoutDir, "checkout", "--quiet", "--detach", ref); err != nil {
		return fmt.Errorf("checkout source.git ref %s: %w", ref, err)
	}
	head, err := gitOutput(ctx, checkoutDir, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("verify source.git checkout: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(head), ref) {
		return fmt.Errorf("source.git checkout resolved %s, want %s", strings.TrimSpace(head), ref)
	}
	return nil
}

func gitRun(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return nil
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
