package gestalt_test

import (
	"context"
	"errors"
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
	Echo                string `json:"echo"`
	Region              string `json:"region"`
	RegionPresent       bool   `json:"region_present"`
	SubjectID           string `json:"subject_id"`
	SubjectKind         string `json:"subject_kind"`
	CredentialMode      string `json:"credential_mode"`
	CredentialSubjectID string `json:"credential_subject_id"`
}

type execProvider struct{}

func (p *execProvider) Configure(context.Context, string, map[string]any) error { return nil }

func (p *execProvider) echo(_ context.Context, in execInput, req gestalt.Request) (gestalt.Response[execOutput], error) {
	region, ok := req.ConnectionParam("region")
	return gestalt.OK(execOutput{
		Echo:                in.Value,
		Region:              region,
		RegionPresent:       ok,
		SubjectID:           req.Subject.ID,
		SubjectKind:         req.Subject.Kind,
		CredentialMode:      req.Credential.Mode,
		CredentialSubjectID: req.Credential.SubjectID,
	}), nil
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
		gestalt.Register(
			gestalt.Operation[struct{}, struct{}]{
				ID:     "bad_request",
				Method: http.MethodPost,
			},
			func(*execProvider, context.Context, struct{}, gestalt.Request) (gestalt.Response[struct{}], error) {
				return gestalt.Response[struct{}]{}, gestalt.Error(http.StatusBadRequest, "invalid input")
			},
		),
		gestalt.Register(
			gestalt.Operation[struct{}, struct{}]{
				ID:     "plain_error",
				Method: http.MethodPost,
			},
			func(*execProvider, context.Context, struct{}, gestalt.Request) (gestalt.Response[struct{}], error) {
				return gestalt.Response[struct{}]{}, errors.New("boom")
			},
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
	if result.Body != `{"echo":"hello","region":"","region_present":false,"subject_id":"","subject_kind":"","credential_mode":"","credential_subject_id":""}` {
		t.Fatalf("body = %q, want %q", result.Body, `{"echo":"hello","region":"","region_present":false,"subject_id":"","subject_kind":"","credential_mode":"","credential_subject_id":""}`)
	}

	ctxWithRequest := gestalt.WithConnectionParams(ctx, map[string]string{"region": "iad"})
	ctxWithRequest = gestalt.WithSubject(ctxWithRequest, gestalt.Subject{
		ID:         "user:user-123",
		Kind:       "user",
		AuthSource: "api_token",
	})
	ctxWithRequest = gestalt.WithCredential(ctxWithRequest, gestalt.Credential{
		Mode:      "identity",
		SubjectID: "identity:__identity__",
	})
	result, err = router.Execute(ctxWithRequest, provider, "echo", map[string]any{"value": "hello"}, "tok")
	if err != nil {
		t.Fatalf("Execute(with params): %v", err)
	}
	if result.Body != `{"echo":"hello","region":"iad","region_present":true,"subject_id":"user:user-123","subject_kind":"user","credential_mode":"identity","credential_subject_id":"identity:__identity__"}` {
		t.Fatalf("body with params = %q, want %q", result.Body, `{"echo":"hello","region":"iad","region_present":true,"subject_id":"user:user-123","subject_kind":"user","credential_mode":"identity","credential_subject_id":"identity:__identity__"}`)
	}

	result, err = router.Execute(ctx, provider, "nonexistent", nil, "tok")
	if err != nil {
		t.Fatalf("Execute(nonexistent): %v", err)
	}
	if result.Status != http.StatusNotFound {
		t.Fatalf("nonexistent status = %d, want %d", result.Status, http.StatusNotFound)
	}

	result, err = router.Execute(ctx, provider, "bad_request", nil, "tok")
	if err != nil {
		t.Fatalf("Execute(bad_request): %v", err)
	}
	if result.Status != http.StatusBadRequest {
		t.Fatalf("bad_request status = %d, want %d", result.Status, http.StatusBadRequest)
	}
	if result.Body != `{"error":"invalid input"}` {
		t.Fatalf("bad_request body = %q, want %q", result.Body, `{"error":"invalid input"}`)
	}

	result, err = router.Execute(ctx, provider, "plain_error", nil, "tok")
	if err != nil {
		t.Fatalf("Execute(plain_error): %v", err)
	}
	if result.Status != http.StatusInternalServerError {
		t.Fatalf("plain_error status = %d, want %d", result.Status, http.StatusInternalServerError)
	}
	if result.Body != `{"error":"internal error"}` {
		t.Fatalf("plain_error body = %q, want %q", result.Body, `{"error":"internal error"}`)
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

func TestRouterManifestMetadata(t *testing.T) {
	t.Parallel()

	router := gestalt.MustRouter(
		gestalt.Register(
			gestalt.Operation[execInput, execOutput]{
				ID:     "echo",
				Method: http.MethodPost,
			},
			(*execProvider).echo,
		),
	).WithManifestMetadata(gestalt.ManifestMetadata{
		SecuritySchemes: map[string]gestalt.HTTPSecurityScheme{
			"slack": {
				Type: gestalt.HTTPSecuritySchemeTypeSlackSignature,
				Secret: &gestalt.HTTPSecretRef{
					Env: "SLACK_SIGNING_SECRET",
				},
			},
		},
		HTTP: map[string]gestalt.HTTPBinding{
			"command": {
				Path:     "/command",
				Method:   http.MethodPost,
				Security: "slack",
				Target:   "echo",
				RequestBody: &gestalt.HTTPRequestBody{
					Required: true,
					Content: map[string]gestalt.HTTPMediaType{
						"application/x-www-form-urlencoded": {},
					},
				},
				Ack: &gestalt.HTTPAck{
					Status: 200,
					Headers: map[string]string{
						"content-type": "application/json",
					},
					Body: map[string]any{
						"response_type": "ephemeral",
						"attachments": []any{
							map[string]any{"text": "Working on it"},
						},
					},
				},
			},
		},
	})

	metadata := router.ManifestMetadata()
	if got := metadata.SecuritySchemes["slack"].Type; got != gestalt.HTTPSecuritySchemeTypeSlackSignature {
		t.Fatalf("security scheme type = %q, want %q", got, gestalt.HTTPSecuritySchemeTypeSlackSignature)
	}
	binding, ok := metadata.HTTP["command"]
	if !ok {
		t.Fatal("manifest metadata missing command binding")
	}
	if binding.Target != "echo" {
		t.Fatalf("binding target = %q, want %q", binding.Target, "echo")
	}
	if binding.Ack == nil {
		t.Fatal("binding ack is nil")
	}
	body, ok := binding.Ack.Body.(map[string]any)
	if !ok {
		t.Fatalf("binding ack body type = %T, want map[string]any", binding.Ack.Body)
	}
	attachments, ok := body["attachments"].([]any)
	if !ok || len(attachments) != 1 {
		t.Fatalf("binding attachments = %#v, want one attachment", body["attachments"])
	}

	metadata.SecuritySchemes["slack"] = gestalt.HTTPSecurityScheme{Type: gestalt.HTTPSecuritySchemeTypeNone}
	binding.Ack.Headers["content-type"] = "text/plain"
	body["response_type"] = "changed"
	attachments[0].(map[string]any)["text"] = "changed"

	original := router.ManifestMetadata()
	if got := original.SecuritySchemes["slack"].Type; got != gestalt.HTTPSecuritySchemeTypeSlackSignature {
		t.Fatalf("original security scheme type = %q, want %q", got, gestalt.HTTPSecuritySchemeTypeSlackSignature)
	}
	originalBinding := original.HTTP["command"]
	if got := originalBinding.Ack.Headers["content-type"]; got != "application/json" {
		t.Fatalf("original ack header = %q, want %q", got, "application/json")
	}
	originalBody, ok := originalBinding.Ack.Body.(map[string]any)
	if !ok {
		t.Fatalf("original ack body type = %T, want map[string]any", originalBinding.Ack.Body)
	}
	if got := originalBody["response_type"]; got != "ephemeral" {
		t.Fatalf("original ack response_type = %#v, want %#v", got, "ephemeral")
	}
	originalAttachments, ok := originalBody["attachments"].([]any)
	if !ok || len(originalAttachments) != 1 {
		t.Fatalf("original attachments = %#v, want one attachment", originalBody["attachments"])
	}
	if got := originalAttachments[0].(map[string]any)["text"]; got != "Working on it" {
		t.Fatalf("original attachment text = %#v, want %#v", got, "Working on it")
	}
}
