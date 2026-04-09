package gestalt_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

type allTypesInput struct {
	Name     string         `json:"name" required:"true" doc:"the name"`
	Count    int            `json:"count" required:"false" default:"5"`
	Score    float64        `json:"score,omitempty"`
	Active   bool           `json:"active"`
	Tags     []string       `json:"tags"`
	Metadata map[string]any `json:"metadata"`
	When     time.Time      `json:"when"`
	Data     []byte         `json:"data"`
	Optional *string        `json:"optional"`
}

type allTypesOutput struct {
	OK bool `json:"ok"`
}

type allTypesProvider struct{}

func (p *allTypesProvider) Configure(context.Context, string, map[string]any) error { return nil }

func (p *allTypesProvider) handleAllTypes(_ context.Context, _ allTypesInput, _ gestalt.Request) (gestalt.Response[allTypesOutput], error) {
	return gestalt.OK(allTypesOutput{OK: true}), nil
}

func TestRouterCatalogParameterTypes(t *testing.T) {
	t.Parallel()

	router := gestalt.MustNamedRouter[allTypesProvider]("param-types",
		gestalt.Register(
			gestalt.Operation[allTypesInput, allTypesOutput]{
				ID:     "all_types",
				Method: http.MethodPost,
			},
			(*allTypesProvider).handleAllTypes,
		),
	)

	catalog := router.Catalog()
	if catalog == nil {
		t.Fatal("catalog is nil")
	}
	if len(catalog.Operations) != 1 {
		t.Fatalf("operations count = %d, want 1", len(catalog.Operations))
	}

	params := catalog.Operations[0].Parameters
	index := make(map[string]gestalt.CatalogParameter, len(params))
	for _, p := range params {
		index[p.Name] = p
	}

	checks := []struct {
		name     string
		typ      string
		required bool
		defVal   any
		desc     string
	}{
		{"name", "string", true, nil, "the name"},
		{"count", "integer", false, int64(5), ""},
		{"score", "number", false, nil, ""},
		{"active", "boolean", true, nil, ""},
		{"tags", "array", false, nil, ""},
		{"metadata", "object", false, nil, ""},
		{"when", "string", true, nil, ""},
		{"data", "string", false, nil, ""},
		{"optional", "string", false, nil, ""},
	}

	for _, c := range checks {
		p, ok := index[c.name]
		if !ok {
			t.Fatalf("parameter %q not found in catalog", c.name)
		}
		if p.Type != c.typ {
			t.Fatalf("parameter %q type = %q, want %q", c.name, p.Type, c.typ)
		}
		if p.Required != c.required {
			t.Fatalf("parameter %q required = %v, want %v", c.name, p.Required, c.required)
		}
		if c.defVal != nil && p.Default != c.defVal {
			t.Fatalf("parameter %q default = %v (%T), want %v (%T)", c.name, p.Default, p.Default, c.defVal, c.defVal)
		}
		if c.desc != "" && p.Description != c.desc {
			t.Fatalf("parameter %q description = %q, want %q", c.name, p.Description, c.desc)
		}
	}
}

type execInput struct {
	Value string `json:"value"`
}

type execOutput struct {
	Echo string `json:"echo"`
}

type execProvider struct{}

func (p *execProvider) Configure(context.Context, string, map[string]any) error { return nil }

func (p *execProvider) echo(_ context.Context, in execInput, _ gestalt.Request) (gestalt.Response[execOutput], error) {
	return gestalt.OK(execOutput{Echo: in.Value}), nil
}

func TestRouterOperationExecution(t *testing.T) {
	t.Parallel()

	router := gestalt.MustNamedRouter[execProvider]("exec-test",
		gestalt.Register(
			gestalt.Operation[execInput, execOutput]{
				ID:     "echo",
				Method: http.MethodPost,
			},
			(*execProvider).echo,
		),
	)

	provider := &execProvider{}
	ctx := context.Background()

	result, err := router.Execute(ctx, provider, "echo", map[string]any{"value": "hello"}, "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusOK)
	}
	if result.Body != `{"echo":"hello"}` {
		t.Fatalf("body = %q, want %q", result.Body, `{"echo":"hello"}`)
	}

	result, err = router.Execute(ctx, provider, "nonexistent", nil, "tok")
	if err != nil {
		t.Fatalf("Execute(nonexistent): %v", err)
	}
	if result.Status != http.StatusNotFound {
		t.Fatalf("nonexistent status = %d, want %d", result.Status, http.StatusNotFound)
	}

	var nilRouter *gestalt.Router[execProvider]
	result, err = nilRouter.Execute(ctx, provider, "echo", nil, "tok")
	if err != nil {
		t.Fatalf("Execute(nil router): %v", err)
	}
	if result.Status != http.StatusInternalServerError {
		t.Fatalf("nil router status = %d, want %d", result.Status, http.StatusInternalServerError)
	}
}

func TestRouterCatalogName(t *testing.T) {
	t.Parallel()

	router := gestalt.MustNamedRouter[execProvider]("original-name",
		gestalt.Register(
			gestalt.Operation[execInput, execOutput]{
				ID:     "echo",
				Method: http.MethodPost,
			},
			(*execProvider).echo,
		),
	)

	renamed := router.WithName("new-name")

	if renamed.Catalog().Name != "new-name" {
		t.Fatalf("renamed catalog name = %q, want %q", renamed.Catalog().Name, "new-name")
	}
	if router.Catalog().Name != "original-name" {
		t.Fatalf("original catalog name = %q, want %q", router.Catalog().Name, "original-name")
	}
}
