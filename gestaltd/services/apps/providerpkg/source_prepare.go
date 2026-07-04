package providerpkg

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

const envWriteCatalog = "GESTALT_APP_WRITE_CATALOG"

type sourceCatalogOptions struct {
	SkipExplicitRun bool
	RefreshExisting bool
}

func PrepareSourceManifest(manifestPath string) ([]byte, *providermanifestv1.Manifest, error) {
	return prepareSourceManifest(manifestPath, false, SourceBuildOptions{})
}

func PrepareSourceManifestForExecution(manifestPath string, opts SourceBuildOptions) ([]byte, *providermanifestv1.Manifest, error) {
	return prepareSourceManifest(manifestPath, false, opts)
}

func prepareSourceManifestForPreparedInstallWithOptions(manifestPath string, opts SourceBuildOptions) ([]byte, *providermanifestv1.Manifest, error) {
	return prepareSourceManifest(manifestPath, true, opts)
}

// Local prepare may create catalog.yaml from run; packaging must regenerate it
// from the packaged entrypoint instead. Manifest-backed apps keep checked-in
// catalog.yaml because generateSourceStaticCatalog does not run for them.
func explicitRunStaleCatalog(manifest *providermanifestv1.Manifest) bool {
	if !HasExplicitSourceRun(manifest) {
		return false
	}
	if manifest != nil && manifest.Spec != nil && manifest.Spec.IsManifestBacked() {
		return false
	}
	return true
}

func prepareSourceManifest(manifestPath string, packaging bool, buildOpts SourceBuildOptions) ([]byte, *providermanifestv1.Manifest, error) {
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
	if !packaging {
		if err := RunSourceInstall(manifestPath, manifest, buildOpts); err != nil {
			return nil, nil, err
		}
	}
	if err := ensurePrepareOnlyBuild(manifestPath, manifest, buildOpts); err != nil {
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
	if manifest != nil && manifest.Spec != nil && manifest.Spec.IsManifestBacked() {
		return nil
	}
	var resolved ResolvedSourceExecution
	if HasMultipleSourceRuns(manifest) {
		resolved = explicitRunExecution(rootDir, manifest)
	} else {
		var err error
		resolved, err = resolveSourceExecution(manifestPath, manifest, providermanifestv1.KindApp, SourceBuildOptions{}, opts.SkipExplicitRun)
		if err != nil {
			return fmt.Errorf("prepare source provider for static catalog: %w", err)
		}
	}
	if opts.SkipExplicitRun && resolved.Intent == SourceExecutionIntentPackagedEntrypoint {
		if _, err := os.Stat(resolved.Command); err != nil && os.IsNotExist(err) && HasExplicitSourceRun(manifest) {
			resolved = explicitRunExecution(rootDir, manifest)
		}
	}
	execution := resolved.SourceExecution
	if execution.Cleanup != nil {
		defer execution.Cleanup()
	}

	cmd := exec.Command(execution.Command, execution.Args...)
	cmd.Dir = execution.Workdir
	cmd.Env = mergePhaseEnv(os.Environ(), envMapToSlice(execution.Env))
	cmd.Env = append(cmd.Env, envWriteCatalog+"="+catalogPath)
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
