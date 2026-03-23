package tools

import (
	"testing"

	"github.com/valon-technologies/gestalt/core"
)

func TestParameterSchema_Empty(t *testing.T) {
	t.Parallel()
	schema := ParameterSchema(nil)

	if schema["type"] != "object" {
		t.Fatalf("expected type=object, got %v", schema["type"])
	}
	props := schema["properties"].(map[string]any)
	if len(props) != 0 {
		t.Fatalf("expected empty properties, got %v", props)
	}
	if _, ok := schema["required"]; ok {
		t.Fatal("expected no required key for empty params")
	}
}

func TestParameterSchema_MixedRequiredOptional(t *testing.T) {
	t.Parallel()
	params := []core.Parameter{
		{Name: "channel", Type: "string", Description: "target channel", Required: true},
		{Name: "limit", Type: "integer", Required: false},
		{Name: "verbose", Type: "bool", Description: "enable verbose output", Required: true},
	}

	schema := ParameterSchema(params)

	props := schema["properties"].(map[string]any)
	if len(props) != 3 {
		t.Fatalf("expected 3 properties, got %d", len(props))
	}

	channelProp := props["channel"].(map[string]any)
	if channelProp["type"] != "string" {
		t.Errorf("channel type: got %v, want string", channelProp["type"])
	}
	if channelProp["description"] != "target channel" {
		t.Errorf("channel description: got %v", channelProp["description"])
	}

	limitProp := props["limit"].(map[string]any)
	if limitProp["type"] != "integer" {
		t.Errorf("limit type: got %v, want integer", limitProp["type"])
	}
	if _, ok := limitProp["description"]; ok {
		t.Error("limit should have no description")
	}

	required := schema["required"].([]string)
	if len(required) != 2 || required[0] != "channel" || required[1] != "verbose" {
		t.Errorf("required: got %v, want [channel verbose]", required)
	}
}

func TestSchemaType_Mappings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"string", "string"},
		{"integer", "integer"},
		{"int", "integer"},
		{"number", "number"},
		{"float", "number"},
		{"double", "number"},
		{"boolean", "boolean"},
		{"bool", "boolean"},
		{"array", "array"},
		{"object", "object"},
		{"", "string"},
		{"unknown", "string"},
	}
	for _, tt := range tests {
		got := schemaType(tt.input)
		if got != tt.want {
			t.Errorf("schemaType(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCapabilitiesToTools(t *testing.T) {
	t.Parallel()
	caps := []core.Capability{
		{
			Provider:    "acme",
			Operation:   "list_widgets",
			Description: "List all widgets",
			Parameters: []core.Parameter{
				{Name: "status", Type: "string", Required: true},
			},
		},
		{
			Provider:    "acme",
			Operation:   "create_widget",
			Description: "Create a widget",
			Parameters:  nil,
		},
	}

	defs := CapabilitiesToTools(caps)
	if len(defs) != 2 {
		t.Fatalf("expected 2 tool definitions, got %d", len(defs))
	}

	if defs[0].Name != "acme_list_widgets" {
		t.Errorf("tool 0 name: got %q, want %q", defs[0].Name, "acme_list_widgets")
	}
	if defs[0].Description != "List all widgets" {
		t.Errorf("tool 0 description: got %q", defs[0].Description)
	}
	required := defs[0].InputSchema["required"].([]string)
	if len(required) != 1 || required[0] != "status" {
		t.Errorf("tool 0 required: got %v", required)
	}

	if defs[1].Name != "acme_create_widget" {
		t.Errorf("tool 1 name: got %q, want %q", defs[1].Name, "acme_create_widget")
	}
	if _, ok := defs[1].InputSchema["required"]; ok {
		t.Error("tool 1 should have no required key")
	}
}
