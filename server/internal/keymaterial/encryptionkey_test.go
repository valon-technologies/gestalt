package keymaterial_test

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/keymaterial"
	"gopkg.in/yaml.v3"
)

func TestResolveServerEncryptionKeyCreatesSQLiteMetadata(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "gestalt.db")
	cfg := &config.Config{
		Datastore: config.DatastoreConfig{
			Provider: "sqlite",
			Config:   sqliteConfigNode(dbPath),
		},
		Server: config.ServerConfig{
			EncryptionKey: "shared-passphrase",
		},
	}

	first, err := keymaterial.ResolveServerEncryptionKey(cfg)
	if err != nil {
		t.Fatalf("ResolveServerEncryptionKey: %v", err)
	}
	if len(first.Primary) != 32 {
		t.Fatalf("primary key length = %d, want 32", len(first.Primary))
	}
	if len(first.LegacyFallback) != 32 {
		t.Fatalf("legacy fallback key length = %d, want 32", len(first.LegacyFallback))
	}
	if !first.Created {
		t.Fatal("expected first resolve to create metadata")
	}
	if first.MetadataPath != dbPath+metadataSuffix() {
		t.Fatalf("metadata path = %q, want %q", first.MetadataPath, dbPath+metadataSuffix())
	}
	if _, err := os.Stat(first.MetadataPath); err != nil {
		t.Fatalf("metadata file missing: %v", err)
	}

	second, err := keymaterial.ResolveServerEncryptionKey(cfg)
	if err != nil {
		t.Fatalf("ResolveServerEncryptionKey (second): %v", err)
	}
	if second.Created {
		t.Fatal("did not expect second resolve to recreate metadata")
	}
	if !bytes.Equal(first.Primary, second.Primary) {
		t.Fatal("primary key changed after metadata persisted")
	}
}

func TestResolveServerEncryptionKeyUsesDistinctSQLiteMetadataPerDatabase(t *testing.T) {
	t.Parallel()

	cfg1 := &config.Config{
		Datastore: config.DatastoreConfig{
			Provider: "sqlite",
			Config:   sqliteConfigNode(filepath.Join(t.TempDir(), "one.db")),
		},
		Server: config.ServerConfig{EncryptionKey: "shared-passphrase"},
	}
	cfg2 := &config.Config{
		Datastore: config.DatastoreConfig{
			Provider: "sqlite",
			Config:   sqliteConfigNode(filepath.Join(t.TempDir(), "two.db")),
		},
		Server: config.ServerConfig{EncryptionKey: "shared-passphrase"},
	}

	key1, err := keymaterial.ResolveServerEncryptionKey(cfg1)
	if err != nil {
		t.Fatalf("ResolveServerEncryptionKey cfg1: %v", err)
	}
	key2, err := keymaterial.ResolveServerEncryptionKey(cfg2)
	if err != nil {
		t.Fatalf("ResolveServerEncryptionKey cfg2: %v", err)
	}
	if bytes.Equal(key1.Primary, key2.Primary) {
		t.Fatal("expected different sqlite deployments to derive different primary keys")
	}
}

func TestResolveServerEncryptionKeySkipsMetadataForHexKey(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "gestalt.db")
	raw := bytes.Repeat([]byte{0x42}, 32)
	cfg := &config.Config{
		Datastore: config.DatastoreConfig{
			Provider: "sqlite",
			Config:   sqliteConfigNode(dbPath),
		},
		Server: config.ServerConfig{
			EncryptionKey: hex.EncodeToString(raw),
		},
	}

	keys, err := keymaterial.ResolveServerEncryptionKey(cfg)
	if err != nil {
		t.Fatalf("ResolveServerEncryptionKey: %v", err)
	}
	if !bytes.Equal(keys.Primary, raw) {
		t.Fatal("hex key was not decoded directly")
	}
	if len(keys.LegacyFallback) != 0 {
		t.Fatal("hex key should not produce a legacy fallback key")
	}
	if keys.MetadataPath != "" {
		t.Fatalf("hex key metadata path = %q, want empty", keys.MetadataPath)
	}
	if _, err := os.Stat(dbPath + metadataSuffix()); !os.IsNotExist(err) {
		t.Fatalf("metadata file unexpectedly created: %v", err)
	}
}

func sqliteConfigNode(path string) yaml.Node {
	return yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "path", Tag: "!!str"},
			{Kind: yaml.ScalarNode, Value: path, Tag: "!!str"},
		},
	}
}

func metadataSuffix() string {
	return ".keymeta.json"
}
