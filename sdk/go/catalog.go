package gestalt

import (
	"fmt"

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

func (c *Catalog) GetName() string {
	if c == nil {
		return ""
	}
	return c.Name
}

func (c *Catalog) GetDisplayName() string {
	if c == nil {
		return ""
	}
	return c.DisplayName
}

func (c *Catalog) GetDescription() string {
	if c == nil {
		return ""
	}
	return c.Description
}

func (c *Catalog) GetIconSvg() string {
	if c == nil {
		return ""
	}
	return c.IconSvg
}

func (c *Catalog) GetOperations() []*CatalogOperation {
	if c == nil {
		return nil
	}
	return c.Operations
}

// CatalogOperation describes one callable operation in an app catalog.
type CatalogOperation struct {
	Id             string                `json:"id,omitempty"`
	Method         string                `json:"method,omitempty"`
	Title          string                `json:"title,omitempty"`
	Description    string                `json:"description,omitempty"`
	InputSchema    string                `json:"inputSchema,omitempty"`
	OutputSchema   string                `json:"outputSchema,omitempty"`
	Annotations    *OperationAnnotations `json:"annotations,omitempty"`
	Parameters     []*CatalogParameter   `json:"parameters,omitempty"`
	RequiredScopes []string              `json:"requiredScopes,omitempty"`
	Tags           []string              `json:"tags,omitempty"`
	ReadOnly       bool                  `json:"readOnly,omitempty"`
	Visible        *bool                 `json:"visible,omitempty"`
	Transport      string                `json:"transport,omitempty"`
	AllowedRoles   []string              `json:"allowedRoles,omitempty"`
}

func (o *CatalogOperation) GetId() string {
	if o == nil {
		return ""
	}
	return o.Id
}

func (o *CatalogOperation) GetMethod() string {
	if o == nil {
		return ""
	}
	return o.Method
}

func (o *CatalogOperation) GetTitle() string {
	if o == nil {
		return ""
	}
	return o.Title
}

func (o *CatalogOperation) GetDescription() string {
	if o == nil {
		return ""
	}
	return o.Description
}

func (o *CatalogOperation) GetInputSchema() string {
	if o == nil {
		return ""
	}
	return o.InputSchema
}

func (o *CatalogOperation) GetOutputSchema() string {
	if o == nil {
		return ""
	}
	return o.OutputSchema
}

func (o *CatalogOperation) GetAnnotations() *OperationAnnotations {
	if o == nil {
		return nil
	}
	return o.Annotations
}

func (o *CatalogOperation) GetParameters() []*CatalogParameter {
	if o == nil {
		return nil
	}
	return o.Parameters
}

func (o *CatalogOperation) GetRequiredScopes() []string {
	if o == nil {
		return nil
	}
	return o.RequiredScopes
}

func (o *CatalogOperation) GetTags() []string {
	if o == nil {
		return nil
	}
	return o.Tags
}

func (o *CatalogOperation) GetReadOnly() bool {
	if o == nil {
		return false
	}
	return o.ReadOnly
}

func (o *CatalogOperation) GetVisible() bool {
	if o == nil || o.Visible == nil {
		return false
	}
	return *o.Visible
}

func (o *CatalogOperation) GetTransport() string {
	if o == nil {
		return ""
	}
	return o.Transport
}

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

func (p *CatalogParameter) GetName() string {
	if p == nil {
		return ""
	}
	return p.Name
}

func (p *CatalogParameter) GetType() string {
	if p == nil {
		return ""
	}
	return p.Type
}

func (p *CatalogParameter) GetDescription() string {
	if p == nil {
		return ""
	}
	return p.Description
}

func (p *CatalogParameter) GetRequired() bool {
	if p == nil {
		return false
	}
	return p.Required
}

func (p *CatalogParameter) GetDefault() any {
	if p == nil {
		return nil
	}
	return p.Default
}

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

func (a *OperationAnnotations) GetReadOnlyHint() bool {
	if a == nil || a.ReadOnlyHint == nil {
		return false
	}
	return *a.ReadOnlyHint
}

func (a *OperationAnnotations) GetIdempotentHint() bool {
	if a == nil || a.IdempotentHint == nil {
		return false
	}
	return *a.IdempotentHint
}

func (a *OperationAnnotations) GetDestructiveHint() bool {
	if a == nil || a.DestructiveHint == nil {
		return false
	}
	return *a.DestructiveHint
}

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
	if pb, ok := value.(*structpb.Value); ok {
		return cloneProtoValue(pb)
	}
	if pb, ok := value.(structpb.Value); ok {
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
		OutputSchema:   op.OutputSchema,
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
	if pb, ok := value.(*structpb.Value); ok {
		return cloneProtoValue(pb), nil
	}
	if pb, ok := value.(structpb.Value); ok {
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
