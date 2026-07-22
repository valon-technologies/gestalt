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
	absoluteWorkflowsPath, err := filepath.Abs(StaticWorkflowsPath(rootDir))
	if err != nil {
		return fmt.Errorf("resolve static workflows path %q: %w", StaticWorkflowsFile, err)
	}

	catalogExists, err := staticMetadataFileExists(absoluteCatalogPath)
	if err != nil {
		return err
	}
	workflowsExists, err := staticMetadataFileExists(absoluteWorkflowsPath)
	if err != nil {
		return err
	}

	needCatalog := !catalogExists || opts.RefreshExisting
	needWorkflows := !workflowsExists || opts.RefreshExisting
	if !needCatalog && !needWorkflows {
		return nil
	}

	if opts.RefreshExisting {
		if needCatalog {
			if err := os.Remove(absoluteCatalogPath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove static catalog %q: %w", StaticCatalogFile, err)
			}
		}
		if needWorkflows {
			if err := removeStaticWorkflows(rootDir); err != nil {
				return err
			}
		}
	}

	catalogOut := ""
	if needCatalog {
		catalogOut = absoluteCatalogPath
	}
	workflowsOut := ""
	if needWorkflows {
		workflowsOut = absoluteWorkflowsPath
	}
	if err := generateSourceStaticMetadata(manifestPath, manifest, catalogOut, workflowsOut, opts); err != nil {
		return err
	}
	if needCatalog {
		if _, err := os.Stat(absoluteCatalogPath); err != nil {
			if os.IsNotExist(err) && !StaticCatalogRequired(manifest) {
				return nil
			}
			return fmt.Errorf("provider static catalog %q not found: %w", StaticCatalogFile, err)
		}
	}
	return nil
}

func staticMetadataFileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("stat %q: %w", path, err)
}

func generateSourceStaticMetadata(manifestPath string, manifest *providermanifestv1.Manifest, catalogPath, workflowsPath string, opts sourceCatalogOptions) error {
	if manifest != nil && manifest.Spec != nil && manifest.Spec.IsManifestBacked() {
		return nil
	}
	if catalogPath == "" && workflowsPath == "" {
		return nil
	}
	var resolved ResolvedSourceExecution
	if HasMultipleSourceRuns(manifest) {
		resolved = explicitRunExecution(filepath.Dir(manifestPath), manifest)
	} else {
		var err error
		resolved, err = resolveSourceExecution(manifestPath, manifest, providermanifestv1.KindApp, SourceBuildOptions{}, opts.SkipExplicitRun)
		if err != nil {
			return fmt.Errorf("prepare source provider for static metadata: %w", err)
		}
	}
	if opts.SkipExplicitRun && resolved.Intent == SourceExecutionIntentPackagedEntrypoint {
		if _, err := os.Stat(resolved.Command); err != nil && os.IsNotExist(err) && HasExplicitSourceRun(manifest) {
			resolved = explicitRunExecution(filepath.Dir(manifestPath), manifest)
		}
	}
	execution := resolved.SourceExecution
	if execution.Cleanup != nil {
		defer execution.Cleanup()
	}

	cmd := exec.Command(execution.Command, execution.Args...)
	cmd.Dir = execution.Workdir
	cmd.Env = mergePhaseEnv(os.Environ(), envMapToSlice(execution.Env))
	if catalogPath != "" {
		cmd.Env = append(cmd.Env, envWriteCatalog+"="+catalogPath)
	}
	if workflowsPath != "" {
		cmd.Env = append(cmd.Env, envWriteWorkflows+"="+workflowsPath)
	}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		msg := bytes.TrimSpace(output.Bytes())
		if len(msg) == 0 {
			return fmt.Errorf("generate static provider metadata: %w", err)
		}
		return fmt.Errorf("generate static provider metadata: %w\n%s", err, msg)
	}
	return nil
}
