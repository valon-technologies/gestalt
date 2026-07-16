package publicsurface_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/pipeline"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
)

type clientCase struct {
	ID            string         `json:"id"`
	PublicRequest map[string]any `json:"publicRequest"`
	WireRequest   map[string]any `json:"wireRequest"`
}

func loadClientCases(t *testing.T) []clientCase {
	t.Helper()
	root, err := pipeline.FindRepoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "sdk", "testdata", "public_conformance", "client_cases.json"))
	if err != nil {
		t.Fatalf("read client_cases.json: %v", err)
	}
	var cases []clientCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("unmarshal client_cases.json: %v", err)
	}
	return cases
}

func TestClientCasesInvokeRESTMetadata(t *testing.T) {
	t.Parallel()

	invokeInput := &model.Message{
		FullName: "gestalt.provider.v1.AppInvokeRequest",
		Name:     "AppInvokeRequest",
		Fields: []*model.Field{
			{Name: "app", JSONName: "app", Kind: model.KindScalar, Scalar: model.ScalarString},
			{Name: "operation", JSONName: "operation", Kind: model.KindScalar, Scalar: model.ScalarString},
			{Name: "params", JSONName: "params", Kind: model.KindJSONStruct},
			{Name: "context", JSONName: "context", Kind: model.KindJSONStruct},
		},
	}
	schema := &model.Schema{
		Messages: []*model.Message{invokeInput},
		Services: []*model.Service{{
			Name: "App",
			Methods: []*model.Method{{
				Name:       "Invoke",
				FullMethod: "/gestalt.provider.v1.App/Invoke",
				Public:     true,
				Stream:     model.Unary,
				Input:      invokeInput,
				HTTP:       &model.HTTPRule{Verb: "POST", Path: "/api/v2/app/{app}/operations/{operation}", Body: "*"},
				PublicPolicy: &model.PublicPolicy{
					Fill: []string{"context"},
				},
			}},
		}},
	}

	view := publicsurface.Build(schema)
	methods, err := publicsurface.ParseMethods(schema, view)
	if err != nil {
		t.Fatalf("ParseMethods: %v", err)
	}
	if len(methods) != 1 {
		t.Fatalf("methods = %d, want 1", len(methods))
	}
	pm := methods[0]
	if pm.REST == nil {
		t.Fatal("Invoke missing REST metadata")
	}

	cases := loadClientCases(t)
	var invokeCase *clientCase
	for i := range cases {
		if cases[i].ID == "invoke_success" {
			invokeCase = &cases[i]
			break
		}
	}
	if invokeCase == nil {
		t.Fatal("missing invoke_success case")
	}

	for _, pf := range pm.REST.PathFields {
		if _, ok := invokeCase.WireRequest[pf.JSONName]; !ok {
			t.Fatalf("REST path field %q missing from wireRequest fixture", pf.JSONName)
		}
	}
	for key := range invokeCase.PublicRequest {
		if key == "context" {
			continue
		}
		found := false
		for _, f := range invokeInput.Fields {
			if f.JSONName == key || f.Name == key {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("publicRequest field %q not in input message", key)
		}
	}
}

func TestParseMethodsRejectsMalformedPathTemplate(t *testing.T) {
	t.Parallel()

	input := &model.Message{
		FullName: "gestalt.provider.v1.AppInvokeRequest",
		Name:     "AppInvokeRequest",
		Fields: []*model.Field{
			{Name: "app", JSONName: "app", Kind: model.KindScalar, Scalar: model.ScalarString},
		},
	}
	for _, path := range []string{"/api/{app", "/api/{}", "/api/{missing}"} {
		path := path
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			schema := &model.Schema{
				Messages: []*model.Message{input},
				Services: []*model.Service{{
					Name: "App",
					Methods: []*model.Method{{
						Name:       "Invoke",
						FullMethod: "/gestalt.provider.v1.App/Invoke",
						Public:     true,
						Stream:     model.Unary,
						Input:      input,
						HTTP:       &model.HTTPRule{Verb: "GET", Path: path},
					}},
				}},
			}
			if _, err := publicsurface.ParseMethods(schema, publicsurface.Build(schema)); err == nil {
				t.Fatalf("ParseMethods(%q) = nil, want error", path)
			}
		})
	}
}
