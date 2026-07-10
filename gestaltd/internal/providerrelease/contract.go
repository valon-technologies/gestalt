package providerrelease

import (
	"bytes"
	"fmt"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

// Requires and Compatibility mirror app registry entry contract fields. They are
// snapshotted into provider-release.yaml at release finalization time.
type Requires struct {
	Apps map[string]AppRequirement `yaml:"apps,omitempty" json:"apps,omitempty"`
}

type AppRequirement struct {
	Version    string                          `yaml:"version,omitempty" json:"version,omitempty"`
	Operations map[string]OperationRequirement `yaml:"operations,omitempty" json:"operations,omitempty"`
}

type OperationRequirement struct {
	InputSchemaHash string `yaml:"inputSchemaHash,omitempty" json:"inputSchemaHash,omitempty"`
}

type Compatibility struct {
	MinGestaltdVersion string `yaml:"minGestaltdVersion,omitempty" json:"minGestaltdVersion,omitempty"`
}

// ParseContract prefers contract fields decoded on the release archive manifest struct
// and falls back to parsing raw manifest bytes for older packages that only embed
// contract metadata in the archive file.
func ParseContract(manifest *providermanifestv1.Manifest, raw []byte) (Requires, Compatibility, error) {
	requires, compatibility := ContractFromManifest(manifest)
	if len(requires.Apps) > 0 || compatibility.MinGestaltdVersion != "" {
		return requires, compatibility, nil
	}
	return ParseContractFromManifestRaw(raw)
}

func ContractFromManifest(manifest *providermanifestv1.Manifest) (Requires, Compatibility) {
	if manifest == nil {
		return Requires{}, Compatibility{}
	}
	var requires Requires
	if manifest.Dependencies != nil && len(manifest.Dependencies.Apps) > 0 {
		requires.Apps = make(map[string]AppRequirement, len(manifest.Dependencies.Apps))
		for name, dep := range manifest.Dependencies.Apps {
			req := AppRequirement{Version: dep.Version}
			if len(dep.Operations) > 0 {
				req.Operations = make(map[string]OperationRequirement, len(dep.Operations))
				for opName, opDep := range dep.Operations {
					req.Operations[opName] = OperationRequirement{InputSchemaHash: opDep.InputSchemaHash}
				}
			}
			requires.Apps[name] = req
		}
	}
	var compatibility Compatibility
	if manifest.Compatibility != nil {
		compatibility.MinGestaltdVersion = manifest.Compatibility.MinGestaltdVersion
	}
	return requires, compatibility
}

func ParseContractFromManifestRaw(raw []byte) (Requires, Compatibility, error) {
	if len(raw) == 0 {
		return Requires{}, Compatibility{}, nil
	}
	var doc struct {
		Dependencies struct {
			Apps map[string]AppRequirement `json:"apps" yaml:"apps"`
		} `json:"dependencies" yaml:"dependencies"`
		Compatibility Compatibility `json:"compatibility" yaml:"compatibility"`
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(false)
	if err := dec.Decode(&doc); err != nil {
		return Requires{}, Compatibility{}, fmt.Errorf("decode manifest contract: %w", err)
	}
	requires := Requires{}
	if len(doc.Dependencies.Apps) > 0 {
		requires.Apps = doc.Dependencies.Apps
	}
	return requires, doc.Compatibility, nil
}
