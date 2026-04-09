package gestalt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

// Request carries execution-scoped metadata into typed handlers.
type Request struct {
	Token            string
	ConnectionParams map[string]string
}

// ConnectionParam returns one resolved connection parameter by name and whether
// it was present in the request.
func (r Request) ConnectionParam(name string) (string, bool) {
	if r.ConnectionParams == nil {
		return "", false
	}
	value, ok := r.ConnectionParams[name]
	return value, ok
}

// Response is the typed handler result marshaled into the provider response body.
// A zero Status defaults to 200.
type Response[T any] struct {
	Status int
	Body   T
}

// OK returns a typed JSON response with status 200.
func OK[T any](body T) Response[T] {
	return Response[T]{Status: http.StatusOK, Body: body}
}

// Operation describes one statically declared executable operation.
// Input and output types are used for typed dispatch and catalog generation.
type Operation[In any, Out any] struct {
	ID          string
	Method      string
	Title       string
	Description string
	Tags        []string
	ReadOnly    bool
	Visible     *bool
}

type Registration[P any] struct {
	catalogOp *proto.CatalogOperation
	execute   func(context.Context, *P, map[string]any, Request) (*OperationResult, error)
	err       error
}

// Register ties a typed operation definition to a typed handler.
func Register[P any, In any, Out any](
	op Operation[In, Out],
	handler func(*P, context.Context, In, Request) (Response[Out], error),
) Registration[P] {
	catOp, err := catalogOperationFor(op)
	if err != nil {
		return Registration[P]{err: err}
	}
	return Registration[P]{
		catalogOp: catOp,
		execute: func(ctx context.Context, provider *P, rawParams map[string]any, req Request) (*OperationResult, error) {
			var input In
			if err := decodeParams(rawParams, &input); err != nil {
				return nil, newOperationError(http.StatusBadRequest, fmt.Sprintf("decode params for %q: %v", op.ID, err), err)
			}

			resp, err := handler(provider, ctx, input, req)
			if err != nil {
				return nil, newOperationError(http.StatusInternalServerError, err.Error(), err)
			}

			status := resp.Status
			if status == 0 {
				status = http.StatusOK
			}
			body, err := json.Marshal(resp.Body)
			if err != nil {
				return nil, newOperationError(http.StatusInternalServerError, fmt.Sprintf("marshal response for %q: %v", op.ID, err), err)
			}
			return &OperationResult{Status: status, Body: string(body)}, nil
		},
	}
}

// Router dispatches provider Execute calls against typed handlers and derives
// the corresponding static executable catalog.
type Router[P any] struct {
	catalog  *proto.Catalog
	handlers map[string]func(context.Context, *P, map[string]any, Request) (*OperationResult, error)
}

// NewRouter constructs a typed router from registrations. Source-provider flows
// derive the router name from plugin.yaml at build time.
func NewRouter[P any](registrations ...Registration[P]) (*Router[P], error) {
	return newRouter("", registrations...)
}

func newRouter[P any](name string, registrations ...Registration[P]) (*Router[P], error) {
	router := &Router[P]{
		catalog: &proto.Catalog{
			Name:       name,
			Operations: make([]*proto.CatalogOperation, 0, len(registrations)),
		},
		handlers: make(map[string]func(context.Context, *P, map[string]any, Request) (*OperationResult, error), len(registrations)),
	}
	for i := range registrations {
		reg := registrations[i]
		if reg.err != nil {
			return nil, reg.err
		}
		opID := reg.catalogOp.GetId()
		if _, exists := router.handlers[opID]; exists {
			return nil, fmt.Errorf("duplicate operation id %q", opID)
		}
		router.handlers[opID] = reg.execute
		router.catalog.Operations = append(router.catalog.Operations, reg.catalogOp)
	}
	return router, nil
}

// MustRouter panics if [NewRouter] returns an error.
func MustRouter[P any](registrations ...Registration[P]) *Router[P] {
	router, err := NewRouter(registrations...)
	if err != nil {
		panic(err)
	}
	return router
}

func (r *Router[P]) Catalog() *proto.Catalog {
	if r == nil {
		return nil
	}
	return cloneCatalog(r.catalog)
}

func (r *Router[P]) WithName(name string) *Router[P] {
	if r == nil {
		return nil
	}
	cat := cloneCatalog(r.catalog)
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		cat.Name = trimmed
	}
	handlers := make(map[string]func(context.Context, *P, map[string]any, Request) (*OperationResult, error), len(r.handlers))
	for opID, handler := range r.handlers {
		handlers[opID] = handler
	}
	return &Router[P]{
		catalog:  cat,
		handlers: handlers,
	}
}

func cloneCatalog(src *proto.Catalog) *proto.Catalog {
	if src == nil {
		return &proto.Catalog{}
	}
	ops := make([]*proto.CatalogOperation, len(src.Operations))
	copy(ops, src.Operations)
	return &proto.Catalog{
		Name:        src.Name,
		DisplayName: src.DisplayName,
		Description: src.Description,
		IconSvg:     src.IconSvg,
		Operations:  ops,
	}
}

// Execute decodes params into the typed input struct and dispatches the named operation.
func (r *Router[P]) Execute(ctx context.Context, provider *P, operation string, params map[string]any, token string) (*OperationResult, error) {
	if r == nil {
		return operationResult(http.StatusInternalServerError, routerNilMessage), nil
	}
	handler, ok := r.handlers[operation]
	if !ok {
		return operationResult(http.StatusNotFound, unknownOperationMessage), nil
	}
	result := protectedOperationResult(operation, func() (*OperationResult, error) {
		return handler(ctx, provider, params, Request{
			Token:            token,
			ConnectionParams: ConnectionParams(ctx),
		})
	})
	if result == nil {
		return operationResult(http.StatusInternalServerError, nilResultMessage), nil
	}
	return result, nil
}

func catalogOperationFor[In any, Out any](op Operation[In, Out]) (*proto.CatalogOperation, error) {
	id := strings.TrimSpace(op.ID)
	if id == "" {
		return nil, fmt.Errorf("operation id is required")
	}
	params, err := catalogParametersFor[In]()
	if err != nil {
		return nil, fmt.Errorf("operation %q: %w", id, err)
	}
	catOp := &proto.CatalogOperation{
		Id:          id,
		Method:      normalizeMethod(op.Method),
		Title:       strings.TrimSpace(op.Title),
		Description: strings.TrimSpace(op.Description),
		Parameters:  params,
		Tags:        append([]string(nil), op.Tags...),
		ReadOnly:    op.ReadOnly,
	}
	if op.Visible != nil {
		catOp.Visible = op.Visible
	}
	return catOp, nil
}

func catalogParametersFor[In any]() ([]*proto.CatalogParameter, error) {
	t := underlyingType(reflect.TypeFor[In]())
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("input type %s must be a struct", t)
	}
	if t.NumField() == 0 {
		return nil, nil
	}

	params := make([]*proto.CatalogParameter, 0, t.NumField())
	for i := range t.NumField() {
		field := t.Field(i)
		if field.Anonymous {
			return nil, fmt.Errorf("field %s: embedded fields are not supported", field.Name)
		}
		if field.PkgPath != "" {
			continue
		}
		name, omitempty, include := jsonField(field)
		if !include {
			continue
		}
		paramType, err := catalogParameterType(field.Type)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", field.Name, err)
		}
		required, err := fieldRequired(field, omitempty)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", field.Name, err)
		}
		param := &proto.CatalogParameter{
			Name:        name,
			Type:        paramType,
			Description: fieldDescription(field),
			Required:    required,
		}
		if def, ok, err := fieldDefault(field); err != nil {
			return nil, fmt.Errorf("field %s: %w", field.Name, err)
		} else if ok {
			v, vErr := structpb.NewValue(def)
			if vErr != nil {
				return nil, fmt.Errorf("field %s: %w", field.Name, vErr)
			}
			param.Default = v
		}
		params = append(params, param)
	}
	return params, nil
}

func decodeParams(raw map[string]any, dst any) error {
	if raw == nil {
		raw = map[string]any{}
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

func normalizeMethod(method string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		return http.MethodPost
	}
	return method
}

func jsonField(field reflect.StructField) (name string, omitempty, include bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", false, false
	}
	if tag == "" {
		return lowerCamel(field.Name), false, true
	}
	parts := strings.Split(tag, ",")
	name = parts[0]
	if name == "" {
		name = lowerCamel(field.Name)
	}
	for _, option := range parts[1:] {
		if option == "omitempty" {
			omitempty = true
		}
	}
	return name, omitempty, true
}

func lowerCamel(name string) string {
	if name == "" {
		return ""
	}
	return strings.ToLower(name[:1]) + name[1:]
}

func fieldDescription(field reflect.StructField) string {
	if desc := strings.TrimSpace(field.Tag.Get("doc")); desc != "" {
		return desc
	}
	return strings.TrimSpace(field.Tag.Get("description"))
}

func fieldRequired(field reflect.StructField, omitempty bool) (bool, error) {
	if tag := strings.TrimSpace(field.Tag.Get("required")); tag != "" {
		required, err := strconv.ParseBool(tag)
		if err != nil {
			return false, fmt.Errorf("parse required tag %q: %w", tag, err)
		}
		return required, nil
	}
	return !omitempty && !isOptionalType(field.Type), nil
}

func fieldDefault(field reflect.StructField) (any, bool, error) {
	tag := strings.TrimSpace(field.Tag.Get("default"))
	if tag == "" {
		return nil, false, nil
	}
	return parseDefaultValue(underlyingType(field.Type), tag)
}

func parseDefaultValue(t reflect.Type, value string) (any, bool, error) {
	switch t.Kind() {
	case reflect.String:
		return value, true, nil
	case reflect.Bool:
		v, err := strconv.ParseBool(value)
		if err != nil {
			return nil, false, fmt.Errorf("parse bool default %q: %w", value, err)
		}
		return v, true, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, false, fmt.Errorf("parse integer default %q: %w", value, err)
		}
		return v, true, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return nil, false, fmt.Errorf("parse unsigned integer default %q: %w", value, err)
		}
		return v, true, nil
	case reflect.Float32, reflect.Float64:
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, false, fmt.Errorf("parse number default %q: %w", value, err)
		}
		return v, true, nil
	default:
		return nil, false, fmt.Errorf("default tags are only supported on scalar fields, got %s", t)
	}
}

func catalogParameterType(t reflect.Type) (string, error) {
	t = underlyingType(t)
	switch t.Kind() {
	case reflect.String:
		return "string", nil
	case reflect.Bool:
		return "boolean", nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer", nil
	case reflect.Float32, reflect.Float64:
		return "number", nil
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return "string", nil
		}
		return "array", nil
	case reflect.Map, reflect.Interface:
		return "object", nil
	case reflect.Struct:
		if t.PkgPath() == "time" && t.Name() == "Time" {
			return "string", nil
		}
		return "object", nil
	default:
		return "", fmt.Errorf("unsupported field type %s", t)
	}
}

func underlyingType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func isOptionalType(t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		return true
	}
	switch t.Kind() {
	case reflect.Interface, reflect.Map, reflect.Slice:
		return true
	default:
		return false
	}
}
