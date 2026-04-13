package providerhost

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core/catalog"

	"google.golang.org/protobuf/types/known/structpb"
)

func structFromMap(values map[string]any) (*structpb.Struct, error) {
	if len(values) == 0 {
		return nil, nil
	}
	normalized, err := normalizeStructMap(values)
	if err != nil {
		return nil, err
	}
	return structpb.NewStruct(normalized)
}

func mapFromStruct(s *structpb.Struct) map[string]any {
	if s == nil {
		return nil
	}
	return s.AsMap()
}

func catalogFromProto(src *proto.Catalog) (*catalog.Catalog, error) {
	if src == nil {
		return nil, nil
	}
	cat := &catalog.Catalog{
		Name:        src.GetName(),
		DisplayName: src.GetDisplayName(),
		Description: src.GetDescription(),
		IconSVG:     src.GetIconSvg(),
		Operations:  make([]catalog.CatalogOperation, 0, len(src.GetOperations())),
	}
	for _, op := range src.GetOperations() {
		catOp := catalog.CatalogOperation{
			ID:             op.GetId(),
			Method:         op.GetMethod(),
			Title:          op.GetTitle(),
			Description:    op.GetDescription(),
			InputSchema:    jsonRawFromString(op.GetInputSchema()),
			OutputSchema:   jsonRawFromString(op.GetOutputSchema()),
			RequiredScopes: op.GetRequiredScopes(),
			Tags:           op.GetTags(),
			ReadOnly:       op.GetReadOnly(),
			Visible:        op.Visible,
			Transport:      op.GetTransport(),
		}
		if ann := op.GetAnnotations(); ann != nil {
			catOp.Annotations = catalog.OperationAnnotations{
				ReadOnlyHint:    ann.ReadOnlyHint,
				IdempotentHint:  ann.IdempotentHint,
				DestructiveHint: ann.DestructiveHint,
				OpenWorldHint:   ann.OpenWorldHint,
			}
		}
		for _, p := range op.GetParameters() {
			catOp.Parameters = append(catOp.Parameters, catalog.CatalogParameter{
				Name:        p.GetName(),
				Type:        p.GetType(),
				Description: p.GetDescription(),
				Required:    p.GetRequired(),
				Default:     protoValueToAny(p.GetDefault()),
			})
		}
		cat.Operations = append(cat.Operations, catOp)
	}
	if err := cat.Validate(); err != nil {
		return nil, err
	}
	return cat, nil
}

func catalogToProto(cat *catalog.Catalog) *proto.Catalog {
	if cat == nil {
		return nil
	}
	out := &proto.Catalog{
		Name:        cat.Name,
		DisplayName: cat.DisplayName,
		Description: cat.Description,
		IconSvg:     cat.IconSVG,
		Operations:  make([]*proto.CatalogOperation, 0, len(cat.Operations)),
	}
	for i := range cat.Operations {
		op := &cat.Operations[i]
		pOp := &proto.CatalogOperation{
			Id:             op.ID,
			Method:         op.Method,
			Title:          op.Title,
			Description:    op.Description,
			InputSchema:    string(op.InputSchema),
			OutputSchema:   string(op.OutputSchema),
			RequiredScopes: op.RequiredScopes,
			Tags:           op.Tags,
			ReadOnly:       op.ReadOnly,
			Visible:        op.Visible,
			Transport:      op.Transport,
		}
		ann := op.Annotations
		if ann.ReadOnlyHint != nil || ann.IdempotentHint != nil || ann.DestructiveHint != nil || ann.OpenWorldHint != nil {
			pOp.Annotations = &proto.OperationAnnotations{
				ReadOnlyHint:    ann.ReadOnlyHint,
				IdempotentHint:  ann.IdempotentHint,
				DestructiveHint: ann.DestructiveHint,
				OpenWorldHint:   ann.OpenWorldHint,
			}
		}
		for _, p := range op.Parameters {
			param := &proto.CatalogParameter{
				Name:        p.Name,
				Type:        p.Type,
				Description: p.Description,
				Required:    p.Required,
			}
			if p.Default != nil {
				if v, err := structpb.NewValue(p.Default); err == nil {
					param.Default = v
				}
			}
			pOp.Parameters = append(pOp.Parameters, param)
		}
		out.Operations = append(out.Operations, pOp)
	}
	return out
}

func jsonRawFromString(s string) json.RawMessage {
	if s == "" {
		return nil
	}
	return json.RawMessage(s)
}

func protoValueToAny(v *structpb.Value) any {
	if v == nil {
		return nil
	}
	return v.AsInterface()
}

func normalizeStructMap(values map[string]any) (map[string]any, error) {
	normalized := make(map[string]any, len(values))
	for key, value := range values {
		out, err := normalizeStructValue(value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		normalized[key] = out
	}
	return normalized, nil
}

func normalizeStructValue(value any) (any, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano), nil
	case *time.Time:
		if v == nil {
			return nil, nil
		}
		return v.UTC().Format(time.RFC3339Nano), nil
	case map[string]any:
		return normalizeStructMap(v)
	}

	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		return nil, nil
	}

	switch rv.Kind() {
	case reflect.Map:
		if rv.IsNil() {
			return nil, nil
		}
		if rv.Type().Key().Kind() != reflect.String {
			return value, nil
		}
		normalized := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out, err := normalizeStructValue(iter.Value().Interface())
			if err != nil {
				return nil, fmt.Errorf("%s: %w", iter.Key().String(), err)
			}
			normalized[iter.Key().String()] = out
		}
		return normalized, nil
	case reflect.Slice, reflect.Array:
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			return nil, nil
		}
		normalized := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out, err := normalizeStructValue(rv.Index(i).Interface())
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			normalized[i] = out
		}
		return normalized, nil
	default:
		return value, nil
	}
}
