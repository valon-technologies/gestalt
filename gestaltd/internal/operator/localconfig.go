package operator

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

const (
	localConfigDirName = ".gestaltd"

	defaultHTTPBinProvider = config.DefaultProviderRepo + "/plugins/httpbin"
	defaultHTTPBinVersion  = "0.0.1-alpha.1"
)

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
	if providersDir := os.Getenv("GESTALT_PROVIDERS_DIR"); providersDir != "" {
		cfg = defaultLocalSourceConfig(providersDir, dbPath, hex.EncodeToString(key))
	}

	if err := os.WriteFile(configPath, []byte(cfg), 0o600); err != nil {
		return "", fmt.Errorf("write config: %w", err)
	}
	slog.Info("generated default config", "path", configPath)
	return configPath, nil
}

func defaultManagedConfig(dbPath, encryptionKey string) string {
	return fmt.Sprintf(`server:
  public:
    port: 8080
  encryptionKey: %q
  providers:
    indexeddb: main
providers:
  indexeddb:
    main:
      source:
        ref: %s
        version: %s
      config:
        dsn: %q
  secrets:
    env:
      source: env
plugins:
  httpbin:
    displayName: HTTPBin
    source:
      ref: %s
      version: %s
    allowedHosts:
      - httpbin.org
`, encryptionKey, config.DefaultIndexedDBProvider, config.DefaultIndexedDBVersion, "sqlite://"+dbPath, defaultHTTPBinProvider, defaultHTTPBinVersion)
}

func defaultLocalSourceConfig(providersDir, dbPath, encryptionKey string) string {
	return fmt.Sprintf(`server:
  public:
    port: 8080
  encryptionKey: %q
  providers:
    indexeddb: main
providers:
  indexeddb:
    main:
      source:
        path: %q
      config:
        dsn: %q
  ui:
    root:
      source:
        path: %q
      path: /
  secrets:
    env:
      source: env
`, encryptionKey, filepath.Join(providersDir, "indexeddb", "relationaldb", "manifest.yaml"), "sqlite://"+dbPath, filepath.Join(providersDir, "web", "default", "manifest.yaml"))
}
