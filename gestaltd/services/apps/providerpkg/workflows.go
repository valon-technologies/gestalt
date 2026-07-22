package providerpkg

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/valon-technologies/gestalt/server/services/apps/packageio"
)

const (
	StaticWorkflowsFile = packageio.StaticWorkflowsFile
	envWriteWorkflows   = "GESTALT_APP_WRITE_WORKFLOWS"
)

func StaticWorkflowsPath(rootDir string) string {
	return packageio.StaticWorkflowsPath(rootDir)
}

func ReadStaticWorkflows(rootDir string) (*packageio.StaticWorkflowDefinitions, error) {
	return packageio.ReadStaticWorkflows(rootDir)
}

func removeStaticWorkflows(rootDir string) error {
	workflowsPath := StaticWorkflowsPath(rootDir)
	if err := os.Remove(workflowsPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove static workflows %q: %w", StaticWorkflowsFile, err)
	}
	return nil
}

func staticWorkflowsPathForManifest(manifestPath string) string {
	return StaticWorkflowsPath(filepath.Dir(manifestPath))
}
