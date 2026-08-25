package providermanifestv1_test

import (
	"encoding/json"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

func TestSpecAccessRoundTripsThroughJSONAndYAML(t *testing.T) {
	t.Parallel()

	original := providermanifestv1.Spec{
		Access: &providermanifestv1.ProviderAccess{
			DefaultOperations: []string{"conversations.list", "chat.postMessage"},
		},
	}
	for _, tc := range []struct {
		name      string
		marshal   func(any) ([]byte, error)
		unmarshal func([]byte, any) error
	}{
		{name: "json", marshal: json.Marshal, unmarshal: json.Unmarshal},
		{name: "yaml", marshal: yaml.Marshal, unmarshal: yaml.Unmarshal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			encoded, err := tc.marshal(original)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var got providermanifestv1.Spec
			if err := tc.unmarshal(encoded, &got); err != nil {
				t.Fatalf("Unmarshal: %v\n%s", err, encoded)
			}
			if got.Access == nil || len(got.Access.DefaultOperations) != 2 ||
				got.Access.DefaultOperations[0] != "conversations.list" ||
				got.Access.DefaultOperations[1] != "chat.postMessage" {
				t.Fatalf("access = %#v, want default operations round trip", got.Access)
			}
		})
	}
}

func TestManifestJSONSchemaAcceptsSpecAccess(t *testing.T) {
	t.Parallel()

	var schemaDocument any
	if err := json.Unmarshal(providermanifestv1.ManifestJSONSchema, &schemaDocument); err != nil {
		t.Fatalf("decode embedded schema: %v", err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("manifest.schema.json", schemaDocument); err != nil {
		t.Fatalf("add schema resource: %v", err)
	}
	schema, err := compiler.Compile("manifest.schema.json")
	if err != nil {
		t.Fatalf("compile embedded schema: %v", err)
	}
	manifest := map[string]any{
		"kind":    "app",
		"source":  "github.com/acme/apps/slack",
		"version": "1.0.0",
		"spec": map[string]any{
			"access": map[string]any{
				"defaultOperations": []any{"conversations.list"},
			},
		},
	}
	if err := schema.Validate(manifest); err != nil {
		t.Fatalf("manifest with spec.access failed schema validation: %v", err)
	}
}
