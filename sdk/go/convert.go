package gestalt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
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

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func timeToProto(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value)
}

func timePtrToProto(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return timestamppb.New(*value)
}

func protoToTime(value *timestamppb.Timestamp) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.AsTime()
}

func protoToTimePtr(value *timestamppb.Timestamp) *time.Time {
	if value == nil {
		return nil
	}
	t := value.AsTime()
	return &t
}
