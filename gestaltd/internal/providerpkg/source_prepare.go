package providerpkg

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

const envWriteCatalog = "GESTALT_PLUGIN_WRITE_CATALOG"

func PrepareSourceManifest(manifestPath string) ([]byte, *providermanifestv1.Manifest, error) {
	data, manifest, err := ReadSourceManifestFile(manifestPath)
	if err != nil {
		return nil, nil, err
	}
	format := ManifestFormatFromPath(manifestPath)
	originalManifest, err := DecodeSourceManifestFormat(data, format)
	if err != nil {
		return nil, nil, err
	}
	originalEncoded, err := EncodeSourceManifestFormat(originalManifest, format)
	if err != nil {
		return nil, nil, err
	}
	if err := EnsureSourceStaticCatalog(manifestPath, manifest); err != nil {
		return nil, nil, err
	}
	updatedEncoded, err := EncodeSourceManifestFormat(manifest, format)
	if err != nil {
		return nil, nil, err
	}
	if !bytes.Equal(originalEncoded, updatedEncoded) {
		data = updatedEncoded
	}
	return data, manifest, nil
}

func EnsureSourceStaticCatalog(manifestPath string, manifest *providermanifestv1.Manifest) error {
	if manifest == nil || manifest.Kind != providermanifestv1.KindPlugin {
		return nil
	}
	rootDir := filepath.Dir(manifestPath)
	catalogPath := StaticCatalogPath(rootDir)
	absoluteCatalogPath, err := filepath.Abs(catalogPath)
	if err != nil {
		return fmt.Errorf("resolve static catalog path %q: %w", catalogPath, err)
	}
	if err := generateSourceStaticCatalog(rootDir, absoluteCatalogPath); err != nil {
		return err
	}
	if _, err := os.Stat(absoluteCatalogPath); err != nil {
		if os.IsNotExist(err) && !StaticCatalogRequired(manifest) {
			return nil
		}
		return fmt.Errorf("provider static catalog %q not found: %w", StaticCatalogFile, err)
	}
	return nil
}

func generateSourceStaticCatalog(rootDir, catalogPath string) error {
	command, args, cleanup, err := SourceProviderExecutionCommand(rootDir, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		if errors.Is(err, ErrNoSourceProviderPackage) {
			return nil
		}
		return fmt.Errorf("prepare synthesized source provider for static catalog: %w", err)
	}
	if cleanup != nil {
		defer cleanup()
	}

	cmd := exec.Command(command, args...)
	cmd.Env = append(
		os.Environ(),
		envWriteCatalog+"="+catalogPath,
	)
	execEnv, err := SourceProviderExecutionEnv(rootDir, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return fmt.Errorf("prepare synthesized source provider environment for static catalog: %w", err)
	}
	for key, value := range execEnv {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		msg := bytes.TrimSpace(output.Bytes())
		if len(msg) == 0 {
			return fmt.Errorf("generate static catalog: %w", err)
		}
		return fmt.Errorf("generate static catalog: %w\n%s", err, msg)
	}
	return nil
}
