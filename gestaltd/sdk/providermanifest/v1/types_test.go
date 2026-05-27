package providermanifestv1

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSpecRejectsLegacyGraphQLFieldsYAML(t *testing.T) {
	t.Parallel()

	var spec Spec
	err := yaml.Unmarshal([]byte(`
surfaces:
  graphql:
    url: https://example.com/graphql
allowedOperations:
  teams:
    graphql:
      operationType: query
      arguments: []
      selectionSet: id
`), &spec)
	if err == nil || !strings.Contains(err.Error(), "operationType") {
		t.Fatalf("yaml.Unmarshal error = %v, want legacy graphql field rejection", err)
	}
}

func TestSpecRejectsLegacyGraphQLFieldsJSON(t *testing.T) {
	t.Parallel()

	var spec Spec
	err := json.Unmarshal([]byte(`{
  "surfaces": {
    "graphql": {
      "url": "https://example.com/graphql"
    }
  },
  "allowedOperations": {
    "teams": {
      "graphql": {
        "operationType": "query",
        "arguments": [],
        "selectionSet": "id"
      }
    }
  }
}`), &spec)
	if err == nil || !strings.Contains(err.Error(), "operationType") {
		t.Fatalf("json.Unmarshal error = %v, want legacy graphql field rejection", err)
	}
}
