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
	localConfigDirName       = ".gestaltd"
	legacyLocalConfigDirName = ".gestalt"
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
		filepath.Join(home, legacyLocalConfigDirName, "config.yaml"),
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
	cfg := fmt.Sprintf(`auth:
  provider: none
datastore:
  provider: sqlite
  config:
    path: %q
secrets:
  provider: env
server:
  port: 8080
  encryption_key: %q
`, dbPath, hex.EncodeToString(key))

	if err := os.WriteFile(configPath, []byte(cfg), 0o600); err != nil {
		return "", fmt.Errorf("write config: %w", err)
	}
	slog.Info("generated default config", "path", configPath)
	return configPath, nil
}
