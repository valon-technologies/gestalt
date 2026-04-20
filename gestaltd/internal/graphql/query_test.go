package graphql

import (
	"strings"
	"testing"
)

func TestGenerateQuerySimple(t *testing.T) {
	t.Parallel()

	schema := &Schema{
		Types: []FullType{
			{Kind: "OBJECT", Name: "Team", Fields: []Field{
				{Name: "id", Type: TypeRef{Kind: "SCALAR", Name: strPtr("ID")}},
				{Name: "name", Type: TypeRef{Kind: "SCALAR", Name: strPtr("String")}},
			}},
		},
	}
	schema.buildIndex()

	field := Field{
		Name: "team",
		Args: []InputValue{
			{Name: "id", Type: TypeRef{Kind: "NON_NULL", OfType: &TypeRef{Kind: "SCALAR", Name: strPtr("String")}}},
		},
		Type: TypeRef{Kind: "OBJECT", Name: strPtr("Team")},
	}

	query := generateQuery(schema, field, false, "")

	if !strings.HasPrefix(query, "query(") {
		t.Errorf("should start with query(: %s", query)
	}
	if !strings.Contains(query, "$id: String!") {
		t.Errorf("should declare $id variable: %s", query)
	}
	if !strings.Contains(query, "team(id: $id)") {
		t.Errorf("should pass id argument: %s", query)
	}
	if !strings.Contains(query, "{ id name }") {
		t.Errorf("should select scalar fields: %s", query)
	}
}

func TestGenerateQueryMutation(t *testing.T) {
	t.Parallel()

	schema := &Schema{
		Types: []FullType{
			{Kind: "OBJECT", Name: "Result", Fields: []Field{
				{Name: "success", Type: TypeRef{Kind: "SCALAR", Name: strPtr("Boolean")}},
			}},
		},
	}
	schema.buildIndex()

	field := Field{
		Name: "deleteItem",
		Args: []InputValue{
			{Name: "id", Type: TypeRef{Kind: "NON_NULL", OfType: &TypeRef{Kind: "SCALAR", Name: strPtr("ID")}}},
		},
		Type: TypeRef{Kind: "OBJECT", Name: strPtr("Result")},
	}

	query := generateQuery(schema, field, true, "")

	if !strings.HasPrefix(query, "mutation(") {
		t.Errorf("should start with mutation(: %s", query)
	}
}

func TestGenerateQueryNoArgs(t *testing.T) {
	t.Parallel()

	schema := &Schema{
		Types: []FullType{
			{Kind: "OBJECT", Name: "User", Fields: []Field{
				{Name: "id", Type: TypeRef{Kind: "SCALAR", Name: strPtr("ID")}},
				{Name: "email", Type: TypeRef{Kind: "SCALAR", Name: strPtr("String")}},
			}},
		},
	}
	schema.buildIndex()

	field := Field{
		Name: "viewer",
		Type: TypeRef{Kind: "OBJECT", Name: strPtr("User")},
	}

	query := generateQuery(schema, field, false, "")

	if !strings.HasPrefix(query, "query { viewer") {
		t.Errorf("should be simple query: %s", query)
	}
	if strings.Contains(query, "(") && strings.Contains(query, "$") {
		t.Errorf("should not have variable declaration: %s", query)
	}
}

func TestConnectionTypeDetection(t *testing.T) {
	t.Parallel()

	schema := &Schema{
		Types: []FullType{
			{Kind: "OBJECT", Name: "IssueConnection", Fields: []Field{
				{Name: "nodes", Type: TypeRef{Kind: "LIST", OfType: &TypeRef{Kind: "OBJECT", Name: strPtr("Issue")}}},
				{Name: "pageInfo", Type: TypeRef{Kind: "OBJECT", Name: strPtr("PageInfo")}},
			}},
			{Kind: "OBJECT", Name: "Issue", Fields: []Field{
				{Name: "id", Type: TypeRef{Kind: "SCALAR", Name: strPtr("ID")}},
				{Name: "title", Type: TypeRef{Kind: "SCALAR", Name: strPtr("String")}},
			}},
			{Kind: "OBJECT", Name: "PageInfo", Fields: []Field{
				{Name: "hasNextPage", Type: TypeRef{Kind: "SCALAR", Name: strPtr("Boolean")}},
				{Name: "endCursor", Type: TypeRef{Kind: "SCALAR", Name: strPtr("String")}},
			}},
		},
	}
	schema.buildIndex()

	field := Field{
		Name: "issues",
		Type: TypeRef{Kind: "OBJECT", Name: strPtr("IssueConnection")},
	}

	query := generateQuery(schema, field, false, "")

	if !strings.Contains(query, "nodes { id title }") {
		t.Errorf("should unwrap connection nodes: %s", query)
	}
	if !strings.Contains(query, "pageInfo { hasNextPage endCursor }") {
		t.Errorf("should include pageInfo: %s", query)
	}
}

func TestSelectionSetDepthLimit(t *testing.T) {
	t.Parallel()

	schema := &Schema{
		Types: []FullType{
			{Kind: "OBJECT", Name: "A", Fields: []Field{
				{Name: "id", Type: TypeRef{Kind: "SCALAR", Name: strPtr("ID")}},
				{Name: "b", Type: TypeRef{Kind: "OBJECT", Name: strPtr("B")}},
			}},
			{Kind: "OBJECT", Name: "B", Fields: []Field{
				{Name: "name", Type: TypeRef{Kind: "SCALAR", Name: strPtr("String")}},
				{Name: "c", Type: TypeRef{Kind: "OBJECT", Name: strPtr("C")}},
			}},
			{Kind: "OBJECT", Name: "C", Fields: []Field{
				{Name: "value", Type: TypeRef{Kind: "SCALAR", Name: strPtr("String")}},
				{Name: "d", Type: TypeRef{Kind: "OBJECT", Name: strPtr("D")}},
			}},
			{Kind: "OBJECT", Name: "D", Fields: []Field{
				{Name: "deep", Type: TypeRef{Kind: "SCALAR", Name: strPtr("String")}},
			}},
		},
	}
	schema.buildIndex()

	field := Field{
		Name: "getA",
		Type: TypeRef{Kind: "OBJECT", Name: strPtr("A")},
	}

	query := generateQuery(schema, field, false, "")

	if !strings.Contains(query, "value") {
		t.Errorf("depth 2 should include C.value: %s", query)
	}
	if strings.Contains(query, "deep") {
		t.Errorf("depth 2 should NOT include D.deep: %s", query)
	}
}

func TestSelectionSetCyclePrevention(t *testing.T) {
	t.Parallel()

	schema := &Schema{
		Types: []FullType{
			{Kind: "OBJECT", Name: "Node", Fields: []Field{
				{Name: "id", Type: TypeRef{Kind: "SCALAR", Name: strPtr("ID")}},
				{Name: "parent", Type: TypeRef{Kind: "OBJECT", Name: strPtr("Node")}},
			}},
		},
	}
	schema.buildIndex()

	field := Field{
		Name: "getNode",
		Type: TypeRef{Kind: "OBJECT", Name: strPtr("Node")},
	}

	query := generateQuery(schema, field, false, "")

	if strings.Count(query, "parent") > 1 {
		t.Errorf("should not recurse into self-referential type: %s", query)
	}
}

func TestFormatTypeRef(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		ref  TypeRef
		want string
	}{
		{
			"simple",
			TypeRef{Kind: "SCALAR", Name: strPtr("String")},
			"String",
		},
		{
			"non-null",
			TypeRef{Kind: "NON_NULL", OfType: &TypeRef{Kind: "SCALAR", Name: strPtr("Int")}},
			"Int!",
		},
		{
			"list",
			TypeRef{Kind: "LIST", OfType: &TypeRef{Kind: "SCALAR", Name: strPtr("String")}},
			"[String]",
		},
		{
			"non-null list of non-null",
			TypeRef{Kind: "NON_NULL", OfType: &TypeRef{
				Kind: "LIST", OfType: &TypeRef{
					Kind: "NON_NULL", OfType: &TypeRef{Kind: "SCALAR", Name: strPtr("ID")},
				},
			}},
			"[ID!]!",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := formatTypeRef(tc.ref)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
