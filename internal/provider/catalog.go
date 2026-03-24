package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func LoadFile(path string) (*Definition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(path, ".json") {
		return unmarshalJSON(data, path)
	}
	return unmarshalYAML(data, path)
}

func LoadFromDir(name string, dirs []string) (*Definition, error) {
	for _, dir := range dirs {
		for _, ext := range []string{".yaml", ".yml", ".json"} {
			path := filepath.Join(dir, name+ext)
			def, err := LoadFile(path)
			if err == nil {
				return def, nil
			}
			if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
		}
	}
	return nil, fmt.Errorf("no provider found for %q in provider_dirs (set openapi or provider in config)", name)
}

func unmarshalJSON(data []byte, source string) (*Definition, error) {
	var def Definition
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", source, err)
	}
	if def.Provider == "" {
		return nil, fmt.Errorf("%s: missing provider field", source)
	}
	return &def, nil
}

func unmarshalYAML(data []byte, source string) (*Definition, error) {
	var def Definition
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", source, err)
	}
	if def.Provider == "" {
		return nil, fmt.Errorf("%s: missing provider field", source)
	}
	return &def, nil
}
