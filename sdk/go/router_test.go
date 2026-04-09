package gestalt_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
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

	router := gestalt.MustRouter(
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
	index := make(map[string]*proto.CatalogParameter, len(params))
	for _, p := range params {
		index[p.GetName()] = p
	}

	checks := []struct {
		name     string
		typ      string
		required bool
		desc     string
	}{
		{"name", "string", true, "the name"},
		{"count", "integer", false, ""},
		{"score", "number", false, ""},
		{"active", "boolean", true, ""},
		{"tags", "array", false, ""},
		{"metadata", "object", false, ""},
		{"when", "string", true, ""},
		{"data", "string", false, ""},
		{"optional", "string", false, ""},
	}

	for _, c := range checks {
		p, ok := index[c.name]
		if !ok {
			t.Fatalf("parameter %q not found in catalog", c.name)
		}
		if p.GetType() != c.typ {
			t.Fatalf("parameter %q type = %q, want %q", c.name, p.GetType(), c.typ)
		}
		if p.GetRequired() != c.required {
			t.Fatalf("parameter %q required = %v, want %v", c.name, p.GetRequired(), c.required)
		}
		if c.desc != "" && p.GetDescription() != c.desc {
			t.Fatalf("parameter %q description = %q, want %q", c.name, p.GetDescription(), c.desc)
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

	router := gestalt.MustRouter(
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

	router := gestalt.MustRouter(
		gestalt.Register(
			gestalt.Operation[execInput, execOutput]{
				ID:     "echo",
				Method: http.MethodPost,
			},
			(*execProvider).echo,
		),
	).WithName("original-name")

	renamed := router.WithName("new-name")

	if renamed.Catalog().Name != "new-name" {
		t.Fatalf("renamed catalog name = %q, want %q", renamed.Catalog().GetName(), "new-name")
	}
	if router.Catalog().GetName() != "original-name" {
		t.Fatalf("original catalog name = %q, want %q", router.Catalog().GetName(), "original-name")
	}
}
