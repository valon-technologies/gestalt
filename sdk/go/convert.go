package gestalt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"gopkg.in/yaml.v3"
)

func writeCatalogYAML(cat *proto.Catalog, path string) error {
	if cat == nil {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove catalog %q: %w", path, err)
		}
		return nil
	}
	jsonData, err := protojson.MarshalOptions{UseProtoNames: true, EmitDefaultValues: false}.Marshal(cat)
	if err != nil {
		return fmt.Errorf("marshal catalog: %w", err)
	}
	var m any
	if err := json.Unmarshal(jsonData, &m); err != nil {
		return fmt.Errorf("unmarshal catalog JSON: %w", err)
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(m); err != nil {
		return fmt.Errorf("encode catalog YAML: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("close YAML encoder: %w", err)
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
