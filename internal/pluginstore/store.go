package pluginstore

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	pluginpkg "github.com/valon-technologies/gestalt/internal/pluginpkg"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/sdk/pluginmanifest/v1"
)

var refPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._-]*@[A-Za-z0-9][A-Za-z0-9._+:-]*$`)

type Ref struct {
	Publisher string
	Name      string
	Version   string
}

func ParseRef(raw string) (Ref, error) {
	if strings.TrimSpace(raw) != raw {
		return Ref{}, fmt.Errorf("invalid plugin ref %q: leading or trailing whitespace is not allowed", raw)
	}
	if !refPattern.MatchString(raw) {
		return Ref{}, fmt.Errorf("invalid plugin ref %q: expected publisher/name@version", raw)
	}

	left, version, ok := strings.Cut(raw, "@")
	if !ok || version == "" {
		return Ref{}, fmt.Errorf("invalid plugin ref %q: expected publisher/name@version", raw)
	}
	publisher, name, ok := strings.Cut(left, "/")
	if !ok || publisher == "" || name == "" {
		return Ref{}, fmt.Errorf("invalid plugin ref %q: expected publisher/name@version", raw)
	}

	return Ref{Publisher: publisher, Name: name, Version: version}, nil
}

func (r Ref) String() string {
	if r.Publisher == "" && r.Name == "" && r.Version == "" {
		return ""
	}
	return r.Publisher + "/" + r.Name + "@" + r.Version
}

func (r Ref) IsZero() bool {
	return r.Publisher == "" && r.Name == "" && r.Version == ""
}

func storeRootForConfigPath(configPath string) string {
	if configPath == "" {
		return filepath.Join(".gestalt", "plugins")
	}
	return filepath.Join(filepath.Dir(configPath), ".gestalt", "plugins")
}

type Store struct {
	root string
}

func New(configPath string) *Store {
	return &Store{root: storeRootForConfigPath(configPath)}
}

func (s *Store) Root() string {
	if s == nil {
		return ""
	}
	return s.root
}

type InstalledPlugin struct {
	Ref            Ref
	Root           string
	ManifestPath   string
	ExecutablePath string
	ArtifactPath   string
	SHA256         string
	Manifest       *pluginmanifestv1.Manifest
	Artifact       *pluginmanifestv1.Artifact
}

func (s *Store) Install(packagePath string) (*InstalledPlugin, error) {
	if s == nil {
		return nil, fmt.Errorf("store is required")
	}
	_, manifest, err := pluginpkg.ReadPackageManifest(packagePath)
	if err != nil {
		return nil, err
	}
	ref, err := refFromManifest(manifest)
	if err != nil {
		return nil, err
	}
	artifact, err := pluginpkg.CurrentPlatformArtifact(manifest)
	if err != nil {
		return nil, err
	}
	artifactBytes, err := pluginpkg.ReadArchiveEntry(packagePath, artifact.Path)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(artifactBytes)
	if got := hex.EncodeToString(sum[:]); got != artifact.SHA256 {
		return nil, fmt.Errorf("artifact digest mismatch for %s: package has %s, manifest expects %s", artifact.Path, got, artifact.SHA256)
	}

	root := s.root
	if root == "" {
		return nil, fmt.Errorf("store root is required")
	}
	destDir := filepath.Join(root, ref.Publisher, ref.Name, ref.Version)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("create plugin directory: %w", err)
	}
	if err := pluginpkg.ExtractPackage(packagePath, destDir); err != nil {
		return nil, err
	}

	manifestPath := filepath.Join(destDir, pluginpkg.ManifestFile)
	executablePath, err := executablePathForManifest(destDir, manifest)
	if err != nil {
		return nil, err
	}

	installed := &InstalledPlugin{
		Ref:            ref,
		Root:           destDir,
		ManifestPath:   manifestPath,
		ExecutablePath: executablePath,
		ArtifactPath:   artifact.Path,
		SHA256:         artifact.SHA256,
		Manifest:       manifest,
		Artifact:       artifact,
	}
	return installed, nil
}

func (s *Store) List() ([]InstalledPlugin, error) {
	if s == nil {
		return nil, fmt.Errorf("store is required")
	}
	root := s.root
	if root == "" {
		return nil, fmt.Errorf("store root is required")
	}
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat store root: %w", err)
	}

	var items []InstalledPlugin
	pattern := filepath.Join(root, "*", "*", "*", pluginpkg.ManifestFile)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("list plugin manifests: %w", err)
	}
	for _, manifestPath := range matches {
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			return nil, fmt.Errorf("read manifest %s: %w", manifestPath, err)
		}
		manifest, err := pluginpkg.DecodeManifest(data)
		if err != nil {
			return nil, fmt.Errorf("decode manifest %s: %w", manifestPath, err)
		}
		ref, err := refFromManifest(manifest)
		if err != nil {
			return nil, err
		}
		artifact, err := pluginpkg.CurrentPlatformArtifact(manifest)
		if err != nil {
			return nil, err
		}
		executablePath, err := executablePathForManifest(filepath.Dir(manifestPath), manifest)
		if err != nil {
			return nil, err
		}
		items = append(items, InstalledPlugin{
			Ref:            ref,
			Root:           filepath.Dir(manifestPath),
			ManifestPath:   manifestPath,
			ExecutablePath: executablePath,
			ArtifactPath:   artifact.Path,
			SHA256:         artifact.SHA256,
			Manifest:       manifest,
			Artifact:       artifact,
		})
	}

	slices.SortFunc(items, func(a, b InstalledPlugin) int {
		return strings.Compare(a.Ref.String(), b.Ref.String())
	})
	return items, nil
}

func (s *Store) Resolve(ref Ref) (*InstalledPlugin, error) {
	if s == nil {
		return nil, fmt.Errorf("store is required")
	}
	if ref.IsZero() {
		return nil, fmt.Errorf("plugin ref is required")
	}
	root := filepath.Join(s.root, ref.Publisher, ref.Name, ref.Version)
	manifestPath := filepath.Join(root, pluginpkg.ManifestFile)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read installed manifest %q: %w", manifestPath, err)
	}
	manifest, err := pluginpkg.DecodeManifest(data)
	if err != nil {
		return nil, fmt.Errorf("decode installed manifest %q: %w", manifestPath, err)
	}
	installedRef, err := refFromManifest(manifest)
	if err != nil {
		return nil, err
	}
	if installedRef != ref {
		return nil, fmt.Errorf("installed plugin %s does not match requested ref %s", installedRef, ref)
	}
	artifact, err := pluginpkg.CurrentPlatformArtifact(manifest)
	if err != nil {
		return nil, err
	}
	executablePath, err := executablePathForManifest(root, manifest)
	if err != nil {
		return nil, err
	}
	return &InstalledPlugin{
		Ref:            ref,
		Root:           root,
		ManifestPath:   manifestPath,
		ExecutablePath: executablePath,
		ArtifactPath:   artifact.Path,
		SHA256:         artifact.SHA256,
		Manifest:       manifest,
		Artifact:       artifact,
	}, nil
}

func RootForConfigPath(configPath string) string {
	return storeRootForConfigPath(configPath)
}

func refFromManifest(manifest *pluginmanifestv1.Manifest) (Ref, error) {
	if manifest == nil {
		return Ref{}, fmt.Errorf("manifest is required")
	}
	ref, err := ParseRef(manifest.ID + "@" + manifest.Version)
	if err != nil {
		return Ref{}, fmt.Errorf("manifest id/version must form a valid plugin ref: %w", err)
	}
	return ref, nil
}

func executablePathForManifest(root string, manifest *pluginmanifestv1.Manifest) (string, error) {
	if manifest == nil {
		return "", fmt.Errorf("manifest is required")
	}
	var entry *pluginmanifestv1.Entrypoint
	for _, kind := range manifest.Kinds {
		switch kind {
		case pluginmanifestv1.KindProvider:
			entry = manifest.Entrypoints.Provider
		case pluginmanifestv1.KindRuntime:
			entry = manifest.Entrypoints.Runtime
		default:
			continue
		}
		if entry != nil {
			break
		}
	}
	if entry == nil {
		return "", fmt.Errorf("manifest does not define an executable entrypoint")
	}
	if entry.ArtifactPath == "" {
		return "", fmt.Errorf("manifest entrypoint artifact_path is required")
	}
	return filepath.Join(root, filepath.FromSlash(entry.ArtifactPath)), nil
}
