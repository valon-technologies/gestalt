package gestalt

import (
	"encoding/json"
	"fmt"
	"strings"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// Catalog describes the operations an app exposes to Gestalt.
type Catalog struct {
	Name        string              `json:"name,omitempty"`
	DisplayName string              `json:"displayName,omitempty"`
	Description string              `json:"description,omitempty"`
	IconSvg     string              `json:"iconSvg,omitempty"`
	Operations  []*CatalogOperation `json:"operations,omitempty"`
}

// GetName returns the name field; it is safe to call on a nil receiver.
func (c *Catalog) GetName() string {
	if c == nil {
		return ""
	}
	return c.Name
}

// GetDisplayName returns the display name field; it is safe to call on a nil receiver.
func (c *Catalog) GetDisplayName() string {
	if c == nil {
		return ""
	}
	return c.DisplayName
}

// GetDescription returns the description field; it is safe to call on a nil receiver.
func (c *Catalog) GetDescription() string {
	if c == nil {
		return ""
	}
	return c.Description
}

// GetIconSvg returns the icon svg field; it is safe to call on a nil receiver.
func (c *Catalog) GetIconSvg() string {
	if c == nil {
		return ""
	}
	return c.IconSvg
}

// GetOperations returns the operations field; it is safe to call on a nil receiver.
func (c *Catalog) GetOperations() []*CatalogOperation {
	if c == nil {
		return nil
	}
	return c.Operations
}

// CatalogOperation describes one callable operation in an app catalog.
type CatalogOperation struct {
	Id          string `json:"id,omitempty"`
	Method      string `json:"method,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	InputSchema string `json:"inputSchema,omitempty"`
	// OutputSchema is deprecated; use Response. Kept for backward-compatible
	// catalog authoring. When set, it is emitted as a unary response schema.
	OutputSchema   string                 `json:"outputSchema,omitempty"`
	Response       *OperationResponseSpec `json:"response,omitempty"`
	Annotations    *OperationAnnotations  `json:"annotations,omitempty"`
	Parameters     []*CatalogParameter    `json:"parameters,omitempty"`
	RequiredScopes []string               `json:"requiredScopes,omitempty"`
	Tags           []string               `json:"tags,omitempty"`
	ReadOnly       bool                   `json:"readOnly,omitempty"`
	Visible        *bool                  `json:"visible,omitempty"`
	Transport      string                 `json:"transport,omitempty"`
	AllowedRoles   []string               `json:"allowedRoles,omitempty"`
}

// GetId returns the id field; it is safe to call on a nil receiver.
func (o *CatalogOperation) GetId() string {
	if o == nil {
		return ""
	}
	return o.Id
}

// GetMethod returns the method field; it is safe to call on a nil receiver.
func (o *CatalogOperation) GetMethod() string {
	if o == nil {
		return ""
	}
	return o.Method
}

// GetTitle returns the title field; it is safe to call on a nil receiver.
func (o *CatalogOperation) GetTitle() string {
	if o == nil {
		return ""
	}
	return o.Title
}

// GetDescription returns the description field; it is safe to call on a nil receiver.
func (o *CatalogOperation) GetDescription() string {
	if o == nil {
		return ""
	}
	return o.Description
}

// GetInputSchema returns the input schema field; it is safe to call on a nil receiver.
func (o *CatalogOperation) GetInputSchema() string {
	if o == nil {
		return ""
	}
	return o.InputSchema
}

// GetOutputSchema returns the legacy output schema field; it is safe to call on a nil receiver.
func (o *CatalogOperation) GetOutputSchema() string {
	if o == nil {
		return ""
	}
	return o.OutputSchema
}

// GetResponse returns the response spec; it is safe to call on a nil receiver.
func (o *CatalogOperation) GetResponse() *OperationResponseSpec {
	if o == nil {
		return nil
	}
	return o.Response
}

// GetAnnotations returns the annotations field; it is safe to call on a nil receiver.
func (o *CatalogOperation) GetAnnotations() *OperationAnnotations {
	if o == nil {
		return nil
	}
	return o.Annotations
}

// GetParameters returns the parameters field; it is safe to call on a nil receiver.
func (o *CatalogOperation) GetParameters() []*CatalogParameter {
	if o == nil {
		return nil
	}
	return o.Parameters
}

// GetRequiredScopes returns the required scopes field; it is safe to call on a nil receiver.
func (o *CatalogOperation) GetRequiredScopes() []string {
	if o == nil {
		return nil
	}
	return o.RequiredScopes
}

// GetTags returns the tags field; it is safe to call on a nil receiver.
func (o *CatalogOperation) GetTags() []string {
	if o == nil {
		return nil
	}
	return o.Tags
}

// GetReadOnly returns the read only field; it is safe to call on a nil receiver.
func (o *CatalogOperation) GetReadOnly() bool {
	if o == nil {
		return false
	}
	return o.ReadOnly
}

// GetVisible returns the visible field; it is safe to call on a nil receiver.
func (o *CatalogOperation) GetVisible() bool {
	if o == nil || o.Visible == nil {
		return false
	}
	return *o.Visible
}

// GetTransport returns the transport field; it is safe to call on a nil receiver.
func (o *CatalogOperation) GetTransport() string {
	if o == nil {
		return ""
	}
	return o.Transport
}

// GetAllowedRoles returns the allowed roles field; it is safe to call on a nil receiver.
func (o *CatalogOperation) GetAllowedRoles() []string {
	if o == nil {
		return nil
	}
	return o.AllowedRoles
}

// CatalogParameter describes one input parameter in an app catalog operation.
type CatalogParameter struct {
	Name        string `json:"name,omitempty"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Default     any    `json:"default,omitempty"`
	HasDefault  bool   `json:"-"`
}

// GetName returns the name field; it is safe to call on a nil receiver.
func (p *CatalogParameter) GetName() string {
	if p == nil {
		return ""
	}
	return p.Name
}

// GetType returns the type field; it is safe to call on a nil receiver.
func (p *CatalogParameter) GetType() string {
	if p == nil {
		return ""
	}
	return p.Type
}

// GetDescription returns the description field; it is safe to call on a nil receiver.
func (p *CatalogParameter) GetDescription() string {
	if p == nil {
		return ""
	}
	return p.Description
}

// GetRequired returns the required field; it is safe to call on a nil receiver.
func (p *CatalogParameter) GetRequired() bool {
	if p == nil {
		return false
	}
	return p.Required
}

// GetDefault returns the default field; it is safe to call on a nil receiver.
func (p *CatalogParameter) GetDefault() any {
	if p == nil {
		return nil
	}
	return p.Default
}

// GetHasDefault returns the has default field; it is safe to call on a nil receiver.
func (p *CatalogParameter) GetHasDefault() bool {
	if p == nil {
		return false
	}
	return p.HasDefault || p.Default != nil
}

// OperationAnnotations carries optional host hints about operation behavior.
type OperationAnnotations struct {
	ReadOnlyHint    *bool `json:"readOnlyHint,omitempty"`
	IdempotentHint  *bool `json:"idempotentHint,omitempty"`
	DestructiveHint *bool `json:"destructiveHint,omitempty"`
	OpenWorldHint   *bool `json:"openWorldHint,omitempty"`
}

// UnaryResponseSpec describes a unary (fully materialized) operation response.
// Schema is a JSON-encoded schema object (JSON Schema shape).
type UnaryResponseSpec struct {
	Schema string `json:"schema,omitempty"`
}

// StreamResponseSpec describes a streaming operation response. MediaType names
// the representation (for example application/x-ndjson); ItemSchema is an
// optional JSON-encoded schema describing one yielded item.
type StreamResponseSpec struct {
	MediaType  string `json:"mediaType,omitempty"`
	ItemSchema string `json:"itemSchema,omitempty"`
}

// OperationResponseSpec declares how an operation responds. Either Unary or
// Stream is set; both nil means unary with no schema.
type OperationResponseSpec struct {
	Unary  *UnaryResponseSpec  `json:"unary,omitempty"`
	Stream *StreamResponseSpec `json:"stream,omitempty"`
}

// IsStream reports whether this response spec declares a streaming response.
func (r *OperationResponseSpec) IsStream() bool {
	return r != nil && r.Stream != nil
}

// cloneOperationResponseSpec returns a deep copy of spec.
func cloneOperationResponseSpec(spec *OperationResponseSpec) *OperationResponseSpec {
	if spec == nil {
		return nil
	}
	out := &OperationResponseSpec{}
	if spec.Unary != nil {
		out.Unary = &UnaryResponseSpec{Schema: spec.Unary.Schema}
	}
	if spec.Stream != nil {
		out.Stream = &StreamResponseSpec{
			MediaType:  spec.Stream.MediaType,
			ItemSchema: spec.Stream.ItemSchema,
		}
	}
	return out
}

// operationResponseSpecToProto converts the SDK response spec to the proto
// OperationResponseSpec. When spec is nil but legacyOutputSchema is non-empty,
// it is mapped to a unary response with that schema for backward compatibility.
func operationResponseSpecToProto(spec *OperationResponseSpec, legacyOutputSchema string) *proto.OperationResponseSpec {
	if spec != nil {
		if spec.Stream != nil {
			return &proto.OperationResponseSpec{
				Kind: &proto.OperationResponseSpec_Stream{
					Stream: &proto.StreamResponseSpec{
						MediaType:  spec.Stream.MediaType,
						ItemSchema: schemaStringToStruct(spec.Stream.ItemSchema),
					},
				},
			}
		}
		if spec.Unary != nil {
			return &proto.OperationResponseSpec{
				Kind: &proto.OperationResponseSpec_Unary{
					Unary: &proto.UnaryResponseSpec{
						Schema: schemaStringToStruct(spec.Unary.Schema),
					},
				},
			}
		}
	}
	if legacyOutputSchema == "" {
		return nil
	}
	return &proto.OperationResponseSpec{
		Kind: &proto.OperationResponseSpec_Unary{
			Unary: &proto.UnaryResponseSpec{
				Schema: schemaStringToStruct(legacyOutputSchema),
			},
		},
	}
}

// schemaStringToStruct parses a JSON-encoded schema string into a protobuf
// Struct. An empty string or parse error yields nil.
func schemaStringToStruct(schema string) *structpb.Struct {
	schema = strings.TrimSpace(schema)
	if schema == "" {
		return nil
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(schema), &v); err != nil {
		return nil
	}
	st, err := structpb.NewStruct(v)
	if err != nil {
		return nil
	}
	return st
}

// GetReadOnlyHint returns the read only hint field; it is safe to call on a nil receiver.
func (a *OperationAnnotations) GetReadOnlyHint() bool {
	if a == nil || a.ReadOnlyHint == nil {
		return false
	}
	return *a.ReadOnlyHint
}

// GetIdempotentHint returns the idempotent hint field; it is safe to call on a nil receiver.
func (a *OperationAnnotations) GetIdempotentHint() bool {
	if a == nil || a.IdempotentHint == nil {
		return false
	}
	return *a.IdempotentHint
}

// GetDestructiveHint returns the destructive hint field; it is safe to call on a nil receiver.
func (a *OperationAnnotations) GetDestructiveHint() bool {
	if a == nil || a.DestructiveHint == nil {
		return false
	}
	return *a.DestructiveHint
}

// GetOpenWorldHint returns the open world hint field; it is safe to call on a nil receiver.
func (a *OperationAnnotations) GetOpenWorldHint() bool {
	if a == nil || a.OpenWorldHint == nil {
		return false
	}
	return *a.OpenWorldHint
}

func cloneCatalog(src *Catalog) *Catalog {
	if src == nil {
		return &Catalog{}
	}
	out := &Catalog{
		Name:        src.Name,
		DisplayName: src.DisplayName,
		Description: src.Description,
		IconSvg:     src.IconSvg,
		Operations:  make([]*CatalogOperation, 0, len(src.Operations)),
	}
	for _, op := range src.Operations {
		out.Operations = append(out.Operations, cloneCatalogOperation(op))
	}
	return out
}

func cloneCatalogOperation(src *CatalogOperation) *CatalogOperation {
	if src == nil {
		return nil
	}
	out := &CatalogOperation{
		Id:             src.Id,
		Method:         src.Method,
		Title:          src.Title,
		Description:    src.Description,
		InputSchema:    src.InputSchema,
		OutputSchema:   src.OutputSchema,
		Response:       cloneOperationResponseSpec(src.Response),
		Annotations:    cloneOperationAnnotations(src.Annotations),
		Parameters:     make([]*CatalogParameter, 0, len(src.Parameters)),
		RequiredScopes: append([]string(nil), src.RequiredScopes...),
		Tags:           append([]string(nil), src.Tags...),
		ReadOnly:       src.ReadOnly,
		Transport:      src.Transport,
		AllowedRoles:   append([]string(nil), src.AllowedRoles...),
	}
	if src.Visible != nil {
		visible := *src.Visible
		out.Visible = &visible
	}
	for _, param := range src.Parameters {
		out.Parameters = append(out.Parameters, cloneCatalogParameter(param))
	}
	return out
}

func cloneCatalogParameter(src *CatalogParameter) *CatalogParameter {
	if src == nil {
		return nil
	}
	return &CatalogParameter{
		Name:        src.Name,
		Type:        src.Type,
		Description: src.Description,
		Required:    src.Required,
		Default:     cloneCatalogDefault(src.Default),
		HasDefault:  src.HasDefault,
	}
}

func cloneOperationAnnotations(src *OperationAnnotations) *OperationAnnotations {
	if src == nil {
		return nil
	}
	return &OperationAnnotations{
		ReadOnlyHint:    cloneBool(src.ReadOnlyHint),
		IdempotentHint:  cloneBool(src.IdempotentHint),
		DestructiveHint: cloneBool(src.DestructiveHint),
		OpenWorldHint:   cloneBool(src.OpenWorldHint),
	}
}

func cloneBool(src *bool) *bool {
	if src == nil {
		return nil
	}
	v := *src
	return &v
}

func cloneCatalogDefault(value any) any {
	if value == nil {
		return nil
	}
	switch pb := value.(type) {
	case *structpb.Value:
		return cloneProtoValue(pb)
	case structpb.Value:
		return *cloneProtoValue(&pb)
	}
	return value
}

func catalogToProto(cat *Catalog) (*proto.Catalog, error) {
	if cat == nil {
		return nil, nil
	}
	out := &proto.Catalog{
		Name:        cat.Name,
		DisplayName: cat.DisplayName,
		Description: cat.Description,
		IconSvg:     cat.IconSvg,
		Operations:  make([]*proto.CatalogOperation, 0, len(cat.Operations)),
	}
	for i, op := range cat.Operations {
		pbOp, err := catalogOperationToProto(op)
		if err != nil {
			return nil, fmt.Errorf("operation %d: %w", i, err)
		}
		out.Operations = append(out.Operations, pbOp)
	}
	return out, nil
}

func catalogOperationToProto(op *CatalogOperation) (*proto.CatalogOperation, error) {
	if op == nil {
		return nil, nil
	}
	out := &proto.CatalogOperation{
		Id:             op.Id,
		Method:         op.Method,
		Title:          op.Title,
		Description:    op.Description,
		InputSchema:    op.InputSchema,
		Response:       operationResponseSpecToProto(op.Response, op.OutputSchema),
		Annotations:    operationAnnotationsToProto(op.Annotations),
		Parameters:     make([]*proto.CatalogParameter, 0, len(op.Parameters)),
		RequiredScopes: append([]string(nil), op.RequiredScopes...),
		Tags:           append([]string(nil), op.Tags...),
		ReadOnly:       op.ReadOnly,
		Visible:        cloneBool(op.Visible),
		Transport:      op.Transport,
		AllowedRoles:   append([]string(nil), op.AllowedRoles...),
	}
	for i, param := range op.Parameters {
		pbParam, err := catalogParameterToProto(param)
		if err != nil {
			return nil, fmt.Errorf("parameter %d: %w", i, err)
		}
		out.Parameters = append(out.Parameters, pbParam)
	}
	return out, nil
}

func catalogParameterToProto(param *CatalogParameter) (*proto.CatalogParameter, error) {
	if param == nil {
		return nil, nil
	}
	out := &proto.CatalogParameter{
		Name:        param.Name,
		Type:        param.Type,
		Description: param.Description,
		Required:    param.Required,
	}
	if param.GetHasDefault() {
		value, err := catalogDefaultToProto(param.Default)
		if err != nil {
			return nil, fmt.Errorf("default: %w", err)
		}
		out.Default = value
	}
	return out, nil
}

func operationAnnotationsToProto(annotations *OperationAnnotations) *proto.OperationAnnotations {
	if annotations == nil {
		return nil
	}
	return &proto.OperationAnnotations{
		ReadOnlyHint:    cloneBool(annotations.ReadOnlyHint),
		IdempotentHint:  cloneBool(annotations.IdempotentHint),
		DestructiveHint: cloneBool(annotations.DestructiveHint),
		OpenWorldHint:   cloneBool(annotations.OpenWorldHint),
	}
}

func catalogDefaultToProto(value any) (*structpb.Value, error) {
	switch pb := value.(type) {
	case *structpb.Value:
		return cloneProtoValue(pb), nil
	case structpb.Value:
		return cloneProtoValue(&pb), nil
	}
	return structpb.NewValue(value)
}

func cloneProtoValue(value *structpb.Value) *structpb.Value {
	if value == nil {
		return nil
	}
	return gproto.Clone(value).(*structpb.Value)
}
