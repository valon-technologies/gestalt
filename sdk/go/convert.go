package gestalt

import (
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

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

func catalogToJSON(cat *Catalog) (string, error) {
	if cat == nil {
		return "", nil
	}

	type opWithTransport struct {
		CatalogOperation
		Transport string `json:"transport,omitempty"`
	}

	type wireCatalog struct {
		Name        string            `json:"name"`
		DisplayName string            `json:"displayName"`
		Description string            `json:"description"`
		IconSVG     string            `json:"iconSvg,omitempty"`
		Operations  []opWithTransport `json:"operations"`
	}

	ops := make([]opWithTransport, len(cat.Operations))
	for i, op := range cat.Operations {
		ops[i] = opWithTransport{CatalogOperation: op, Transport: "plugin"}
	}

	data, err := json.Marshal(wireCatalog{
		Name:        cat.Name,
		DisplayName: cat.DisplayName,
		Description: cat.Description,
		IconSVG:     cat.IconSVG,
		Operations:  ops,
	})
	if err != nil {
		return "", fmt.Errorf("marshal catalog: %w", err)
	}
	return string(data), nil
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
