package operator

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

const (
	localConfigDirName = ".gestaltd"
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
	return fmt.Sprintf(`datastores:
  sqlite:
    driver: sqlite
    dsn: %q
datastore: sqlite
secrets:
  provider: env
server:
  public:
    port: 8080
  encryptionKey: %q
`, dbPath, encryptionKey)
}

func defaultLocalSourceConfig(providersDir, dbPath, encryptionKey string) string {
	return fmt.Sprintf(`datastores:
  sqlite:
    driver: sqlite
    dsn: %q
datastore: sqlite
ui:
  provider:
    source:
      path: %q
secrets:
  provider: env
server:
  public:
    port: 8080
  encryptionKey: %q
`, dbPath, filepath.Join(providersDir, "web", "default", "manifest.yaml"), encryptionKey)
}
