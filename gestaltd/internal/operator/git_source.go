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
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/plugins/providerpkg"
)

const (
	gitSourceRefType        = "git"
	gitArtifactModeSource   = "source"
	gitArtifactModePrefer   = "prefer"
	gitArtifactModeRequire  = "require"
	gitResolvedModeSource   = "source"
	gitResolvedModeSnapshot = "snapshot"
)

type gitSnapshotSource struct {
	MetadataURL string
	GestaltRef  string
}

func gitSourceDef(entry *config.ProviderEntry) *config.GitSourceDef {
	if entry == nil {
		return nil
	}
	return entry.Source.GitSource()
}

func gitSourceArtifactMode(git *config.GitSourceDef) string {
	if git == nil {
		return ""
	}
	mode := strings.TrimSpace(git.ArtifactMode)
	if mode == "" {
		return gitArtifactModeSource
	}
	return mode
}

func gitSourceLockRef(entry *config.ProviderEntry, resolvedMode, resolvedGestaltRef string) *LockSourceRef {
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
		ArtifactMode:       gitSourceArtifactMode(git),
		ResolvedMode:       strings.TrimSpace(resolvedMode),
		ResolvedGestaltRef: strings.TrimSpace(resolvedGestaltRef),
	}
}

func gitSourceFingerprintLocation(entry *config.ProviderEntry) string {
	ref := gitSourceLockRef(entry, "", "")
	if ref == nil {
		return ""
	}
	return strings.Join([]string{
		ref.Type,
		ref.Repo,
		ref.Ref,
		ref.Path,
		ref.ArtifactRepository,
		ref.ArtifactMode,
	}, "\x00")
}

func gitSourceMatchesLockRef(entry *config.ProviderEntry, lockRef *LockSourceRef) bool {
	expected := gitSourceLockRef(entry, "", "")
	if expected == nil || lockRef == nil {
		return false
	}
	if lockRef.Type != gitSourceRefType ||
		lockRef.Repo != expected.Repo ||
		!strings.EqualFold(lockRef.Ref, expected.Ref) ||
		lockRef.Path != expected.Path ||
		lockRef.ArtifactRepository != expected.ArtifactRepository ||
		lockRef.ArtifactMode != expected.ArtifactMode {
		return false
	}
	switch expected.ArtifactMode {
	case gitArtifactModeRequire:
		return lockRef.ResolvedMode == gitResolvedModeSnapshot
	case gitArtifactModeSource:
		return lockRef.ResolvedMode == gitResolvedModeSource
	default:
		return lockRef.ResolvedMode == gitResolvedModeSnapshot || lockRef.ResolvedMode == gitResolvedModeSource
	}
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
	owner, name, err := githubRepoPath(git.Repo)
	if err != nil {
		return gitSnapshotSource{}, err
	}
	base := strings.TrimRight(strings.TrimSpace(repo.URL), "/")
	_, ref, manifestPath := git.NormalizedLocationParts()
	providerDir := path.Dir(manifestPath)
	if providerDir == "." {
		providerDir = ""
	}
	parts := []string{base, "github.com", owner, name, ref}
	if providerDir != "" {
		parts = append(parts, strings.Split(providerDir, "/")...)
	}
	parts = append(parts, "provider-release.yaml")
	return gitSnapshotSource{
		MetadataURL: strings.Join(parts, "/"),
		GestaltRef:  strings.TrimSpace(repo.GestaltRef),
	}, nil
}

func githubRepoPath(raw string) (string, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", fmt.Errorf("parse source.git.repo: %w", err)
	}
	if parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "github.com") {
		return "", "", fmt.Errorf("source.git snapshots require https://github.com/<owner>/<repo>[.git]")
	}
	clean := strings.Trim(path.Clean(parsed.Path), "/")
	parts := strings.Split(clean, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("source.git snapshots require https://github.com/<owner>/<repo>[.git]")
	}
	return parts[0], strings.TrimSuffix(parts[1], ".git"), nil
}

func (l *Lifecycle) gitSourceManifestPath(ctx context.Context, paths lifecyclePaths, entry *config.ProviderEntry) (string, error) {
	git := gitSourceDef(entry)
	if git == nil {
		return "", fmt.Errorf("source.git is required")
	}
	repo, ref, manifestRel := git.NormalizedLocationParts()
	cacheRoot := filepath.Join(paths.artifactsDir, ".gestaltd", "git-source-cache")
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

func (l *Lifecycle) lockGitProviderEntryForSource(ctx context.Context, cfg *config.Config, paths lifecyclePaths, name string, plugin *config.ProviderEntry, configMap map[string]any) (LockEntry, error) {
	destDir := providerDestDir(paths, name)
	if entry, installed, ok, err := l.tryLockGitSnapshotSource(ctx, cfg, paths, providermanifestv1.KindPlugin, name, fmt.Sprintf("provider %q", name), destDir, plugin); ok || err != nil {
		if err != nil {
			return LockEntry{}, err
		}
		if err := providerpkg.ValidateConfigForManifest(installed.ManifestPath, installed.Manifest, providermanifestv1.KindPlugin, configMap); err != nil {
			return LockEntry{}, fmt.Errorf("provider config validation for provider %q: %w", name, err)
		}
		return finalizeGitInstalledLockEntry(paths, name, plugin, entry, installed, providermanifestv1.KindPlugin, false)
	}

	install, err := l.prepareGitSourceInstall(ctx, paths, providermanifestv1.KindPlugin, name, destDir, plugin)
	if err != nil {
		return LockEntry{}, err
	}
	if err := providerpkg.ValidateConfigForManifest(install.manifestPath, install.manifest, providermanifestv1.KindPlugin, configMap); err != nil {
		return LockEntry{}, fmt.Errorf("provider config validation for provider %q: %w", name, err)
	}
	return gitLocalLockEntryFromPreparedInstall(paths, providermanifestv1.KindPlugin, name, plugin, install, false)
}

func (l *Lifecycle) lockGitComponentEntryForSource(ctx context.Context, cfg *config.Config, paths lifecyclePaths, kind, name, destDir string, plugin *config.ProviderEntry, configMap map[string]any) (LockEntry, error) {
	subject := fmt.Sprintf("%s %q", kind, name)
	if entry, installed, ok, err := l.tryLockGitSnapshotSource(ctx, cfg, paths, kind, name, subject, destDir, plugin); ok || err != nil {
		if err != nil {
			return LockEntry{}, err
		}
		if err := providerpkg.ValidateConfigForManifest(installed.ManifestPath, installed.Manifest, kind, configMap); err != nil {
			return LockEntry{}, fmt.Errorf("provider config validation for %s %q: %w", kind, name, err)
		}
		return finalizeGitInstalledLockEntry(paths, name, plugin, entry, installed, kind, false)
	}

	install, err := l.prepareGitSourceInstall(ctx, paths, kind, name, destDir, plugin)
	if err != nil {
		return LockEntry{}, err
	}
	if err := providerpkg.ValidateConfigForManifest(install.manifestPath, install.manifest, kind, configMap); err != nil {
		return LockEntry{}, fmt.Errorf("provider config validation for %s %q: %w", kind, name, err)
	}
	return gitLocalLockEntryFromPreparedInstall(paths, kind, name, plugin, install, false)
}

func (l *Lifecycle) lockGitUIEntryForSource(ctx context.Context, cfg *config.Config, paths lifecyclePaths, name string, plugin *config.ProviderEntry, destDir, subject string, configMap map[string]any) (LockEntry, error) {
	if entry, installed, ok, err := l.tryLockGitSnapshotSource(ctx, cfg, paths, providermanifestv1.KindUI, name, subject, destDir, plugin); ok || err != nil {
		if err != nil {
			return LockEntry{}, err
		}
		if err := providerpkg.ValidateConfigForManifest(installed.ManifestPath, installed.Manifest, providermanifestv1.KindUI, configMap); err != nil {
			return LockEntry{}, fmt.Errorf("provider config validation for %s: %w", subject, err)
		}
		return finalizeGitInstalledLockEntry(paths, "ui:"+name, plugin, entry, installed, providermanifestv1.KindUI, true)
	}

	install, err := l.prepareGitSourceInstall(ctx, paths, providermanifestv1.KindUI, name, destDir, plugin)
	if err != nil {
		return LockEntry{}, err
	}
	if err := providerpkg.ValidateConfigForManifest(install.manifestPath, install.manifest, providermanifestv1.KindUI, configMap); err != nil {
		return LockEntry{}, fmt.Errorf("provider config validation for %s: %w", subject, err)
	}
	return gitLocalLockEntryFromPreparedInstall(paths, providermanifestv1.KindUI, "ui:"+name, plugin, install, true)
}

func (l *Lifecycle) tryLockGitSnapshotSource(ctx context.Context, cfg *config.Config, paths lifecyclePaths, expectedKind, name, subject, destDir string, plugin *config.ProviderEntry) (LockEntry, *installedPackage, bool, error) {
	git := gitSourceDef(plugin)
	mode := gitSourceArtifactMode(git)
	if mode == gitArtifactModeSource {
		return LockEntry{}, nil, false, nil
	}
	if cfg == nil {
		if mode == gitArtifactModeRequire {
			return LockEntry{}, nil, false, fmt.Errorf("%s source.git snapshot resolution requires loaded config", subject)
		}
		return LockEntry{}, nil, false, nil
	}
	snapshot, err := resolveGitSnapshotSource(cfg, plugin)
	if err != nil {
		if mode == gitArtifactModeRequire {
			return LockEntry{}, nil, false, fmt.Errorf("%s resolve source.git snapshot: %w", subject, err)
		}
		return LockEntry{}, nil, false, nil
	}
	metadataProvider := *plugin
	metadataProvider.Source = config.NewMetadataSource(snapshot.MetadataURL)
	metadataProvider.Source.Auth = plugin.Source.Auth
	installed, entry, err := l.installMetadataSourcePackage(ctx, expectedKind, name, subject, destDir, &metadataProvider, paths.configDir)
	if err != nil {
		if mode == gitArtifactModeRequire {
			return LockEntry{}, nil, false, fmt.Errorf("%s source.git snapshot %s: %w", subject, snapshot.MetadataURL, err)
		}
		return LockEntry{}, nil, false, nil
	}
	entry.Source = snapshot.MetadataURL
	entry.SourceRef = gitSourceLockRef(plugin, gitResolvedModeSnapshot, snapshot.GestaltRef)
	return entry, installed, true, nil
}

func (l *Lifecycle) prepareGitSourceInstall(ctx context.Context, paths lifecyclePaths, kind, name, destDir string, plugin *config.ProviderEntry) (*preparedInstall, error) {
	manifestPath, err := l.gitSourceManifestPath(ctx, paths, plugin)
	if err != nil {
		return nil, err
	}
	install, err := prepareLocalSourceInstall(kind, name, manifestPath, destDir)
	if err != nil {
		return nil, err
	}
	if err := validateInstalledManifestKind(kind, name, install.manifest); err != nil {
		return nil, err
	}
	return install, nil
}

func (l *Lifecycle) stageGitSourceInstall(ctx context.Context, paths lifecyclePaths, kind, name, destDir string, plugin *config.ProviderEntry) (*preparedInstall, func() error, func() error, error) {
	manifestPath, err := l.gitSourceManifestPath(ctx, paths, plugin)
	if err != nil {
		return nil, nil, nil, err
	}
	return stageLocalSourceInstall(kind, name, manifestPath, destDir)
}

func gitLocalLockEntryFromPreparedInstall(paths lifecyclePaths, kind, fingerprintName string, plugin *config.ProviderEntry, install *preparedInstall, ui bool) (LockEntry, error) {
	var entry LockEntry
	var err error
	if ui {
		entry, err = localUILockEntryFromPreparedInstall(paths, strings.TrimPrefix(fingerprintName, "ui:"), plugin, install)
	} else {
		entry, err = localLockEntryFromPreparedInstall(paths, kind, fingerprintName, plugin, install)
	}
	if err != nil {
		return LockEntry{}, err
	}
	entry.Source = canonicalGitSourceLocation(plugin)
	entry.SourceRef = gitSourceLockRef(plugin, gitResolvedModeSource, "")
	entry.Package = install.manifest.Source
	entry.Kind = install.manifest.Kind
	entry.Runtime = releaseRuntimeForManifest(install.manifest, archivePolicyKind(kind))
	entry.Version = install.manifest.Version
	return entry, nil
}

func finalizeGitInstalledLockEntry(paths lifecyclePaths, fingerprintName string, plugin *config.ProviderEntry, entry LockEntry, installed *installedPackage, kind string, ui bool) (LockEntry, error) {
	var fingerprint string
	var err error
	if ui {
		fingerprint, err = NamedUIProviderFingerprint(strings.TrimPrefix(fingerprintName, "ui:"), plugin, paths.configDir)
	} else {
		fingerprint, err = ProviderFingerprint(fingerprintName, plugin, paths.configDir)
	}
	if err != nil {
		return LockEntry{}, fmt.Errorf("fingerprinting %s provider: %w", kind, err)
	}
	manifestPath, err := filepath.Rel(paths.artifactsDir, installed.ManifestPath)
	if err != nil {
		return LockEntry{}, fmt.Errorf("compute manifest path for %s provider: %w", kind, err)
	}
	entry.Fingerprint = fingerprint
	entry.Manifest = filepath.ToSlash(manifestPath)
	if ui {
		assetRoot, err := filepath.Rel(paths.artifactsDir, installed.AssetRoot)
		if err != nil {
			return LockEntry{}, fmt.Errorf("compute asset root path for ui provider: %w", err)
		}
		entry.AssetRoot = filepath.ToSlash(assetRoot)
		return entry, nil
	}
	executableRel := ""
	if installed.ExecutablePath != "" {
		executableRel, err = filepath.Rel(paths.artifactsDir, installed.ExecutablePath)
		if err != nil {
			return LockEntry{}, fmt.Errorf("compute executable path for %s provider: %w", kind, err)
		}
	}
	entry.Executable = filepath.ToSlash(executableRel)
	return entry, nil
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
