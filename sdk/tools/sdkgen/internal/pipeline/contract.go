package pipeline

import (
	"fmt"
	"path/filepath"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
)

const contractRelPath = "sdk/testdata/public_conformance/surface_v1.json"

func contractFileSet(schema *model.Schema) (*fileset.FileSet, error) {
	contract, err := publicsurface.BuildContract(schema)
	if err != nil {
		return nil, err
	}
	data, err := publicsurface.MarshalContract(contract)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	set := fileset.New()
	if err := set.Add("surface_v1.json", data); err != nil {
		return nil, err
	}
	return set, nil
}

func reconcileContract(repoRoot string, schema *model.Schema) (fileset.Report, error) {
	set, err := contractFileSet(schema)
	if err != nil {
		return fileset.Report{}, err
	}
	root := filepath.Join(repoRoot, "sdk/testdata/public_conformance")
	return fileset.Reconcile(root, set, fileset.JSON, contractStaleScope)
}

func checkContract(repoRoot string, schema *model.Schema) (fileset.Drift, error) {
	set, err := contractFileSet(schema)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(repoRoot, "sdk/testdata/public_conformance")
	return fileset.Check(root, set, fileset.JSON, contractStaleScope)
}

func contractStaleScope(rel string) bool {
	return rel == "surface_v1.json"
}

func appendContractDrift(repoRoot string, schema *model.Schema, drift fileset.Drift) (fileset.Drift, error) {
	d, err := checkContract(repoRoot, schema)
	if err != nil {
		return nil, err
	}
	for _, entry := range d {
		drift = append(drift, fileset.DriftEntry{Kind: entry.Kind, Path: contractRelPath})
	}
	return drift, nil
}

func reconcileContractReport(repoRoot string, schema *model.Schema) error {
	_, err := reconcileContract(repoRoot, schema)
	if err != nil {
		return fmt.Errorf("reconcile contract: %w", err)
	}
	return nil
}
