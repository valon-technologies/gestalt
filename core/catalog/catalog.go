package catalog

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Catalog is the normalized on-disk representation for a provider's tool
// surface. It intentionally carries richer metadata than core.Operation so the
// rest of Gestalt can derive runtime and MCP views from a single source.
type Catalog struct {
	Name        string             `yaml:"name"                   json:"name"`
	DisplayName string             `yaml:"display_name"           json:"displayName"`
	Description string             `yaml:"description"            json:"description"`
	IconSVG     string             `yaml:"icon_svg,omitempty"     json:"iconSvg,omitempty"`
	BaseURL     string             `yaml:"base_url,omitempty"     json:"baseUrl,omitempty"`
	AuthStyle   string             `yaml:"auth_style,omitempty"   json:"authStyle,omitempty"`
	Headers     map[string]string  `yaml:"headers,omitempty"      json:"headers,omitempty"`
	Operations  []CatalogOperation `yaml:"operations"             json:"operations"`
}

type CatalogOperation struct {
	ID             string               `yaml:"id"                       json:"id"`
	ProviderID     string               `yaml:"provider_id,omitempty"    json:"providerId,omitempty"`
	Method         string               `yaml:"method"                   json:"method"`
	Path           string               `yaml:"path"                     json:"path"`
	Title          string               `yaml:"title,omitempty"          json:"title,omitempty"`
	Description    string               `yaml:"description,omitempty"    json:"description,omitempty"`
	InputSchema    json.RawMessage      `yaml:"-"                        json:"inputSchema,omitempty"`
	Annotations    OperationAnnotations `yaml:"annotations,omitempty"    json:"annotations,omitempty"`
	Parameters     []CatalogParameter   `yaml:"parameters,omitempty"     json:"parameters,omitempty"`
	RequiredScopes []string             `yaml:"required_scopes,omitempty" json:"requiredScopes,omitempty"`
	Tags           []string             `yaml:"tags,omitempty"           json:"tags,omitempty"`
	ReadOnly       bool                 `yaml:"read_only,omitempty"      json:"readOnly,omitempty"`
	Visible        *bool                `yaml:"visible,omitempty"        json:"visible,omitempty"`
	Transport      string               `yaml:"transport,omitempty"      json:"transport,omitempty"`
	Query          string               `yaml:"query,omitempty"          json:"query,omitempty"`
}

type OperationAnnotations struct {
	ReadOnlyHint    *bool `yaml:"read_only_hint,omitempty"    json:"readOnlyHint,omitempty"`
	IdempotentHint  *bool `yaml:"idempotent_hint,omitempty"   json:"idempotentHint,omitempty"`
	DestructiveHint *bool `yaml:"destructive_hint,omitempty"  json:"destructiveHint,omitempty"`
	OpenWorldHint   *bool `yaml:"open_world_hint,omitempty"   json:"openWorldHint,omitempty"`
}

type CatalogParameter struct {
	Name        string `yaml:"name"                 json:"name"`
	Type        string `yaml:"type"                 json:"type"`
	Location    string `yaml:"location,omitempty"   json:"location,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Required    bool   `yaml:"required,omitempty"   json:"required,omitempty"`
	Default     any    `yaml:"default,omitempty"    json:"default,omitempty"`
}

func LoadCatalogYAML(data []byte) (*Catalog, error) {
	var catalog Catalog
	if err := yaml.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("parsing catalog YAML: %w", err)
	}
	if err := catalog.Validate(); err != nil {
		return nil, err
	}
	return &catalog, nil
}

func MustLoadCatalogYAML(data []byte) *Catalog {
	catalog, err := LoadCatalogYAML(data)
	if err != nil {
		panic(err)
	}
	return catalog
}

var validAuthStyles = map[string]struct{}{
	"":       {},
	"bearer": {},
	"raw":    {},
	"none":   {},
}

func IsValidAuthStyle(s string) bool {
	_, ok := validAuthStyles[strings.ToLower(strings.TrimSpace(s))]
	return ok
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
	if !IsValidAuthStyle(c.AuthStyle) {
		return fmt.Errorf("catalog %q has unknown auth_style %q", c.Name, c.AuthStyle)
	}

	seen := make(map[string]struct{}, len(c.Operations))
	for i := range c.Operations {
		op := &c.Operations[i]
		if strings.TrimSpace(op.ID) == "" {
			return fmt.Errorf("catalog %q has operation with empty id", c.Name)
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
	}

	return nil
}
