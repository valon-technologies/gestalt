package operator

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	localConfigDirName     = ".gestaltd"
	defaultProviderRepo    = "github.com/valon-technologies/gestalt-providers"
	defaultProviderVersion = "0.0.1-alpha.1"
	providersDirEnvVar     = "GESTALT_PROVIDERS_DIR"
)

var legacyDefaultProviderSourceSubpaths = map[string]map[string]string{
	"auth": {
		"google": "auth/google",
		"oidc":   "auth/oidc",
	},
	"datastore": {
		"postgres": "datastore/postgres",
		"sqlite":   "datastore/sqlite",
	},
}

func DefaultLocalConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, localConfigDirName, "config.yaml")
}

func LocalConfigPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, localConfigDirName, "config.yaml"),
	}
}
func GenerateDefaultConfig(configDir string) (string, error) {
	configPath := filepath.Join(configDir, "config.yaml")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", fmt.Errorf("generate encryption key: %w", err)
	}

	dbPath := filepath.Join(configDir, "gestalt.db")
	cfg := defaultManagedConfig(dbPath, hex.EncodeToString(key))
	if manifestPath := detectLocalProviderManifestPath("datastore/sqlite"); manifestPath != "" {
		cfg = defaultLocalSourceConfig(manifestPath, dbPath, hex.EncodeToString(key))
	}

	if err := os.WriteFile(configPath, []byte(cfg), 0o600); err != nil {
		return "", fmt.Errorf("write config: %w", err)
	}
	slog.Info("generated default config", "path", configPath)
	return configPath, nil
}

func defaultManagedConfig(dbPath, encryptionKey string) string {
	return fmt.Sprintf(`datastore:
  provider:
    source:
      ref: %s/datastore/sqlite
      version: %s
  config:
    path: %q
secrets:
  provider: env
server:
  port: 8080
  encryption_key: %q
`, defaultProviderRepo, defaultProviderVersion, dbPath, encryptionKey)
}

func defaultLocalSourceConfig(manifestPath, dbPath, encryptionKey string) string {
	return fmt.Sprintf(`datastore:
  provider:
    source:
      path: %q
  config:
    path: %q
secrets:
  provider: env
server:
  port: 8080
  encryption_key: %q
`, manifestPath, dbPath, encryptionKey)
}

// RepairDefaultConfig rewrites legacy auth/datastore provider shorthands and
// repairs default managed provider refs to local source manifests when a local
// gestalt-providers checkout is available.
func RepairDefaultConfig(configPath string) (bool, error) {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read config: %w", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return false, nil
	}
	root := documentMappingNode(&doc)
	if root == nil {
		return false, nil
	}

	updated := false
	updated = rewriteLegacyComponentProvider(root, "auth") || updated
	updated = rewriteLegacyComponentProvider(root, "datastore") || updated
	updated = rewriteManagedComponentProviderToLocalSource(root, "auth") || updated
	updated = rewriteManagedComponentProviderToLocalSource(root, "datastore") || updated
	if !updated {
		return false, nil
	}

	encoded, err := yaml.Marshal(root)
	if err != nil {
		return false, fmt.Errorf("marshal migrated config: %w", err)
	}
	info, err := os.Stat(configPath)
	if err != nil {
		return false, fmt.Errorf("stat config: %w", err)
	}
	tempPath := configPath + ".tmp"
	if err := os.WriteFile(tempPath, encoded, info.Mode().Perm()); err != nil {
		return false, fmt.Errorf("write migrated config: %w", err)
	}
	if err := os.Rename(tempPath, configPath); err != nil {
		_ = os.Remove(tempPath)
		return false, fmt.Errorf("replace migrated config: %w", err)
	}

	slog.Info("repaired default config", "path", configPath)
	return true, nil
}

func rewriteLegacyComponentProvider(root *yaml.Node, kind string) bool {
	componentNode := mappingValueNode(root, kind)
	if componentNode == nil || componentNode.Kind != yaml.MappingNode {
		return false
	}
	providerNode := mappingValueNode(componentNode, "provider")
	if providerNode == nil || providerNode.Kind != yaml.ScalarNode {
		return false
	}
	value := strings.TrimSpace(providerNode.Value)
	if providerNode.Tag == "!!null" || value == "" {
		return false
	}
	sourceSubpath, ok := legacyDefaultProviderSourceSubpaths[kind][value]
	if !ok {
		return false
	}
	*providerNode = legacyProviderNode(sourceSubpath)
	return true
}

func legacyProviderNode(sourceSubpath string) yaml.Node {
	if manifestPath := detectLocalProviderManifestPath(sourceSubpath); manifestPath != "" {
		return localProviderNode(manifestPath)
	}
	return yamlMappingNode(
		yamlScalarNode("source"),
		yamlMappingNodePtr(
			yamlScalarNode("ref"),
			yamlScalarNode(defaultProviderRepo+"/"+sourceSubpath),
			yamlScalarNode("version"),
			yamlScalarNode(defaultProviderVersion),
		),
	)
}

func rewriteManagedComponentProviderToLocalSource(root *yaml.Node, kind string) bool {
	componentNode := mappingValueNode(root, kind)
	if componentNode == nil || componentNode.Kind != yaml.MappingNode {
		return false
	}
	providerNode := mappingValueNode(componentNode, "provider")
	if providerNode == nil || providerNode.Kind != yaml.MappingNode {
		return false
	}
	sourceNode := mappingValueNode(providerNode, "source")
	if sourceNode == nil || sourceNode.Kind != yaml.MappingNode {
		return false
	}
	refNode := mappingValueNode(sourceNode, "ref")
	if refNode == nil || refNode.Kind != yaml.ScalarNode {
		return false
	}

	refValue := strings.TrimSpace(refNode.Value)
	for _, sourceSubpath := range legacyDefaultProviderSourceSubpaths[kind] {
		if refValue != defaultProviderRepo+"/"+sourceSubpath {
			continue
		}
		versionNode := mappingValueNode(sourceNode, "version")
		if versionNode != nil {
			versionValue := strings.TrimSpace(versionNode.Value)
			if versionValue != "" && versionValue != defaultProviderVersion {
				return false
			}
		}
		manifestPath := detectLocalProviderManifestPath(sourceSubpath)
		if manifestPath == "" {
			return false
		}
		*providerNode = localProviderNode(manifestPath)
		return true
	}

	return false
}

func localProviderNode(manifestPath string) yaml.Node {
	return yamlMappingNode(
		yamlScalarNode("source"),
		yamlMappingNodePtr(
			yamlScalarNode("path"),
			yamlScalarNode(manifestPath),
		),
	)
}

func detectLocalProviderManifestPath(sourceSubpath string) string {
	for _, root := range localProviderSearchRoots() {
		if manifestPath := resolveLocalProviderManifestPath(root, sourceSubpath); manifestPath != "" {
			return manifestPath
		}
	}
	return ""
}

func localProviderSearchRoots() []string {
	var roots []string
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		clean := filepath.Clean(path)
		for _, existing := range roots {
			if existing == clean {
				return
			}
		}
		roots = append(roots, clean)
	}

	add(os.Getenv(providersDirEnvVar))

	if cwd, err := os.Getwd(); err == nil {
		add(filepath.Join(cwd, "gestalt-providers"))
		add(filepath.Join(cwd, "..", "gestalt-providers"))
	}
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		add(filepath.Join(exeDir, "..", "libexec", "gestalt-providers"))
		add(filepath.Join(exeDir, "..", "share", "gestalt-providers"))
		add(filepath.Join(exeDir, "..", "..", "gestalt-providers"))
	}

	return roots
}

func resolveLocalProviderManifestPath(root, sourceSubpath string) string {
	if root == "" {
		return ""
	}
	relPath := filepath.FromSlash(sourceSubpath)
	candidates := []string{
		filepath.Join(root, relPath, "plugin.yaml"),
		filepath.Join(root, "components", relPath, "plugin.yaml"),
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func documentMappingNode(doc *yaml.Node) *yaml.Node {
	if doc == nil {
		return nil
	}
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			return nil
		}
		return doc.Content[0]
	}
	if doc.Kind == yaml.MappingNode {
		return doc
	}
	return nil
}

func mappingValueNode(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func yamlMappingNode(entries ...*yaml.Node) yaml.Node {
	return yaml.Node{Kind: yaml.MappingNode, Content: entries}
}

func yamlMappingNodePtr(entries ...*yaml.Node) *yaml.Node {
	node := yamlMappingNode(entries...)
	return &node
}

func yamlScalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}
