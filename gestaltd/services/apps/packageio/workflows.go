package packageio

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const StaticWorkflowsFile = "workflows.yaml"

type StaticWorkflowDefinitions struct {
	Definitions []StaticWorkflowDefinition `yaml:"definitions,omitempty"`
}

type StaticWorkflowDefinition struct {
	ID    string                `yaml:"id,omitempty"`
	Steps []StaticWorkflowAppCall `yaml:"steps,omitempty"`
}

type StaticWorkflowAppCall struct {
	App       string `yaml:"app"`
	Operation string `yaml:"operation,omitempty"`
}

func StaticWorkflowsPath(rootDir string) string {
	if rootDir == "" {
		return StaticWorkflowsFile
	}
	return filepath.Join(rootDir, StaticWorkflowsFile)
}

func ReadStaticWorkflows(rootDir string) (*StaticWorkflowDefinitions, error) {
	workflowsPath := StaticWorkflowsPath(rootDir)
	data, err := os.ReadFile(workflowsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read static workflows %q: %w", workflowsPath, err)
	}
	var workflows StaticWorkflowDefinitions
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&workflows); err != nil {
		return nil, fmt.Errorf("decode static workflows %q: %w", filepath.Base(workflowsPath), err)
	}
	if len(workflows.Definitions) == 0 {
		return nil, nil
	}
	return &workflows, nil
}
