package pluginpkg

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

const envWriteCatalog = "GESTALT_PLUGIN_WRITE_CATALOG"

func PrepareSourceManifest(manifestPath string) ([]byte, *pluginmanifestv1.Manifest, error) {
	data, manifest, err := ReadSourceManifestFile(manifestPath)
	if err != nil {
		return nil, nil, err
	}
	if err := EnsureSourceStaticCatalog(manifestPath, manifest); err != nil {
		return nil, nil, err
	}
	return data, manifest, nil
}

func EnsureSourceStaticCatalog(manifestPath string, manifest *pluginmanifestv1.Manifest) error {
	if manifest == nil || manifest.Provider == nil {
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
	cmd.Env = append(os.Environ(), envWriteCatalog+"="+catalogPath)
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
