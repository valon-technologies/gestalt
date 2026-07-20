package catalog

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var validSegment = regexp.MustCompile(`^[a-zA-Z0-9_]([a-zA-Z0-9_-]*[a-zA-Z0-9_])?$`)

const (
	TransportMCPPassthrough = "mcp-passthrough"
	TransportREST           = "rest"
	TransportApp            = "app"
)

// Catalog is the normalized on-disk representation for a provider's tool
// surface. It intentionally carries richer metadata than core.Operation so the
// rest of Gestalt can derive runtime and MCP views from a single source.
type Catalog struct {
	Name        string             `yaml:"name"                   json:"name"`
	DisplayName string             `yaml:"displayName"           json:"displayName"`
	Description string             `yaml:"description"            json:"description"`
	IconSVG     string             `yaml:"iconSvg,omitempty"     json:"iconSvg,omitempty"`
	BaseURL     string             `yaml:"baseUrl,omitempty"     json:"baseUrl,omitempty"`
	AuthStyle   string             `yaml:"authStyle,omitempty"   json:"authStyle,omitempty"`
	Headers     map[string]string  `yaml:"headers,omitempty"      json:"headers,omitempty"`
	Operations  []CatalogOperation `yaml:"operations"             json:"operations"`
}

// OperationResponseSpec declares how an operation responds. Either Unary or
// Stream is set; both nil means unary with no schema.
type OperationResponseSpec struct {
	Unary  *UnaryResponseSpec  `yaml:"unary,omitempty"  json:"unary,omitempty"`
	Stream *StreamResponseSpec `yaml:"stream,omitempty" json:"stream,omitempty"`
}

// UnaryResponseSpec describes a unary operation response. Schema is a JSON
// object (JSON Schema shape).
type UnaryResponseSpec struct {
	Schema json.RawMessage `yaml:"schema,omitempty" json:"schema,omitempty"`
}

// StreamResponseSpec describes a streaming operation response.
type StreamResponseSpec struct {
	MediaType  string          `yaml:"mediaType,omitempty"  json:"mediaType,omitempty"`
	ItemSchema json.RawMessage `yaml:"itemSchema,omitempty" json:"itemSchema,omitempty"`
}

// IsStream reports whether this response spec declares a streaming response.
func (r *OperationResponseSpec) IsStream() bool {
	return r != nil && r.Stream != nil
}

type CatalogOperation struct {
	ID             string                 `yaml:"id"                       json:"id"`
	ProviderID     string                 `yaml:"providerId,omitempty"    json:"providerId,omitempty"`
	Method         string                 `yaml:"method"                   json:"method"`
	Path           string                 `yaml:"path"                     json:"path"`
	Title          string                 `yaml:"title,omitempty"          json:"title,omitempty"`
	Description    string                 `yaml:"description,omitempty"    json:"description,omitempty"`
	InputSchema    json.RawMessage        `yaml:"inputSchema,omitempty"    json:"inputSchema,omitempty"`
	OutputSchema   json.RawMessage        `yaml:"outputSchema,omitempty"   json:"outputSchema,omitempty"`
	Response       *OperationResponseSpec `yaml:"response,omitempty"       json:"response,omitempty"`
	Annotations    CapabilityAnnotations  `yaml:"annotations,omitempty"    json:"annotations,omitempty"`
	AllowedRoles   []string               `yaml:"allowedRoles,omitempty"   json:"allowedRoles,omitempty"`
	Parameters     []CatalogParameter     `yaml:"parameters,omitempty"     json:"parameters,omitempty"`
	RequiredScopes []string               `yaml:"requiredScopes,omitempty" json:"requiredScopes,omitempty"`
	Tags           []string               `yaml:"tags,omitempty"           json:"tags,omitempty"`
	ReadOnly       bool                   `yaml:"readOnly,omitempty"      json:"readOnly,omitempty"`
	Visible        *bool                  `yaml:"visible,omitempty"        json:"visible,omitempty"`
	Transport      string                 `yaml:"transport,omitempty"      json:"transport,omitempty"`
	Query          string                 `yaml:"query,omitempty"          json:"query,omitempty"`
	OperationName  string                 `yaml:"operationName,omitempty"  json:"operationName,omitempty"`
}

func OperationVisibleByDefault(op CatalogOperation) bool {
	return op.Visible == nil || *op.Visible
}

func OperationByID(cat *Catalog, id string) (CatalogOperation, bool) {
	if cat == nil || strings.TrimSpace(id) == "" {
		return CatalogOperation{}, false
	}
	for i := range cat.Operations {
		if cat.Operations[i].ID == id {
			return cat.Operations[i], true
		}
	}
	return CatalogOperation{}, false
}

// MergeTags returns provider-owned search tags with empty values removed and
// case-insensitive duplicates collapsed while preserving the first spelling.
func MergeTags(groups ...[]string) []string {
	var out []string
	seen := make(map[string]struct{})
	for _, group := range groups {
		for _, tag := range group {
			tag = strings.TrimSpace(tag)
			if tag == "" {
				continue
			}
			key := strings.ToLower(tag)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, tag)
		}
	}
	return out
}

func (o *CatalogOperation) UnmarshalYAML(value *yaml.Node) error {
	// responseYAML is an intermediate type that accepts YAML maps for schema
	// fields (json.RawMessage cannot unmarshal from YAML maps directly).
	type unaryResponseYAML struct {
		Schema any `yaml:"schema,omitempty"`
	}
	type streamResponseYAML struct {
		MediaType  string `yaml:"mediaType,omitempty"`
		ItemSchema any    `yaml:"itemSchema,omitempty"`
	}
	type responseYAML struct {
		Unary  *unaryResponseYAML  `yaml:"unary,omitempty"`
		Stream *streamResponseYAML `yaml:"stream,omitempty"`
	}
	type catalogOperationYAML struct {
		ID             string                `yaml:"id"`
		ProviderID     string                `yaml:"providerId,omitempty"`
		Method         string                `yaml:"method"`
		Path           string                `yaml:"path"`
		Title          string                `yaml:"title,omitempty"`
		Description    string                `yaml:"description,omitempty"`
		InputSchema    any                   `yaml:"inputSchema,omitempty"`
		OutputSchema   any                   `yaml:"outputSchema,omitempty"`
		Response       *responseYAML         `yaml:"response,omitempty"`
		Annotations    CapabilityAnnotations `yaml:"annotations,omitempty"`
		AllowedRoles   []string              `yaml:"allowedRoles,omitempty"`
		Parameters     []CatalogParameter    `yaml:"parameters,omitempty"`
		RequiredScopes []string              `yaml:"requiredScopes,omitempty"`
		Tags           []string              `yaml:"tags,omitempty"`
		ReadOnly       bool                  `yaml:"readOnly,omitempty"`
		Visible        *bool                 `yaml:"visible,omitempty"`
		Transport      string                `yaml:"transport,omitempty"`
		Query          string                `yaml:"query,omitempty"`
		OperationName  string                `yaml:"operationName,omitempty"`
	}

	var aux catalogOperationYAML
	if err := value.Decode(&aux); err != nil {
		return err
	}

	inputSchema, err := rawJSONFromValue(aux.InputSchema)
	if err != nil {
		return fmt.Errorf("marshal inputSchema: %w", err)
	}
	outputSchema, err := rawJSONFromValue(aux.OutputSchema)
	if err != nil {
		return fmt.Errorf("marshal outputSchema: %w", err)
	}

	// Convert the intermediate response YAML into OperationResponseSpec.
	var response *OperationResponseSpec
	if aux.Response != nil {
		response = &OperationResponseSpec{}
		if aux.Response.Unary != nil {
			schema, err := rawJSONFromValue(aux.Response.Unary.Schema)
			if err != nil {
				return fmt.Errorf("marshal response.unary.schema: %w", err)
			}
			response.Unary = &UnaryResponseSpec{Schema: schema}
		}
		if aux.Response.Stream != nil {
			itemSchema, err := rawJSONFromValue(aux.Response.Stream.ItemSchema)
			if err != nil {
				return fmt.Errorf("marshal response.stream.itemSchema: %w", err)
			}
			response.Stream = &StreamResponseSpec{
				MediaType:  aux.Response.Stream.MediaType,
				ItemSchema: itemSchema,
			}
		}
	}
	// Map legacy outputSchema to response.unary.schema when response is unset.
	if response == nil && len(outputSchema) > 0 {
		response = &OperationResponseSpec{
			Unary: &UnaryResponseSpec{Schema: outputSchema},
		}
	}

	*o = CatalogOperation{
		ID:             aux.ID,
		ProviderID:     aux.ProviderID,
		Method:         aux.Method,
		Path:           aux.Path,
		Title:          aux.Title,
		Description:    aux.Description,
		InputSchema:    inputSchema,
		OutputSchema:   outputSchema,
		Response:       response,
		Annotations:    aux.Annotations,
		AllowedRoles:   aux.AllowedRoles,
		Parameters:     aux.Parameters,
		RequiredScopes: aux.RequiredScopes,
		Tags:           aux.Tags,
		ReadOnly:       aux.ReadOnly,
		Visible:        aux.Visible,
		Transport:      aux.Transport,
		Query:          aux.Query,
		OperationName:  aux.OperationName,
	}
	return nil
}

type CapabilityAnnotations struct {
	ReadOnlyHint    *bool `yaml:"readOnlyHint,omitempty" json:"readOnlyHint,omitempty"`
	IdempotentHint  *bool `yaml:"idempotentHint,omitempty" json:"idempotentHint,omitempty"`
	DestructiveHint *bool `yaml:"destructiveHint,omitempty" json:"destructiveHint,omitempty"`
	OpenWorldHint   *bool `yaml:"openWorldHint,omitempty" json:"openWorldHint,omitempty"`
}

func CloneCapabilityAnnotations(a CapabilityAnnotations) CapabilityAnnotations {
	readOnly, idempotent, destructive, openWorld := CloneHintPointerFields(
		a.ReadOnlyHint,
		a.IdempotentHint,
		a.DestructiveHint,
		a.OpenWorldHint,
	)
	return CapabilityAnnotations{
		ReadOnlyHint:    readOnly,
		IdempotentHint:  idempotent,
		DestructiveHint: destructive,
		OpenWorldHint:   openWorld,
	}
}

// cloneOperationResponseSpec returns a deep copy of spec.
func cloneOperationResponseSpec(spec *OperationResponseSpec) *OperationResponseSpec {
	if spec == nil {
		return nil
	}
	out := &OperationResponseSpec{}
	if spec.Unary != nil {
		out.Unary = &UnaryResponseSpec{}
		if spec.Unary.Schema != nil {
			out.Unary.Schema = append(json.RawMessage(nil), spec.Unary.Schema...)
		}
	}
	if spec.Stream != nil {
		out.Stream = &StreamResponseSpec{MediaType: spec.Stream.MediaType}
		if spec.Stream.ItemSchema != nil {
			out.Stream.ItemSchema = append(json.RawMessage(nil), spec.Stream.ItemSchema...)
		}
	}
	return out
}

type CatalogParameter struct {
	Name        string `yaml:"name"                  json:"name"`
	WireName    string `yaml:"wireName,omitempty"    json:"wireName,omitempty"`
	Type        string `yaml:"type"                  json:"type"`
	Location    string `yaml:"location,omitempty"    json:"location,omitempty"`
	Description string `yaml:"description,omitempty"  json:"description,omitempty"`
	Required    bool   `yaml:"required,omitempty"    json:"required,omitempty"`
	Internal    bool   `yaml:"internal,omitempty"    json:"internal,omitempty"`
	Default     any    `yaml:"default,omitempty"     json:"default,omitempty"`
}

func (c *Catalog) Clone() *Catalog {
	if c == nil {
		return nil
	}
	out := &Catalog{
		Name:        c.Name,
		DisplayName: c.DisplayName,
		Description: c.Description,
		IconSVG:     c.IconSVG,
		BaseURL:     c.BaseURL,
		AuthStyle:   c.AuthStyle,
		Headers:     make(map[string]string, len(c.Headers)),
		Operations:  make([]CatalogOperation, len(c.Operations)),
	}
	for k, v := range c.Headers {
		out.Headers[k] = v
	}
	for i := range c.Operations {
		op := c.Operations[i]
		outOp := op
		if op.InputSchema != nil {
			outOp.InputSchema = append(json.RawMessage(nil), op.InputSchema...)
		}
		if op.OutputSchema != nil {
			outOp.OutputSchema = append(json.RawMessage(nil), op.OutputSchema...)
		}
		outOp.Response = cloneOperationResponseSpec(op.Response)
		if op.AllowedRoles != nil {
			outOp.AllowedRoles = append([]string(nil), op.AllowedRoles...)
		}
		if op.Parameters != nil {
			outOp.Parameters = append([]CatalogParameter(nil), op.Parameters...)
		}
		if op.RequiredScopes != nil {
			outOp.RequiredScopes = append([]string(nil), op.RequiredScopes...)
		}
		if op.Tags != nil {
			outOp.Tags = append([]string(nil), op.Tags...)
		}
		if op.Visible != nil {
			visible := *op.Visible
			outOp.Visible = &visible
		}
		outOp.Annotations = CloneCapabilityAnnotations(op.Annotations)
		out.Operations[i] = outOp
	}
	return out
}

var validAuthStyles = map[string]struct{}{
	"":       {},
	"bearer": {},
	"raw":    {},
	"none":   {},
	"basic":  {},
}

func isValidAuthStyle(s string) bool {
	_, ok := validAuthStyles[strings.ToLower(strings.TrimSpace(s))]
	return ok
}

func (c *Catalog) ValidateMCPCompat() error {
	for i := range c.Operations {
		op := &c.Operations[i]
		seen := make(map[string]struct{}, len(op.Parameters))
		for _, param := range op.Parameters {
			if _, dup := seen[param.Name]; dup {
				return fmt.Errorf("catalog %q operation %q has duplicate parameter name %q (likely from bracket normalization; rename one in the OpenAPI spec)", c.Name, op.ID, param.Name)
			}
			seen[param.Name] = struct{}{}
		}
	}
	return nil
}

func (c *Catalog) Validate() error {
	if c == nil {
		return fmt.Errorf("catalog is nil")
	}
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("catalog name is required")
	}
	if len(c.Operations) == 0 {
		return fmt.Errorf("catalog %q must declare at least one operation", c.Name)
	}
	if !isValidAuthStyle(c.AuthStyle) {
		return fmt.Errorf("catalog %q has unknown authStyle %q", c.Name, c.AuthStyle)
	}

	seen := make(map[string]struct{}, len(c.Operations))
	for i := range c.Operations {
		op := &c.Operations[i]
		if strings.TrimSpace(op.ID) == "" {
			return fmt.Errorf("catalog %q has operation with empty id", c.Name)
		}
		if err := validateOperationID(op.ID); err != nil {
			return fmt.Errorf("catalog %q operation %q: %w", c.Name, op.ID, err)
		}
		if _, ok := seen[op.ID]; ok {
			return fmt.Errorf("catalog %q has duplicate operation id %q", c.Name, op.ID)
		}
		seen[op.ID] = struct{}{}

		for _, param := range op.Parameters {
			if strings.TrimSpace(param.Name) == "" {
				return fmt.Errorf("catalog %q operation %q has parameter with empty name", c.Name, op.ID)
			}
			if strings.TrimSpace(param.Type) == "" {
				return fmt.Errorf("catalog %q operation %q parameter %q is missing type", c.Name, op.ID, param.Name)
			}
		}

		if len(op.AllowedRoles) > 0 {
			roles := op.AllowedRoles[:0]
			seenRoles := make(map[string]struct{}, len(op.AllowedRoles))
			for _, role := range op.AllowedRoles {
				role = strings.TrimSpace(role)
				if role == "" {
					return fmt.Errorf("catalog %q operation %q has allowedRoles entry with empty value", c.Name, op.ID)
				}
				if _, ok := seenRoles[role]; ok {
					continue
				}
				seenRoles[role] = struct{}{}
				roles = append(roles, role)
			}
			op.AllowedRoles = roles
		}
	}

	return nil
}

func validateOperationID(id string) error {
	for _, seg := range strings.Split(id, ".") {
		if !validSegment.MatchString(seg) {
			return fmt.Errorf("id contains invalid characters; each segment must be alphanumeric, underscore, or hyphen (no leading/trailing hyphen)")
		}
	}
	return nil
}

func rawJSONFromValue(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}
