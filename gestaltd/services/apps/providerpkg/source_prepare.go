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

type sourceCatalogOptions struct {
	SkipExplicitRun bool
	RefreshExisting bool
}

func PrepareSourceManifest(manifestPath string) ([]byte, *providermanifestv1.Manifest, error) {
	return prepareSourceManifest(manifestPath, false)
}

func prepareSourceManifestForPreparedInstall(manifestPath string) ([]byte, *providermanifestv1.Manifest, error) {
	return prepareSourceManifest(manifestPath, true)
}

// Local prepare may create catalog.yaml from run; packaging must regenerate it
// from the packaged entrypoint instead.
func explicitRunStaleCatalog(manifest *providermanifestv1.Manifest) bool {
	return HasExplicitSourceRun(manifest)
}

func prepareSourceManifest(manifestPath string, packaging bool) ([]byte, *providermanifestv1.Manifest, error) {
	absoluteManifestPath, err := filepath.Abs(manifestPath)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve manifest path: %w", err)
	}
	manifestPath = absoluteManifestPath

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
	if err := ensurePrepareOnlyBuild(manifestPath, manifest, SourceBuildOptions{}); err != nil {
		return nil, nil, err
	}
	catalogOptions := sourceCatalogOptions{SkipExplicitRun: packaging}
	if packaging {
		catalogOptions.RefreshExisting = explicitRunStaleCatalog(manifest)
	}
	if err := ensureSourceStaticCatalog(manifestPath, manifest, catalogOptions); err != nil {
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
	return ensureSourceStaticCatalog(manifestPath, manifest, sourceCatalogOptions{})
}

func ensureSourceStaticCatalog(manifestPath string, manifest *providermanifestv1.Manifest, opts sourceCatalogOptions) error {
	if manifest == nil || manifest.Kind != providermanifestv1.KindApp {
		return nil
	}
	rootDir := filepath.Dir(manifestPath)
	catalogPath := StaticCatalogPath(rootDir)
	absoluteCatalogPath, err := filepath.Abs(catalogPath)
	if err != nil {
		return fmt.Errorf("resolve static catalog path %q: %w", catalogPath, err)
	}
	if _, err := os.Stat(absoluteCatalogPath); err == nil && !opts.RefreshExisting {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat static catalog %q: %w", StaticCatalogFile, err)
	}
	if opts.RefreshExisting {
		if err := os.Remove(absoluteCatalogPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove static catalog %q: %w", StaticCatalogFile, err)
		}
	}
	if err := generateSourceStaticCatalog(manifestPath, rootDir, manifest, absoluteCatalogPath, opts); err != nil {
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

func generateSourceStaticCatalog(manifestPath, rootDir string, manifest *providermanifestv1.Manifest, catalogPath string, opts sourceCatalogOptions) error {
	var execEnv map[string]string
	resolved, err := resolveSourceExecution(manifestPath, manifest, providermanifestv1.KindApp, SourceBuildOptions{}, opts.SkipExplicitRun)
	if err != nil {
		if errors.Is(err, ErrNoSourceProviderPackage) {
			return nil
		}
		return fmt.Errorf("prepare synthesized source provider for static catalog: %w", err)
	}
	if resolved.Intent == SourceExecutionIntentSynthesizedSDK {
		execEnv, err = SourceProviderExecutionEnv(rootDir, runtime.GOOS, runtime.GOARCH)
		if err != nil {
			return fmt.Errorf("prepare synthesized source provider environment for static catalog: %w", err)
		}
	}
	execution := resolved.SourceExecution
	if execution.Cleanup != nil {
		defer execution.Cleanup()
	}

	cmd := exec.Command(execution.Command, execution.Args...)
	cmd.Dir = execution.Workdir
	cmd.Env = append(
		os.Environ(),
		envWriteCatalog+"="+catalogPath,
	)
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
