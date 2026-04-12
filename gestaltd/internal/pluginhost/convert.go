package pluginhost

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"

	"google.golang.org/protobuf/types/known/structpb"
)

const (
	transportTimeLayout     = time.RFC3339Nano
	legacySQLDateTimeLayout = "2006-01-02 15:04:05"
	legacySQLDateTimeMicros = "2006-01-02 15:04:05.999999"
	legacySQLDateTimeNanos  = "2006-01-02 15:04:05.999999999"
	transportBytesPrefix    = "b64:"
)

type valueCodec struct {
	columnType indexeddb.ColumnType
	known      bool
}

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

func structFromRecord(values indexeddb.Record, schema *indexeddb.ObjectStoreSchema) (*structpb.Struct, error) {
	if len(values) == 0 {
		return nil, nil
	}
	normalized, err := normalizeRecordForStruct(values, schema)
	if err != nil {
		return nil, err
	}
	return structpb.NewStruct(normalized)
}

func recordFromStruct(s *structpb.Struct, schema *indexeddb.ObjectStoreSchema) (indexeddb.Record, error) {
	if s == nil {
		return nil, nil
	}
	return decodeRecordFromStruct(s.AsMap(), schema)
}

func protoValuesFromAny(values []any, codecs []valueCodec) ([]*structpb.Value, error) {
	pbValues := make([]*structpb.Value, len(values))
	for i, value := range values {
		codec := codecAt(codecs, i)
		normalized, err := encodeTransportValue(value, codec)
		if err != nil {
			return nil, fmt.Errorf("marshal index value %d: %w", i, err)
		}
		pv, err := structpb.NewValue(normalized)
		if err != nil {
			return nil, fmt.Errorf("marshal index value %d: %w", i, err)
		}
		pbValues[i] = pv
	}
	return pbValues, nil
}

func anyFromProtoValues(values []*structpb.Value, codecs []valueCodec) ([]any, error) {
	out := make([]any, len(values))
	for i, value := range values {
		codec := codecAt(codecs, i)
		decoded, err := decodeTransportValue(value.AsInterface(), codec)
		if err != nil {
			return nil, fmt.Errorf("decode index value %d: %w", i, err)
		}
		out[i] = decoded
	}
	return out, nil
}

func protoKeyRangeFromRange(r *indexeddb.KeyRange, codec valueCodec) (*proto.KeyRange, error) {
	if r == nil {
		return nil, nil
	}
	kr := &proto.KeyRange{
		LowerOpen: r.LowerOpen,
		UpperOpen: r.UpperOpen,
	}
	if r.Lower != nil {
		normalized, err := encodeTransportValue(r.Lower, codec)
		if err != nil {
			return nil, fmt.Errorf("marshal key range lower: %w", err)
		}
		v, err := structpb.NewValue(normalized)
		if err != nil {
			return nil, fmt.Errorf("marshal key range lower: %w", err)
		}
		kr.Lower = v
	}
	if r.Upper != nil {
		normalized, err := encodeTransportValue(r.Upper, codec)
		if err != nil {
			return nil, fmt.Errorf("marshal key range upper: %w", err)
		}
		v, err := structpb.NewValue(normalized)
		if err != nil {
			return nil, fmt.Errorf("marshal key range upper: %w", err)
		}
		kr.Upper = v
	}
	return kr, nil
}

func keyRangeFromProto(kr *proto.KeyRange, codec valueCodec) (*indexeddb.KeyRange, error) {
	if kr == nil {
		return nil, nil
	}
	r := &indexeddb.KeyRange{
		LowerOpen: kr.GetLowerOpen(),
		UpperOpen: kr.GetUpperOpen(),
	}
	if kr.GetLower() != nil {
		decoded, err := decodeTransportValue(kr.GetLower().AsInterface(), codec)
		if err != nil {
			return nil, fmt.Errorf("decode key range lower: %w", err)
		}
		r.Lower = decoded
	}
	if kr.GetUpper() != nil {
		decoded, err := decodeTransportValue(kr.GetUpper().AsInterface(), codec)
		if err != nil {
			return nil, fmt.Errorf("decode key range upper: %w", err)
		}
		r.Upper = decoded
	}
	return r, nil
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

func schemaColumnCodec(schema *indexeddb.ObjectStoreSchema, field string) valueCodec {
	if schema == nil {
		return valueCodec{}
	}
	for _, col := range schema.Columns {
		if col.Name == field {
			return valueCodec{columnType: col.Type, known: true}
		}
	}
	return valueCodec{}
}

func schemaPrimaryKeyCodec(schema *indexeddb.ObjectStoreSchema) valueCodec {
	if schema == nil {
		return valueCodec{}
	}
	for _, col := range schema.Columns {
		if col.PrimaryKey {
			return valueCodec{columnType: col.Type, known: true}
		}
	}
	for _, col := range schema.Columns {
		if col.Name == "id" {
			return valueCodec{columnType: col.Type, known: true}
		}
	}
	return valueCodec{}
}

func schemaIndexCodecs(schema *indexeddb.ObjectStoreSchema, index string) []valueCodec {
	if schema == nil {
		return nil
	}
	for _, idx := range schema.Indexes {
		if idx.Name != index {
			continue
		}
		codecs := make([]valueCodec, len(idx.KeyPath))
		for i, field := range idx.KeyPath {
			codecs[i] = schemaColumnCodec(schema, field)
		}
		return codecs
	}
	return nil
}

func codecAt(codecs []valueCodec, index int) valueCodec {
	if index < len(codecs) {
		return codecs[index]
	}
	return valueCodec{}
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
		return formatTransportTime(v), nil
	case *time.Time:
		if v == nil {
			return nil, nil
		}
		return formatTransportTime(*v), nil
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

func normalizeRecordForStruct(values indexeddb.Record, schema *indexeddb.ObjectStoreSchema) (map[string]any, error) {
	normalized := make(map[string]any, len(values))
	for key, value := range values {
		out, err := encodeTransportValue(value, schemaColumnCodec(schema, key))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		normalized[key] = out
	}
	return normalized, nil
}

func decodeRecordFromStruct(values map[string]any, schema *indexeddb.ObjectStoreSchema) (indexeddb.Record, error) {
	record := make(indexeddb.Record, len(values))
	for key, value := range values {
		out, err := decodeTransportValue(value, schemaColumnCodec(schema, key))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		record[key] = out
	}
	return record, nil
}

func encodeTransportValue(value any, codec valueCodec) (any, error) {
	if !codec.known {
		return normalizeStructValue(value)
	}

	switch codec.columnType {
	case indexeddb.TypeTime:
		return encodeTransportTime(value)
	case indexeddb.TypeBytes:
		return encodeTransportBytes(value)
	case indexeddb.TypeInt:
		return encodeTransportInt(value)
	case indexeddb.TypeFloat:
		return encodeTransportFloat(value)
	case indexeddb.TypeBool:
		return encodeTransportBool(value)
	default:
		return normalizeStructValue(value)
	}
}

func decodeTransportValue(value any, codec valueCodec) (any, error) {
	if !codec.known {
		return value, nil
	}

	switch codec.columnType {
	case indexeddb.TypeTime:
		return decodeTransportTime(value)
	case indexeddb.TypeBytes:
		return decodeTransportBytes(value)
	case indexeddb.TypeInt:
		return decodeTransportInt(value)
	case indexeddb.TypeFloat:
		return decodeTransportFloat(value)
	case indexeddb.TypeBool:
		return decodeTransportBool(value)
	default:
		return value, nil
	}
}

func encodeTransportTime(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	switch v := value.(type) {
	case time.Time:
		return formatTransportTime(v), nil
	case *time.Time:
		if v == nil {
			return nil, nil
		}
		return formatTransportTime(*v), nil
	case string:
		parsed, err := parseTransportTime(v)
		if err != nil {
			return nil, err
		}
		return formatTransportTime(parsed), nil
	default:
		return nil, fmt.Errorf("expected time-compatible value, got %T", value)
	}
}

func decodeTransportTime(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	switch v := value.(type) {
	case time.Time:
		return v, nil
	case *time.Time:
		if v == nil {
			return nil, nil
		}
		return *v, nil
	case string:
		return parseTransportTime(v)
	default:
		return nil, fmt.Errorf("expected time-compatible value, got %T", value)
	}
}

func encodeTransportBytes(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	switch v := value.(type) {
	case []byte:
		return transportBytesPrefix + base64.StdEncoding.EncodeToString(v), nil
	case string:
		return transportBytesPrefix + base64.StdEncoding.EncodeToString([]byte(v)), nil
	default:
		return nil, fmt.Errorf("expected bytes-compatible value, got %T", value)
	}
}

func decodeTransportBytes(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	s, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("expected bytes-compatible value, got %T", value)
	}
	if !strings.HasPrefix(s, transportBytesPrefix) {
		return []byte(s), nil
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, transportBytesPrefix))
	if err != nil {
		return nil, fmt.Errorf("decode bytes: %w", err)
	}
	return decoded, nil
}

func encodeTransportInt(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := int64FromValue(value)
	if err != nil {
		return nil, err
	}
	return strconv.FormatInt(parsed, 10), nil
}

func decodeTransportInt(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	return int64FromValue(value)
}

func encodeTransportFloat(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	return float64FromValue(value)
}

func decodeTransportFloat(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	return float64FromValue(value)
}

func encodeTransportBool(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	return boolFromValue(value)
}

func decodeTransportBool(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	return boolFromValue(value)
}

func formatTransportTime(t time.Time) string {
	return t.UTC().Format(transportTimeLayout)
}

func parseTransportTime(value string) (time.Time, error) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		legacySQLDateTimeNanos,
		legacySQLDateTimeMicros,
		legacySQLDateTimeLayout,
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid time string %q", value)
}

func int64FromValue(value any) (int64, error) {
	switch v := value.(type) {
	case string:
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			return parsed, nil
		}
		parsed, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid integer string %q", v)
		}
		if parsed > math.MaxInt64 {
			return 0, fmt.Errorf("integer %d exceeds int64", parsed)
		}
		return int64(parsed), nil
	}

	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		return 0, fmt.Errorf("invalid integer value")
	}

	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if rv.Uint() > math.MaxInt64 {
			return 0, fmt.Errorf("integer %d exceeds int64", rv.Uint())
		}
		return int64(rv.Uint()), nil
	case reflect.Float32, reflect.Float64:
		f := rv.Float()
		if math.Trunc(f) != f {
			return 0, fmt.Errorf("expected whole number, got %v", f)
		}
		return int64(f), nil
	default:
		return 0, fmt.Errorf("expected integer-compatible value, got %T", value)
	}
}

func float64FromValue(value any) (float64, error) {
	if s, ok := value.(string); ok {
		parsed, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid float string %q", s)
		}
		return parsed, nil
	}

	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		return 0, fmt.Errorf("invalid float value")
	}

	switch rv.Kind() {
	case reflect.Float32, reflect.Float64:
		return rv.Float(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint()), nil
	default:
		return 0, fmt.Errorf("expected float-compatible value, got %T", value)
	}
}

func boolFromValue(value any) (bool, error) {
	if s, ok := value.(string); ok {
		parsed, err := strconv.ParseBool(s)
		if err != nil {
			return false, fmt.Errorf("invalid bool string %q", s)
		}
		return parsed, nil
	}

	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		return false, fmt.Errorf("invalid bool value")
	}

	switch rv.Kind() {
	case reflect.Bool:
		return rv.Bool(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int() != 0, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return rv.Uint() != 0, nil
	case reflect.Float32, reflect.Float64:
		return rv.Float() != 0, nil
	default:
		return false, fmt.Errorf("expected bool-compatible value, got %T", value)
	}
}
