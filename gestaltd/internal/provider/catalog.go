package provider

import (
	"encoding/json"
	"fmt"
	"os"
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
