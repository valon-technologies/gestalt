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

func ResolveConfigPaths(flagValues []string) []string {
	if len(flagValues) > 0 {
		return append([]string(nil), flagValues...)
	}
	if envPath := os.Getenv("GESTALT_CONFIG"); envPath != "" {
		return []string{envPath}
	}
	if _, err := os.Stat("config.yaml"); err == nil {
		return []string{"config.yaml"}
	}
	for _, p := range LocalConfigPaths() {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return []string{p}
		}
	}
	if p := DefaultLocalConfigPath(); p != "" {
		return []string{p}
	}
	return nil
}

func ResolveStartConfigPaths(flagValues []string) ([]string, error) {
	configPaths := ResolveConfigPaths(flagValues)
	if len(flagValues) > 0 || len(configPaths) == 0 {
		return configPaths, nil
	}

	defaultPath := DefaultLocalConfigPath()
	primaryPath := configPaths[0]
	if defaultPath == "" || primaryPath != defaultPath {
		return configPaths, nil
	}

	if _, err := os.Stat(primaryPath); err == nil {
		return configPaths, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat default config: %w", err)
	}

	generated, err := GenerateDefaultConfig(filepath.Dir(primaryPath))
	if err != nil {
		return nil, err
	}
	return []string{generated}, nil
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
	return fmt.Sprintf(`apiVersion: gestaltd.config/v3
server:
  public:
    port: 8080
  encryptionKey: %q
  providers:
    indexeddb: main
providers:
  indexeddb:
    main:
      source: %s
      config:
        dsn: %q
  ui:
    root:
      source: %s
      path: /
  secrets:
    env:
      source: env
plugins:
  httpbin:
    displayName: HTTPBin
    source: %s
    allowedHosts:
      - httpbin.org
`, encryptionKey,
		config.DefaultProviderMetadataURL(config.DefaultIndexedDBProvider, config.DefaultIndexedDBVersion),
		"sqlite://"+dbPath,
		config.DefaultProviderMetadataURL(config.DefaultUIProvider, config.DefaultUIVersion),
		config.DefaultProviderMetadataURL(defaultHTTPBinProvider, defaultHTTPBinVersion))
}

func defaultLocalSourceConfig(providersDir, dbPath, encryptionKey string) string {
	return fmt.Sprintf(`apiVersion: gestaltd.config/v3
server:
  public:
    port: 8080
  encryptionKey: %q
  providers:
    externalCredentials: default
    indexeddb: main
providers:
  externalCredentials:
    default:
      source:
        path: %q
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
`, encryptionKey,
		config.DefaultLocalProviderManifestPath(providersDir, config.DefaultExternalCredentialsProvider),
		config.DefaultLocalProviderManifestPath(providersDir, config.DefaultIndexedDBProvider),
		"sqlite://"+dbPath,
		config.DefaultLocalProviderManifestPath(providersDir, config.DefaultUIProvider))
}
