package providerrelease

import (
	"bytes"
	"fmt"

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
