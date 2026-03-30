package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/apiexec"
)

const (
	KindScalar      = "SCALAR"
	KindObject      = "OBJECT"
	KindEnum        = "ENUM"
	KindInputObject = "INPUT_OBJECT"
	KindNonNull     = "NON_NULL"
	KindList        = "LIST"
)

type Schema struct {
	QueryType    *TypeName  `json:"queryType"`
	MutationType *TypeName  `json:"mutationType"`
	Types        []FullType `json:"types"`
	typeIndex    map[string]*FullType
}

type TypeName struct {
	Name string `json:"name"`
}

type FullType struct {
	Kind        string       `json:"kind"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Fields      []Field      `json:"fields"`
	InputFields []InputValue `json:"inputFields"`
	EnumValues  []EnumValue  `json:"enumValues"`
}

type Field struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Args        []InputValue `json:"args"`
	Type        TypeRef      `json:"type"`
}

type InputValue struct {
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	Type         TypeRef `json:"type"`
	DefaultValue *string `json:"defaultValue"`
}

type EnumValue struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type TypeRef struct {
	Kind   string   `json:"kind"`
	Name   *string  `json:"name"`
	OfType *TypeRef `json:"ofType"`
}

func (s *Schema) buildIndex() {
	s.typeIndex = make(map[string]*FullType, len(s.Types))
	for i := range s.Types {
		s.typeIndex[s.Types[i].Name] = &s.Types[i]
	}
}

func (s *Schema) lookupType(name string) *FullType {
	if s.typeIndex == nil {
		s.buildIndex()
	}
	return s.typeIndex[name]
}

const introspectionQuery = `query IntrospectionQuery {
  __schema {
    queryType { name }
    mutationType { name }
    types {
      kind
      name
      description
      fields(includeDeprecated: false) {
        name
        description
        args {
          name
          description
          type { ...TypeRef }
          defaultValue
        }
        type { ...TypeRef }
      }
      inputFields {
        name
        description
        type { ...TypeRef }
        defaultValue
      }
      enumValues(includeDeprecated: false) {
        name
        description
      }
    }
  }
}

fragment TypeRef on __Type {
  kind
  name
  ofType {
    kind
    name
    ofType {
      kind
      name
      ofType {
        kind
        name
        ofType {
          kind
          name
          ofType {
            kind
            name
            ofType {
              kind
              name
            }
          }
        }
      }
    }
  }
}`

var defaultClient = &http.Client{Timeout: 30 * time.Second}

func introspect(ctx context.Context, endpoint string) (*Schema, error) {
	result, err := apiexec.DoGraphQL(ctx, defaultClient, apiexec.GraphQLRequest{
		URL:   endpoint,
		Query: introspectionQuery,
	})
	if err != nil {
		return nil, fmt.Errorf("introspection query: %w", err)
	}

	var resp struct {
		Schema Schema `json:"__schema"`
	}
	if err := json.Unmarshal([]byte(result.Body), &resp); err != nil {
		return nil, fmt.Errorf("parsing introspection response: %w", err)
	}

	resp.Schema.buildIndex()
	return &resp.Schema, nil
}

func (ref TypeRef) namedType() string {
	if ref.Name != nil {
		return *ref.Name
	}
	if ref.OfType != nil {
		return ref.OfType.namedType()
	}
	return ""
}

func (ref TypeRef) isNonNull() bool {
	return ref.Kind == KindNonNull
}

func (ref TypeRef) isList() bool {
	if ref.Kind == KindList {
		return true
	}
	if ref.Kind == KindNonNull && ref.OfType != nil {
		return ref.OfType.isList()
	}
	return false
}

func (ref TypeRef) innerType() TypeRef {
	r := ref
	for r.Kind == KindNonNull || r.Kind == KindList {
		if r.OfType == nil {
			break
		}
		r = *r.OfType
	}
	return r
}
