package integration

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/toolshed/core"
	"gopkg.in/yaml.v3"
)

// Catalog is the normalized on-disk representation for a provider's tool surface.
// It is intentionally richer than core.Operation so Toolshed can compile AI-facing
// metadata while still exposing the current operation contract.
type Catalog struct {
	Name        string             `yaml:"name"`
	DisplayName string             `yaml:"display_name"`
	Description string             `yaml:"description"`
	BaseURL     string             `yaml:"base_url,omitempty"`
	AuthStyle   string             `yaml:"auth_style,omitempty"`
	Headers     map[string]string  `yaml:"headers,omitempty"`
	Operations  []CatalogOperation `yaml:"operations"`
}

type CatalogOperation struct {
	ID             string             `yaml:"id"`
	ProviderID     string             `yaml:"provider_id,omitempty"`
	Method         string             `yaml:"method"`
	Path           string             `yaml:"path"`
	Description    string             `yaml:"description,omitempty"`
	Parameters     []CatalogParameter `yaml:"parameters,omitempty"`
	RequiredScopes []string           `yaml:"required_scopes,omitempty"`
	Tags           []string           `yaml:"tags,omitempty"`
	ReadOnly       bool               `yaml:"read_only,omitempty"`
	Visible        *bool              `yaml:"visible,omitempty"`
}

type CatalogParameter struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Location    string `yaml:"location,omitempty"`
	Description string `yaml:"description,omitempty"`
	Required    bool   `yaml:"required,omitempty"`
	Default     any    `yaml:"default,omitempty"`
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
	if _, err := c.AuthStyleValue(); err != nil {
		return err
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

		if strings.TrimSpace(op.Method) == "" {
			return fmt.Errorf("catalog %q operation %q is missing method", c.Name, op.ID)
		}
		if strings.TrimSpace(op.Path) == "" {
			return fmt.Errorf("catalog %q operation %q is missing path", c.Name, op.ID)
		}

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

func (c *Catalog) OperationsList() []core.Operation {
	ops := make([]core.Operation, 0, len(c.Operations))
	for i := range c.Operations {
		op := &c.Operations[i]
		params := make([]core.Parameter, 0, len(op.Parameters))
		for _, param := range op.Parameters {
			params = append(params, core.Parameter{
				Name:        param.Name,
				Type:        param.Type,
				Description: param.Description,
				Required:    param.Required,
				Default:     param.Default,
			})
		}
		ops = append(ops, core.Operation{
			Name:        op.ID,
			Description: op.Description,
			Method:      strings.ToUpper(strings.TrimSpace(op.Method)),
			Parameters:  params,
		})
	}
	return ops
}

func (c *Catalog) EndpointsMap() map[string]Endpoint {
	endpoints := make(map[string]Endpoint, len(c.Operations))
	for i := range c.Operations {
		op := &c.Operations[i]
		endpoints[op.ID] = Endpoint{
			Method: strings.ToUpper(strings.TrimSpace(op.Method)),
			Path:   op.Path,
		}
	}
	return endpoints
}

func (c *Catalog) AuthStyleValue() (AuthStyle, error) {
	switch strings.ToLower(strings.TrimSpace(c.AuthStyle)) {
	case "", "bearer":
		return AuthStyleBearer, nil
	case "raw":
		return AuthStyleRaw, nil
	case "none":
		return AuthStyleNone, nil
	default:
		return AuthStyleBearer, fmt.Errorf("catalog %q has unknown auth_style %q", c.Name, c.AuthStyle)
	}
}

func BaseFromCatalog(catalog *Catalog, runtime Base) (Base, error) {
	if err := catalog.Validate(); err != nil {
		return Base{}, err
	}

	authStyle, err := catalog.AuthStyleValue()
	if err != nil {
		return Base{}, err
	}

	base := runtime
	base.IntegrationName = catalog.Name
	base.IntegrationDisplay = catalog.DisplayName
	base.IntegrationDesc = catalog.Description
	if base.BaseURL == "" {
		base.BaseURL = catalog.BaseURL
	}
	base.AuthStyle = authStyle
	base.Operations = catalog.OperationsList()
	base.Endpoints = catalog.EndpointsMap()
	base.Headers = mergeHeaders(catalog.Headers, runtime.Headers)

	return base, nil
}

func mergeHeaders(baseHeaders, overrideHeaders map[string]string) map[string]string {
	if len(baseHeaders) == 0 && len(overrideHeaders) == 0 {
		return nil
	}

	merged := copyHeaders(baseHeaders)
	if merged == nil {
		merged = make(map[string]string, len(overrideHeaders))
	}
	for key, value := range overrideHeaders {
		merged[key] = value
	}
	return merged
}
