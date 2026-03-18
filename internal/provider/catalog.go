package provider

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func LoadFile(path string) (*Definition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return unmarshalDefinition(data, path)
}

func LoadFromDir(name string, dirs []string) (*Definition, error) {
	for _, dir := range dirs {
		path := filepath.Join(dir, name+".yaml")
		def, err := LoadFile(path)
		if err == nil {
			return def, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("no provider found for %q in provider_dirs (set openapi or provider in config)", name)
}

func unmarshalDefinition(data []byte, source string) (*Definition, error) {
	var def Definition
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", source, err)
	}
	if def.Provider == "" {
		return nil, fmt.Errorf("%s: missing provider field", source)
	}
	return &def, nil
}
