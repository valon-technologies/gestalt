package keymaterial

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/internal/config"
)

const (
	keyMetadataVersion  = 1
	keyMetadataDir      = ".gestalt"
	keyMetadataFile     = "encryption-key.json"
	keyMetadataSuffix   = ".keymeta.json"
	argon2SaltSizeBytes = 16
)

type EncryptionKeySet struct {
	Primary        []byte
	LegacyFallback []byte
	MetadataPath   string
	Created        bool
}

type keyMetadata struct {
	Version                  int    `json:"version"`
	Argon2IDSaltHex          string `json:"argon2id_salt_hex"`
	LegacyStaticSaltFallback bool   `json:"legacy_static_salt_fallback,omitempty"`
}

func ResolveServerEncryptionKey(cfg *config.Config) (EncryptionKeySet, error) {
	if cfg == nil || cfg.Server.EncryptionKey == "" {
		return EncryptionKeySet{}, nil
	}

	if key, ok := crypto.DecodeHexKey(cfg.Server.EncryptionKey); ok {
		return EncryptionKeySet{Primary: key}, nil
	}

	metadataPath, ok, err := metadataPath(cfg)
	if err != nil {
		return EncryptionKeySet{}, err
	}
	if !ok {
		return EncryptionKeySet{Primary: crypto.DeriveKey(cfg.Server.EncryptionKey)}, nil
	}

	meta, created, err := loadOrCreateKeyMetadata(metadataPath)
	if err != nil {
		return EncryptionKeySet{}, err
	}

	salt, err := hex.DecodeString(meta.Argon2IDSaltHex)
	if err != nil || len(salt) == 0 {
		return EncryptionKeySet{}, fmt.Errorf("parsing encryption key metadata %s: invalid argon2id salt", metadataPath)
	}

	keys := EncryptionKeySet{
		Primary:      crypto.DeriveKeyWithSalt(cfg.Server.EncryptionKey, salt),
		MetadataPath: metadataPath,
		Created:      created,
	}
	if meta.LegacyStaticSaltFallback {
		keys.LegacyFallback = crypto.DeriveKey(cfg.Server.EncryptionKey)
	}
	return keys, nil
}

func metadataPath(cfg *config.Config) (string, bool, error) {
	if cfg.Datastore.Provider == "sqlite" {
		var sqliteCfg struct {
			Path string `yaml:"path"`
		}
		if cfg.Datastore.Config.Kind != 0 {
			if err := cfg.Datastore.Config.Decode(&sqliteCfg); err != nil {
				return "", false, fmt.Errorf("resolving sqlite key metadata path: %w", err)
			}
		}
		if sqliteCfg.Path == "" {
			sqliteCfg.Path = "./gestalt.db"
		}
		return sqliteCfg.Path + keyMetadataSuffix, true, nil
	}
	if cfg.ResolvedConfigPath == "" {
		return "", false, nil
	}
	return filepath.Join(filepath.Dir(cfg.ResolvedConfigPath), keyMetadataDir, keyMetadataFile), true, nil
}

func loadOrCreateKeyMetadata(path string) (keyMetadata, bool, error) {
	meta, err := readKeyMetadata(path)
	if err == nil {
		return meta, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return keyMetadata{}, false, err
	}

	salt := make([]byte, argon2SaltSizeBytes)
	if _, err := rand.Read(salt); err != nil {
		return keyMetadata{}, false, fmt.Errorf("generating argon2id salt: %w", err)
	}

	meta = keyMetadata{
		Version:                  keyMetadataVersion,
		Argon2IDSaltHex:          hex.EncodeToString(salt),
		LegacyStaticSaltFallback: true,
	}
	if err := writeKeyMetadata(path, meta); err != nil {
		if errors.Is(err, os.ErrExist) {
			meta, err = readKeyMetadata(path)
			if err != nil {
				return keyMetadata{}, false, err
			}
			return meta, false, nil
		}
		return keyMetadata{}, false, err
	}
	return meta, true, nil
}

func readKeyMetadata(path string) (keyMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return keyMetadata{}, err
	}
	var meta keyMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return keyMetadata{}, fmt.Errorf("parsing encryption key metadata %s: %w", path, err)
	}
	if meta.Version != keyMetadataVersion {
		return keyMetadata{}, fmt.Errorf("parsing encryption key metadata %s: unsupported version %d", path, meta.Version)
	}
	if meta.Argon2IDSaltHex == "" {
		return keyMetadata{}, fmt.Errorf("parsing encryption key metadata %s: missing argon2id salt", path)
	}
	return meta, nil
}

func writeKeyMetadata(path string, meta keyMetadata) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating encryption key metadata directory: %w", err)
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding encryption key metadata: %w", err)
	}
	data = append(data, '\n')

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return fmt.Errorf("writing encryption key metadata %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing encryption key metadata %s: %w", path, err)
	}
	return nil
}
