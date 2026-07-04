package providerpkg

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

const errMultipleRunCommandsRequireDevMode = "multiple run commands require dev mode support"

func HasMultipleSourceRuns(manifest *providermanifestv1.Manifest) bool {
	if manifest == nil || manifest.Run == nil {
		return false
	}
	return len(manifest.Run.PhaseCommands()) > 1
}

func RejectMultipleSourceRuns(manifest *providermanifestv1.Manifest) error {
	if HasMultipleSourceRuns(manifest) {
		return errors.New(errMultipleRunCommandsRequireDevMode)
	}
	return nil
}

func verifyAppBuildOutputs(manifestPath string, manifest *providermanifestv1.Manifest, outputPath, outputRel, outputKind string, opts SourceBuildOptions) error {
	staticDir := SourceStaticBuildDir(manifestPath)
	staticOK, err := sourceStaticQualifies(staticDir)
	if err != nil {
		return err
	}
	if outputRel != "" {
		if err := verifySourceBuildOutput(outputPath, outputRel, outputKind, opts); err == nil {
			if staticOK {
				return verifySourceStaticBuildOutput(staticDir)
			}
			return nil
		} else if HasExplicitSourceRun(manifest) && !staticOK {
			return fmt.Errorf("build produced neither executable nor static output with index.html")
		} else if !staticOK {
			return err
		}
		return verifySourceStaticBuildOutput(staticDir)
	}
	if staticOK {
		return verifySourceStaticBuildOutput(staticDir)
	}
	return fmt.Errorf("build produced neither executable nor static output with index.html")
}

func stagePreparedStaticBundle(manifestPath, stagingDir string, manifest *providermanifestv1.Manifest) error {
	staticDir := SourceStaticBuildDir(manifestPath)
	ok, err := sourceStaticQualifies(staticDir)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	dstDir := filepath.Join(stagingDir, "static")
	if err := os.RemoveAll(dstDir); err != nil {
		return fmt.Errorf("remove staged static dir: %w", err)
	}
	if err := copyPreparedInstallDir(staticDir, dstDir); err != nil {
		return fmt.Errorf("stage static bundle: %w", err)
	}
	if manifest.Spec == nil {
		manifest.Spec = &providermanifestv1.Spec{}
	}
	manifest.Spec.AssetRoot = "static"
	return nil
}
