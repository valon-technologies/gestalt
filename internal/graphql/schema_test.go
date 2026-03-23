package graphql

import (
	"encoding/json"
	"testing"
)

func strPtr(s string) *string { return &s }

func TestInputTypeToJSONSchemaScalars(t *testing.T) {
	t.Parallel()

	schema := &Schema{}
	schema.buildIndex()

	cases := []struct {
		name     string
		ref      TypeRef
		wantType string
	}{
		{"String", TypeRef{Kind: "SCALAR", Name: strPtr("String")}, "string"},
		{"Int", TypeRef{Kind: "SCALAR", Name: strPtr("Int")}, "integer"},
		{"Float", TypeRef{Kind: "SCALAR", Name: strPtr("Float")}, "number"},
		{"Boolean", TypeRef{Kind: "SCALAR", Name: strPtr("Boolean")}, "boolean"},
		{"ID", TypeRef{Kind: "SCALAR", Name: strPtr("ID")}, "string"},
		{"DateTime", TypeRef{Kind: "SCALAR", Name: strPtr("DateTime")}, "string"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := inputTypeToJSONSchema(schema, tc.ref, make(map[string]bool))
			var parsed map[string]any
			if err := json.Unmarshal(result, &parsed); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if parsed["type"] != tc.wantType {
				t.Errorf("type: got %v, want %s", parsed["type"], tc.wantType)
			}
		})
	}
}

func TestInputTypeToJSONSchemaEnum(t *testing.T) {
	t.Parallel()

	schema := &Schema{
		Types: []FullType{
			{
				Kind: "ENUM",
				Name: "Priority",
				EnumValues: []EnumValue{
					{Name: "LOW"},
					{Name: "MEDIUM"},
					{Name: "HIGH"},
				},
			},
		},
	}
	schema.buildIndex()

	ref := TypeRef{Kind: "ENUM", Name: strPtr("Priority")}
	result := inputTypeToJSONSchema(schema, ref, make(map[string]bool))

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed["type"] != "string" {
		t.Errorf("type: got %v, want string", parsed["type"])
	}

	enumVals, ok := parsed["enum"].([]any)
	if !ok || len(enumVals) != 3 {
		t.Fatalf("enum: got %v, want 3 values", parsed["enum"])
	}
	if enumVals[0] != "LOW" || enumVals[1] != "MEDIUM" || enumVals[2] != "HIGH" {
		t.Errorf("enum values: got %v", enumVals)
	}
}

func TestInputTypeToJSONSchemaInputObject(t *testing.T) {
	t.Parallel()

	schema := &Schema{
		Types: []FullType{
			{
				Kind: "INPUT_OBJECT",
				Name: "CreateInput",
				InputFields: []InputValue{
					{
						Name: "title",
						Type: TypeRef{Kind: "NON_NULL", OfType: &TypeRef{Kind: "SCALAR", Name: strPtr("String")}},
					},
					{
						Name: "count",
						Type: TypeRef{Kind: "SCALAR", Name: strPtr("Int")},
					},
				},
			},
		},
	}
	schema.buildIndex()

	ref := TypeRef{Kind: "INPUT_OBJECT", Name: strPtr("CreateInput")}
	result := inputTypeToJSONSchema(schema, ref, make(map[string]bool))

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed["type"] != "object" {
		t.Errorf("type: got %v, want object", parsed["type"])
	}

	props, ok := parsed["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties: got %T, want map", parsed["properties"])
	}
	if len(props) != 2 {
		t.Errorf("properties count: got %d, want 2", len(props))
	}

	titleProp, ok := props["title"].(map[string]any)
	if !ok {
		t.Fatalf("title property: got %T", props["title"])
	}
	if titleProp["type"] != "string" {
		t.Errorf("title type: got %v, want string", titleProp["type"])
	}

	required, ok := parsed["required"].([]any)
	if !ok || len(required) != 1 {
		t.Fatalf("required: got %v, want [title]", parsed["required"])
	}
	if required[0] != "title" {
		t.Errorf("required[0]: got %v, want title", required[0])
	}
}

func TestInputTypeToJSONSchemaList(t *testing.T) {
	t.Parallel()

	schema := &Schema{}
	schema.buildIndex()

	ref := TypeRef{
		Kind:   "LIST",
		OfType: &TypeRef{Kind: "SCALAR", Name: strPtr("String")},
	}
	result := inputTypeToJSONSchema(schema, ref, make(map[string]bool))

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed["type"] != "array" {
		t.Errorf("type: got %v, want array", parsed["type"])
	}

	items, ok := parsed["items"].(map[string]any)
	if !ok {
		t.Fatalf("items: got %T, want map", parsed["items"])
	}
	if items["type"] != "string" {
		t.Errorf("items type: got %v, want string", items["type"])
	}
}

func TestInputTypeToJSONSchemaNestedList(t *testing.T) {
	t.Parallel()

	schema := &Schema{}
	schema.buildIndex()

	ref := TypeRef{
		Kind: "LIST",
		OfType: &TypeRef{
			Kind:   "LIST",
			OfType: &TypeRef{Kind: "SCALAR", Name: strPtr("String")},
		},
	}
	result := inputTypeToJSONSchema(schema, ref, make(map[string]bool))

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed["type"] != "array" {
		t.Fatalf("outer type: got %v, want array", parsed["type"])
	}

	items, ok := parsed["items"].(map[string]any)
	if !ok {
		t.Fatalf("items: got %T, want map", parsed["items"])
	}
	if items["type"] != "array" {
		t.Errorf("inner type: got %v, want array (nested list)", items["type"])
	}

	innerItems, ok := items["items"].(map[string]any)
	if !ok {
		t.Fatalf("inner items: got %T, want map", items["items"])
	}
	if innerItems["type"] != "string" {
		t.Errorf("innermost type: got %v, want string", innerItems["type"])
	}
}

func TestInputTypeToJSONSchemaCycleDetection(t *testing.T) {
	t.Parallel()

	schema := &Schema{
		Types: []FullType{
			{
				Kind: "INPUT_OBJECT",
				Name: "Filter",
				InputFields: []InputValue{
					{
						Name: "name",
						Type: TypeRef{Kind: "SCALAR", Name: strPtr("String")},
					},
					{
						Name: "and",
						Type: TypeRef{Kind: "LIST", OfType: &TypeRef{Kind: "INPUT_OBJECT", Name: strPtr("Filter")}},
					},
				},
			},
		},
	}
	schema.buildIndex()

	ref := TypeRef{Kind: "INPUT_OBJECT", Name: strPtr("Filter")}
	result := inputTypeToJSONSchema(schema, ref, make(map[string]bool))

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("unmarshal: %v (raw: %s)", err, string(result))
	}

	if parsed["type"] != "object" {
		t.Errorf("type: got %v, want object", parsed["type"])
	}

	props := parsed["properties"].(map[string]any)
	andProp := props["and"].(map[string]any)
	if andProp["type"] != "array" {
		t.Errorf("and type: got %v, want array", andProp["type"])
	}
	items := andProp["items"].(map[string]any)
	if items["type"] != "object" {
		t.Errorf("and items type: got %v, want object (cycle truncated)", items["type"])
	}
	if _, hasProps := items["properties"]; hasProps {
		t.Error("cycle should be truncated: and.items should not have properties")
	}
}

func TestArgsToJSONSchema(t *testing.T) {
	t.Parallel()

	schema := &Schema{}
	schema.buildIndex()

	args := []InputValue{
		{
			Name:        "id",
			Description: "The item ID",
			Type:        TypeRef{Kind: "NON_NULL", OfType: &TypeRef{Kind: "SCALAR", Name: strPtr("ID")}},
		},
		{
			Name: "limit",
			Type: TypeRef{Kind: "SCALAR", Name: strPtr("Int")},
		},
	}

	result := argsToJSONSchema(schema, args)

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed["type"] != "object" {
		t.Errorf("type: got %v, want object", parsed["type"])
	}

	props := parsed["properties"].(map[string]any)
	idProp := props["id"].(map[string]any)
	if idProp["type"] != "string" {
		t.Errorf("id type: got %v, want string", idProp["type"])
	}
	if idProp["description"] != "The item ID" {
		t.Errorf("id description: got %v", idProp["description"])
	}

	required := parsed["required"].([]any)
	if len(required) != 1 || required[0] != "id" {
		t.Errorf("required: got %v, want [id]", required)
	}
}

func TestNonNullUnwrapping(t *testing.T) {
	t.Parallel()

	schema := &Schema{}
	schema.buildIndex()

	ref := TypeRef{
		Kind:   "NON_NULL",
		OfType: &TypeRef{Kind: "SCALAR", Name: strPtr("String")},
	}
	result := inputTypeToJSONSchema(schema, ref, make(map[string]bool))

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed["type"] != "string" {
		t.Errorf("type: got %v, want string (NON_NULL unwrapped)", parsed["type"])
	}
}
