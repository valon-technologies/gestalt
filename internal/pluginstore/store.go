package pluginstore

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	pluginpkg "github.com/valon-technologies/gestalt/internal/pluginpkg"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/sdk/pluginmanifest/v1"
)

var pluginIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._-]*@[A-Za-z0-9][A-Za-z0-9._+:-]*$`)

type PluginID struct {
	Publisher string
	Name      string
	Version   string
}

func ParsePluginID(raw string) (PluginID, error) {
	if strings.TrimSpace(raw) != raw {
		return PluginID{}, fmt.Errorf("invalid plugin identifier %q: leading or trailing whitespace is not allowed", raw)
	}
	if !pluginIDPattern.MatchString(raw) {
		return PluginID{}, fmt.Errorf("invalid plugin identifier %q: expected publisher/name@version", raw)
	}

	left, version, ok := strings.Cut(raw, "@")
	if !ok || version == "" {
		return PluginID{}, fmt.Errorf("invalid plugin identifier %q: expected publisher/name@version", raw)
	}
	publisher, name, ok := strings.Cut(left, "/")
	if !ok || publisher == "" || name == "" {
		return PluginID{}, fmt.Errorf("invalid plugin identifier %q: expected publisher/name@version", raw)
	}

	return PluginID{Publisher: publisher, Name: name, Version: version}, nil
}

func (r PluginID) String() string {
	if r.Publisher == "" && r.Name == "" && r.Version == "" {
		return ""
	}
	return r.Publisher + "/" + r.Name + "@" + r.Version
}

func (r PluginID) IsZero() bool {
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

type InstalledPlugin struct {
	PluginID       PluginID
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
	id, err := pluginIDFromManifest(manifest)
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
	destDir := filepath.Join(root, id.Publisher, id.Name, id.Version)
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
		PluginID:       id,
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

func (s *Store) InstallFromDir(dirPath string) (*InstalledPlugin, error) {
	if s == nil {
		return nil, fmt.Errorf("store is required")
	}
	_, manifest, _, err := pluginpkg.LoadManifestFromPath(dirPath)
	if err != nil {
		return nil, err
	}
	id, err := pluginIDFromManifest(manifest)
	if err != nil {
		return nil, err
	}
	artifact, err := pluginpkg.CurrentPlatformArtifact(manifest)
	if err != nil {
		return nil, err
	}

	artifactPath := filepath.Join(dirPath, filepath.FromSlash(artifact.Path))
	artifactFile, err := os.Open(artifactPath)
	if err != nil {
		return nil, fmt.Errorf("open artifact %s: %w", artifact.Path, err)
	}
	sum := sha256.New()
	if _, err := io.Copy(sum, artifactFile); err != nil {
		_ = artifactFile.Close()
		return nil, fmt.Errorf("read artifact %s: %w", artifact.Path, err)
	}
	_ = artifactFile.Close()
	if got := hex.EncodeToString(sum.Sum(nil)); got != artifact.SHA256 {
		return nil, fmt.Errorf("artifact digest mismatch for %s: directory has %s, manifest expects %s", artifact.Path, got, artifact.SHA256)
	}

	root := s.root
	if root == "" {
		return nil, fmt.Errorf("store root is required")
	}
	destDir := filepath.Join(root, id.Publisher, id.Name, id.Version)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("create plugin directory: %w", err)
	}

	manifestSrc := filepath.Join(dirPath, pluginpkg.ManifestFile)
	if err := copyFile(manifestSrc, filepath.Join(destDir, pluginpkg.ManifestFile)); err != nil {
		return nil, fmt.Errorf("copy manifest: %w", err)
	}

	artifactDest := filepath.Join(destDir, filepath.FromSlash(artifact.Path))
	if err := os.MkdirAll(filepath.Dir(artifactDest), 0755); err != nil {
		return nil, fmt.Errorf("create artifact directory: %w", err)
	}
	if err := copyFile(artifactPath, artifactDest); err != nil {
		return nil, fmt.Errorf("copy artifact: %w", err)
	}

	for _, schemaRel := range configSchemaPaths(manifest, dirPath) {
		schemaSrc := filepath.Join(dirPath, filepath.FromSlash(schemaRel))
		schemaDest := filepath.Join(destDir, filepath.FromSlash(schemaRel))
		if err := os.MkdirAll(filepath.Dir(schemaDest), 0755); err != nil {
			return nil, fmt.Errorf("create schema directory: %w", err)
		}
		if err := copyFile(schemaSrc, schemaDest); err != nil {
			return nil, fmt.Errorf("copy config schema %s: %w", schemaRel, err)
		}
	}

	executablePath, err := executablePathForManifest(destDir, manifest)
	if err != nil {
		return nil, err
	}

	return &InstalledPlugin{
		PluginID:       id,
		Root:           destDir,
		ManifestPath:   filepath.Join(destDir, pluginpkg.ManifestFile),
		ExecutablePath: executablePath,
		ArtifactPath:   artifact.Path,
		SHA256:         artifact.SHA256,
		Manifest:       manifest,
		Artifact:       artifact,
	}, nil
}

func pluginIDFromManifest(manifest *pluginmanifestv1.Manifest) (PluginID, error) {
	if manifest == nil {
		return PluginID{}, fmt.Errorf("manifest is required")
	}
	id, err := ParsePluginID(manifest.ID + "@" + manifest.Version)
	if err != nil {
		return PluginID{}, fmt.Errorf("manifest id/version must form a valid plugin identifier: %w", err)
	}
	return id, nil
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

const runtimeConfigSchemaPath = "schemas/config.schema.json"

func configSchemaPaths(manifest *pluginmanifestv1.Manifest, dirPath string) []string {
	var paths []string
	if manifest.Provider != nil && manifest.Provider.ConfigSchemaPath != "" {
		paths = append(paths, manifest.Provider.ConfigSchemaPath)
	}
	runtimeSchema := filepath.Join(dirPath, filepath.FromSlash(runtimeConfigSchemaPath))
	if _, err := os.Stat(runtimeSchema); err == nil {
		paths = append(paths, runtimeConfigSchemaPath)
	}
	return paths
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
